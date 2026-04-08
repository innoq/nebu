package matrix

// ─── Story 4-17: POST /_matrix/client/v3/rooms/{roomId}/receipt/{receiptType}/{eventId} ─
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until Story 4-17 is implemented.
//
// Test strategy:
//   - mockReceiptCoreClient implements ReceiptsCoreClient (consumer-defined interface,
//     Go convention) — defined here alongside the tests.
//   - buildAuthedReceiptsHandler wires JWTMiddleware → ReceiptsHandler in a mux
//     registered with the correct POST pattern so r.PathValue("roomId"),
//     r.PathValue("receiptType"), and r.PathValue("eventId") resolve correctly
//     (Go 1.22+ standard library routing).
//   - A capturedReq field lets tests inspect the gRPC SendReceiptRequest forwarded
//     by the handler (room_id, user_id, receipt_type, event_id).
//   - The invalid-receiptType test uses "m.fully_read" (not "m.read"); the handler
//     must return 400 M_INVALID_PARAM BEFORE calling Core.
//   - The unauthenticated test deliberately omits the Authorization header;
//     JWTMiddleware must return 401 before the handler is reached.
//   - The room-not-found test uses gRPC status NotFound; the handler maps to 404.
//   - The non-member test uses gRPC status PermissionDenied; the handler maps to 403.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ─── Mock gRPC core client ────────────────────────────────────────────────────

// mockReceiptCoreClient implements ReceiptsCoreClient (defined in receipts.go).
// capturedReq records the last SendReceiptRequest forwarded so tests can assert
// the handler built the correct gRPC payload.

type mockReceiptCoreClient struct {
	resp        *pb.SendReceiptResponse
	err         error
	capturedReq *pb.SendReceiptRequest
}

func (m *mockReceiptCoreClient) SendReceipt(_ context.Context, req *pb.SendReceiptRequest) (*pb.SendReceiptResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildAuthedReceiptsHandler wires JWTMiddleware → ReceiptsHandler and registers
// it on a mux with the correct POST pattern so r.PathValue works correctly.
//
// Returns the http.Handler ready for httptest, the OIDC test server, and a
// makeToken closure that mints a valid signed JWT each time it is called.
//
// The JWT sub is always "test-sub-123", so the authenticated user_id is
// "@test-sub-123:test.local".
func buildAuthedReceiptsHandler(t *testing.T, mock *mockReceiptCoreClient) (http.Handler, *httptest.Server, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewReceiptsHandler(ReceiptsConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil)(
		http.HandlerFunc(handler.PostReceipt),
	)

	// Wrap in a mux with named path parameters so PathValue resolves.
	mux := http.NewServeMux()
	mux.Handle("POST /rooms/{roomId}/receipt/{receiptType}/{eventId}", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, oidcSrv, makeToken
}

// ─── Test 1: Happy path — m.read receipt ─────────────────────────────────────
//
// AC #2 — POST to /{roomId}/receipt/m.read/{eventId} with valid JWT and mock
// returning success → 200 {}.
// The mock receives SendReceiptRequest{room_id, user_id, receipt_type: "m.read", event_id}.

func TestPostReceipt_HappyPath(t *testing.T) {
	mock := &mockReceiptCoreClient{
		resp: &pb.SendReceiptResponse{},
	}

	mux, _, makeToken := buildAuthedReceiptsHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodPost,
		"/rooms/!room1:test.local/receipt/m.read/$eventId123",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty JSON object {}, got %v", resp)
	}

	// Assert the gRPC request was built correctly.
	if mock.capturedReq == nil {
		t.Fatal("expected SendReceipt to be called, but capturedReq is nil")
	}
	if mock.capturedReq.RoomId != "!room1:test.local" {
		t.Errorf("expected room_id %q, got %q", "!room1:test.local", mock.capturedReq.RoomId)
	}
	if mock.capturedReq.UserId != "@test-sub-123:test.local" {
		t.Errorf("expected user_id %q, got %q", "@test-sub-123:test.local", mock.capturedReq.UserId)
	}
	if mock.capturedReq.ReceiptType != "m.read" {
		t.Errorf("expected receipt_type %q, got %q", "m.read", mock.capturedReq.ReceiptType)
	}
	if mock.capturedReq.EventId != "$eventId123" {
		t.Errorf("expected event_id %q, got %q", "$eventId123", mock.capturedReq.EventId)
	}
}

// ─── Test 2: Happy path — m.read.private receipt ─────────────────────────────
//
// MAJOR-1 — POST to /{roomId}/receipt/m.read.private/{eventId} with valid JWT
// and mock returning success → 200 {}.
// Assert mock.capturedReq.ReceiptType == "m.read.private" (forwarded to Core).

func TestPostReceipt_ReadPrivate_HappyPath(t *testing.T) {
	mock := &mockReceiptCoreClient{
		resp: &pb.SendReceiptResponse{},
	}

	mux, _, makeToken := buildAuthedReceiptsHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodPost,
		"/rooms/!room1:test.local/receipt/m.read.private/$eventId456",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty JSON object {}, got %v", resp)
	}

	// Assert the gRPC request was forwarded with the correct receipt_type.
	if mock.capturedReq == nil {
		t.Fatal("expected SendReceipt to be called, but capturedReq is nil")
	}
	if mock.capturedReq.ReceiptType != "m.read.private" {
		t.Errorf("expected receipt_type %q, got %q", "m.read.private", mock.capturedReq.ReceiptType)
	}
	if mock.capturedReq.RoomId != "!room1:test.local" {
		t.Errorf("expected room_id %q, got %q", "!room1:test.local", mock.capturedReq.RoomId)
	}
	if mock.capturedReq.UserId != "@test-sub-123:test.local" {
		t.Errorf("expected user_id %q, got %q", "@test-sub-123:test.local", mock.capturedReq.UserId)
	}
	if mock.capturedReq.EventId != "$eventId456" {
		t.Errorf("expected event_id %q, got %q", "$eventId456", mock.capturedReq.EventId)
	}
}

