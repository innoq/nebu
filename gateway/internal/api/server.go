//go:build go1.22

// Package api contains the Admin API handler implementations.
// Stub operations return 501 Not Implemented; Story 6.4 wires ListAdminUsers + GetAdminUser.
package api

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nebu/nebu/internal/audit"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AdminServer implements StrictServerInterface.
//
// DB and CoreClient are populated in main.go (Option A from Dev Notes: fields on AdminServer).
// Users is the UserRepository for Admin user queries (Story 6.4).
// Deactivation is the DeactivationRepository for user deactivation/reactivation (Story 6.5).
// Roles is the RoleOverrideRepository for role grant/revoke and override merging (Story 6.6).
// Rooms is the RoomRepository for Admin room queries (Story 6.7).
// RoomDefaults is the RoomDefaultsRepository for server-wide room defaults (Story 6.8).
// ServerConfig is the ServerConfigRepository for server config read/write (Story 6.10).
// Metrics is the MetricsRepository for DB-derived metric counts (Story 6.10).
// Secret is the AES-256-GCM master key used to encrypt oidc_client_secret (Story 6.10).
type AdminServer struct {
	DB           *sql.DB
	CoreClient   pb.CoreServiceClient
	Users        UserRepository
	Deactivation DeactivationRepository // Story 6.5
	Roles        RoleOverrideRepository // Story 6.6
	Rooms        RoomRepository         // Story 6.7
	RoomDefaults RoomDefaultsRepository // Story 6.8
	ServerConfig ServerConfigRepository // Story 6.10
	Metrics      MetricsRepository      // Story 6.10
	Secret       []byte                 // Story 6.10: AES-256-GCM key for oidc_client_secret encryption
}

// Ensure AdminServer satisfies the generated StrictServerInterface at compile time.
var _ StrictServerInterface = (*AdminServer)(nil)

// GetAdminConfig implements AC#1: GET /api/v1/admin/config
// Returns all readable server_config keys combined with room_defaults.
// SECURITY: oidc_client_secret is NEVER included in the response (write-only field).
// Returns 501 if ServerConfig or RoomDefaults repositories are not wired.
func (s *AdminServer) GetAdminConfig(ctx context.Context, _ GetAdminConfigRequestObject) (GetAdminConfigResponseObject, error) {
	if s.ServerConfig == nil || s.RoomDefaults == nil {
		return GetAdminConfig501Response{}, nil
	}

	cfgData, err := s.ServerConfig.GetServerConfig(ctx)
	if err != nil {
		return nil, err
	}

	maxMembers, visibility, err := s.RoomDefaults.GetRoomDefaults(ctx)
	if err != nil {
		return nil, err
	}

	return &getAdminConfigOKResponse{
		instanceName:          cfgData.InstanceName,
		oidcIssuer:            cfgData.OIDCIssuer,
		oidcClientID:          cfgData.OIDCClientID,
		roomDefaultMaxMembers: maxMembers,
		roomDefaultVisibility: visibility,
		auditLogRetentionDays: cfgData.AuditLogRetentionDays,
		oidcDirectoryEnabled:  cfgData.OidcDirectoryEnabled,
		oidcDirectoryEndpoint: cfgData.OidcDirectoryEndpoint,
	}, nil
}

