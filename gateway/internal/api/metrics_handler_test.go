//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.10:
// Metrics API (GET /admin/metrics).
//
// RED PHASE — all tests fail until implementation is complete.
// This file will not compile until:
//   - MetricsRepository interface is defined (metrics_repo.go)
//     with method: GetMetricsCounts(ctx) (*MetricsCounts, error)
//   - MetricsCounts struct is defined with fields:
//     RoomCount, ArchivedRoomCount, RegisteredUsers, DeactivatedUsers int
//   - AdminServer gains Metrics MetricsRepository field (server.go)
//   - AdminServer.GetAdminMetrics handler is implemented (server.go)
//   - GET /api/v1/admin/metrics is already registered (router.go line 38) — no change needed
//   - make gen-api has regenerated api_gen.go with AdminMetricsResponse schema applied
//     (replacing EmptyResponse placeholder)
//   - proto/core.proto: no new RPC needed — uses existing GetMetrics RPC
//   - pb.GetMetricsResponse must have active_sessions (int32) + msg_per_sec (float32)
//     fields — already present in core.proto
//
// Covered Acceptance Criteria:
//
//	AC#3  GET /api/v1/admin/metrics — all 6 fields present with correct types [P0]
//	AC#3  GET /api/v1/admin/metrics — DB counts come from MetricsRepository [P0]
//	AC#3  GET /api/v1/admin/metrics — active_sessions + msg_per_sec come from gRPC GetMetrics [P0]
//	AC#5  Router test: GET /admin/metrics with nil Metrics repo → 501 [P0]
package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"encoding/json"
	"testing"

	"github.com/nebu/nebu/internal/api"
	"github.com/nebu/nebu/internal/middleware"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"
)

// ── Test doubles: MetricsRepository ──────────────────────────────────────────

// mockMetricsRepository implements api.MetricsRepository for unit tests.
// It returns preset counts for the DB-derived fields (rooms + users).
type mockMetricsRepository struct {
	counts *api.MetricsCounts
	getErr error
}

func (m *mockMetricsRepository) GetMetricsCounts(
	_ context.Context,
) (*api.MetricsCounts, error) {
	return m.counts, m.getErr
}

// ── Test doubles: CoreServiceClient for metrics tests ─────────────────────────

// mockCoreClientForMetrics captures GetMetrics calls.
// It satisfies pb.CoreServiceClient via embedding; only GetMetrics is overridden.
type mockCoreClientForMetrics struct {
	pb.CoreServiceClient // embed to satisfy interface

	// GetMetrics return values
	metricsResp *pb.GetMetricsResponse
	metricsErr  error
}

func (m *mockCoreClientForMetrics) GetMetrics(
	_ context.Context,
	_ *pb.GetMetricsRequest,
	_ ...grpc.CallOption,
) (*pb.GetMetricsResponse, error) {
	if m.metricsResp != nil {
		return m.metricsResp, m.metricsErr
	}
	return &pb.GetMetricsResponse{
		ActiveSessions: 0,
		MsgPerSec:      0.0,
	}, m.metricsErr
}

func (m *mockCoreClientForMetrics) WriteAuditLog(
	_ context.Context,
	_ *pb.WriteAuditLogRequest,
	_ ...grpc.CallOption,
) (*pb.WriteAuditLogResponse, error) {
	return &pb.WriteAuditLogResponse{}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// noopJWTMiddlewareForMetrics injects instance_admin role and a test actor ID.
func noopJWTMiddlewareForMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, "instance_admin")
		ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@admin:example.com")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// doGetAdminMetrics sends GET /api/v1/admin/metrics to an AdminServer with the given repos.
func doGetAdminMetrics(
	t *testing.T,
	metricsRepo api.MetricsRepository,
	coreClient pb.CoreServiceClient,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv := &api.AdminServer{
		Metrics:    metricsRepo,
		CoreClient: coreClient,
	}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForMetrics, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// ── AC#3: GET /admin/metrics — all 6 fields present with correct types ────────

// TestGetAdminMetrics_AllSixFieldsPresent covers AC#3 + AT#4 [P0]:
// GET /api/v1/admin/metrics must return all 6 required fields in the response JSON.
// Types: active_sessions (integer), room_count (integer), archived_room_count (integer),
//        msg_per_sec_1m (number/float), registered_users (integer), deactivated_users (integer).
//
// Failing reason: GetAdminMetrics handler is a stub returning 501.
func TestGetAdminMetrics_AllSixFieldsPresent(t *testing.T) {
	metricsRepo := &mockMetricsRepository{
		counts: &api.MetricsCounts{
			RoomCount:         5,
			ArchivedRoomCount: 2,
			RegisteredUsers:   10,
			DeactivatedUsers:  1,
		},
	}
	coreClient := &mockCoreClientForMetrics{
		metricsResp: &pb.GetMetricsResponse{
			ActiveSessions: 3,
			MsgPerSec:      1.5,
		},
	}

	w := doGetAdminMetrics(t, metricsRepo, coreClient)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#3/AT#4] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#3/AT#4] response is not valid JSON: %v", err)
	}

	// Verify all 6 required fields are present.
	requiredFields := []string{
		"active_sessions",
		"room_count",
		"archived_room_count",
		"msg_per_sec_1m",
		"registered_users",
		"deactivated_users",
	}
	for _, field := range requiredFields {
		if _, ok := resp[field]; !ok {
			t.Errorf("[AC#3/AT#4] required field %q is missing from response; got: %v", field, resp)
		}
	}
}

