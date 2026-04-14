package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
)

// ErrProfileNotFound is returned by ProfileDB.GetProfile when no profile row exists.
var ErrProfileNotFound = errors.New("profile not found")

// ProfileCoreClient is the consumer-defined interface for UpdateProfile gRPC calls.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type ProfileCoreClient interface {
	UpdateProfile(ctx context.Context, req *pb.UpdateProfileRequest) (*pb.UpdateProfileResponse, error)
}

// ProfileDB is the interface for reading profile data directly from PostgreSQL.
// Defined by the consumer (this handler) per Go interface convention (ADR-009).
// Returns ErrProfileNotFound when no row exists for the given userID.
type ProfileDB interface {
	GetProfile(ctx context.Context, userID string) (*ProfileData, error)
}

// ProfileData holds the public-facing profile fields returned from PostgreSQL.
type ProfileData struct {
	DisplayName string // may be empty
	AvatarURL   string // may be empty
}

// ProfileHandler handles GET/PUT /_matrix/client/v3/profile/{userId}[/displayname|/avatar_url].
type ProfileHandler struct {
	coreClient ProfileCoreClient
	serverName string
	db         ProfileDB
}

// ProfileConfig holds dependencies for NewProfileHandler.
type ProfileConfig struct {
	CoreClient ProfileCoreClient
	ServerName string
	DB         ProfileDB
}

// NewProfileHandler constructs a ProfileHandler from the provided config.
func NewProfileHandler(cfg ProfileConfig) *ProfileHandler {
	return &ProfileHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
		db:         cfg.DB,
	}
}

// GetProfile handles GET /_matrix/client/v3/profile/{userId}.
// No JWT required — public endpoint per Matrix spec.
// Reads directly from the profiles PostgreSQL table (no gRPC round-trip).
//
// Flow:
//  1. Extract userId from r.PathValue("userId").
//  2. Call h.db.GetProfile(r.Context(), userId).
//  3. If not found → 404 M_NOT_FOUND.
//  4. Return 200 {"displayname": "...", "avatar_url": "..."}.
func (h *ProfileHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")

	profile, err := h.db.GetProfile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, ErrProfileNotFound) {
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Profile not found")
			return
		}
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"displayname": profile.DisplayName,
		"avatar_url":  profile.AvatarURL,
	})
}

// PutDisplayname handles PUT /_matrix/client/v3/profile/{userId}/displayname.
// Requires JWT auth. Checks path userId == authenticated user. Validates 1–128 chars.
//
// Flow:
//  1. Extract userId from path.
//  2. Extract sub from JWT context → build authedUserID.
//  3. If userId != authedUserID → 403 M_FORBIDDEN (before any Core call).
//  4. Decode body: {"displayname": "..."}.
//  5. Validate: len(displayname) in [1,128] → 400 M_INVALID_PARAM if not.
//  6. Call gRPC UpdateProfile with displayname set; avatar_url = "".
//  7. Map gRPC errors: PermissionDenied → 403; default → 500.
//  8. Return 200 {}.
func (h *ProfileHandler) PutDisplayname(w http.ResponseWriter, r *http.Request) {
	pathUserID := r.PathValue("userId")

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	authedUserID := userID

	if pathUserID != authedUserID {
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You can only update your own profile")
		return
	}

	var body struct {
		Displayname string `json:"displayname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_NOT_JSON", "Invalid JSON body")
		return
	}

	if utf8.RuneCountInString(body.Displayname) < 1 || utf8.RuneCountInString(body.Displayname) > 128 {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "displayname must be between 1 and 128 characters")
		return
	}

	grpcCtx := coregrpc.WithUserMetadata(r.Context(), authedUserID, systemRole)
	_, err := h.coreClient.UpdateProfile(grpcCtx, &pb.UpdateProfileRequest{
		UserId:      authedUserID,
		Displayname: body.Displayname,
		AvatarUrl:   "",
	})
	if err != nil {
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{})
}

// PutAvatarURL handles PUT /_matrix/client/v3/profile/{userId}/avatar_url.
// Requires JWT auth. Checks path userId == authenticated user. Validates mxc:// prefix.
//
// Flow:
//  1. Extract userId from path.
//  2. Extract sub from JWT context → build authedUserID.
//  3. If userId != authedUserID → 403 M_FORBIDDEN.
//  4. Decode body: {"avatar_url": "..."}.
//  5. Validate: strings.HasPrefix(avatarURL, "mxc://") → 400 M_INVALID_PARAM if not.
//  6. Call gRPC UpdateProfile with avatar_url set; displayname = "".
//  7. Map gRPC errors: PermissionDenied → 403; default → 500.
//  8. Return 200 {}.
func (h *ProfileHandler) PutAvatarURL(w http.ResponseWriter, r *http.Request) {
	pathUserID := r.PathValue("userId")

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	authedUserID := userID

	if pathUserID != authedUserID {
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You can only update your own profile")
		return
	}

	var body struct {
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_NOT_JSON", "Invalid JSON body")
		return
	}

	if !strings.HasPrefix(body.AvatarURL, "mxc://") {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "avatar_url must be an mxc:// URI")
		return
	}

	grpcCtx := coregrpc.WithUserMetadata(r.Context(), authedUserID, systemRole)
	_, err := h.coreClient.UpdateProfile(grpcCtx, &pb.UpdateProfileRequest{
		UserId:      authedUserID,
		Displayname: "",
		AvatarUrl:   body.AvatarURL,
	})
	if err != nil {
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{})
}
