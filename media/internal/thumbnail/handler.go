package thumbnail

// handler.go — HTTP handler for GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}
//
// Story 12.5: Thumbnail Generation — On-Demand, Sandboxed
//
// Handler flow:
//  1. Validate width + height query params (required, integer, > 0)
//  2. Validate mediaId charset (A-Za-z0-9_- only, per Matrix spec security section)
//  3. DB lookup via MediaStore → 404 M_NOT_FOUND if nil
//  4. storage.Get → ErrNotFound→404, ErrStorageUnavailable→502
//  5. Decrypt AES-256-GCM ciphertext
//  6. Detect MIME type from magic bytes (NOT Content-Type header)
//  7. Reject unsupported types → 400 M_BAD_JSON
//  8. Generate thumbnail (animated GIF path or static JPEG path)
//  9. Set Cache-Control: max-age=86400, Content-Type, Content-Disposition: inline
// 10. Write 200 + thumbnail bytes

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	mediacrypto "github.com/nebu/nebu/media/internal/crypto"
	"github.com/nebu/nebu/media/internal/storage"
)

// mediaIDPattern matches valid Matrix media IDs: alphanumeric, underscore, hyphen only.
// Spec v1.18 security section: "allowing only alphanumeric (A-Za-z0-9), _ and - characters
// in the media-id segment."
var mediaIDPattern = regexp.MustCompile(`^[A-Za-z0-9_\-]+$`)

// MediaStore is the thumbnail handler's consumer-defined interface for DB access.
// pgMediaStore in main.go satisfies this structurally (same shape as download.MediaStore).
type MediaStore interface {
	GetMediaFile(ctx context.Context, serverName, mediaID string) (*MediaFileRow, error)
}

// MediaFileRow holds the data read from media_files for thumbnail generation.
// Consumer-defined — only the fields needed by this handler are included.
type MediaFileRow struct {
	MediaID     string
	ServerName  string
	ContentType string // stored at upload time; NOT used for MIME detection (use magic bytes)
	AESKeyHex   string // 64 hex chars (32 bytes)
	NonceHex    string // 24 hex chars (12 bytes)
}

// HandlerConfig contains configuration for the thumbnail Handler.
type HandlerConfig struct {
	DB      MediaStore
	Storage storage.Storer
}

// Handler handles GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}.
type Handler struct {
	db      MediaStore
	storage storage.Storer
}

// NewHandler creates a new thumbnail Handler with the given configuration.
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
// Defined locally — thumbnail package must not depend on sibling packages (download, upload).
func writeError(w http.ResponseWriter, statusCode int, errcode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(matrixError{ErrCode: errcode, Err: message})
}