// ─── Test 3 (was 2): Unauthenticated request → 401 ───────────────────────────
//
// AC #2 — JWTMiddleware must reject requests missing Authorization header.
// Core must NOT be called.

func TestPostReceipt_Unauthenticated(t *testing.T) {
	mock := &mockReceiptCoreClient{
		resp: &pb.SendReceiptResponse{},
	}

	mux, _, _ := buildAuthedReceiptsHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodPost,
		"/rooms/!room1:test.local/receipt/m.read/$eventId123",
		nil,
	)
	// Deliberately omit Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	// Core must not have been called.
	if mock.capturedReq != nil {
		t.Error("expected SendReceipt NOT to be called on unauthenticated request, but capturedReq is set")
	}
}

// ─── Test 3: Room not found → 404 ────────────────────────────────────────────
//
// AC #2 — when gRPC Core returns codes.NotFound, the handler must return 404
// with errcode M_NOT_FOUND.

func TestPostReceipt_RoomNotFound(t *testing.T) {
	notFoundErr := status.Error(codes.NotFound, "room not found")
	mock := &mockReceiptCoreClient{
		err: notFoundErr,
	}

	mux, _, makeToken := buildAuthedReceiptsHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodPost,
		"/rooms/!nonexistent:test.local/receipt/m.read/$eventId123",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %s", errResp.ErrCode)
	}
}

// ─── Test 4: Invalid receiptType → 400 ───────────────────────────────────────
//
// AC #2 — only "m.read" is a supported receiptType; any other value must return
// 400 M_INVALID_PARAM BEFORE calling Core.
//
// This test uses "m.fully_read" as the unsupported type (a real Matrix type that
// Nebu MVP intentionally does not support).

func TestPostReceipt_InvalidReceiptType(t *testing.T) {
	mock := &mockReceiptCoreClient{
		resp: &pb.SendReceiptResponse{},
	}

	mux, _, makeToken := buildAuthedReceiptsHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodPost,
		"/rooms/!room1:test.local/receipt/m.fully_read/$eventId123",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %s", errResp.ErrCode)
	}

	// Core must NOT have been called (validation happens before gRPC call).
	if mock.capturedReq != nil {
		t.Error("expected SendReceipt NOT to be called for invalid receiptType, but capturedReq is set")
	}
}

// ─── Test 5: Not a room member → 403 ─────────────────────────────────────────
//
// AC #2 — when gRPC Core returns codes.PermissionDenied (user is not a member),
// the handler must return 403 M_FORBIDDEN.

func TestPostReceipt_NotMember(t *testing.T) {
	permErr := status.Error(codes.PermissionDenied, "not a room member")
	mock := &mockReceiptCoreClient{
		err: permErr,
	}

	mux, _, makeToken := buildAuthedReceiptsHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodPost,
		"/rooms/!room1:test.local/receipt/m.read/$eventId123",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %s", errResp.ErrCode)
	}
}
