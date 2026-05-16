//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.10:
// Server Config API (GET + PATCH /admin/config).
//
// RED PHASE — all tests fail until implementation is complete.
// This file will not compile until:
//   - ServerConfigRepository interface is defined (server_config_repo.go)
//     with methods: GetServerConfig(ctx) (*ServerConfigData, error)
//                   UpsertServerConfigKey(ctx, key, value string) error
//   - ServerConfigData struct is defined with fields:
//     InstanceName, OIDCIssuer, OIDCClientID string; AuditLogRetentionDays int
//   - AdminServer gains ServerConfig ServerConfigRepository + Secret []byte fields (server.go)
//   - AdminServer gains PatchAdminConfig handler (server.go)
//   - GET /api/v1/admin/config is already registered (router.go line 37) — no change needed
//   - PATCH /api/v1/admin/config is registered in router.go
//   - make gen-api has regenerated api_gen.go with PatchAdminConfigRequestObject,
//     PatchAdminConfig200Response, PatchAdminConfig400Response, PatchAdminConfig501Response
//     on StrictServerInterface
//   - proto/core.proto adds InvalidateAllAdminSessions RPC; make proto regenerates
//     gateway/internal/grpc/pb/core_grpc.pb.go with InvalidateAllAdminSessions method
//     on CoreServiceClient
//
// Covered Acceptance Criteria:
//
//	AC#1  GET /api/v1/admin/config — oidc_client_secret NEVER in response [P0]
//	AC#2  PATCH /api/v1/admin/config — oidc_issuer change calls InvalidateAllAdminSessions [P0]
//	AC#2  PATCH /api/v1/admin/config — non-OIDC-field change does NOT call InvalidateAllAdminSessions [P0]
//	AC#2  PATCH /api/v1/admin/config — audit_log_retention_days out-of-range → 400 [P0]
//	AC#5  Router test: GET /admin/config with nil ServerConfig → 501 [P0]
//	AC#5  Router test: PATCH /admin/config with nil ServerConfig → 501 [P0]
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nebu/nebu/internal/api"
	"github.com/nebu/nebu/internal/middleware"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ── Test doubles: ServerConfigRepository ──────────────────────────────────────

// mockServerConfigRepository implements api.ServerConfigRepository for unit tests.
// It simulates a server_config DB with preset return values.
// oidc_client_secret is intentionally NOT part of ServerConfigData (write-only field).
type mockServerConfigRepository struct {
	// GetServerConfig return values
	configData *api.ServerConfigData
	getErr     error

	// UpsertServerConfigKey tracking
	upsertCalled  bool
	upsertedKeys  []string
	upsertErr     error
	capturedValues map[string]string // key → stored value (for encryption assertions)
}

func (m *mockServerConfigRepository) GetServerConfig(
	_ context.Context,
) (*api.ServerConfigData, error) {
	return m.configData, m.getErr
}

func (m *mockServerConfigRepository) UpsertServerConfigKey(
	_ context.Context,
	key, value string,
) error {
	m.upsertCalled = true
	m.upsertedKeys = append(m.upsertedKeys, key)
	if m.capturedValues == nil {
		m.capturedValues = make(map[string]string)
	}
	m.capturedValues[key] = value
	return m.upsertErr
}

// ── Test doubles: RoomDefaultsRepository (reused from room_defaults_handler_test.go) ──

// Note: mockRoomDefaultsRepository is already defined in room_defaults_handler_test.go
// within package api_test. We define a local alias struct for config handler tests
// to avoid symbol conflicts while keeping tests independent.
type mockRoomDefaultsForConfig struct {
	getMaxMembers int
	getVisibility string
	getErr        error
}

func (m *mockRoomDefaultsForConfig) UpsertRoomDefaults(
	_ context.Context,
	_ int,
	_ string,
) error {
	return nil // not called in GET /admin/config tests
}

func (m *mockRoomDefaultsForConfig) GetRoomDefaults(
	_ context.Context,
) (int, string, error) {
	return m.getMaxMembers, m.getVisibility, m.getErr
}

// ── Test doubles: CoreServiceClient for config tests ─────────────────────────

// mockCoreClientForConfig captures calls to InvalidateAllAdminSessions, WriteAuditLog,
// and UpdateServerConfig. It satisfies pb.CoreServiceClient via embedding; only the
// methods used by the config handlers are overridden.
type mockCoreClientForConfig struct {
	pb.CoreServiceClient // embed to satisfy interface

	// InvalidateAllAdminSessions tracking
	invalidateAllCalled bool
	invalidateAllErr    error

	// WriteAuditLog tracking
	auditCalled bool

	// UpdateServerConfig tracking (Story 14-1b: matrix_user_id_claim via gRPC)
	updateServerConfigCalled             bool
	updateServerConfigFailPrecondition   bool
	updateServerConfigErr                error
	capturedMatrixUserIDClaim            string
}

