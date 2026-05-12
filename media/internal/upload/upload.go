package upload

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	mediacrypto "github.com/nebu/nebu/media/internal/crypto"
	"github.com/nebu/nebu/media/internal/storage"
)

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
	DB         MediaStore
	Storage    storage.Storer // replaces StoragePath — use LocalStorer or MinIOStorer
	ServerName string         // Matrix server name
	MaxBytes   int64          // NEBU_MEDIA_MAX_UPLOAD_BYTES (default 50 MiB)
}

// Handler handles POST /_matrix/media/v3/upload.
type Handler struct {
	db         MediaStore
	storage    storage.Storer
	serverName string
	maxBytes   int64
}

// NewHandler creates a new upload Handler with the given configuration.
func NewHandler(cfg HandlerConfig) *Handler {
	maxBytes := cfg.MaxBytes
	if maxBytes == 0 {
		maxBytes = 52428800 // 50 MiB default
	}
	return &Handler{
		db:         cfg.DB,
		storage:    cfg.Storage,
		serverName: cfg.ServerName,
		maxBytes:   maxBytes,
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

// ServeHTTP implements http.Handler for POST /_matrix/media/v3/upload.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// AC #1 — Authentication: require Bearer token presence.
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Missing or invalid access token")
		return
	}

	// Extract sub from token — for MVP, use the raw token value as user ID.
	// A real implementation would validate the JWT; here we accept any bearer.
	uploaderUserID := strings.TrimPrefix(authHeader, "Bearer ")

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