// TestGetAdminMetrics_FieldTypes covers AC#3 [P0]:
// Numeric fields must be numeric types in JSON, not strings.
// - active_sessions, room_count, archived_room_count, registered_users, deactivated_users: integer
// - msg_per_sec_1m: float (number)
//
// Failing reason: GetAdminMetrics handler is a stub returning 501.
func TestGetAdminMetrics_FieldTypes(t *testing.T) {
	metricsRepo := &mockMetricsRepository{
		counts: &api.MetricsCounts{
			RoomCount:         5,
			ArchivedRoomCount: 2,
			RegisteredUsers:   10,
			DeactivatedUsers:  1,
		},
	}
	coreClient := &mockCoreClientForMetrics{
		metricsResp: &pb.GetMetricsResponse{
			ActiveSessions: 3,
			MsgPerSec:      1.5,
		},
	}

	w := doGetAdminMetrics(t, metricsRepo, coreClient)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#3] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Decode via json.Number to distinguish int from float from string.
	dec := json.NewDecoder(w.Body)
	dec.UseNumber()
	var resp map[string]any
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("[AC#3] response is not valid JSON: %v", err)
	}

	// Integer fields must decode as json.Number and be parseable as int64.
	intFields := []string{
		"active_sessions",
		"room_count",
		"archived_room_count",
		"registered_users",
		"deactivated_users",
	}
	for _, field := range intFields {
		v, ok := resp[field]
		if !ok {
			t.Errorf("[AC#3] field %q missing from response", field)
			continue
		}
		n, isNum := v.(json.Number)
		if !isNum {
			t.Errorf("[AC#3] field %q must be a number, got %T: %v", field, v, v)
			continue
		}
		if _, err := n.Int64(); err != nil {
			t.Errorf("[AC#3] field %q must be an integer, but Int64() failed: %v (value: %s)", field, err, n)
		}
	}

	// msg_per_sec_1m must be a number (float OK).
	v, ok := resp["msg_per_sec_1m"]
	if !ok {
		t.Error("[AC#3] field 'msg_per_sec_1m' missing from response")
	} else {
		if _, isNum := v.(json.Number); !isNum {
			t.Errorf("[AC#3] field 'msg_per_sec_1m' must be a number, got %T: %v", v, v)
		}
	}
}

// TestGetAdminMetrics_CorrectValues covers AC#3 + AT#4 [P0]:
// The handler must combine DB counts from MetricsRepository with gRPC values from GetMetrics.
// Specific values must match the mock return values exactly.
//
// Failing reason: GetAdminMetrics handler is a stub returning 501.
func TestGetAdminMetrics_CorrectValues(t *testing.T) {
	metricsRepo := &mockMetricsRepository{
		counts: &api.MetricsCounts{
			RoomCount:         7,
			ArchivedRoomCount: 3,
			RegisteredUsers:   42,
			DeactivatedUsers:  5,
		},
	}
	coreClient := &mockCoreClientForMetrics{
		metricsResp: &pb.GetMetricsResponse{
			ActiveSessions: 12,
			MsgPerSec:      2.75,
		},
	}

	w := doGetAdminMetrics(t, metricsRepo, coreClient)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#3/AT#4] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#3/AT#4] response is not valid JSON: %v", err)
	}

	// DB-derived fields (from MetricsRepository).
	if v := resp["room_count"]; v != float64(7) {
		t.Errorf("[AC#3/AT#4] expected room_count=7, got %v", v)
	}
	if v := resp["archived_room_count"]; v != float64(3) {
		t.Errorf("[AC#3/AT#4] expected archived_room_count=3, got %v", v)
	}
	if v := resp["registered_users"]; v != float64(42) {
		t.Errorf("[AC#3/AT#4] expected registered_users=42, got %v", v)
	}
	if v := resp["deactivated_users"]; v != float64(5) {
		t.Errorf("[AC#3/AT#4] expected deactivated_users=5, got %v", v)
	}

	// gRPC-derived fields (from GetMetrics via CoreServiceClient).
	if v := resp["active_sessions"]; v != float64(12) {
		t.Errorf("[AC#3/AT#4] expected active_sessions=12 (from gRPC GetMetrics), got %v", v)
	}
	// msg_per_sec_1m tolerance: float comparison with small delta.
	if v, ok := resp["msg_per_sec_1m"].(float64); !ok || v < 2.74 || v > 2.76 {
		t.Errorf("[AC#3/AT#4] expected msg_per_sec_1m≈2.75 (from gRPC GetMetrics), got %v", resp["msg_per_sec_1m"])
	}
}

