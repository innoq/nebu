package upload

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/coreos/go-oidc/v3/oidc"
	mediacrypto "github.com/nebu/nebu/media/internal/crypto"
	"github.com/nebu/nebu/media/internal/storage"
)

// TokenVerifier abstracts identity extraction from a bearer token.
// Implementations verify the token and return the uploader's subject identity.
// In production, use NewOIDCTokenVerifier to wrap a *oidc.IDTokenVerifier.
// In tests, implement a simple mock that returns a fixed subject string.
// When nil, the handler returns 503 M_UNAVAILABLE (Story 12.8 fail-closed).
type TokenVerifier interface {
	// VerifyToken verifies rawToken and returns the uploader's subject identity.
	// Returns ("", error) on any verification failure.
	VerifyToken(ctx context.Context, rawToken string) (string, error)
}

// OIDCTokenVerifier wraps *oidc.IDTokenVerifier to implement TokenVerifier.
// It extracts the configured OIDC claim from the verified token.
//
// Story 12.11 — SEC Fix F-1: the claim used as the audit trail user identity
// is now operator-configurable via NEBU_OIDC_USER_ID_CLAIM (default: "name").
type OIDCTokenVerifier struct {
	verifier  *oidc.IDTokenVerifier
	claimName string // OIDC claim to use as uploader identity (e.g. "sub", "name", "email")
}

// NewOIDCTokenVerifier creates an OIDCTokenVerifier from an *oidc.IDTokenVerifier.
// claimName specifies which OIDC claim to use as the user identity in audit records.
// Defaults to "name" if empty (matching migration 000044 and the gateway DB default).
func NewOIDCTokenVerifier(v *oidc.IDTokenVerifier, claimName string) *OIDCTokenVerifier {
	if claimName == "" {
		claimName = "name"
	}
	return &OIDCTokenVerifier{verifier: v, claimName: claimName}
}

// VerifyToken implements TokenVerifier using go-oidc/v3 JWT verification.
// Extracts the configured claim (claimName) as the uploader identity.
// Falls back to "sub" with a warning if the configured claim is missing.
func (o *OIDCTokenVerifier) VerifyToken(ctx context.Context, rawToken string) (string, error) {
	idToken, err := o.verifier.Verify(ctx, rawToken)
	if err != nil {
		return "", err
	}
	var rawClaims map[string]interface{}
	if err := idToken.Claims(&rawClaims); err != nil {
		return "", err
	}
	return extractClaimFromMap(rawClaims, o.claimName)
}

// extractClaimFromMap returns the string value of claimName from rawClaims.
//
// Story 12.11 — AC-F1-1..4: configurable claim extraction with sub fallback.
//
// Lookup order:
//  1. If claimName is present in rawClaims and has a non-empty string value,
//     return it.
//  2. If claimName is missing (or empty/non-string), fall back to "sub" with a
//     warning log (AC-F1-4).
//  3. If neither claimName nor "sub" is present, return an error.
//
// This is a pure function (no I/O side effects besides logging) — easy to unit
// test without OIDC infrastructure.
func extractClaimFromMap(rawClaims map[string]interface{}, claimName string) (string, error) {
	// Try the configured claim first.
	if val, ok := rawClaims[claimName]; ok {
		if s, ok := val.(string); ok && s != "" {
			return s, nil
		}
	}

	// Configured claim missing or empty — fall back to "sub".
	if sub, ok := rawClaims["sub"].(string); ok && sub != "" {
		slog.Warn("media upload: configured OIDC claim not found, falling back to sub",
			"configured_claim", claimName)
		return sub, nil
	}

	return "", fmt.Errorf("token missing both configured claim %q and sub", claimName)
}