func (m *mockCoreClientForConfig) InvalidateAllAdminSessions(
	_ context.Context,
	_ *pb.InvalidateAllAdminSessionsRequest,
	_ ...grpc.CallOption,
) (*pb.InvalidateAllAdminSessionsResponse, error) {
	m.invalidateAllCalled = true
	return &pb.InvalidateAllAdminSessionsResponse{Ok: true}, m.invalidateAllErr
}

func (m *mockCoreClientForConfig) WriteAuditLog(
	_ context.Context,
	_ *pb.WriteAuditLogRequest,
	_ ...grpc.CallOption,
) (*pb.WriteAuditLogResponse, error) {
	m.auditCalled = true
	return &pb.WriteAuditLogResponse{}, nil
}

func (m *mockCoreClientForConfig) UpdateServerConfig(
	_ context.Context,
	req *pb.UpdateServerConfigRequest,
	_ ...grpc.CallOption,
) (*pb.UpdateServerConfigResponse, error) {
	m.updateServerConfigCalled = true
	if req != nil {
		m.capturedMatrixUserIDClaim = req.GetMatrixUserIdClaim()
	}
	if m.updateServerConfigFailPrecondition {
		return nil, status.Error(codes.FailedPrecondition, "matrix_user_id_claim cannot be changed after bootstrap")
	}
	return &pb.UpdateServerConfigResponse{Ok: true}, m.updateServerConfigErr
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// noopJWTMiddlewareForConfig injects instance_admin role and a test actor ID.
func noopJWTMiddlewareForConfig(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, "instance_admin")
		ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@admin:example.com")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// doGetAdminConfig sends GET /api/v1/admin/config to an AdminServer with the given repos.
func doGetAdminConfig(
	t *testing.T,
	configRepo api.ServerConfigRepository,
	roomDefaultsRepo api.RoomDefaultsRepository,
	coreClient pb.CoreServiceClient,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv := &api.AdminServer{
		ServerConfig: configRepo,
		RoomDefaults: roomDefaultsRepo,
		CoreClient:   coreClient,
	}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForConfig, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// doPatchAdminConfig sends PATCH /api/v1/admin/config with the given JSON body.
func doPatchAdminConfig(
	t *testing.T,
	configRepo api.ServerConfigRepository,
	roomDefaultsRepo api.RoomDefaultsRepository,
	coreClient pb.CoreServiceClient,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv := &api.AdminServer{
		ServerConfig: configRepo,
		RoomDefaults: roomDefaultsRepo,
		CoreClient:   coreClient,
		// Secret is intentionally omitted — oidc_client_secret encryption is not the
		// primary focus of these unit tests (Secret defaults to nil/empty, which the
		// handler must tolerate for non-secret field updates).
	}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForConfig, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/config",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// ── AC#1: GET /admin/config — oidc_client_secret must NEVER appear ────────────

// TestGetAdminConfig_OIDCClientSecretNeverInResponse covers AC#1 + AT#1 [P0]:
// The GET /api/v1/admin/config response must never include the "oidc_client_secret"
// key, even if the DB row for that key exists.
//
// This is the primary security invariant of this handler.
// Failing reason: GetAdminConfig handler is a stub returning 501.
func TestGetAdminConfig_OIDCClientSecretNeverInResponse(t *testing.T) {
	// ServerConfigData intentionally has no OIDCClientSecret field.
	// The mock simulates a DB that has oidc_client_secret stored (write-only).
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Acme Corp Chat",
			OIDCIssuer:            "https://dex.example.com",
			OIDCClientID:          "nebu-client",
			AuditLogRetentionDays: 2555,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{
		getMaxMembers: 50,
		getVisibility: "private",
	}
	coreClient := &mockCoreClientForConfig{}

	w := doGetAdminConfig(t, configRepo, roomDefaultsRepo, coreClient)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1/AT#1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Parse as raw map to detect any key named "oidc_client_secret" regardless of nesting.
	rawBody := w.Body.String()
	if strings.Contains(rawBody, "oidc_client_secret") {
		t.Errorf("[AC#1/AT#1] SECURITY VIOLATION: response body contains 'oidc_client_secret' — this field must NEVER be returned:\n%s", rawBody)
	}

	// Also verify the response is valid JSON with the expected readable fields.
	var resp map[string]any
	if err := json.NewDecoder(strings.NewReader(rawBody)).Decode(&resp); err != nil {
		t.Fatalf("[AC#1/AT#1] response is not valid JSON: %v", err)
	}
	expectedFields := []string{
		"instance_name",
		"oidc_issuer",
		"oidc_client_id",
		"room_default_max_members",
		"room_default_visibility",
		"audit_log_retention_days",
		"oidc_directory_enabled",
		"oidc_directory_endpoint",
	}
	for _, field := range expectedFields {
		if _, ok := resp[field]; !ok {
			t.Errorf("[AC#1/AT#1] expected field %q in response, not found; response: %s", field, rawBody)
		}
	}
}

// TestGetAdminConfig_ReturnsCorrectValues covers AC#1 [P0]:
// GET /api/v1/admin/config must return values from both ServerConfigRepository
// and RoomDefaultsRepository combined.
//
// Failing reason: GetAdminConfig handler is a stub returning 501.
func TestGetAdminConfig_ReturnsCorrectValues(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Test Instance",
			OIDCIssuer:            "https://oidc.test",
			OIDCClientID:          "test-client",
			AuditLogRetentionDays: 365,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{
		getMaxMembers: 100,
		getVisibility: "public",
	}
	coreClient := &mockCoreClientForConfig{}

	w := doGetAdminConfig(t, configRepo, roomDefaultsRepo, coreClient)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}

	if v, ok := resp["instance_name"]; !ok || v != "Test Instance" {
		t.Errorf("[AC#1] expected instance_name='Test Instance', got %v", v)
	}
	if v, ok := resp["oidc_issuer"]; !ok || v != "https://oidc.test" {
		t.Errorf("[AC#1] expected oidc_issuer='https://oidc.test', got %v", v)
	}
	if v, ok := resp["oidc_client_id"]; !ok || v != "test-client" {
		t.Errorf("[AC#1] expected oidc_client_id='test-client', got %v", v)
	}
	// JSON numbers decode as float64 by default
	if v, ok := resp["audit_log_retention_days"]; !ok || v != float64(365) {
		t.Errorf("[AC#1] expected audit_log_retention_days=365, got %v", v)
	}
	if v, ok := resp["room_default_max_members"]; !ok || v != float64(100) {
		t.Errorf("[AC#1] expected room_default_max_members=100, got %v", v)
	}
	if v, ok := resp["room_default_visibility"]; !ok || v != "public" {
		t.Errorf("[AC#1] expected room_default_visibility='public', got %v", v)
	}
}

// TestGetAdminConfig_MissingKeys_ReturnsDefaults covers AC#1 [P1]:
// When ServerConfigRepository returns empty strings and zero values for missing
// keys, the handler must return the documented defaults.
//
// Failing reason: GetAdminConfig handler is a stub returning 501.
func TestGetAdminConfig_MissingKeys_ReturnsDefaults(t *testing.T) {
	// Empty config — all keys missing in DB → defaults apply.
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "",
			OIDCIssuer:            "",
			OIDCClientID:          "",
			AuditLogRetentionDays: 2555, // default per AC#1
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{
		getMaxMembers: 0,
		getVisibility: "private",
	}
	coreClient := &mockCoreClientForConfig{}

	w := doGetAdminConfig(t, configRepo, roomDefaultsRepo, coreClient)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200 for missing keys, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}

	// audit_log_retention_days must default to 2555
	if v, ok := resp["audit_log_retention_days"]; !ok || v != float64(2555) {
		t.Errorf("[AC#1] expected audit_log_retention_days default=2555, got %v", v)
	}
}

// ── AC#5 Router test: GET /admin/config nil ServerConfig → 501 ───────────────

// TestGetAdminConfig_NilServerConfigRepo_Returns501 covers AC#5 [P0]:
// When AdminServer.ServerConfig is nil, GET /api/v1/admin/config must return 501.
//
// Failing reason: GetAdminConfig handler currently returns 501 as a stub
// but does not apply the nil-guard pattern — once implemented it must still
// return 501 when the repo is missing.
func TestGetAdminConfig_NilServerConfigRepo_Returns501(t *testing.T) {
	mux := http.NewServeMux()
	// AdminServer with no ServerConfig wired — simulates pre-Story-6.10 configuration.
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddlewareForConfig, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("[AC#5] expected 501 for nil ServerConfig, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ── AC#2: PATCH /admin/config — OIDC field change triggers session invalidation ─

// TestPatchAdminConfig_OIDCIssuerChange_InvalidatesAllAdminSessions covers AC#2 + AT#2 [P0]:
// PATCH with oidc_issuer changed must call gRPC InvalidateAllAdminSessions exactly once.
//
// Failing reason: PatchAdminConfig handler does not exist (stub or not compiled yet).
func TestPatchAdminConfig_OIDCIssuerChange_InvalidatesAllAdminSessions(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Test",
			OIDCIssuer:            "https://old.dex.example",
			OIDCClientID:          "test-client",
			AuditLogRetentionDays: 2555,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{
		getMaxMembers: 50,
		getVisibility: "private",
	}
	coreClient := &mockCoreClientForConfig{}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"oidc_issuer": "https://new.dex.example"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2/AT#2] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if !coreClient.invalidateAllCalled {
		t.Error("[AC#2/AT#2] expected InvalidateAllAdminSessions to be called when oidc_issuer changes")
	}

	if !coreClient.auditCalled {
		t.Error("[AC#2] expected audit.LogEvent called after successful PATCH")
	}

	// Response must not expose oidc_client_secret (same guard as GET)
	if strings.Contains(w.Body.String(), "oidc_client_secret") {
		t.Errorf("[AC#2/AT#2] SECURITY VIOLATION: PATCH response contains 'oidc_client_secret':\n%s", w.Body.String())
	}
}

// TestPatchAdminConfig_OIDCClientIDChange_InvalidatesAllAdminSessions covers AC#2 [P0]:
// PATCH with oidc_client_id changed must also call InvalidateAllAdminSessions.
//
// Failing reason: PatchAdminConfig handler does not exist yet.
func TestPatchAdminConfig_OIDCClientIDChange_InvalidatesAllAdminSessions(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Test",
			OIDCIssuer:            "https://dex.example",
			OIDCClientID:          "old-client",
			AuditLogRetentionDays: 2555,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"oidc_client_id": "new-client"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if !coreClient.invalidateAllCalled {
		t.Error("[AC#2] expected InvalidateAllAdminSessions to be called when oidc_client_id changes")
	}
}

// TestPatchAdminConfig_OIDCClientSecretChange_InvalidatesAllAdminSessions covers AC#2 [P0]:
// PATCH with oidc_client_secret changed must call InvalidateAllAdminSessions.
//
// Failing reason: PatchAdminConfig handler does not exist yet.
func TestPatchAdminConfig_OIDCClientSecretChange_InvalidatesAllAdminSessions(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Test",
			OIDCIssuer:            "https://dex.example",
			OIDCClientID:          "test-client",
			AuditLogRetentionDays: 2555,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	// PATCH with oidc_client_secret requires a non-empty Secret (encryption key).
	// Without one, the handler returns 500 — see the dedicated NoEncryptionKey test below.
	secret := []byte("0123456789abcdef0123456789abcdef")
	w := doPatchAdminConfigWithSecret(t, configRepo, roomDefaultsRepo, coreClient,
		secret, `{"oidc_client_secret": "new-secret-value"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if !coreClient.invalidateAllCalled {
		t.Error("[AC#2] expected InvalidateAllAdminSessions to be called when oidc_client_secret changes")
	}
}

// TestPatchAdminConfig_OIDCClientSecret_NoEncryptionKey_Returns5xx covers a
// security invariant added during code review:
// When AdminServer.Secret is empty/nil, PATCHing oidc_client_secret must NOT
// silently fall back to plaintext storage. The handler must surface the
// misconfiguration so it cannot pass undetected.
func TestPatchAdminConfig_OIDCClientSecret_NoEncryptionKey_Returns5xx(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Test",
			OIDCIssuer:            "https://dex.example",
			OIDCClientID:          "test-client",
			AuditLogRetentionDays: 2555,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	// doPatchAdminConfig wires Secret=nil — exactly the misconfiguration case.
	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"oidc_client_secret": "new-secret-value"}`)

	if w.Code < 500 {
		t.Errorf("expected 5xx for oidc_client_secret PATCH with no encryption key, got %d; body: %s", w.Code, w.Body.String())
	}

	// The plaintext value must NOT have been stored.
	if storedValue, ok := configRepo.capturedValues["oidc_client_secret"]; ok {
		t.Errorf("SECURITY VIOLATION: oidc_client_secret was stored despite missing encryption key: %q", storedValue)
	}
}

// ── AC#2: PATCH /admin/config — non-OIDC field change does NOT invalidate ─────

// TestPatchAdminConfig_InstanceNameChange_NoSessionInvalidation covers AC#2 + AT#3 [P0]:
// PATCH with only instance_name changed must NOT call InvalidateAllAdminSessions.
//
// Failing reason: PatchAdminConfig handler does not exist yet.
func TestPatchAdminConfig_InstanceNameChange_NoSessionInvalidation(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Old Name",
			OIDCIssuer:            "https://dex.example",
			OIDCClientID:          "test-client",
			AuditLogRetentionDays: 2555,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"instance_name": "New Name"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2/AT#3] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if coreClient.invalidateAllCalled {
		t.Error("[AC#2/AT#3] InvalidateAllAdminSessions must NOT be called when only instance_name changes")
	}
}

// TestPatchAdminConfig_AuditLogRetentionChange_NoSessionInvalidation covers AC#2 [P1]:
// PATCH with only audit_log_retention_days changed must NOT call InvalidateAllAdminSessions.
//
// Failing reason: PatchAdminConfig handler does not exist yet.
func TestPatchAdminConfig_AuditLogRetentionChange_NoSessionInvalidation(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			AuditLogRetentionDays: 2555,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"audit_log_retention_days": 365}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200 for audit_log_retention_days change, got %d; body: %s", w.Code, w.Body.String())
	}

	if coreClient.invalidateAllCalled {
		t.Error("[AC#2] InvalidateAllAdminSessions must NOT be called when only audit_log_retention_days changes")
	}
}

