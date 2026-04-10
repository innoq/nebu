package download

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	mediacrypto "github.com/nebu/nebu/media/internal/crypto"
)

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
	DB          MediaStore
	StoragePath string // NEBU_MEDIA_STORAGE_PATH
}

// Handler handles GET /_matrix/media/v3/download/{serverName}/{mediaId}.
type Handler struct {
	db          MediaStore
	storagePath string
}

// NewHandler creates a new download Handler with the given configuration.
func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{
		db:          cfg.DB,
		storagePath: cfg.StoragePath,
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

	// AC #3 — Read encrypted file from disk.
	filePath := filepath.Join(h.storagePath, serverName, mediaID)
	ciphertext, err := os.ReadFile(filePath)
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
	w.Header().Set("Content-Type", row.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", mediaID))
	w.Header().Set("Content-Length", strconv.Itoa(len(plaintext)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(plaintext)
}

// thumbnailStub returns 501 M_UNRECOGNIZED for all thumbnail requests.
// Thumbnails are Phase 2; the endpoint is registered to avoid 404 confusion.
// AC #6
func thumbnailStub(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"errcode": "M_UNRECOGNIZED",
		"error":   "Thumbnails not supported in this version",
	})
}