// blockedContentTypes is the set of MIME types that must not be accepted at upload.
// These types can be rendered as HTML/JavaScript by browsers if served inline,
// enabling stored XSS attacks against the Nebu origin.
// Comparison uses the normalized base type (parameters stripped).
var blockedContentTypes = map[string]bool{
	"text/html":                     true,
	"application/xhtml+xml":         true,
	"text/javascript":               true,
	"application/javascript":        true,
	"image/svg+xml":                 true,
	"application/x-shockwave-flash": true,
}

// MediaStore is the consumer-defined interface for persisting media_files rows.
type MediaStore interface {
	InsertMediaFile(ctx context.Context, row MediaFileRow) error
}

// MediaFileRow holds the data to be written to the media_files table.
type MediaFileRow struct {
	MediaID        string
	ServerName     string
	ContentType    string
	FileSize       int64
	AESKeyHex      string
	NonceHex       string
	UploaderUserID string
	UploadedAt     int64 // Unix ms
}

// HandlerConfig contains configuration for the upload Handler.
type HandlerConfig struct {
	DB           MediaStore
	Storage      storage.Storer // replaces StoragePath — use LocalStorer or MinIOStorer
	ServerName   string         // Matrix server name
	MaxBytes     int64          // NEBU_MEDIA_MAX_UPLOAD_BYTES (default 50 MiB)
	OIDCVerifier TokenVerifier  // Story 12.7: JWT verifier. nil = 503 M_UNAVAILABLE (Story 12.8 fail-closed).
}

// Handler handles POST /_matrix/media/v3/upload.
type Handler struct {
	db           MediaStore
	storage      storage.Storer
	serverName   string
	maxBytes     int64
	oidcVerifier TokenVerifier
}

// NewHandler creates a new upload Handler with the given configuration.
func NewHandler(cfg HandlerConfig) *Handler {
	maxBytes := cfg.MaxBytes
	if maxBytes == 0 {
		maxBytes = 52428800 // 50 MiB default
	}
	return &Handler{
		db:           cfg.DB,
		storage:      cfg.Storage,
		serverName:   cfg.ServerName,
		maxBytes:     maxBytes,
		oidcVerifier: cfg.OIDCVerifier,
	}
}

// matrixError is the standard Matrix error JSON format.
type matrixError struct {
	ErrCode string `json:"errcode"`
	Err     string `json:"error"`
}

// writeError writes a Matrix-format error response.
func writeError(w http.ResponseWriter, statusCode int, errcode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(matrixError{ErrCode: errcode, Err: message})
}

// generateUUID generates a random UUID v4 using crypto/rand.
func generateUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	// Set version 4 and variant bits per RFC 4122.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// formatMatrixUserID builds a canonical Matrix user ID from an OIDC claim value
// and a server name. The localpart is sanitised (lowercase, only [a-z0-9._-] kept,
// spaces replaced with underscore). Returns "@unknown:<serverName>" if sanitise
// produces an empty string.
//
// This mirrors gateway/internal/grpc/metadata.go sanitiseLocalpart but is
// intentionally duplicated to avoid cross-binary coupling between the media
// gateway and the API gateway binaries (Story 12.9).
func formatMatrixUserID(localpart, serverName string) string {
	safe := sanitiseLocalpart(localpart)
	if safe == "" {
		safe = "unknown"
	}
	return "@" + safe + ":" + serverName
}

// sanitiseLocalpart lowercases s and keeps only Matrix-safe characters.
// Returns "" if the result is empty.
func sanitiseLocalpart(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) {
			b.WriteRune('_')
		}
		// drop all other characters
	}
	return b.String()
}

