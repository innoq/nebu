//go:build go1.22

// Package api contains the Admin API handler implementations.
// Stub operations return 501 Not Implemented; Story 6.4 wires ListAdminUsers + GetAdminUser.
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nebu/nebu/internal/audit"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
)

// AdminServer implements StrictServerInterface.
//
// DB and CoreClient are populated in main.go (Option A from Dev Notes: fields on AdminServer).
// Users is the UserRepository for Admin user queries (Story 6.4).
// Deactivation is the DeactivationRepository for user deactivation/reactivation (Story 6.5).
//
// TODO(Story 6.6): Users will also need to merge role_overrides table once it exists.
type AdminServer struct {
	DB           *sql.DB
	CoreClient   pb.CoreServiceClient
	Users        UserRepository
	Deactivation DeactivationRepository // Story 6.5
}

// Ensure AdminServer satisfies the generated StrictServerInterface at compile time.
var _ StrictServerInterface = (*AdminServer)(nil)

func (s *AdminServer) GetAdminConfig(_ context.Context, _ GetAdminConfigRequestObject) (GetAdminConfigResponseObject, error) {
	return GetAdminConfig501Response{}, nil
}

func (s *AdminServer) GetAdminMetrics(_ context.Context, _ GetAdminMetricsRequestObject) (GetAdminMetricsResponseObject, error) {
	return GetAdminMetrics501Response{}, nil
}

func (s *AdminServer) ListAdminRooms(_ context.Context, _ ListAdminRoomsRequestObject) (ListAdminRoomsResponseObject, error) {
	return ListAdminRooms501Response{}, nil
}

// ListAdminUsers implements AC#1: GET /api/v1/admin/users
// Query params: cursor (optional), limit (1–100, default 20), search (optional).
// Emits an audit log event on success (never-raise: audit failure does not block the response).
// Returns 501 if no UserRepository is wired (pre-Story-6.4 stub behaviour).
func (s *AdminServer) ListAdminUsers(ctx context.Context, request ListAdminUsersRequestObject) (ListAdminUsersResponseObject, error) {
	// Guard: if no UserRepository is wired, fall back to 501 stub.
	// This keeps the router_test.go Story 6.3 tests passing with an empty AdminServer{}.
	if s.Users == nil {
		return ListAdminUsers501Response{}, nil
	}

	// ── Parse limit ──────────────────────────────────────────────────────────
	limit := 20
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if limit < 1 || limit > 100 {
		return &listUsers400Resp{msg: "limit must be between 1 and 100"}, nil
	}

	// ── Parse cursor ─────────────────────────────────────────────────────────
	var afterID, afterCreatedAt string
	if request.Params.Cursor != nil && *request.Params.Cursor != "" {
		var err error
		afterID, afterCreatedAt, err = DecodeCursor(*request.Params.Cursor)
		if err != nil {
			return &listUsers400Resp{msg: "Invalid cursor"}, nil
		}
	}

	// ── Parse search ─────────────────────────────────────────────────────────
	search := ""
	if request.Params.Search != nil {
		search = *request.Params.Search
	}

	// ── Repository call ───────────────────────────────────────────────────────
	users, total, nextCursor, err := s.Users.ListUsers(ctx, afterID, afterCreatedAt, limit, search)
	if err != nil {
		return nil, err
	}

	// ── Audit log (never-raise) ───────────────────────────────────────────────
	// The JWT middleware populates ContextKeyUserID in the context passed via
	// the StrictHandler chain — see middleware/auth.go.
	if s.CoreClient != nil {
		actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
		_ = audit.LogEvent(ctx, s.CoreClient, actorID, "admin_user_viewed", "user", "", nil, "success", "")
	}

	return &listAdminUsersOKResponse{
		users:      users,
		total:      total,
		nextCursor: nextCursor,
	}, nil
}

// GetAdminUser implements AC#2: GET /api/v1/admin/users/{userId}
// Returns a single AdminUserDetail or 404 if not found.
// Emits an audit log event on success.
// Returns 404 if no UserRepository is wired (safety fallback).
func (s *AdminServer) GetAdminUser(ctx context.Context, request GetAdminUserRequestObject) (GetAdminUserResponseObject, error) {
	// Guard: if no UserRepository is wired, return 404 (no users in this configuration).
	if s.Users == nil {
		return &getAdminUser404Response{}, nil
	}

	userID := request.UserId

	detail, err := s.Users.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return &getAdminUser404Response{}, nil
	}

	// ── Audit log (never-raise) ───────────────────────────────────────────────
	if s.CoreClient != nil {
		actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
		_ = audit.LogEvent(ctx, s.CoreClient, actorID, "admin_user_viewed", "user", userID, nil, "success", "")
	}

	return &getAdminUserOKResponse{detail: detail}, nil
}

