package matrix_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/matrix"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc"
)

// mockLogoutCoreClient implements matrix.LogoutCoreClient for testing.
// It records the most recent InvalidateUserSessions call and can be configured
// to return an error.
type mockLogoutCoreClient struct {
	capturedUserID   string
	capturedDeviceID string
	returnErr        error
	callCount        int
}

func (m *mockLogoutCoreClient) InvalidateUserSessions(
	_ context.Context,
	req *pb.InvalidateUserSessionsRequest,
	_ ...grpc.CallOption,
) (*pb.InvalidateUserSessionsResponse, error) {
	m.callCount++
	m.capturedUserID = req.UserId
	m.capturedDeviceID = req.DeviceId
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return &pb.InvalidateUserSessionsResponse{Ok: true}, nil
}

func TestPostLogout_ValidToken(t *testing.T) {
	store := middleware.NewDenylist()
	handler := matrix.NewLogoutHandler(store)

	rawToken := "test-token-12345"
	expiry := time.Now().Add(time.Hour)

	req := httptest.NewRequest("POST", "/_matrix/client/v3/logout", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyTokenExpiry, expiry)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.PostLogout(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !store.IsInvalidated(rawToken) {
		t.Error("expected token to be invalidated after logout")
	}
}

func TestPostLogout_AddsCorrectExpiry(t *testing.T) {
	store := middleware.NewDenylist()
	handler := matrix.NewLogoutHandler(store)

	rawToken := "expiry-test-token"
	// Use a past expiry — the handler should still call Invalidate(), but IsInvalidated()
	// returns false because the entry is already expired. This proves the handler
	// passes the context expiry through (not a hardcoded future value).
	expiry := time.Now().Add(-time.Second)

	req := httptest.NewRequest("POST", "/_matrix/client/v3/logout", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyTokenExpiry, expiry)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.PostLogout(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if store.IsInvalidated(rawToken) {
		t.Error("expected expired entry to not be reported as invalidated")
	}
}

func TestPostLogout_EmptyBody(t *testing.T) {
	store := middleware.NewDenylist()
	handler := matrix.NewLogoutHandler(store)

	req := httptest.NewRequest("POST", "/_matrix/client/v3/logout", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	ctx := context.WithValue(req.Context(), middleware.ContextKeyTokenExpiry, time.Now().Add(time.Hour))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.PostLogout(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if body != "{}\n" {
		t.Errorf("expected body to be {}\\n, got %q", body)
	}
}

// ─── MAJOR-1 (Story 9-22): Tests for NewLogoutHandlerWithCore ────────────────
//
// These tests verify that when a LogoutHandler is created with a Core gRPC client:
//   1. InvalidateUserSessions is called with the correct user_id and device_id.
//   2. A Core gRPC failure does not cause a 500 — the handler still returns 200
//      (JWT denylist invalidation already happened; Core cleanup is best-effort).

// TestPostLogout_WithCore_CallsInvalidateUserSessions verifies that PostLogout
// calls InvalidateUserSessions with the correct user_id and device_id from
// the request context (set by JWTMiddleware from the JWT "sub" and "did" claims).
func TestPostLogout_WithCore_CallsInvalidateUserSessions(t *testing.T) {
	store := middleware.NewDenylist()
	mockCore := &mockLogoutCoreClient{}

	handler := matrix.NewLogoutHandlerWithCore(matrix.LogoutConfig{
		Store:      store,
		CoreClient: mockCore,
	})

	rawToken := "logout-core-test-token"
	expiry := time.Now().Add(time.Hour)

	req := httptest.NewRequest("POST", "/_matrix/client/v3/logout", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	ctx := req.Context()
	ctx = context.WithValue(ctx, middleware.ContextKeyTokenExpiry, expiry)
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@alice:test.local")
	ctx = context.WithValue(ctx, middleware.ContextKeyDeviceID, "DEVICE_D1")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.PostLogout(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	// Token must be invalidated in the local denylist
	if !store.IsInvalidated(rawToken) {
		t.Error("expected token to be invalidated in denylist after logout")
	}

	// InvalidateUserSessions must have been called exactly once
	if mockCore.callCount != 1 {
		t.Errorf("expected InvalidateUserSessions called once, got %d", mockCore.callCount)
	}

	// Must forward the correct user_id
	if mockCore.capturedUserID != "@alice:test.local" {
		t.Errorf("expected user_id=@alice:test.local, got %q", mockCore.capturedUserID)
	}

	// Must forward the correct device_id
	if mockCore.capturedDeviceID != "DEVICE_D1" {
		t.Errorf("expected device_id=DEVICE_D1, got %q", mockCore.capturedDeviceID)
	}
}

// TestPostLogout_WithCore_CoreErrorDoesNotReturn500 verifies that if the Core
// gRPC call fails, PostLogout still returns 200 (the JWT is already invalidated;
// Core sync-token cleanup is best-effort / non-fatal).
func TestPostLogout_WithCore_CoreErrorDoesNotReturn500(t *testing.T) {
	store := middleware.NewDenylist()
	mockCore := &mockLogoutCoreClient{
		returnErr: errors.New("grpc: core unavailable"),
	}

	handler := matrix.NewLogoutHandlerWithCore(matrix.LogoutConfig{
		Store:      store,
		CoreClient: mockCore,
	})

	rawToken := "logout-core-error-token"
	expiry := time.Now().Add(time.Hour)

	req := httptest.NewRequest("POST", "/_matrix/client/v3/logout", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	ctx := req.Context()
	ctx = context.WithValue(ctx, middleware.ContextKeyTokenExpiry, expiry)
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@bob:test.local")
	ctx = context.WithValue(ctx, middleware.ContextKeyDeviceID, "DEVICE_BOB")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.PostLogout(rr, req)

	// Must still return 200 even when Core gRPC fails
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 even on Core gRPC error, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// Token must still be invalidated in the local denylist
	if !store.IsInvalidated(rawToken) {
		t.Error("expected token to be invalidated in denylist even when Core gRPC fails")
	}
}