// PatchAdminConfig implements AC#2: PATCH /api/v1/admin/config
// Partial update: any subset of updatable keys may be provided.
// oidc_client_secret is encrypted with AES-256-GCM before storage.
// If oidc_issuer, oidc_client_id, or oidc_client_secret changes → calls InvalidateAllAdminSessions.
// Emits audit log (never-raise).
// Returns 501 if ServerConfig repository is not wired.
func (s *AdminServer) PatchAdminConfig(ctx context.Context, req PatchAdminConfigRequestObject) (PatchAdminConfigResponseObject, error) {
	if s.ServerConfig == nil {
		return PatchAdminConfig501Response{}, nil
	}

	body := req.Body
	if body == nil {
		return &patchAdminConfig400Resp{msg: "request body is required"}, nil
	}

	// Validate audit_log_retention_days range (1–36500) before any writes.
	if body.AuditLogRetentionDays != nil {
		days := *body.AuditLogRetentionDays
		if days < 1 || days > 36500 {
			return &patchAdminConfig400Resp{msg: "audit_log_retention_days must be between 1 and 36500"}, nil
		}
	}

	// Upsert each changed key and track which keys were changed for audit + OIDC invalidation.
	changedKeys := []string{}
	oidcChanged := false

	if body.InstanceName != nil {
		if err := s.ServerConfig.UpsertServerConfigKey(ctx, "instance_name", *body.InstanceName); err != nil {
			return nil, err
		}
		changedKeys = append(changedKeys, "instance_name")
	}

	if body.OidcIssuer != nil {
		if err := s.ServerConfig.UpsertServerConfigKey(ctx, "oidc_issuer", *body.OidcIssuer); err != nil {
			return nil, err
		}
		changedKeys = append(changedKeys, "oidc_issuer")
		oidcChanged = true
	}

	if body.OidcClientId != nil {
		if err := s.ServerConfig.UpsertServerConfigKey(ctx, "oidc_client_id", *body.OidcClientId); err != nil {
			return nil, err
		}
		changedKeys = append(changedKeys, "oidc_client_id")
		oidcChanged = true
	}

	if body.OidcClientSecret != nil {
		// Encrypt oidc_client_secret with AES-256-GCM before storing.
		// Never log or return the plaintext secret.
		// MINOR fix (review): bubble up encryption errors as 500 rather than silently
		// returning 200 with the secret unstored — that would let callers believe the
		// PATCH succeeded while leaving the OIDC config in an inconsistent state.
		encrypted, err := encryptAES256GCMForAPI(s.Secret, *body.OidcClientSecret)
		if err != nil {
			slog.Error("PatchAdminConfig: failed to encrypt oidc_client_secret", "err", err)
			return nil, err
		}
		if storeErr := s.ServerConfig.UpsertServerConfigKey(ctx, "oidc_client_secret", encrypted); storeErr != nil {
			return nil, storeErr
		}
		changedKeys = append(changedKeys, "oidc_client_secret")
		oidcChanged = true
	}

	if body.AuditLogRetentionDays != nil {
		daysStr := strconv.Itoa(*body.AuditLogRetentionDays)
		if err := s.ServerConfig.UpsertServerConfigKey(ctx, "audit_log_retention_days", daysStr); err != nil {
			return nil, err
		}
		changedKeys = append(changedKeys, "audit_log_retention_days")
	}

	// Story 14-2a: OIDC directory integration fields (ADR-015 Protocol A).
	// OidcDirectoryEnabled is a bool pointer — non-nil means the caller explicitly provided the value.
	// Storing as "true"/"false" string in the key-value server_config table.
	if body.OidcDirectoryEnabled != nil {
		enabledStr := "false"
		if *body.OidcDirectoryEnabled {
			enabledStr = "true"
		}
		if err := s.ServerConfig.UpsertServerConfigKey(ctx, "oidc_directory_enabled", enabledStr); err != nil {
			return nil, err
		}
		changedKeys = append(changedKeys, "oidc_directory_enabled")
	}

	if body.OidcDirectoryEndpoint != nil {
		if err := s.ServerConfig.UpsertServerConfigKey(ctx, "oidc_directory_endpoint", *body.OidcDirectoryEndpoint); err != nil {
			return nil, err
		}
		changedKeys = append(changedKeys, "oidc_directory_endpoint")
	}

	// matrix_user_id_claim goes through Core gRPC — Core owns the bootstrap-lock logic.
	// FAILED_PRECONDITION (code 9) means post-bootstrap lock is active → 400 M_FORBIDDEN.
	if body.MatrixUserIdClaim != nil && *body.MatrixUserIdClaim != "" {
		if s.CoreClient == nil {
			return PatchAdminConfig501Response{}, nil
		}
		_, grpcErr := s.CoreClient.UpdateServerConfig(ctx, &pb.UpdateServerConfigRequest{
			MatrixUserIdClaim: *body.MatrixUserIdClaim,
		})
		if grpcErr != nil {
			if st, ok := status.FromError(grpcErr); ok && st.Code() == codes.FailedPrecondition {
				return &patchAdminConfig400ForbiddenResp{
					msg: "matrix_user_id_claim cannot be changed after bootstrap",
				}, nil
			}
			slog.Error("PatchAdminConfig: UpdateServerConfig gRPC error", "err", grpcErr)
			return nil, grpcErr
		}
		changedKeys = append(changedKeys, "matrix_user_id_claim")
	}

	// Invalidate all admin sessions if any OIDC field changed (best-effort — log on error, do not fail).
	if oidcChanged && s.CoreClient != nil {
		_, grpcErr := s.CoreClient.InvalidateAllAdminSessions(ctx, &pb.InvalidateAllAdminSessionsRequest{})
		if grpcErr != nil {
			slog.Warn("PatchAdminConfig: InvalidateAllAdminSessions failed — admin sessions may still be valid",
				"err", grpcErr)
		}
	}

	// Audit log (never-raise).
	if s.CoreClient != nil && len(changedKeys) > 0 {
		actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
		_ = audit.LogEvent(ctx, s.CoreClient, actorID, "server_config_updated", "server", "config",
			map[string]any{"changed_keys": changedKeys}, "success", "")
	} else if s.CoreClient == nil {
		slog.Warn("PatchAdminConfig audit skipped — CoreClient is nil")
	}

	// Return full updated config (same as GET) — reuse the GET logic but adapt the response type.
	getResp, err := s.GetAdminConfig(ctx, GetAdminConfigRequestObject{})
	if err != nil {
		return nil, err
	}
	// Wrap the GET response as a PATCH 200 response.
	// getAdminConfigOKResponse implements VisitGetAdminConfigResponse but not VisitPatchAdminConfigResponse.
	// We use a bridge wrapper to avoid code duplication.
	if okResp, ok := getResp.(*getAdminConfigOKResponse); ok {
		return &patchAdminConfig200Resp{inner: okResp}, nil
	}
	// Fallback: 501 if something unexpected happened (e.g. repos became nil between calls).
	return PatchAdminConfig501Response{}, nil
}