func (s *AdminServer) ListComplianceAccessRequests(_ context.Context, _ ListComplianceAccessRequestsRequestObject) (ListComplianceAccessRequestsResponseObject, error) {
	return ListComplianceAccessRequests501Response{}, nil
}

func (s *AdminServer) GetHealth(_ context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	return GetHealth200JSONResponse{Status: "ok"}, nil
}

// ── Response types ────────────────────────────────────────────────────────────

// listAdminUsersOKResponse serialises AdminUser list + pagination metadata as JSON.
// Implements ListAdminUsersResponseObject.
type listAdminUsersOKResponse struct {
	users      []AdminUser
	total      int
	nextCursor string
}

func (resp *listAdminUsersOKResponse) VisitListAdminUsersResponse(w http.ResponseWriter) error {
	type meta struct {
		Total      int    `json:"total"`
		NextCursor string `json:"next_cursor,omitempty"`
	}
	type envelope struct {
		Data []AdminUser `json:"data"`
		Meta meta        `json:"meta"`
	}
	data := resp.users
	if data == nil {
		data = []AdminUser{}
	}
	body := envelope{
		Data: data,
		Meta: meta{Total: resp.total, NextCursor: resp.nextCursor},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(body)
}

// listUsers400Resp writes a 400 M_BAD_REQUEST error.
// Implements ListAdminUsersResponseObject.
type listUsers400Resp struct{ msg string }

func (r *listUsers400Resp) VisitListAdminUsersResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_BAD_REQUEST", "message": r.msg},
	})
}

// getAdminUserOKResponse serialises AdminUserDetail as JSON.
// Implements GetAdminUserResponseObject.
type getAdminUserOKResponse struct{ detail *AdminUserDetail }

func (resp *getAdminUserOKResponse) VisitGetAdminUserResponse(w http.ResponseWriter) error {
	type envelope struct {
		Data *AdminUserDetail `json:"data"`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(envelope{Data: resp.detail})
}

// getAdminUser404Response writes a 404 M_NOT_FOUND error.
// Implements GetAdminUserResponseObject.
type getAdminUser404Response struct{}

func (r *getAdminUser404Response) VisitGetAdminUserResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_NOT_FOUND", "message": "User not found"},
	})
}

// ── Story 6.5: Deactivate / Reactivate handlers ───────────────────────────────