// TestGetAdminMetrics_ZeroValues_Returns200 covers AC#3 [P1]:
// All-zero metrics must still return 200 with all 6 fields (zero is a valid value).
//
// Failing reason: GetAdminMetrics handler is a stub returning 501.
func TestGetAdminMetrics_ZeroValues_Returns200(t *testing.T) {
	metricsRepo := &mockMetricsRepository{
		counts: &api.MetricsCounts{
			RoomCount:         0,
			ArchivedRoomCount: 0,
			RegisteredUsers:   0,
			DeactivatedUsers:  0,
		},
	}
	coreClient := &mockCoreClientForMetrics{
		metricsResp: &pb.GetMetricsResponse{
			ActiveSessions: 0,
			MsgPerSec:      0.0,
		},
	}

	w := doGetAdminMetrics(t, metricsRepo, coreClient)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#3] expected status 200 for all-zero metrics, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#3] response is not valid JSON: %v", err)
	}

	// All 6 fields must be present even when zero.
	requiredFields := []string{
		"active_sessions",
		"room_count",
		"archived_room_count",
		"msg_per_sec_1m",
		"registered_users",
		"deactivated_users",
	}
	for _, field := range requiredFields {
		if _, ok := resp[field]; !ok {
			t.Errorf("[AC#3] field %q must be present even when zero; response: %v", field, resp)
		}
	}
}

// ── AC#5 Router test: GET /admin/metrics nil Metrics repo → 501 ──────────────

// TestGetAdminMetrics_NilMetricsRepo_Returns501 covers AC#5 [P0]:
// When AdminServer.Metrics is nil, GET /api/v1/admin/metrics must return 501.
//
// This verifies the nil-guard pattern is in place for the Metrics handler.
// Failing reason: GetAdminMetrics handler is currently a stub returning 501
// but once implemented it must still return 501 when Metrics is nil.
func TestGetAdminMetrics_NilMetricsRepo_Returns501(t *testing.T) {
	mux := http.NewServeMux()
	// AdminServer with no Metrics wired — simulates pre-Story-6.10 configuration.
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddlewareForMetrics, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("[AC#5] expected 501 for nil Metrics repo, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestGetAdminMetrics_RouteAlreadyRegistered covers AC#5 [P0]:
// GET /api/v1/admin/metrics must be registered in RegisterAdminRoutes.
// A 404 means the route was accidentally removed.
//
// This is a regression guard — the route is already registered in router.go line 38.
func TestGetAdminMetrics_RouteAlreadyRegistered(t *testing.T) {
	metricsRepo := &mockMetricsRepository{
		counts: &api.MetricsCounts{
			RoomCount:       1,
			RegisteredUsers: 1,
		},
	}
	coreClient := &mockCoreClientForMetrics{}
	mux := http.NewServeMux()
	srv := &api.AdminServer{
		Metrics:    metricsRepo,
		CoreClient: coreClient,
	}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForMetrics, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("[AC#5] GET /api/v1/admin/metrics is not registered — got 404")
	}
}

// TestGetAdminMetrics_NilCoreClient_DoesNotPanic covers AC#3 [P1]:
// If CoreClient is nil, the handler must not panic — it should either return
// partial metrics (from DB only) with zero gRPC values, or return 200 with defaults.
// It must NOT return 500 or panic.
//
// Failing reason: GetAdminMetrics handler is a stub returning 501.
func TestGetAdminMetrics_NilCoreClient_DoesNotPanic(t *testing.T) {
	metricsRepo := &mockMetricsRepository{
		counts: &api.MetricsCounts{
			RoomCount:         3,
			ArchivedRoomCount: 1,
			RegisteredUsers:   20,
			DeactivatedUsers:  2,
		},
	}

	mux := http.NewServeMux()
	// CoreClient is nil — handler must handle this gracefully.
	srv := &api.AdminServer{
		Metrics:    metricsRepo,
		CoreClient: nil,
	}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForMetrics, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/metrics", nil)
	w := httptest.NewRecorder()

	// Must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("[AC#3] GetAdminMetrics panicked with nil CoreClient: %v", r)
		}
	}()
	mux.ServeHTTP(w, req)

	// Must not return 500 — either 200 (with zero gRPC values) or 501 (if nil CoreClient → fallback).
	if w.Code == http.StatusInternalServerError {
		t.Errorf("[AC#3] GetAdminMetrics returned 500 with nil CoreClient; expected 200 (with zero gRPC values) or graceful degradation")
	}
}