// GetAdminMetrics implements AC#3: GET /api/v1/admin/metrics
// Returns DB-derived counts from MetricsRepository combined with gRPC-derived
// active_sessions and msg_per_sec from GetMetrics.
// Returns 501 if Metrics repository is not wired.
func (s *AdminServer) GetAdminMetrics(ctx context.Context, _ GetAdminMetricsRequestObject) (GetAdminMetricsResponseObject, error) {
	if s.Metrics == nil {
		return GetAdminMetrics501Response{}, nil
	}

	counts, err := s.Metrics.GetMetricsCounts(ctx)
	if err != nil {
		return nil, err
	}

	// gRPC-derived fields: active_sessions and msg_per_sec (best-effort — zero on error/nil CoreClient).
	var activeSessions int
	var msgPerSec float64
	if s.CoreClient != nil {
		resp, grpcErr := s.CoreClient.GetMetrics(ctx, &pb.GetMetricsRequest{})
		if grpcErr != nil {
			slog.Warn("GetAdminMetrics: GetMetrics gRPC failed — using zero values", "err", grpcErr)
		} else if resp != nil {
			activeSessions = int(resp.ActiveSessions)
			msgPerSec = float64(resp.MsgPerSec)
		}
	} else {
		slog.Warn("GetAdminMetrics: CoreClient is nil — active_sessions and msg_per_sec will be zero")
	}

	return &getAdminMetricsOKResponse{
		activeSessions:    activeSessions,
		roomCount:         counts.RoomCount,
		archivedRoomCount: counts.ArchivedRoomCount,
		msgPerSec1m:       msgPerSec,
		registeredUsers:   counts.RegisteredUsers,
		deactivatedUsers:  counts.DeactivatedUsers,
	}, nil
}

// ── Story 6.10: encryption helper ────────────────────────────────────────────

// errEncryptionKeyMissing is returned by encryptAES256GCMForAPI when the
// AES-256-GCM master key is not configured. The PatchAdminConfig handler maps
// this to a 500 Internal Server Error so misconfiguration is surfaced rather
// than masked by a silent plaintext fallback.
var errEncryptionKeyMissing = errors.New("oidc_client_secret encryption key not configured")


// encryptAES256GCMForAPI encrypts plaintext using AES-256-GCM with a SHA-256 derived key.
// Returns hex-encoded nonce||ciphertext. Mirrors admin.encryptAES256GCM for package isolation.
//
// SECURITY: returns errEncryptionKeyMissing when secret is empty. Storing the
// OIDC client secret in plaintext would defeat the entire encrypted-storage
// invariant of Story 5.29c, so the handler MUST refuse to write rather than
// silently fall back. main.go always wires Secret=internalSecret (non-empty);
// the empty-secret path only fires on misconfiguration or in unit tests that
// PATCH non-secret fields.
func encryptAES256GCMForAPI(secret []byte, plaintext string) (string, error) {
	if len(secret) == 0 {
		return "", errEncryptionKeyMissing
	}
	keyHash := sha256.Sum256(secret)
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// ── Story 6.10: GetAdminConfig / PatchAdminConfig response types ─────────────

// adminConfigResponseBody is the JSON wire format for GET /admin/config and PATCH /admin/config.
// All fields are always present (omitempty is intentionally NOT used — tests verify all keys exist).
// oidc_client_secret is intentionally absent from this struct (AC#1 security invariant).
type adminConfigResponseBody struct {
	InstanceName          string `json:"instance_name"`
	OIDCIssuer            string `json:"oidc_issuer"`
	OIDCClientID          string `json:"oidc_client_id"`
	RoomDefaultMaxMembers int    `json:"room_default_max_members"`
	RoomDefaultVisibility string `json:"room_default_visibility"`
	AuditLogRetentionDays int    `json:"audit_log_retention_days"`
	OidcDirectoryEnabled  bool   `json:"oidc_directory_enabled"`  // Story 14-2a
	OidcDirectoryEndpoint string `json:"oidc_directory_endpoint"` // Story 14-2a
}

// getAdminConfigOKResponse — 200 OK for GET /admin/config.
// Implements GetAdminConfigResponseObject.
type getAdminConfigOKResponse struct {
	instanceName          string
	oidcIssuer            string
	oidcClientID          string
	roomDefaultMaxMembers int
	roomDefaultVisibility string
	auditLogRetentionDays int
	oidcDirectoryEnabled  bool   // Story 14-2a
	oidcDirectoryEndpoint string // Story 14-2a
}

func (r *getAdminConfigOKResponse) VisitGetAdminConfigResponse(w http.ResponseWriter) error {
	body := adminConfigResponseBody{
		InstanceName:          r.instanceName,
		OIDCIssuer:            r.oidcIssuer,
		OIDCClientID:          r.oidcClientID,
		RoomDefaultMaxMembers: r.roomDefaultMaxMembers,
		RoomDefaultVisibility: r.roomDefaultVisibility,
		AuditLogRetentionDays: r.auditLogRetentionDays,
		OidcDirectoryEnabled:  r.oidcDirectoryEnabled,
		OidcDirectoryEndpoint: r.oidcDirectoryEndpoint,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(body)
}

// patchAdminConfig200Resp — 200 OK for PATCH /admin/config.
// Wraps getAdminConfigOKResponse and adapts it to PatchAdminConfigResponseObject.
// Implements PatchAdminConfigResponseObject.
type patchAdminConfig200Resp struct{ inner *getAdminConfigOKResponse }

func (r *patchAdminConfig200Resp) VisitPatchAdminConfigResponse(w http.ResponseWriter) error {
	return r.inner.VisitGetAdminConfigResponse(w)
}

// patchAdminConfig400Resp — 400 M_BAD_JSON validation error.
// Implements PatchAdminConfigResponseObject.
type patchAdminConfig400Resp struct{ msg string }

func (r *patchAdminConfig400Resp) VisitPatchAdminConfigResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_BAD_JSON", "message": r.msg},
	})
}