// ── AC#2: PATCH /admin/config — audit_log_retention_days validation ───────────

// TestPatchAdminConfig_AuditLogRetentionDays_TooLow_Returns400 covers AC#2 [P0]:
// audit_log_retention_days must be in range 1–36500. Value 0 → 400 M_BAD_JSON.
//
// Failing reason: PatchAdminConfig handler does not exist yet.
func TestPatchAdminConfig_AuditLogRetentionDays_TooLow_Returns400(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{AuditLogRetentionDays: 2555},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"audit_log_retention_days": 0}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AC#2] expected status 400 for audit_log_retention_days=0, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_JSON" {
		t.Errorf("[AC#2] expected error code M_BAD_JSON, got %q", resp.Error.Code)
	}

	// Validation must happen before any repository upsert
	if configRepo.upsertCalled {
		t.Error("[AC#2] UpsertServerConfigKey must NOT be called when validation fails")
	}
}

// TestPatchAdminConfig_AuditLogRetentionDays_TooHigh_Returns400 covers AC#2 [P0]:
// audit_log_retention_days must be in range 1–36500. Value 36501 → 400 M_BAD_JSON.
//
// Failing reason: PatchAdminConfig handler does not exist yet.
func TestPatchAdminConfig_AuditLogRetentionDays_TooHigh_Returns400(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{AuditLogRetentionDays: 2555},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"audit_log_retention_days": 36501}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AC#2] expected status 400 for audit_log_retention_days=36501, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_JSON" {
		t.Errorf("[AC#2] expected error code M_BAD_JSON, got %q", resp.Error.Code)
	}
}