// ServeHTTP implements http.Handler for POST /_matrix/media/v3/upload.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// AC #1 / HIGH-2 — Authentication: require Bearer token.
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Missing or invalid access token")
		return
	}
	rawToken := strings.TrimPrefix(authHeader, "Bearer ")

	var uploaderUserID string
	if h.oidcVerifier != nil {
		// Story 12.7/12.8: JWT verification — signature, expiry, audience, subject extraction.
		// Reject with 401 M_UNKNOWN_TOKEN on any verification failure.
		subject, err := h.oidcVerifier.VerifyToken(r.Context(), rawToken)
		if err != nil {
			slog.Error("media upload: JWT verification failed", "err", err)
			writeError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Invalid or expired access token")
			return
		}
		if subject == "" {
			slog.Error("media upload: JWT subject claim empty")
			writeError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Token missing subject claim")
			return
		}
		// Story 12.9: construct canonical Matrix user ID (@localpart:server) so that
		// media_files.uploader_user_id can be correlated with room events without
		// manual claim-mapping. The raw OIDC claim is the localpart; serverName is
		// from HandlerConfig (set from NEBU_SERVER_NAME env var at startup).
		uploaderUserID = formatMatrixUserID(subject, h.serverName)
	} else {
		// Story 12.8 — fail-closed: nil verifier means OIDC startup failed.
		// Reject with 503 M_UNAVAILABLE. This path must NOT be reachable in production
		// (initOIDCVerifier calls os.Exit before wiring a nil verifier).
		writeError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "OIDC verifier not available")
		return
	}

	// AC #2 — Check Content-Length header before reading body.
	if r.ContentLength > h.maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "M_TOO_LARGE", "Upload exceeds maximum allowed size")
		return
	}

	// AC #3 — Read body with LimitReader; detect oversize via counting.
	limitedBody := io.LimitReader(r.Body, h.maxBytes+1)
	bodyBytes, err := io.ReadAll(limitedBody)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to read request body")
		return
	}
	if int64(len(bodyBytes)) > h.maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "M_TOO_LARGE", "Upload exceeds maximum allowed size")
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// AC3 [HIGH-3] — Reject dangerous Content-Types at upload time.
	// Normalize: strip parameters (e.g. "text/html; charset=utf-8" → "text/html").
	baseType, _, _ := mime.ParseMediaType(contentType)
	if baseType == "" {
		// ParseMediaType failed — use simple split as fallback.
		baseType = strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	}
	baseType = strings.ToLower(baseType)
	if blockedContentTypes[baseType] {
		writeError(w, http.StatusBadRequest, "M_BAD_JSON", "Content-Type not permitted for upload")
		return
	}

	// AC #4.1 — Generate media_id (UUID v4).
	mediaID, err := generateUUID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to generate media ID")
		return
	}

	// AC #4.2 — Generate AES-256 key (32 bytes).
	key, err := mediacrypto.GenerateKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to generate encryption key")
		return
	}

	// AC #4.3 — Generate GCM nonce (12 bytes).
	nonce, err := mediacrypto.GenerateNonce()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to generate nonce")
		return
	}

	// AC #4.4 — Encrypt body bytes with AES-256-GCM.
	ciphertext, err := mediacrypto.Encrypt(bodyBytes, key, nonce)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Encryption failed")
		return
	}

	// AC #4.5 — Write encrypted bytes via Storer under key "<serverName>/<mediaID>".
	storageKey := h.serverName + "/" + mediaID
	if err := h.storage.Put(r.Context(), storageKey, bytes.NewReader(ciphertext), int64(len(ciphertext))); err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to store media file")
		return
	}

	// AC #4.6 — Insert row into media_files table.
	row := MediaFileRow{
		MediaID:        mediaID,
		ServerName:     h.serverName,
		ContentType:    contentType,
		FileSize:       int64(len(bodyBytes)),
		AESKeyHex:      hex.EncodeToString(key),
		NonceHex:       hex.EncodeToString(nonce),
		UploaderUserID: uploaderUserID,
		UploadedAt:     time.Now().UnixMilli(),
	}
	if err := h.db.InsertMediaFile(r.Context(), row); err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to record media metadata")
		return
	}

	// AC #5 — Return 200 with mxc:// content_uri.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"content_uri": fmt.Sprintf("mxc://%s/%s", h.serverName, mediaID),
	})
}