// patchAdminConfig400ForbiddenResp — 400 M_FORBIDDEN for post-bootstrap claim lock.
// Used when Core returns FAILED_PRECONDITION for UpdateServerConfig (matrix_user_id_claim).
// Uses the flat Matrix Client-Server API error format: {"errcode": "...", "error": "..."}.
// Implements PatchAdminConfigResponseObject.
type patchAdminConfig400ForbiddenResp struct{ msg string }

func (r *patchAdminConfig400ForbiddenResp) VisitPatchAdminConfigResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	return json.NewEncoder(w).Encode(map[string]any{
		"errcode": "M_FORBIDDEN",
		"error":   r.msg,
	})
}

// ── Story 6.10: GetAdminMetrics response types ───────────────────────────────

// adminMetricsResponseBody is the JSON wire format for GET /admin/metrics.
// All 6 fields are always present (zero is a valid value per AC#3).
type adminMetricsResponseBody struct {
	ActiveSessions    int     `json:"active_sessions"`
	RoomCount         int     `json:"room_count"`
	ArchivedRoomCount int     `json:"archived_room_count"`
	MsgPerSec1m       float64 `json:"msg_per_sec_1m"`
	RegisteredUsers   int     `json:"registered_users"`
	DeactivatedUsers  int     `json:"deactivated_users"`
}

// getAdminMetricsOKResponse — 200 OK for GET /admin/metrics.
// Implements GetAdminMetricsResponseObject.
type getAdminMetricsOKResponse struct {
	activeSessions    int
	roomCount         int
	archivedRoomCount int
	msgPerSec1m       float64
	registeredUsers   int
	deactivatedUsers  int
}

