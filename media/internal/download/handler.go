package download

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	mediacrypto "github.com/nebu/nebu/media/internal/crypto"
	"github.com/nebu/nebu/media/internal/storage"
)

// safeInlineContentTypes is the allowlist of MIME types that may be served with
// Content-Disposition: inline. All other types are served as application/octet-stream
// with Content-Disposition: attachment to prevent stored XSS via inline rendering.
// Based on Matrix CS spec v1.12 media safety requirements.
var safeInlineContentTypes = map[string]bool{
	"image/jpeg":       true,
	"image/png":        true,
	"image/gif":        true,
	"image/webp":       true,
	"image/avif":       true,
	"audio/mpeg":       true,
	"audio/ogg":        true,
	"video/mp4":        true,
	"video/webm":       true,
	"application/pdf":  true,
	"text/plain":       true,
}

// MediaStore is the consumer-defined interface for fetching media_files rows.
type MediaStore interface {
	GetMediaFile(ctx context.Context, serverName, mediaID string) (*MediaFileRow, error)
}

// MediaFileRow holds the data read from the media_files table for download.
// Only the fields needed for download are included (consumer-defined, minimal).
type MediaFileRow struct {
	MediaID     string
	ServerName  string
	ContentType string
	AESKeyHex   string // 64 hex chars (32 bytes)
	NonceHex    string // 24 hex chars (12 bytes)
}

// HandlerConfig contains configuration for the download Handler.
type HandlerConfig struct {
	DB      MediaStore
	Storage storage.Storer // replaces StoragePath — use LocalStorer or MinIOStorer
}

// Handler handles GET /_matrix/media/v3/download/{serverName}/{mediaId}.
type Handler struct {
	db      MediaStore
	storage storage.Storer
}

// NewHandler creates a new download Handler with the given configuration.
func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{
		db:      cfg.DB,
		storage: cfg.Storage,
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

// ServeHTTP implements http.Handler for GET /_matrix/media/v3/download/{serverName}/{mediaId}.
// This endpoint is intentionally unauthenticated per Matrix spec.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	serverName := r.PathValue("serverName")
	mediaID := r.PathValue("mediaId")

	// AC #2 — Look up row in media_files; nil means not found → 404.
	row, err := h.db.GetMediaFile(r.Context(), serverName, mediaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to query media store")
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "M_NOT_FOUND", "Media not found")
		return
	}

	// AC #4 — Decode hex key and nonce.
	keyBytes, err := hex.DecodeString(row.AESKeyHex)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Invalid encryption key in store")
		return
	}
	nonceBytes, err := hex.DecodeString(row.NonceHex)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Invalid nonce in store")
		return
	}

	// AC #3 — Read encrypted bytes via Storer under key "<serverName>/<mediaID>".
	// AC3 (Story 12.4): ErrNotFound → 404 M_NOT_FOUND
	// AC4 (Story 12.4): ErrStorageUnavailable or other errors → 502 M_UNKNOWN
	//   The full error is logged for observability; only a generic message is
	//   returned to the client to prevent credential/endpoint leaks.
	storageKey := serverName + "/" + mediaID
	rc, err := h.storage.Get(r.Context(), storageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "M_NOT_FOUND", "Media not found")
			return
		}
		slog.Error("storage.Get failed", "key", storageKey, "err", err)
		writeError(w, http.StatusBadGateway, "M_UNKNOWN", "Media storage unavailable")
		return
	}
	defer rc.Close()

	ciphertext, err := io.ReadAll(rc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to read media file")
		return
	}

	// AC #4 — Decrypt; GCM auth tag failure maps to 500 M_UNKNOWN.
	plaintext, err := mediacrypto.Decrypt(ciphertext, keyBytes, nonceBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to decrypt media file")
		return
	}

	// AC #5 — Write response with correct headers.
	// AC3-5 [HIGH-3]: Always set X-Content-Type-Options: nosniff to prevent MIME sniffing.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// AC-9 (Story 12.16): Matrix CS API §Media Repository SHOULD headers (v1.4+).
	// Content-Security-Policy prevents XSS and plugin execution on downloaded content.
	// Cross-Origin-Resource-Policy: cross-origin allows cross-origin media loading.
	w.Header().Set("Content-Security-Policy",
		"sandbox; default-src 'none'; script-src 'none'; plugin-types application/pdf; style-src 'unsafe-inline'; object-src 'self';")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")

	// AC-6 (Story 12.16): Use the URL path {fileName} for Content-Disposition when present;
	// fall back to mediaID when the route does not include a {fileName} segment.
	cdName := r.PathValue("fileName")
	if cdName == "" {
		cdName = mediaID
	}

	// AC3-4/3-6 [HIGH-3]: Check stored ContentType against safe-inline allowlist.
	// Normalize: strip parameters before lookup.
	storedBase := strings.ToLower(strings.TrimSpace(strings.SplitN(row.ContentType, ";", 2)[0]))
	if safeInlineContentTypes[storedBase] {
		// Safe type: serve inline with original Content-Type.
		w.Header().Set("Content-Type", row.ContentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", cdName))
	} else {
		// Unsafe type: force octet-stream + attachment to prevent inline rendering.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", cdName))
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(plaintext)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(plaintext)
}

