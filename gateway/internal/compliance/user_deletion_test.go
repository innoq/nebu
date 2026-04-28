package compliance_test

// user_deletion_test.go — Story 5.7: DSGVO Key-Deletion Handler — RED-phase tests
//
// ALL tests in this file are expected to FAIL until Story 5.7 is implemented.
// Failing reason: UserKeyDeletionHandler type and DeleteUserKeys method do not exist.
//                 pb.DeleteUserKeysRequest / pb.DeleteUserKeysResponse do not exist.
//                 The DeleteUserKeys method is not part of pb.CoreServiceClient interface.
//
// Test strategy:
//   - userDeletionMockCoreClient embeds mockCoreClient (same package, handler_test.go)
//     and adds DeleteUserKeys stub controlled by fields.
//   - Context keys (ContextKeySub, ContextKeySystemRole) injected directly — no JWT middleware.
//   - httptest.NewRecorder captures status + body.
//   - No DB access from handler — handler only calls gRPC CoreClient.DeleteUserKeys.
//
// Tests (7 total for Story 5.7 Go layer):
//  1. TestDeleteUserKeys_HappyPath                  — AC1, AC6
//  2. TestDeleteUserKeys_MissingReason_Returns400   — AC1
//  3. TestDeleteUserKeys_ShortReason_Returns400     — AC1
//  4. TestDeleteUserKeys_NonAdmin_Returns403        — AC1
//  5. TestDeleteUserKeys_UnknownUser_Returns404     — AC1, gRPC NOT_FOUND mapping
//  6. TestDeleteUserKeys_ConcurrentDeletion_Returns409 — AC7, gRPC ALREADY_EXISTS mapping
//  7. TestDeleteUserKeys_AuditFailure_Still200      — AC1 never-raise audit policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/compliance"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ─── userDeletionMockCoreClient ───────────────────────────────────────────────
//
// Extends mockCoreClient (declared in handler_test.go, same package) with
// DeleteUserKeys stub. Controlled by:
//   - deleteUserKeysResp: returned on success (non-nil means success path)
//   - deleteUserKeysErr: returned instead of resp when non-nil
//
// WriteAuditLog is already implemented by mockCoreClient (embedded).

type userDeletionMockCoreClient struct {
	mockCoreClient
	deleteUserKeysResp *pb.DeleteUserKeysResponse
	deleteUserKeysErr  error
}

func (m *userDeletionMockCoreClient) DeleteUserKeys(
	_ context.Context,
	_ *pb.DeleteUserKeysRequest,
	_ ...grpc.CallOption,
) (*pb.DeleteUserKeysResponse, error) {
	if m.deleteUserKeysErr != nil {
		return nil, m.deleteUserKeysErr
	}
	return m.deleteUserKeysResp, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

type userKeyDeletionBody struct {
	Reason string `json:"reason"`
}

func validDeletionBody() userKeyDeletionBody {
	return userKeyDeletionBody{
		Reason: "DSGVO deletion request ref GDPR-2026-042",
	}
}

func newDeletionRequest(
	t *testing.T,
	userID string,
	body any,
	systemRole, sub, contentType string,
) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("json.Encode body: %v", err)
	}
	r := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/admin/users/"+userID+"/keys",
		&buf,
	)
	r.Header.Set("Content-Type", contentType)
	// Inject path value as if mux extracted it
	r.SetPathValue("userId", userID)

	ctx := r.Context()
	ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, systemRole)
	ctx = context.WithValue(ctx, middleware.ContextKeySub, sub)
	return r.WithContext(ctx)
}

func newJSONDeletionRequest(t *testing.T, userID string, body any, systemRole, sub string) *http.Request {
	return newDeletionRequest(t, userID, body, systemRole, sub, "application/json")
}

// ─── Test 1: HappyPath → 200 + body ──────────────────────────────────────────
//
// AC1, AC6
//
// Given: instance_admin caller, valid userId, reason ≥ 10 chars,
//        mock CoreClient returns keys_deleted + timestamp
// When:  DELETE /api/v1/admin/users/{userId}/keys with valid body
// Then:  200, body {"user_id":..., "status":"keys_deleted", "keys_deleted_at":<ISO8601>}

