package middleware_test

// Story 5.23 — JWT Denylist Check After Signature Verification
//
// Acceptance Tests (ATDD / Red phase):
//   1. TestJWT_InvalidTokenSkipsDBLookup     — malformed token must NOT reach denylist store
//   2. TestJWT_ValidThenDenylisted_Returns401 — verified token in denylist → 401
//   3. TestJWT_ValidNotDenylisted_Returns200  — verified token not in denylist → 200
//   4. TestJWT_DenylistOnlyOnVerified_PrometheusMetric — counters for stage="verify|denylist"

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/middleware"
	"github.com/prometheus/client_golang/prometheus"
)

// panicStore is a TokenStore whose IsInvalidated panics unconditionally.
// If the middleware calls the denylist before verifying the token this test
// will panic-fail, proving the wrong order.
type panicStore struct{}

func (panicStore) Invalidate(_ string, _ time.Time) error { return nil }
func (panicStore) IsInvalidated(_ string) bool {
	panic("IsInvalidated must never be called for an unverified token")
}

// TestJWT_InvalidTokenSkipsDBLookup verifies that a malformed / unsigned JWT
// does NOT trigger a denylist lookup.  With the old (wrong) order the panic
// store would fire; with the correct order the middleware rejects at
// verifier.Verify and never reaches the store.
func TestJWT_InvalidTokenSkipsDBLookup(t *testing.T) {
	srv, _ := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)

	handler := middleware.JWTMiddleware(
		provider, "nebu-gateway", "nebu_role", panicStore{}, nil,
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler must not be reached for invalid token")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	rr := httptest.NewRecorder()

	// If denylist is called before verify, the panicStore fires → test panic.
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("expected M_UNKNOWN_TOKEN, got %q", body["errcode"])
	}
}

// boolStore is a simple TokenStore backed by a bool.
type boolStore struct{ invalidated bool }

func (b *boolStore) Invalidate(_ string, _ time.Time) error { return nil }
func (b *boolStore) IsInvalidated(_ string) bool            { return b.invalidated }

// TestJWT_ValidThenDenylisted_Returns401 verifies that a cryptographically
// valid JWT that appears in the denylist is rejected with 401.
func TestJWT_ValidThenDenylisted_Returns401(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	rawToken := signJWT(t, srv.URL, key, time.Now().Add(time.Hour))

	store := &boolStore{invalidated: true}

	handler := middleware.JWTMiddleware(
		provider, "nebu-gateway", "nebu_role", store, nil,
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler must not be reached for denylisted token")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("expected M_UNKNOWN_TOKEN, got %q", body["errcode"])
	}
	if body["error"] != "Token has been logged out" {
		t.Errorf("expected 'Token has been logged out', got %q", body["error"])
	}
}

// TestJWT_ValidNotDenylisted_Returns200 verifies the happy path: valid JWT,
// not in denylist → middleware forwards to inner handler with 200.
func TestJWT_ValidNotDenylisted_Returns200(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	rawToken := signJWT(t, srv.URL, key, time.Now().Add(time.Hour))

	store := &boolStore{invalidated: false}

	called := false
	handler := middleware.JWTMiddleware(
		provider, "nebu-gateway", "nebu_role", store, nil,
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("inner handler was not called for a valid, non-denylisted token")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// TestJWT_DenylistOnlyOnVerified_PrometheusMetric verifies that the Prometheus
// counter nebu_jwt_validation_total is incremented with the correct stage/result
// labels.  We use prometheus.DefaultGatherer so we can read back the registered
// global counters.
//
// Scenario A: invalid token  → stage="verify",result="fail"   incremented
//                             → stage="denylist" NOT incremented
// Scenario B: valid, not-denied → stage="verify",result="pass"  incremented
//                              → stage="denylist",result="pass" incremented
func TestJWT_DenylistOnlyOnVerified_PrometheusMetric(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	store := &boolStore{invalidated: false}

	handler := middleware.JWTMiddleware(
		provider, "nebu-gateway", "nebu_role", store, nil,
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Capture baseline before making requests.
	baseline := gatherJWTCounters(t)

	// Scenario A: malformed token.
	reqInvalid := httptest.NewRequest("GET", "/", nil)
	reqInvalid.Header.Set("Authorization", "Bearer invalid.token.here")
	handler.ServeHTTP(httptest.NewRecorder(), reqInvalid)

	// Scenario B: valid token.
	rawToken := signJWT(t, srv.URL, key, time.Now().Add(time.Hour))
	reqValid := httptest.NewRequest("GET", "/", nil)
	reqValid.Header.Set("Authorization", "Bearer "+rawToken)
	handler.ServeHTTP(httptest.NewRecorder(), reqValid)

	after := gatherJWTCounters(t)

	// Scenario A incremented verify/fail by 1.
	delta := func(stage, result string) float64 {
		key := stage + "/" + result
		return after[key] - baseline[key]
	}

	if d := delta("verify", "fail"); d != 1 {
		t.Errorf("verify/fail delta: want 1, got %v", d)
	}
	// Scenario B incremented verify/pass and denylist/pass each by 1.
	if d := delta("verify", "pass"); d != 1 {
		t.Errorf("verify/pass delta: want 1, got %v", d)
	}
	if d := delta("denylist", "pass"); d != 1 {
		t.Errorf("denylist/pass delta: want 1, got %v", d)
	}
	// denylist/fail not triggered (store.invalidated=false, invalid token rejected before denylist).
	if d := delta("denylist", "fail"); d != 0 {
		t.Errorf("denylist/fail delta: want 0, got %v", d)
	}
}

// gatherJWTCounters collects all nebu_jwt_validation_total samples from the
// default Prometheus registry and returns a map of "stage/result" → value.
func gatherJWTCounters(t *testing.T) map[string]float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	out := make(map[string]float64)
	for _, mf := range mfs {
		if mf.GetName() != "nebu_jwt_validation_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			var stage, result string
			for _, lp := range m.GetLabel() {
				switch lp.GetName() {
				case "stage":
					stage = lp.GetValue()
				case "result":
					result = lp.GetValue()
				}
			}
			out[stage+"/"+result] = m.GetCounter().GetValue()
		}
	}
	return out
}