func (r *getAdminMetricsOKResponse) VisitGetAdminMetricsResponse(w http.ResponseWriter) error {
	body := adminMetricsResponseBody{
		ActiveSessions:    r.activeSessions,
		RoomCount:         r.roomCount,
		ArchivedRoomCount: r.archivedRoomCount,
		MsgPerSec1m:       r.msgPerSec1m,
		RegisteredUsers:   r.registeredUsers,
		DeactivatedUsers:  r.deactivatedUsers,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(body)
}

// ListAdminRooms implements AC#1: GET /api/v1/admin/rooms
// Query params: cursor (optional), limit (1–100, default 20), search (optional), status (optional).
// Emits an audit log event on success (never-raise: audit failure does not block the response).
// Returns 501 if no RoomRepository is wired (pre-Story-6.7 stub behaviour).
func (s *AdminServer) ListAdminRooms(ctx context.Context, request ListAdminRoomsRequestObject) (ListAdminRoomsResponseObject, error) {
	// Guard: if no RoomRepository is wired, fall back to 501 stub.
	if s.Rooms == nil {
		return ListAdminRooms501Response{}, nil
	}

	// ── Parse limit ──────────────────────────────────────────────────────────
	limit := 20
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if limit < 1 || limit > 100 {
		return &listRooms400Resp{msg: "limit must be between 1 and 100"}, nil
	}

	// ── Parse cursor ─────────────────────────────────────────────────────────
	var afterID, afterCreatedAt string
	if request.Params.Cursor != nil && *request.Params.Cursor != "" {
		var err error
		afterID, afterCreatedAt, err = DecodeCursor(*request.Params.Cursor)
		if err != nil {
			return &listRooms400Resp{msg: "Invalid cursor"}, nil
		}
	}

	// ── Parse search ─────────────────────────────────────────────────────────
	search := ""
	if request.Params.Search != nil {
		search = *request.Params.Search
	}

	// ── Parse status ─────────────────────────────────────────────────────────
	statusFilter := ""
	if request.Params.Status != nil {
		statusFilter = *request.Params.Status
	}
	if statusFilter != "" && statusFilter != "active" && statusFilter != "archived" {
		return &listRooms400Resp{msg: "status must be 'active' or 'archived'"}, nil
	}

	// ── Repository call ───────────────────────────────────────────────────────
	rooms, total, nextCursor, err := s.Rooms.ListRooms(ctx, afterID, afterCreatedAt, limit, search, statusFilter)
	if err != nil {
		return nil, err
	}

	// ── Audit log (never-raise) ───────────────────────────────────────────────
	if s.CoreClient != nil {
		actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
		_ = audit.LogEvent(ctx, s.CoreClient, actorID, "admin_room_viewed", "room", "", nil, "success", "")
	} else {
		slog.Warn("ListAdminRooms audit skipped — CoreClient is nil")
	}

	return &listAdminRoomsOKResponse{
		rooms:      rooms,
		total:      total,
		nextCursor: nextCursor,
	}, nil
}

// GetAdminRoom implements AC#2: GET /api/v1/admin/rooms/{roomId}
// Returns a single AdminRoomDetail or 404 if not found.
// Emits an audit log event on success.
// Returns 501 if no RoomRepository is wired (safety fallback).
func (s *AdminServer) GetAdminRoom(ctx context.Context, request GetAdminRoomRequestObject) (GetAdminRoomResponseObject, error) {
	// Guard: if no RoomRepository is wired, fall back to 501 stub.
	if s.Rooms == nil {
		return GetAdminRoom501Response{}, nil
	}

	detail, err := s.Rooms.GetRoom(ctx, request.RoomId)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return &getAdminRoom404Response{}, nil
	}

	// ── Audit log (never-raise) ───────────────────────────────────────────────
	if s.CoreClient != nil {
		actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
		_ = audit.LogEvent(ctx, s.CoreClient, actorID, "admin_room_viewed", "room", request.RoomId, nil, "success", "")
	} else {
		slog.Warn("GetAdminRoom audit skipped — CoreClient is nil", "room_id", request.RoomId)
	}

	return &getAdminRoomOKResponse{detail: detail}, nil
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

// ── Story 6.6: AssignAdminUserRole handler ────────────────────────────────────

// AssignAdminUserRole implements AC#2: POST /api/v1/admin/users/{userId}/roles.
// Grants or revokes a role override for a user independent of their OIDC claims.
//
// Validation order:
//  1. Roles repo must be wired — otherwise 501 stub.
//  2. Body must be present and role/action must be valid enum values → 400.
//  3. User must exist → 404 "User not found".
//  4. Self-revoke of instance_admin is blocked → 403.
//  5. grant: upsert into role_overrides → 200.
//  6. revoke: delete from role_overrides → 200; ErrRoleOverrideNotFound → 404.
//  7. Audit log (never-raise).
func (s *AdminServer) AssignAdminUserRole(ctx context.Context, req AssignAdminUserRoleRequestObject) (AssignAdminUserRoleResponseObject, error) {
	if s.Roles == nil {
		return AssignAdminUserRole501Response{}, nil
	}

	userID := req.UserId

	// 1. Validate body — role and action must be present and valid.
	if req.Body == nil {
		return &assignRole400Resp{msg: "request body is required"}, nil
	}
	if !req.Body.Role.Valid() {
		return &assignRole400Resp{msg: "invalid role: must be instance_admin or compliance_officer"}, nil
	}
	if !req.Body.Action.Valid() {
		return &assignRole400Resp{msg: "invalid action: must be grant or revoke"}, nil
	}
	role := string(req.Body.Role)
	action := string(req.Body.Action)

	// 2. User must exist.
	exists, err := s.Roles.UserExists(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return &assignRole404Resp{msg: "User not found"}, nil
	}

	// 3. Self-revoke protection: admin cannot revoke their own instance_admin.
	actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
	if action == string(Revoke) && role == string(InstanceAdmin) && actorID == userID {
		return &assignRole403Resp{msg: "Cannot revoke your own admin role"}, nil
	}

	// 4. Perform grant or revoke.
	var auditAction string
	var responseAction string
	switch AssignUserRoleRequestAction(action) {
	case Grant:
		if err := s.Roles.GrantRoleOverride(ctx, userID, role, actorID); err != nil {
			return nil, err
		}
		auditAction = "role_granted"
		responseAction = "granted"

	case Revoke:
		if err := s.Roles.RevokeRoleOverride(ctx, userID, role); err != nil {
			if errors.Is(err, ErrRoleOverrideNotFound) {
				return &assignRole404Resp{msg: "Role override not found"}, nil
			}
			return nil, err
		}
		auditAction = "role_revoked"
		responseAction = "revoked"
	}

	// 5. Audit log (never-raise).
	if s.CoreClient != nil {
		_ = audit.LogEvent(ctx, s.CoreClient, actorID, auditAction, "user", userID,
			map[string]any{"role": role}, "success", "")
	} else {
		slog.Warn("AssignAdminUserRole audit skipped — CoreClient is nil", "user_id", userID, "action", auditAction)
	}

	return &assignRole200Resp{userID: userID, role: role, action: responseAction}, nil
}

// ── AssignAdminUserRole response types ───────────────────────────────────────

type assignRole200Resp struct {
	userID string
	role   string
	action string
}

func (r *assignRole200Resp) VisitAssignAdminUserRoleResponse(w http.ResponseWriter) error {
	type dataObj struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
		Action string `json:"action"`
	}
	type envelope struct {
		Data dataObj `json:"data"`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(envelope{Data: dataObj{UserID: r.userID, Role: r.role, Action: r.action}})
}

type assignRole400Resp struct{ msg string }

func (r *assignRole400Resp) VisitAssignAdminUserRoleResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_BAD_JSON", "message": r.msg},
	})
}

type assignRole403Resp struct{ msg string }

func (r *assignRole403Resp) VisitAssignAdminUserRoleResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_FORBIDDEN", "message": r.msg},
	})
}

type assignRole404Resp struct{ msg string }

func (r *assignRole404Resp) VisitAssignAdminUserRoleResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_NOT_FOUND", "message": r.msg},
	})
}

// ── Story 6.7: Room List + Get response types ─────────────────────────────────

// listAdminRoomsOKResponse serialises AdminRoom list + pagination metadata as JSON.
// Implements ListAdminRoomsResponseObject.
type listAdminRoomsOKResponse struct {
	rooms      []AdminRoom
	total      int
	nextCursor string
}

func (resp *listAdminRoomsOKResponse) VisitListAdminRoomsResponse(w http.ResponseWriter) error {
	type meta struct {
		Total      int    `json:"total"`
		NextCursor string `json:"next_cursor,omitempty"`
	}
	type envelope struct {
		Data []AdminRoom `json:"data"`
		Meta meta        `json:"meta"`
	}
	data := resp.rooms
	if data == nil {
		data = []AdminRoom{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(envelope{
		Data: data,
		Meta: meta{Total: resp.total, NextCursor: resp.nextCursor},
	})
}

// listRooms400Resp writes a 400 M_BAD_REQUEST error.
// Implements ListAdminRoomsResponseObject.
type listRooms400Resp struct{ msg string }

func (r *listRooms400Resp) VisitListAdminRoomsResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_BAD_REQUEST", "message": r.msg},
	})
}