func TestDeleteUserKeys_HappyPath(t *testing.T) {
	keysDeletedAtMs := time.Now().UTC().UnixMilli()

	client := &userDeletionMockCoreClient{
		deleteUserKeysResp: &pb.DeleteUserKeysResponse{
			Status:        "keys_deleted",
			KeysDeletedAt: keysDeletedAtMs,
		},
	}

	h := &compliance.UserKeyDeletionHandler{CoreClient: client}

	w := httptest.NewRecorder()
	r := newJSONDeletionRequest(t, "user-42", validDeletionBody(), "instance_admin", "admin-sub-1")

	h.DeleteUserKeys(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v — body: %s", err, w.Body.String())
	}
	if resp["user_id"] != "user-42" {
		t.Errorf("expected user_id='user-42', got %q", resp["user_id"])
	}
	if resp["status"] != "keys_deleted" {
		t.Errorf("expected status='keys_deleted', got %q", resp["status"])
	}
	if resp["keys_deleted_at"] == "" {
		t.Error("expected non-empty keys_deleted_at in response")
	}
	// keys_deleted_at must be parseable as RFC 3339 / ISO 8601
	if _, err := time.Parse(time.RFC3339, resp["keys_deleted_at"]); err != nil {
		t.Errorf("keys_deleted_at %q is not a valid RFC 3339 timestamp: %v", resp["keys_deleted_at"], err)
	}
}

// ─── Test 2: MissingReason → 400 ─────────────────────────────────────────────
//
// AC1: Body (required) — 400 M_BAD_JSON when reason field is missing
//
// Given: instance_admin caller, body `{}`
// When:  DELETE request
// Then:  400 M_BAD_JSON "reason is required"

func TestDeleteUserKeys_MissingReason_Returns400(t *testing.T) {
	client := &userDeletionMockCoreClient{}
	h := &compliance.UserKeyDeletionHandler{CoreClient: client}

	body := map[string]string{} // no reason field

	w := httptest.NewRecorder()
	r := newJSONDeletionRequest(t, "user-42", body, "instance_admin", "admin-sub-1")

	h.DeleteUserKeys(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "reason") {
		t.Errorf("expected error message to reference 'reason', got %q", resp["error"])
	}
}

// ─── Test 3: ShortReason → 400 ───────────────────────────────────────────────
//
// AC1: reason < 10 characters → 400 M_BAD_JSON
//
// Given: instance_admin caller, body {"reason": "too short"} (9 chars)
// When:  DELETE request
// Then:  400 M_BAD_JSON "reason must be at least 10 characters"

func TestDeleteUserKeys_ShortReason_Returns400(t *testing.T) {
	client := &userDeletionMockCoreClient{}
	h := &compliance.UserKeyDeletionHandler{CoreClient: client}

	body := userKeyDeletionBody{Reason: "too short"} // exactly 9 chars

	w := httptest.NewRecorder()
	r := newJSONDeletionRequest(t, "user-42", body, "instance_admin", "admin-sub-1")

	h.DeleteUserKeys(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "10 characters") {
		t.Errorf("expected error to mention '10 characters', got %q", resp["error"])
	}
}

// ─── Test 4: NonAdmin → 403 ───────────────────────────────────────────────────
//
// AC1: Role gate — only instance_admin; others → 403 M_FORBIDDEN
//
// Given: Caller with system_role = "compliance_officer"
// When:  DELETE request with valid body
// Then:  403 M_FORBIDDEN

func TestDeleteUserKeys_NonAdmin_Returns403(t *testing.T) {
	client := &userDeletionMockCoreClient{}
	h := &compliance.UserKeyDeletionHandler{CoreClient: client}

	w := httptest.NewRecorder()
	r := newJSONDeletionRequest(t, "user-42", validDeletionBody(), "compliance_officer", "officer-sub-1")

	h.DeleteUserKeys(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode=M_FORBIDDEN, got %q", resp["errcode"])
	}
}

// ─── Test 5: UnknownUser → 404 ────────────────────────────────────────────────
//
// AC1: gRPC NOT_FOUND → 404 M_NOT_FOUND "User not found"
//
// Given: instance_admin caller, valid body, mock CoreClient returns codes.NotFound
// When:  DELETE request
// Then:  404 M_NOT_FOUND "User not found"

