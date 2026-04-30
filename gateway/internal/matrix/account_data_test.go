package matrix

// ─── Story 7-24: GET/PUT /_matrix/client/v3/user/{userId}/account_data/{type}
//                         /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}
//
// Tests written FIRST (ATDD gate) before implementation exists.
// ALL tests in this file are expected to FAIL until account_data.go is created.
//
// Test strategy:
//   - mockAccountDataDB implements AccountDataDB (consumer-defined interface, Go convention).
//   - buildAuthedAccountDataHandler wires JWTMiddleware → AccountDataHandler on a mux.
//   - JWT sub is always "test-sub-123" → authenticated user_id "@test-sub-123:test.local".
//   - Happy path: PUT + GET round-trip for global and per-room account data.
//   - M_NOT_FOUND: GET when no data stored → 404.
//   - M_FORBIDDEN: userId in path ≠ authenticated user → 403, no DB call.
//   - Upsert semantics: second PUT overwrites first (last write wins) → 200, no error.
//   - Bad JSON body: PUT with malformed JSON → 400 M_BAD_JSON.
//   - Unauthenticated: no Bearer → 401 from JWTMiddleware.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Mock AccountDataDB ───────────────────────────────────────────────────────

// mockAccountDataDB implements AccountDataDB (defined in account_data.go).
// Stores rows in an in-memory map keyed by "userID\nroomID\neventType".
// roomID is empty for global account data.
type mockAccountDataDB struct {
	mu    sync.Mutex
	store map[string]json.RawMessage
	// If getErr is non-nil, Get returns it regardless of key.
	getErr error
	// If putErr is non-nil, Put returns it.
	putErr error
}

func newMockAccountDataDB() *mockAccountDataDB {
	return &mockAccountDataDB{store: make(map[string]json.RawMessage)}
}

func (m *mockAccountDataDB) key(userID, roomID, eventType string) string {
	return userID + "\n" + roomID + "\n" + eventType
}

func (m *mockAccountDataDB) GetAccountData(ctx context.Context, userID, roomID, eventType string) (json.RawMessage, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.store[m.key(userID, roomID, eventType)]
	if !ok {
		return nil, ErrAccountDataNotFound
	}
	return v, nil
}

func (m *mockAccountDataDB) PutAccountData(ctx context.Context, userID, roomID, eventType string, content json.RawMessage) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[m.key(userID, roomID, eventType)] = content
	return nil
}

// ─── Helper ──────────────────────────────────────────────────────────────────