// TestPatchAdminConfig_AuditLogRetentionDays_MaxValid_Returns200 covers AC#2 [P1]:
// audit_log_retention_days=36500 is the maximum valid value → 200.
//
// Failing reason: PatchAdminConfig handler does not exist yet.
func TestPatchAdminConfig_AuditLogRetentionDays_MaxValid_Returns200(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{AuditLogRetentionDays: 36500},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"audit_log_retention_days": 36500}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200 for audit_log_retention_days=36500, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestPatchAdminConfig_AuditLogRetentionDays_MinValid_Returns200 covers AC#2 [P1]:
// audit_log_retention_days=1 is the minimum valid value → 200.
//
// Failing reason: PatchAdminConfig handler does not exist yet.
func TestPatchAdminConfig_AuditLogRetentionDays_MinValid_Returns200(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{AuditLogRetentionDays: 1},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"audit_log_retention_days": 1}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200 for audit_log_retention_days=1, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ── AC#5 Router test: PATCH /admin/config nil ServerConfig → 501 ─────────────

// TestPatchAdminConfig_NilServerConfigRepo_Returns501 covers AC#5 [P0]:
// When AdminServer.ServerConfig is nil, PATCH /api/v1/admin/config must return 501.
//
// Failing reason: PATCH /api/v1/admin/config route is not registered yet.
func TestPatchAdminConfig_NilServerConfigRepo_Returns501(t *testing.T) {
	mux := http.NewServeMux()
	// AdminServer with no ServerConfig — simulates pre-Story-6.10 configuration.
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddlewareForConfig, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/config",
		bytes.NewBufferString(`{"instance_name": "Test"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Fatalf("[AC#5] PATCH /api/v1/admin/config is not registered — got 404")
	}
	if w.Code != http.StatusNotImplemented {
		t.Errorf("[AC#5] expected 501 for nil ServerConfig on PATCH /admin/config, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestPatchAdminConfig_RouteRegistered covers AC#5 [P0]:
// PATCH /api/v1/admin/config must be registered — a 404 means the route is absent.
//
// Failing reason: PATCH route is not registered in router.go yet.
func TestPatchAdminConfig_RouteRegistered(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{AuditLogRetentionDays: 2555},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	mux := http.NewServeMux()
	srv := &api.AdminServer{
		ServerConfig: configRepo,
		RoomDefaults: roomDefaultsRepo,
	}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForConfig, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/config",
		bytes.NewBufferString(`{"instance_name": "Test"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("[AC#5] PATCH /api/v1/admin/config is not registered — got 404")
	}
}

// ── AC#2: PATCH returns full config object (same as GET) ─────────────────────

// TestPatchAdminConfig_Returns200WithFullConfigObject covers AC#2 [P0]:
// PATCH must return 200 with the same full config object as GET /admin/config.
// Specifically, oidc_client_secret must not be in the PATCH response either.
//
// Failing reason: PatchAdminConfig handler does not exist yet.
func TestPatchAdminConfig_Returns200WithFullConfigObject(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Updated Name",
			OIDCIssuer:            "https://dex.example",
			OIDCClientID:          "test-client",
			AuditLogRetentionDays: 2555,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{
		getMaxMembers: 50,
		getVisibility: "private",
	}
	coreClient := &mockCoreClientForConfig{}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"instance_name": "Updated Name"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// The response must contain all 6 readable config fields.
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}

	expectedFields := []string{
		"instance_name",
		"oidc_issuer",
		"oidc_client_id",
		"room_default_max_members",
		"room_default_visibility",
		"audit_log_retention_days",
	}
	for _, field := range expectedFields {
		if _, ok := resp[field]; !ok {
			t.Errorf("[AC#2] expected field %q in PATCH response, not found", field)
		}
	}

	// oidc_client_secret must NEVER appear in PATCH response either.
	rawBody := w.Body.String()
	if strings.Contains(rawBody, "oidc_client_secret") {
		t.Errorf("[AC#2] SECURITY VIOLATION: PATCH response contains 'oidc_client_secret':\n%s", rawBody)
	}
}