// getAdminRoomOKResponse serialises AdminRoomDetail as JSON.
// Implements GetAdminRoomResponseObject.
type getAdminRoomOKResponse struct{ detail *AdminRoomDetail }

func (r *getAdminRoomOKResponse) VisitGetAdminRoomResponse(w http.ResponseWriter) error {
	type envelope struct {
		Data *AdminRoomDetail `json:"data"`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(envelope{Data: r.detail})
}

// getAdminRoom404Response writes a 404 M_NOT_FOUND error.
// Implements GetAdminRoomResponseObject.
type getAdminRoom404Response struct{}

func (r *getAdminRoom404Response) VisitGetAdminRoomResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_NOT_FOUND", "message": "Room not found"},
	})
}

// ── Story 6.8: PatchAdminRoom handler ────────────────────────────────────────

// PatchAdminRoom implements AC#1: PATCH /api/v1/admin/rooms/{roomId}.
// Validates optional fields, applies DB patch, notifies Room GenServer via gRPC,
// and emits an audit log. Returns 501 if Rooms repository is not wired.
func (s *AdminServer) PatchAdminRoom(ctx context.Context, req PatchAdminRoomRequestObject) (PatchAdminRoomResponseObject, error) {
	if s.Rooms == nil {
		return PatchAdminRoom501Response{}, nil
	}

	roomID := req.RoomId
	body := req.Body
	if body == nil {
		return &patchRoom400Resp{msg: "request body is required"}, nil
	}

	// ── Validate and build patch ─────────────────────────────────────────────
	patch := RoomPatch{}
	changedFields := map[string]any{}

	if body.MaxMembers != nil {
		v := *body.MaxMembers
		if v < 2 || v > 100000 {
			return &patchRoom400Resp{msg: "max_members must be between 2 and 100000"}, nil
		}
		patch.MaxMembers = &v
		changedFields["max_members"] = v
	}
	if body.Visibility != nil {
		v := string(*body.Visibility)
		if !body.Visibility.Valid() {
			return &patchRoom400Resp{msg: "visibility must be 'public' or 'private'"}, nil
		}
		patch.Visibility = &v
		changedFields["visibility"] = v
	}
	if body.Name != nil {
		v := *body.Name
		// MINOR-1 (review): use UTF-8 rune count, not byte length, for "1–255 chars".
		// Consistent with profile.go:126 (Displayname) and admin/users.go:220 (DisplayName).
		runes := utf8.RuneCountInString(v)
		if runes < 1 || runes > 255 {
			return &patchRoom400Resp{msg: "name must be between 1 and 255 characters"}, nil
		}
		patch.Name = &v
		changedFields["name"] = v
	}
	if body.Topic != nil {
		v := *body.Topic
		// MINOR-1 (review): use UTF-8 rune count, not byte length, for "0–1000 chars".
		if utf8.RuneCountInString(v) > 1000 {
			return &patchRoom400Resp{msg: "topic must be at most 1000 characters"}, nil
		}
		patch.Topic = &v
		changedFields["topic"] = v
	}

	// ── Apply patch (also checks existence) ──────────────────────────────────
	updated, err := s.Rooms.UpdateRoom(ctx, roomID, patch)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return &patchRoom404Resp{}, nil
	}

	// ── Notify Room GenServer via gRPC (only if max_members changed) ─────────
	if patch.MaxMembers != nil && s.CoreClient != nil {
		_, grpcErr := s.CoreClient.UpdateRoomSettings(ctx, &pb.UpdateRoomSettingsRequest{
			RoomId:     roomID,
			MaxMembers: int32(*patch.MaxMembers),
		})
		if grpcErr != nil {
			slog.Warn("UpdateRoomSettings gRPC failed — GenServer state not updated in real time",
				"room_id", roomID, "err", grpcErr)
			// Best-effort: continue (DB is already updated; GenServer will load from DB on next start)
		}
	}

	// ── Audit log (never-raise) ───────────────────────────────────────────────
	// MINOR-2 (review): skip audit for no-op PATCH (empty body / no changed fields)
	// to avoid polluting the audit trail with non-events.
	if len(changedFields) > 0 {
		if s.CoreClient != nil {
			actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
			_ = audit.LogEvent(ctx, s.CoreClient, actorID, "room_settings_updated", "room", roomID,
				map[string]any{"changes": changedFields}, "success", "")
		} else {
			slog.Warn("PatchAdminRoom audit skipped — CoreClient is nil", "room_id", roomID)
		}
	}

	return &patchRoom200Resp{detail: updated}, nil
}

// ── Story 6.8: PutAdminRoomDefaults handler ───────────────────────────────────