func TestDeleteUserKeys_UnknownUser_Returns404(t *testing.T) {
	client := &userDeletionMockCoreClient{
		deleteUserKeysErr: status.Error(codes.NotFound, "user not found"),
	}
	h := &compliance.UserKeyDeletionHandler{CoreClient: client}

	w := httptest.NewRecorder()
	r := newJSONDeletionRequest(t, "unknown-user-99", validDeletionBody(), "instance_admin", "admin-sub-1")

	h.DeleteUserKeys(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_NOT_FOUND" {
		t.Errorf("expected errcode=M_NOT_FOUND, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "not found") {
		t.Errorf("expected error to mention 'not found', got %q", resp["error"])
	}
}

// ─── Test 6: ConcurrentDeletion → 409 ────────────────────────────────────────
//
// AC7: gRPC ALREADY_EXISTS → 409 M_CONFLICT
//
// Given: instance_admin caller, valid body, mock CoreClient returns codes.AlreadyExists
// When:  DELETE request
// Then:  409 M_CONFLICT "User deletion already in progress or completed"

func TestDeleteUserKeys_ConcurrentDeletion_Returns409(t *testing.T) {
	client := &userDeletionMockCoreClient{
		deleteUserKeysErr: status.Error(codes.AlreadyExists, "conflict"),
	}
	h := &compliance.UserKeyDeletionHandler{CoreClient: client}

	w := httptest.NewRecorder()
	r := newJSONDeletionRequest(t, "user-42", validDeletionBody(), "instance_admin", "admin-sub-1")

	h.DeleteUserKeys(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_CONFLICT" {
		t.Errorf("expected errcode=M_CONFLICT, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "in progress or completed") {
		t.Errorf("expected error to mention 'in progress or completed', got %q", resp["error"])
	}
}

// ─── Test 6b: OversizedUserID → 400 ──────────────────────────────────────────
//
// AC1: defence-in-depth — userId path param > 255 chars → 400 M_BAD_JSON
//
// Given: instance_admin caller, valid body, userId is 256+ chars
// When:  DELETE request
// Then:  400 M_BAD_JSON "userId must not exceed 255 characters"

func TestDeleteUserKeys_OversizedUserID_Returns400(t *testing.T) {
	client := &userDeletionMockCoreClient{}
	h := &compliance.UserKeyDeletionHandler{CoreClient: client}

	oversized := strings.Repeat("a", 256) // 256 chars — one over the cap

	w := httptest.NewRecorder()
	r := newJSONDeletionRequest(t, oversized, validDeletionBody(), "instance_admin", "admin-sub-1")

	h.DeleteUserKeys(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for oversized userId, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "255") {
		t.Errorf("expected error to mention '255', got %q", resp["error"])
	}
}

// ─── Test 6c: OversizedReason → 400 ──────────────────────────────────────────
//
// AC1: reason has both min (10) AND max (1000) length validation.
// Body-limit (64 KiB) catches grossly oversized requests, but a dedicated
// max prevents memory amplification on the audit/metadata path.
//
// Given: instance_admin caller, reason 1001+ chars
// When:  DELETE request
// Then:  400 M_BAD_JSON "reason must not exceed 1000 characters"

func TestDeleteUserKeys_OversizedReason_Returns400(t *testing.T) {
	client := &userDeletionMockCoreClient{}
	h := &compliance.UserKeyDeletionHandler{CoreClient: client}

	body := userKeyDeletionBody{Reason: strings.Repeat("x", 1001)} // 1001 chars — one over the cap

	w := httptest.NewRecorder()
	r := newJSONDeletionRequest(t, "user-42", body, "instance_admin", "admin-sub-1")

	h.DeleteUserKeys(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for oversized reason, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "1000") {
		t.Errorf("expected error to mention '1000', got %q", resp["error"])
	}
}

// ─── Test 7: AuditFailure → Still 200 ────────────────────────────────────────
//
// AC1: never-raise audit policy — audit failure must NOT block the 200 response
//
// Given: happy-path conditions, but WriteAuditLog returns an error
// When:  DELETE request succeeds (gRPC DeleteUserKeys returns keys_deleted)
// Then:  200 OK with valid body (audit failure silently swallowed)

func TestDeleteUserKeys_AuditFailure_Still200(t *testing.T) {
	keysDeletedAtMs := time.Now().UTC().UnixMilli()

	client := &userDeletionMockCoreClient{
		deleteUserKeysResp: &pb.DeleteUserKeysResponse{
			Status:        "keys_deleted",
			KeysDeletedAt: keysDeletedAtMs,
		},
	}
	// Make WriteAuditLog fail (inherited from mockCoreClient)
	client.mockCoreClient.failWith = fmt.Errorf("simulated audit gRPC failure")

	h := &compliance.UserKeyDeletionHandler{CoreClient: client}

	w := httptest.NewRecorder()
	r := newJSONDeletionRequest(t, "user-audit-fail", validDeletionBody(), "instance_admin", "admin-sub-audit")

	h.DeleteUserKeys(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("audit failure must not block 200 — got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v — body: %s", err, w.Body.String())
	}
	if resp["status"] != "keys_deleted" {
		t.Errorf("expected status='keys_deleted', got %q", resp["status"])
	}
}