// ── MINOR-2: AES encryption of oidc_client_secret ────────────────────────────

// doPatchAdminConfigWithSecret sends PATCH /api/v1/admin/config with an explicit
// AES-256 secret wired into AdminServer, so encryption is performed.
func doPatchAdminConfigWithSecret(
	t *testing.T,
	configRepo api.ServerConfigRepository,
	roomDefaultsRepo api.RoomDefaultsRepository,
	coreClient pb.CoreServiceClient,
	secret []byte,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv := &api.AdminServer{
		ServerConfig: configRepo,
		RoomDefaults: roomDefaultsRepo,
		CoreClient:   coreClient,
		Secret:       secret,
	}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForConfig, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/config",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// TestPatchAdminConfig_OIDCClientSecret_IsEncryptedInStorage covers AC#2 [P1]:
// When oidc_client_secret is PATCHed, the value stored in the repository must be
// the AES-256-GCM ciphertext — not the plaintext secret.
//
// This guards against accidental plaintext storage of the OIDC client secret.
func TestPatchAdminConfig_OIDCClientSecret_IsEncryptedInStorage(t *testing.T) {
	const plaintextSecret = "mysupersecret"

	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Test",
			OIDCIssuer:            "https://dex.example",
			OIDCClientID:          "test-client",
			AuditLogRetentionDays: 2555,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	// 32-byte key for AES-256.
	secret := []byte("0123456789abcdef0123456789abcdef")

	w := doPatchAdminConfigWithSecret(t, configRepo, roomDefaultsRepo, coreClient,
		secret, `{"oidc_client_secret": "`+plaintextSecret+`"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2/MINOR-2] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// The value stored under "oidc_client_secret" must NOT be the plaintext.
	storedValue, ok := configRepo.capturedValues["oidc_client_secret"]
	if !ok || storedValue == "" {
		t.Fatalf("[AC#2/MINOR-2] expected oidc_client_secret to be stored in repository, but it was not")
	}
	if storedValue == plaintextSecret {
		t.Errorf("[AC#2/MINOR-2] SECURITY VIOLATION: oidc_client_secret stored as plaintext — must be AES-256-GCM encrypted")
	}
}

// ── HIGH fix (CWE-209): DB error must NOT leak into HTTP response body ─────────

// TestPatchAdminConfig_DBError_DoesNotLeakDBMessage covers the HIGH security fix:
// When UpsertServerConfigKey returns a DB error (e.g. "pq: duplicate key ..."),
// the response body must NOT contain any DB-specific error detail.
// The response body must contain only a sanitized "Internal server error" message.
//
// This guards against CWE-209 (Information Exposure Through an Error Message):
// the oapi-codegen default ResponseErrorHandlerFunc calls http.Error(w, err.Error(), 500)
// which would expose internal DB error strings verbatim to HTTP clients.
func TestPatchAdminConfig_DBError_DoesNotLeakDBMessage(t *testing.T) {
	const dbErrorMsg = "pq: duplicate key value violates unique constraint \"server_config_pkey\""

	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Test",
			OIDCIssuer:            "https://dex.example",
			OIDCClientID:          "test-client",
			AuditLogRetentionDays: 2555,
		},
		// Simulate a DB error on any upsert call.
		upsertErr: fmt.Errorf("%s", dbErrorMsg),
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"instance_name": "New Name"}`)

	// Must be a 5xx error.
	if w.Code < 500 {
		t.Fatalf("[HIGH/CWE-209] expected 5xx status when DB fails, got %d; body: %s", w.Code, w.Body.String())
	}

	rawBody := w.Body.String()

	// The DB error message must NOT appear in the response body.
	if strings.Contains(rawBody, "pq:") || strings.Contains(rawBody, "duplicate key") ||
		strings.Contains(rawBody, "server_config_pkey") {
		t.Errorf("[HIGH/CWE-209] SECURITY VIOLATION: DB error message leaked into HTTP response body:\n%s", rawBody)
	}

	// The response body must contain only a sanitized error message.
	var resp map[string]any
	if err := json.NewDecoder(strings.NewReader(rawBody)).Decode(&resp); err != nil {
		t.Fatalf("[HIGH/CWE-209] response is not valid JSON: %v; raw body: %s", err, rawBody)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("[HIGH/CWE-209] expected JSON error envelope with 'error' key, got: %s", rawBody)
	}
	if errObj["message"] == dbErrorMsg {
		t.Errorf("[HIGH/CWE-209] SECURITY VIOLATION: response 'message' is the raw DB error string: %q", errObj["message"])
	}
	// The sanitized message must be generic.
	if msg, ok := errObj["message"].(string); ok {
		if strings.Contains(msg, "pq:") || strings.Contains(msg, "duplicate") {
			t.Errorf("[HIGH/CWE-209] SECURITY VIOLATION: sanitized message still contains DB details: %q", msg)
		}
	}
}

// ── Story 14-1b: AT#1 — POST-bootstrap PATCH with matrix_user_id_claim → 400 ──

// TestPatchAdminConfig_MatrixUserIDClaim_PostBootstrap_Returns400 covers AT#1 (AC1) [P0]:
// When Core returns FAILED_PRECONDITION for UpdateServerConfig (post-bootstrap),
// PATCH /api/v1/admin/config with matrix_user_id_claim must return 400 M_FORBIDDEN.
// UpsertServerConfigKey must NOT be called.
//
// RED PHASE — fails until PatchAdminConfig handles matrix_user_id_claim via gRPC.
func TestPatchAdminConfig_MatrixUserIDClaim_PostBootstrap_Returns400(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Test",
			OIDCIssuer:            "https://dex.example",
			OIDCClientID:          "test-client",
			AuditLogRetentionDays: 2555,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{
		updateServerConfigFailPrecondition: true, // simulates post-bootstrap lock
	}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"matrix_user_id_claim": "email"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AT#1/AC1] expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AT#1/AC1] response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_FORBIDDEN" {
		t.Errorf("[AT#1/AC1] expected errcode='M_FORBIDDEN', got %v", resp["errcode"])
	}
	errMsg, _ := resp["error"].(string)
	if !strings.Contains(errMsg, "matrix_user_id_claim cannot be changed after bootstrap") {
		t.Errorf("[AT#1/AC1] expected error message to contain claim-lock text, got: %q", errMsg)
	}

	// UpsertServerConfigKey must NOT be called (only gRPC path is used for this field)
	if configRepo.upsertCalled {
		t.Error("[AT#1/AC1] UpsertServerConfigKey must NOT be called for matrix_user_id_claim")
	}
}