// DeactivateAdminUser implements AC#1: POST /api/v1/admin/users/{userId}/deactivate.
// Validates the reason, checks user state, calls DeactivateUser, invalidates sessions,
// and emits an audit log. Returns 501 if Deactivation repository is not wired.
func (s *AdminServer) DeactivateAdminUser(ctx context.Context, req DeactivateAdminUserRequestObject) (DeactivateAdminUserResponseObject, error) {
	if s.Deactivation == nil {
		return DeactivateAdminUser501Response{}, nil
	}

	userID := req.UserId

	// 1. Parse + validate body — reason must be at least 10 chars.
	reason := ""
	if req.Body != nil {
		reason = strings.TrimSpace(req.Body.Reason)
	}
	if len(reason) < 10 {
		return &deactivate400Resp{msg: "reason must be at least 10 characters"}, nil
	}

	// 2. Check current user status.
	isActive, _, _, err := s.Deactivation.GetUserStatus(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		return &deactivate404Resp{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !isActive {
		return &deactivate409Resp{msg: "User is already deactivated"}, nil
	}

	// 3. Persist deactivation in DB.
	nowMs := time.Now().UnixMilli()
	if err := s.Deactivation.DeactivateUser(ctx, userID, reason, nowMs); err != nil {
		return nil, err
	}

	// 4. gRPC: invalidate all active sessions (best-effort — log on failure, do not block).
	if s.CoreClient != nil {
		_, grpcErr := s.CoreClient.InvalidateUserSessions(ctx, &pb.InvalidateUserSessionsRequest{UserId: userID})
		if grpcErr != nil {
			slog.Warn("InvalidateUserSessions failed", "user_id", userID, "err", grpcErr)
		}
	} else {
		// MINOR-1 fix (Story 6.5 code review): warn on misconfiguration so a missing
		// CoreClient leaves an audit trail instead of silently skipping session invalidation.
		slog.Warn("InvalidateUserSessions skipped — CoreClient is nil", "user_id", userID)
	}

	// 5. Audit log (never-raise).
	if s.CoreClient != nil {
		actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
		_ = audit.LogEvent(ctx, s.CoreClient, actorID, "user_deactivated", "user", userID,
			map[string]any{"reason": reason}, "success", "")
	}

	return &deactivate200Resp{userID: userID, status: "deactivated"}, nil
}

// ReactivateAdminUser implements AC#2: POST /api/v1/admin/users/{userId}/reactivate.
// Checks blocked states (anonymized, keys_deleted) and current active state,
// then updates DB and emits audit log. Returns 501 if Deactivation repository is not wired.
func (s *AdminServer) ReactivateAdminUser(ctx context.Context, req ReactivateAdminUserRequestObject) (ReactivateAdminUserResponseObject, error) {
	if s.Deactivation == nil {
		return ReactivateAdminUser501Response{}, nil
	}

	userID := req.UserId

	// Check current user status.
	isActive, deletionStatus, anonymizedAt, err := s.Deactivation.GetUserStatus(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		return &reactivate404Resp{}, nil
	}
	if err != nil {
		return nil, err
	}

	// Reactivation check order (AC#2 state machine):
	// 1. anonymized_at IS NOT NULL → blocked
	// 2. deletion_status='keys_deleted' → blocked
	// 3. is_active=true → already active
	if anonymizedAt != 0 {
		return &reactivate409Resp{msg: "Cannot reactivate: user is in anonymized state"}, nil
	}
	if deletionStatus == "keys_deleted" {
		return &reactivate409Resp{msg: "Cannot reactivate: user is in keys_deleted state"}, nil
	}
	if isActive {
		return &reactivate409Resp{msg: "User is already active"}, nil
	}

	// Persist reactivation in DB.
	if err := s.Deactivation.ReactivateUser(ctx, userID); err != nil {
		return nil, err
	}

	// Audit log (never-raise).
	if s.CoreClient != nil {
		actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
		_ = audit.LogEvent(ctx, s.CoreClient, actorID, "user_reactivated", "user", userID, nil, "success", "")
	}

	return &reactivate200Resp{userID: userID, status: "active"}, nil
}

// ── Deactivate response types ─────────────────────────────────────────────────

type deactivate200Resp struct {
	userID string
	status string
}

func (r *deactivate200Resp) VisitDeactivateAdminUserResponse(w http.ResponseWriter) error {
	type dataObj struct {
		UserID string `json:"user_id"`
		Status string `json:"status"`
	}
	type envelope struct {
		Data dataObj `json:"data"`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(envelope{Data: dataObj{UserID: r.userID, Status: r.status}})
}

type deactivate400Resp struct{ msg string }

func (r *deactivate400Resp) VisitDeactivateAdminUserResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_BAD_JSON", "message": r.msg},
	})
}

type deactivate404Resp struct{}

func (r *deactivate404Resp) VisitDeactivateAdminUserResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_NOT_FOUND", "message": "User not found"},
	})
}

type deactivate409Resp struct{ msg string }

func (r *deactivate409Resp) VisitDeactivateAdminUserResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_CONFLICT", "message": r.msg},
	})
}

// ── Reactivate response types ─────────────────────────────────────────────────

type reactivate200Resp struct {
	userID string
	status string
}

func (r *reactivate200Resp) VisitReactivateAdminUserResponse(w http.ResponseWriter) error {
	type dataObj struct {
		UserID string `json:"user_id"`
		Status string `json:"status"`
	}
	type envelope struct {
		Data dataObj `json:"data"`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(envelope{Data: dataObj{UserID: r.userID, Status: r.status}})
}

type reactivate404Resp struct{}

func (r *reactivate404Resp) VisitReactivateAdminUserResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_NOT_FOUND", "message": "User not found"},
	})
}

type reactivate409Resp struct{ msg string }

func (r *reactivate409Resp) VisitReactivateAdminUserResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_CONFLICT", "message": r.msg},
	})
}