// buildAuthedAccountDataHandler wires JWTMiddleware → AccountDataHandler on a mux.
// JWT sub is "test-sub-123" → authenticated user_id "@test-sub-123:test.local".
func buildAuthedAccountDataHandler(t *testing.T, db *mockAccountDataDB) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)
	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")

	handler := NewAccountDataHandler(AccountDataConfig{
		ServerName: "test.local",
		DB:         db,
	})

	mux := http.NewServeMux()
	mux.Handle("GET /user/{userId}/account_data/{type}",
		jwtMiddleware(http.HandlerFunc(handler.GetGlobalAccountData)))
	mux.Handle("PUT /user/{userId}/account_data/{type}",
		jwtMiddleware(http.HandlerFunc(handler.PutGlobalAccountData)))
	mux.Handle("GET /user/{userId}/rooms/{roomId}/account_data/{type}",
		jwtMiddleware(http.HandlerFunc(handler.GetRoomAccountData)))
	mux.Handle("PUT /user/{userId}/rooms/{roomId}/account_data/{type}",
		jwtMiddleware(http.HandlerFunc(handler.PutRoomAccountData)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── Test 1: PUT global account data — happy path → 200 {} ──────────────────
//
// AC1 — PUT /_matrix/client/v3/user/{userId}/account_data/{type} stores data.
// Response: 200 {}.

func TestPutGlobalAccountData_HappyPath(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedAccountDataHandler(t, db)

	body := `{"push_rules":{"global":{}}}`
	req := httptest.NewRequest(http.MethodPut,
		"/user/@test-sub-123:test.local/account_data/m.push_rules",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Body.String()) != "{}" {
		t.Errorf("expected empty JSON object {}, got %s", w.Body.String())
	}
}

// ─── Test 2: GET global account data — returns stored content ────────────────
//
// AC2 — GET retrieves the stored JSON content object.

func TestGetGlobalAccountData_HappyPath(t *testing.T) {
	db := newMockAccountDataDB()
	// Pre-seed store with global account data.
	_ = db.PutAccountData(context.Background(), "@test-sub-123:test.local", "", "m.push_rules", json.RawMessage(`{"global":{}}`))

	mux, makeToken := buildAuthedAccountDataHandler(t, db)

	req := httptest.NewRequest(http.MethodGet,
		"/user/@test-sub-123:test.local/account_data/m.push_rules",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "global") {
		t.Errorf("expected body to contain 'global', got: %s", w.Body.String())
	}
}

// ─── Test 3: GET global account data — not found → 404 M_NOT_FOUND ───────────
//
// AC2 — GET returns M_NOT_FOUND (HTTP 404) if no data exists.

func TestGetGlobalAccountData_NotFound(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedAccountDataHandler(t, db)

	req := httptest.NewRequest(http.MethodGet,
		"/user/@test-sub-123:test.local/account_data/m.nonexistent",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_NOT_FOUND") {
		t.Errorf("expected M_NOT_FOUND in body, got: %s", w.Body.String())
	}
}

// ─── Test 4: PUT global — userId mismatch → 403 M_FORBIDDEN ─────────────────
//
// AC3 — If userId in path ≠ authenticated user's JWT subject, return 403.

func TestPutGlobalAccountData_UserIdMismatch_Forbidden(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedAccountDataHandler(t, db)

	body := `{"push_rules":{}}`
	// JWT sub is "test-sub-123" → "@test-sub-123:test.local", path has @bob:test.local.
	req := httptest.NewRequest(http.MethodPut,
		"/user/@bob:test.local/account_data/m.push_rules",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_FORBIDDEN") {
		t.Errorf("expected M_FORBIDDEN in body, got: %s", w.Body.String())
	}
}

// ─── Test 5: GET room account data — happy path → 200 ────────────────────────
//
// AC2 — GET /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}
// returns the stored content.

func TestGetRoomAccountData_HappyPath(t *testing.T) {
	db := newMockAccountDataDB()
	// Pre-seed store.
	_ = db.PutAccountData(context.Background(),
		"@test-sub-123:test.local", "!room1:test.local", "m.fully_read",
		json.RawMessage(`{"event_id":"$abc"}`))

	mux, makeToken := buildAuthedAccountDataHandler(t, db)

	req := httptest.NewRequest(http.MethodGet,
		"/user/@test-sub-123:test.local/rooms/!room1:test.local/account_data/m.fully_read",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "$abc") {
		t.Errorf("expected event_id $abc in body, got: %s", w.Body.String())
	}
}

// ─── Test 6: GET room account data — not found → 404 ────────────────────────
//
// AC2 — Returns 404 M_NOT_FOUND when no data exists for the triple.

func TestGetRoomAccountData_NotFound(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedAccountDataHandler(t, db)

	req := httptest.NewRequest(http.MethodGet,
		"/user/@test-sub-123:test.local/rooms/!room1:test.local/account_data/m.fully_read",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_NOT_FOUND") {
		t.Errorf("expected M_NOT_FOUND in body, got: %s", w.Body.String())
	}
}

// ─── Test 7: PUT room account data — happy path → 200 {} ─────────────────────
//
// AC1 — PUT /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}
// stores data and returns 200 {}.

func TestPutRoomAccountData_HappyPath(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedAccountDataHandler(t, db)

	body := `{"event_id":"$abc"}`
	req := httptest.NewRequest(http.MethodPut,
		"/user/@test-sub-123:test.local/rooms/!room1:test.local/account_data/m.fully_read",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Body.String()) != "{}" {
		t.Errorf("expected empty JSON object {}, got %s", w.Body.String())
	}
}

// ─── Test 8: PUT room — userId mismatch → 403 M_FORBIDDEN ───────────────────
//
// AC3 — If userId in path ≠ authenticated user, return 403 before any DB call.

func TestPutRoomAccountData_UserIdMismatch_Forbidden(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedAccountDataHandler(t, db)

	body := `{"tags":{}}`
	// JWT sub → "@test-sub-123:test.local", path has @bob:test.local.
	req := httptest.NewRequest(http.MethodPut,
		"/user/@bob:test.local/rooms/!room1:test.local/account_data/m.tag",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_FORBIDDEN") {
		t.Errorf("expected M_FORBIDDEN in body, got: %s", w.Body.String())
	}
}

// ─── Test 9: Upsert semantics — second PUT overwrites first ─────────────────
//
// AC6 — Last write wins; no duplicate-key error.
// AC5 story test: PUT m.tag {"tags":{"m.favourite":{}}} → PUT m.tag {"tags":{}} → GET returns {"tags":{}}.

func TestPutRoomAccountData_UpsertSemantics(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedAccountDataHandler(t, db)

	// First PUT.
	req1 := httptest.NewRequest(http.MethodPut,
		"/user/@test-sub-123:test.local/rooms/!room1:test.local/account_data/m.tag",
		strings.NewReader(`{"tags":{"m.favourite":{}}}`))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+makeToken())
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first PUT: expected 200, got %d", w1.Code)
	}

	// Second PUT (overwrite).
	req2 := httptest.NewRequest(http.MethodPut,
		"/user/@test-sub-123:test.local/rooms/!room1:test.local/account_data/m.tag",
		strings.NewReader(`{"tags":{}}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+makeToken())
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second PUT: expected 200, got %d", w2.Code)
	}

	// GET must return the second PUT's content (last write wins).
	req3 := httptest.NewRequest(http.MethodGet,
		"/user/@test-sub-123:test.local/rooms/!room1:test.local/account_data/m.tag",
		nil)
	req3.Header.Set("Authorization", "Bearer "+makeToken())
	w3 := httptest.NewRecorder()
	mux.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("GET after upsert: expected 200, got %d; body: %s", w3.Code, w3.Body.String())
	}
	body := w3.Body.String()
	if strings.Contains(body, "m.favourite") {
		t.Errorf("expected overwritten content, but body still contains 'm.favourite': %s", body)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("GET response is not valid JSON: %v — body: %s", err, body)
	}
}

// ─── Test 10: PUT — bad JSON body → 400 M_BAD_JSON ──────────────────────────
//
// Validates that malformed request body returns 400 M_BAD_JSON.

func TestPutGlobalAccountData_BadJSON(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedAccountDataHandler(t, db)

	req := httptest.NewRequest(http.MethodPut,
		"/user/@test-sub-123:test.local/account_data/m.push_rules",
		bytes.NewBufferString(`not-valid-json`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_BAD_JSON") {
		t.Errorf("expected M_BAD_JSON in body, got: %s", w.Body.String())
	}
}

// ─── Test 11: Unauthenticated PUT → 401 ─────────────────────────────────────
//
// JWT middleware rejects requests without a valid Bearer token.

func TestPutGlobalAccountData_Unauthenticated(t *testing.T) {
	db := newMockAccountDataDB()
	mux, _ := buildAuthedAccountDataHandler(t, db)

	req := httptest.NewRequest(http.MethodPut,
		"/user/@test-sub-123:test.local/account_data/m.push_rules",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately no Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── Test 12: GET room — userId mismatch → 403 M_FORBIDDEN ──────────────────
//
// AC3 — GET also checks userId ownership.

func TestGetRoomAccountData_UserIdMismatch_Forbidden(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedAccountDataHandler(t, db)

	// JWT sub → "@test-sub-123:test.local", path has @bob:test.local.
	req := httptest.NewRequest(http.MethodGet,
		"/user/@bob:test.local/rooms/!room1:test.local/account_data/m.fully_read",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_FORBIDDEN") {
		t.Errorf("expected M_FORBIDDEN in body, got: %s", w.Body.String())
	}
}

// Compile-time sentinel: errors.Is on ErrAccountDataNotFound must succeed.
var _ = errors.Is(ErrAccountDataNotFound, ErrAccountDataNotFound)