// ── Story 14-1b: AT#2 — PRE-bootstrap PATCH with matrix_user_id_claim → 200 ───

// TestPatchAdminConfig_MatrixUserIDClaim_PreBootstrap_Returns200 covers AT#2 (AC1/AC4) [P0]:
// When Core returns success for UpdateServerConfig (pre-bootstrap),
// PATCH /api/v1/admin/config with matrix_user_id_claim must return 200.
// UpdateServerConfig must be called with the correct claim value.
//
// RED PHASE — fails until PatchAdminConfig handles matrix_user_id_claim via gRPC.
func TestPatchAdminConfig_MatrixUserIDClaim_PreBootstrap_Returns200(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Test",
			OIDCIssuer:            "https://dex.example",
			OIDCClientID:          "test-client",
			AuditLogRetentionDays: 2555,
		},
	}
	roomDefaultsRepo := &mockRoomDefaultsForConfig{getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{
		updateServerConfigFailPrecondition: false, // simulates pre-bootstrap (success)
	}

	w := doPatchAdminConfig(t, configRepo, roomDefaultsRepo, coreClient,
		`{"matrix_user_id_claim": "preferred_username"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT#2/AC1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// UpdateServerConfig must have been called with the correct claim value.
	if !coreClient.updateServerConfigCalled {
		t.Error("[AT#2/AC1] expected UpdateServerConfig to be called for matrix_user_id_claim")
	}
	if coreClient.capturedMatrixUserIDClaim != "preferred_username" {
		t.Errorf("[AT#2/AC1] expected capturedMatrixUserIDClaim='preferred_username', got %q",
			coreClient.capturedMatrixUserIDClaim)
	}
}

// ── Story 14-2a: OIDC Directory Config — oidc_directory_enabled + oidc_directory_endpoint ──

// TestGetAdminConfig_IncludesOidcDirectoryFields — AT#1/AC2 [P0]
//
// RED PHASE: fails until:
//   - ServerConfigData gains OidcDirectoryEnabled bool + OidcDirectoryEndpoint string
//   - GetServerConfig DB query includes 'oidc_directory_enabled' and 'oidc_directory_endpoint'
//   - getAdminConfigOKResponse includes both new fields
//   - adminConfigResponseBody includes both new JSON keys
//
// Verifies that GET /api/v1/admin/config always returns both oidc_directory_enabled
// and oidc_directory_endpoint in the JSON response body (even with defaults).
func TestGetAdminConfig_IncludesOidcDirectoryFields(t *testing.T) {
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Nebu Dev",
			OIDCIssuer:            "https://dex.example.com",
			OIDCClientID:          "nebu-admin",
			AuditLogRetentionDays: 2555,
			OidcDirectoryEnabled:  false, // RED: field does not exist yet
			OidcDirectoryEndpoint: "",    // RED: field does not exist yet
		},
	}
	roomRepo := &mockRoomDefaultsForConfig{getMaxMembers: 100, getVisibility: "private"}
	w := doGetAdminConfig(t, configRepo, roomRepo, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT#1/AC2] expected 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("[AT#1/AC2] failed to unmarshal response: %v", err)
	}

	// Both fields must always be present (no omitempty — existing pattern)
	if _, ok := body["oidc_directory_enabled"]; !ok {
		t.Error("[AT#1/AC2] expected 'oidc_directory_enabled' key in response JSON")
	}
	if _, ok := body["oidc_directory_endpoint"]; !ok {
		t.Error("[AT#1/AC2] expected 'oidc_directory_endpoint' key in response JSON")
	}
	if body["oidc_directory_enabled"] != false {
		t.Errorf("[AT#1/AC2] expected oidc_directory_enabled=false, got %v", body["oidc_directory_enabled"])
	}
	if body["oidc_directory_endpoint"] != "" {
		t.Errorf("[AT#1/AC2] expected oidc_directory_endpoint='', got %v", body["oidc_directory_endpoint"])
	}
}

// TestPatchAdminConfig_OidcDirectory_PersistsAndReturns — AT#2/AC3 [P0]
//
// RED PHASE: fails until:
//   - PatchAdminConfigRequest gains OidcDirectoryEnabled *bool + OidcDirectoryEndpoint *string
//   - PatchAdminConfig handler upserts 'oidc_directory_enabled' and 'oidc_directory_endpoint'
//   - GetAdminConfig returns the updated values in the response
//
// Verifies that PATCH /api/v1/admin/config with oidc_directory_enabled + endpoint
// calls UpsertServerConfigKey for both keys and returns them in the 200 response.
func TestPatchAdminConfig_OidcDirectory_PersistsAndReturns(t *testing.T) {

	enabled := true
	endpoint := "https://idp.example.com/admin/users"

	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Nebu Dev",
			OIDCIssuer:            "https://dex.example.com",
			OIDCClientID:          "nebu-admin",
			AuditLogRetentionDays: 2555,
			OidcDirectoryEnabled:  enabled,  // RED: field does not exist yet
			OidcDirectoryEndpoint: endpoint, // RED: field does not exist yet
		},
	}
	roomRepo := &mockRoomDefaultsForConfig{getMaxMembers: 100, getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	body := fmt.Sprintf(`{"oidc_directory_enabled": %v, "oidc_directory_endpoint": %q}`, enabled, endpoint)
	w := doPatchAdminConfig(t, configRepo, roomRepo, coreClient, body)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT#2/AC3] expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// UpsertServerConfigKey must have been called for both keys
	if !configRepo.upsertCalled {
		t.Fatal("[AT#2/AC3] expected UpsertServerConfigKey to be called")
	}
	if configRepo.capturedValues["oidc_directory_enabled"] != "true" {
		t.Errorf("[AT#2/AC3] expected oidc_directory_enabled='true', got %q",
			configRepo.capturedValues["oidc_directory_enabled"])
	}
	if configRepo.capturedValues["oidc_directory_endpoint"] != endpoint {
		t.Errorf("[AT#2/AC3] expected oidc_directory_endpoint=%q, got %q",
			endpoint, configRepo.capturedValues["oidc_directory_endpoint"])
	}

	// Response JSON must include the updated values
	var respBody map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("[AT#2/AC3] failed to unmarshal response: %v", err)
	}
	if respBody["oidc_directory_enabled"] != true {
		t.Errorf("[AT#2/AC3] expected oidc_directory_enabled=true in response, got %v", respBody["oidc_directory_enabled"])
	}
	if respBody["oidc_directory_endpoint"] != endpoint {
		t.Errorf("[AT#2/AC3] expected oidc_directory_endpoint=%q in response, got %v", endpoint, respBody["oidc_directory_endpoint"])
	}
}

// TestPatchAdminConfig_OidcDirectoryEnabled_False_Persists — AT#2b/AC3 [P1]
//
// RED PHASE: fails until PatchAdminConfig stores "false" correctly for OidcDirectoryEnabled.
//
// Edge case: explicitly setting oidc_directory_enabled to false stores "false" (not empty string).
func TestPatchAdminConfig_OidcDirectoryEnabled_False_Persists(t *testing.T) {

	disabled := false
	configRepo := &mockServerConfigRepository{
		configData: &api.ServerConfigData{
			InstanceName:          "Nebu Dev",
			AuditLogRetentionDays: 2555,
			OidcDirectoryEnabled:  disabled, // RED: field does not exist yet
			OidcDirectoryEndpoint: "",
		},
	}
	roomRepo := &mockRoomDefaultsForConfig{getMaxMembers: 100, getVisibility: "private"}
	coreClient := &mockCoreClientForConfig{}

	body := `{"oidc_directory_enabled": false}`
	w := doPatchAdminConfig(t, configRepo, roomRepo, coreClient, body)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT#2b/AC3] expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if configRepo.capturedValues["oidc_directory_enabled"] != "false" {
		t.Errorf("[AT#2b/AC3] expected oidc_directory_enabled='false', got %q",
			configRepo.capturedValues["oidc_directory_enabled"])
	}
}