// ServeHTTP implements http.Handler for GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}.
// Unauthenticated per Matrix spec (deprecated v3 endpoint).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	serverName := r.PathValue("serverName")
	mediaID := r.PathValue("mediaId")

	// maxThumbDim is the maximum allowed width or height for a thumbnail request.
	// Requests above this cap are rejected with 400 M_BAD_JSON to prevent
	// memory-amplification DoS (imaging allocates 4 * width * height bytes per request).
	const maxThumbDim = 2048

	// Step 1: Validate width (required, integer, > 0, ≤ maxThumbDim).
	widthStr := r.URL.Query().Get("width")
	if widthStr == "" {
		writeError(w, http.StatusBadRequest, "M_BAD_JSON", "width and height query parameters are required")
		return
	}
	width, err := strconv.Atoi(widthStr)
	if err != nil || width <= 0 {
		writeError(w, http.StatusBadRequest, "M_BAD_JSON", "width must be a positive integer")
		return
	}
	if width > maxThumbDim {
		writeError(w, http.StatusBadRequest, "M_BAD_JSON", "width exceeds maximum allowed value of 2048")
		return
	}

	// Step 1: Validate height (required, integer, > 0, ≤ maxThumbDim).
	heightStr := r.URL.Query().Get("height")
	if heightStr == "" {
		writeError(w, http.StatusBadRequest, "M_BAD_JSON", "width and height query parameters are required")
		return
	}
	height, err := strconv.Atoi(heightStr)
	if err != nil || height <= 0 {
		writeError(w, http.StatusBadRequest, "M_BAD_JSON", "height must be a positive integer")
		return
	}
	if height > maxThumbDim {
		writeError(w, http.StatusBadRequest, "M_BAD_JSON", "height exceeds maximum allowed value of 2048")
		return
	}

	// Parse method (default: "scale").
	method := r.URL.Query().Get("method")
	if method == "" {
		method = "scale"
	}

	// Parse animated (default: false).
	animated := false
	if animStr := r.URL.Query().Get("animated"); animStr == "true" {
		animated = true
	}

	// Step 2: Validate mediaId charset.
	// Spec MUST: allow only A-Za-z0-9, _ and - to prevent path traversal.
	if !mediaIDPattern.MatchString(mediaID) {
		writeError(w, http.StatusBadRequest, "M_BAD_JSON", "invalid media ID format")
		return
	}

	// Step 3: DB lookup.
	row, err := h.db.GetMediaFile(r.Context(), serverName, mediaID)
	if err != nil {
		slog.Error("thumbnail: db.GetMediaFile failed", "serverName", serverName, "mediaID", mediaID, "err", err)
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to query media store")
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "M_NOT_FOUND", "Media not found")
		return
	}

	// Step 4: Decode hex key and nonce.
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

	// Step 4: Read encrypted bytes from storage.
	storageKey := serverName + "/" + mediaID
	rc, err := h.storage.Get(r.Context(), storageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "M_NOT_FOUND", "Media not found")
			return
		}
		slog.Error("thumbnail: storage.Get failed", "key", storageKey, "err", err)
		writeError(w, http.StatusBadGateway, "M_UNKNOWN", "Media storage unavailable")
		return
	}
	defer rc.Close()

	ciphertext, err := io.ReadAll(rc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to read media file")
		return
	}

	// Step 5: Decrypt AES-256-GCM.
	plaintext, err := mediacrypto.Decrypt(ciphertext, keyBytes, nonceBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to decrypt media file")
		return
	}

	// Step 6: Detect MIME type from magic bytes (NOT from row.ContentType).
	mimeType := DetectMIMEType(plaintext)

	// Step 7: Reject unsupported MIME types.
	if !AllowedMIMETypes[mimeType] {
		writeError(w, http.StatusBadRequest, "M_BAD_JSON", "Unsupported media type for thumbnail generation")
		return
	}

	// Step 8: Generate thumbnail.
	params := ThumbnailParams{
		Width:    width,
		Height:   height,
		Method:   method,
		Animated: animated,
	}
	thumbBytes, contentType, err := GenerateThumbnail(plaintext, params)
	if err != nil {
		slog.Error("thumbnail: GenerateThumbnail failed", "key", storageKey, "err", err)
		writeError(w, http.StatusInternalServerError, "M_UNKNOWN", "Failed to generate thumbnail")
		return
	}

	// Step 9: Write response headers.
	// X-Content-Type-Options: nosniff — prevent MIME sniffing in legacy browsers (HIGH-3, Story 12.7).
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Content-Type: REQUIRED per spec v1.12.
	w.Header().Set("Content-Type", contentType)
	// Content-Disposition: REQUIRED per spec v1.12; MUST be inline; SHOULD contain filename.
	ext := extensionForContentType(contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", "thumbnail"+ext))
	// Cache-Control: max-age=86400 per AC5.
	w.Header().Set("Cache-Control", "max-age=86400")

	// Step 10: Write 200 + thumbnail bytes.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(thumbBytes)
}

// extensionForContentType returns the file extension for a given image MIME type.
func extensionForContentType(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}