// PutAdminRoomDefaults implements AC#2: PUT /api/v1/admin/config/room-defaults.
// Validates the body, upserts into room_defaults table, and returns the new defaults.
// Returns 501 if RoomDefaults repository is not wired.
func (s *AdminServer) PutAdminRoomDefaults(ctx context.Context, req PutAdminRoomDefaultsRequestObject) (PutAdminRoomDefaultsResponseObject, error) {
	if s.RoomDefaults == nil {
		return PutAdminRoomDefaults501Response{}, nil
	}

	body := req.Body
	if body == nil {
		return &putRoomDefaults400Resp{msg: "request body is required"}, nil
	}

	// ── Validate fields ───────────────────────────────────────────────────────
	if body.DefaultMaxMembers < 0 {
		return &putRoomDefaults400Resp{msg: "default_max_members must be >= 0"}, nil
	}
	if !body.DefaultVisibility.Valid() {
		return &putRoomDefaults400Resp{msg: "default_visibility must be 'public' or 'private'"}, nil
	}

	// ── Upsert into room_defaults ─────────────────────────────────────────────
	if err := s.RoomDefaults.UpsertRoomDefaults(ctx, body.DefaultMaxMembers, string(body.DefaultVisibility)); err != nil {
		return nil, err
	}

	// ── Read back updated values ──────────────────────────────────────────────
	maxMembers, visibility, err := s.RoomDefaults.GetRoomDefaults(ctx)
	if err != nil {
		return nil, err
	}

	// ── Audit log (never-raise) ───────────────────────────────────────────────
	if s.CoreClient != nil {
		actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
		_ = audit.LogEvent(ctx, s.CoreClient, actorID, "room_defaults_updated", "room", "",
			map[string]any{"default_max_members": maxMembers, "default_visibility": visibility}, "success", "")
	} else {
		slog.Warn("PutAdminRoomDefaults audit skipped — CoreClient is nil")
	}

	return &putRoomDefaults200Resp{maxMembers: maxMembers, visibility: visibility}, nil
}

// ── Story 6.8: PatchAdminRoom response types ──────────────────────────────────

// patchRoom200Resp — 200 OK with updated AdminRoomDetail.
// Implements PatchAdminRoomResponseObject.
type patchRoom200Resp struct{ detail *AdminRoomDetail }

func (r *patchRoom200Resp) VisitPatchAdminRoomResponse(w http.ResponseWriter) error {
	type envelope struct {
		Data *AdminRoomDetail `json:"data"`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(envelope{Data: r.detail})
}

// patchRoom400Resp — 400 M_BAD_JSON validation error.
// Implements PatchAdminRoomResponseObject.
type patchRoom400Resp struct{ msg string }

func (r *patchRoom400Resp) VisitPatchAdminRoomResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_BAD_JSON", "message": r.msg},
	})
}

// patchRoom404Resp — 404 M_NOT_FOUND.
// Implements PatchAdminRoomResponseObject.
type patchRoom404Resp struct{}

func (r *patchRoom404Resp) VisitPatchAdminRoomResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_NOT_FOUND", "message": "Room not found"},
	})
}

// ── Story 6.8: PutAdminRoomDefaults response types ────────────────────────────

// putRoomDefaults200Resp — 200 OK with room defaults.
// Implements PutAdminRoomDefaultsResponseObject.
type putRoomDefaults200Resp struct {
	maxMembers int
	visibility string
}

func (r *putRoomDefaults200Resp) VisitPutAdminRoomDefaultsResponse(w http.ResponseWriter) error {
	type dataObj struct {
		DefaultMaxMembers int    `json:"default_max_members"`
		DefaultVisibility string `json:"default_visibility"`
	}
	type envelope struct {
		Data dataObj `json:"data"`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(envelope{Data: dataObj{
		DefaultMaxMembers: r.maxMembers,
		DefaultVisibility: r.visibility,
	}})
}

// putRoomDefaults400Resp — 400 M_BAD_JSON validation error.
// Implements PutAdminRoomDefaultsResponseObject.
type putRoomDefaults400Resp struct{ msg string }

func (r *putRoomDefaults400Resp) VisitPutAdminRoomDefaultsResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_BAD_JSON", "message": r.msg},
	})
}

// ── Story 6.9: ArchiveAdminRoom handler ──────────────────────────────────────

// ArchiveAdminRoom implements AC#1: POST /api/v1/admin/rooms/{roomId}/archive.
// Validates reason (min 10 chars), atomically archives room in DB, calls gRPC
// ArchiveRoom (best-effort), emits audit log.
// Returns 501 if Rooms repository is not wired.
func (s *AdminServer) ArchiveAdminRoom(ctx context.Context, req ArchiveAdminRoomRequestObject) (ArchiveAdminRoomResponseObject, error) {
	if s.Rooms == nil {
		return ArchiveAdminRoom501Response{}, nil
	}

	roomID := req.RoomId
	body := req.Body
	if body == nil {
		return &archiveRoom400Resp{msg: "request body is required"}, nil
	}

	// 1. Validate reason (required, min 10 chars).
	reason := strings.TrimSpace(body.Reason)
	if len(reason) < 10 {
		return &archiveRoom400Resp{msg: "reason must be at least 10 characters"}, nil
	}

	// 2. DB update (atomic conditional UPDATE).
	result, err := s.Rooms.ArchiveRoom(ctx, roomID, reason)
	if errors.Is(err, ErrRoomNotFound) {
		return &archiveRoom404Resp{}, nil
	}
	if errors.Is(err, ErrRoomWrongStatus) {
		return &archiveRoom409Resp{msg: "Room is already archived"}, nil
	}
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &archiveRoom404Resp{}, nil
	}

	// 3. gRPC ArchiveRoom (best-effort — DB is authoritative).
	if s.CoreClient != nil {
		_, grpcErr := s.CoreClient.ArchiveRoom(ctx, &pb.ArchiveRoomRequest{RoomId: roomID})
		if grpcErr != nil {
			slog.Warn("ArchiveRoom gRPC failed — GenServer may still be running",
				"room_id", roomID, "err", grpcErr)
			// Best-effort: continue. Room.Server init/1 will stop on next restart
			// because it checks rooms.status from DB (archived → {:stop, :normal}).
		}
	}

	// 4. Audit log (never-raise).
	if s.CoreClient != nil {
		actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
		_ = audit.LogEvent(ctx, s.CoreClient, actorID, "room_archived", "room", roomID,
			map[string]any{"reason": reason}, "success", "")
	} else {
		slog.Warn("ArchiveAdminRoom audit skipped — CoreClient is nil", "room_id", roomID)
	}

	return &archiveRoom200Resp{roomID: result.RoomID, status: result.Status}, nil
}

// ── Story 6.9: UnarchiveAdminRoom handler ────────────────────────────────────

// UnarchiveAdminRoom implements AC#2: POST /api/v1/admin/rooms/{roomId}/unarchive.
// Atomically unarchives room in DB, calls gRPC UnarchiveRoom (best-effort),
// emits audit log.
// Returns 501 if Rooms repository is not wired.
func (s *AdminServer) UnarchiveAdminRoom(ctx context.Context, req UnarchiveAdminRoomRequestObject) (UnarchiveAdminRoomResponseObject, error) {
	if s.Rooms == nil {
		return UnarchiveAdminRoom501Response{}, nil
	}

	roomID := req.RoomId

	// 1. DB update (atomic conditional UPDATE).
	result, err := s.Rooms.UnarchiveRoom(ctx, roomID)
	if errors.Is(err, ErrRoomNotFound) {
		return &unarchiveRoom404Resp{}, nil
	}
	if errors.Is(err, ErrRoomWrongStatus) {
		return &unarchiveRoom409Resp{msg: "Room is not archived"}, nil
	}
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &unarchiveRoom404Resp{}, nil
	}

	// 2. gRPC UnarchiveRoom (restarts GenServer) — best-effort.
	if s.CoreClient != nil {
		_, grpcErr := s.CoreClient.UnarchiveRoom(ctx, &pb.UnarchiveRoomRequest{RoomId: roomID})
		if grpcErr != nil {
			slog.Warn("UnarchiveRoom gRPC failed — GenServer not restarted",
				"room_id", roomID, "err", grpcErr)
			// Best-effort: continue. GenServer will start on next Matrix event.
		}
	}

	// 3. Audit log (never-raise).
	if s.CoreClient != nil {
		actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
		_ = audit.LogEvent(ctx, s.CoreClient, actorID, "room_unarchived", "room", roomID,
			nil, "success", "")
	} else {
		slog.Warn("UnarchiveAdminRoom audit skipped — CoreClient is nil", "room_id", roomID)
	}

	return &unarchiveRoom200Resp{roomID: result.RoomID, status: result.Status}, nil
}

// ── Story 6.9: Archive / Unarchive response types ────────────────────────────

// archiveRoom200Resp — 200 OK with room_id and status="archived".
// Implements ArchiveAdminRoomResponseObject.
type archiveRoom200Resp struct{ roomID, status string }

func (r *archiveRoom200Resp) VisitArchiveAdminRoomResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(map[string]string{
		"room_id": r.roomID,
		"status":  r.status,
	})
}

// archiveRoom400Resp — 400 M_BAD_JSON validation error.
// Implements ArchiveAdminRoomResponseObject.
type archiveRoom400Resp struct{ msg string }

func (r *archiveRoom400Resp) VisitArchiveAdminRoomResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_BAD_JSON", "message": r.msg},
	})
}

// archiveRoom404Resp — 404 M_NOT_FOUND.
// Implements ArchiveAdminRoomResponseObject.
type archiveRoom404Resp struct{}

func (r *archiveRoom404Resp) VisitArchiveAdminRoomResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_NOT_FOUND", "message": "Room not found"},
	})
}

// archiveRoom409Resp — 409 M_CONFLICT (room already archived).
// Implements ArchiveAdminRoomResponseObject.
type archiveRoom409Resp struct{ msg string }

func (r *archiveRoom409Resp) VisitArchiveAdminRoomResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_CONFLICT", "message": r.msg},
	})
}

// unarchiveRoom200Resp — 200 OK with room_id and status="active".
// Implements UnarchiveAdminRoomResponseObject.
type unarchiveRoom200Resp struct{ roomID, status string }

func (r *unarchiveRoom200Resp) VisitUnarchiveAdminRoomResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(map[string]string{
		"room_id": r.roomID,
		"status":  r.status,
	})
}

// unarchiveRoom404Resp — 404 M_NOT_FOUND.
// Implements UnarchiveAdminRoomResponseObject.
type unarchiveRoom404Resp struct{}

func (r *unarchiveRoom404Resp) VisitUnarchiveAdminRoomResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_NOT_FOUND", "message": "Room not found"},
	})
}

// unarchiveRoom409Resp — 409 M_CONFLICT (room is not archived).
// Implements UnarchiveAdminRoomResponseObject.
type unarchiveRoom409Resp struct{ msg string }

func (r *unarchiveRoom409Resp) VisitUnarchiveAdminRoomResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	return json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "M_CONFLICT", "message": r.msg},
	})
}
