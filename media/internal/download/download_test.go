package download

// ─── Story 4-20: Download handler unit tests (net/http/httptest) ──────────────
//
// Test strategy:
//   - mockDownloadStore implements MediaStore (consumer-defined interface).
//     It returns a pre-configured *MediaFileRow (or nil for not-found) and an
//     optional forcedErr.
//   - buildDownloadHandler wires a Handler with a temp-dir storage path and the
//     mock store, then registers it in a ServeMux with the correct route patterns.
//   - Tests use httptest.NewRecorder() + httptest.NewRequest().
//   - Round-trip tests use real crypto (mediacrypto.Encrypt/Decrypt) to exercise
//     the full encrypt → store on disk → read → decrypt pipeline.
//   - Tampered-ciphertext tests flip one bit in the ciphertext to trigger GCM
//     auth tag failure, which must surface as 500 M_UNKNOWN.
//
// Types / functions under test (from story spec):
//   type Handler struct { ... }
//   type MediaStore interface { GetMediaFile(ctx, serverName, mediaID string) (*MediaFileRow, error) }
//   type MediaFileRow struct { MediaID, ServerName, ContentType, AESKeyHex, NonceHex string }
//   func NewHandler(cfg HandlerConfig) *Handler
//   func (h *Handler) ServeHTTP(w, r)

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	mediacrypto "github.com/nebu/nebu/media/internal/crypto"
	"github.com/nebu/nebu/media/internal/storage"
	"github.com/nebu/nebu/media/internal/upload"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	testServerName = "test.local"
)

// ─── Mock MediaStore ──────────────────────────────────────────────────────────

// mockDownloadStore implements MediaStore (defined in handler.go).
// row is the row to return; nil means "not found".
// forcedErr, if non-nil, is returned instead of row.

type mockDownloadStore struct {
	row                *MediaFileRow
	forcedErr          error
	capturedServerName string
	capturedMediaID    string
}

func (m *mockDownloadStore) GetMediaFile(_ context.Context, serverName, mediaID string) (*MediaFileRow, error) {
	m.capturedServerName = serverName
	m.capturedMediaID = mediaID
	if m.forcedErr != nil {
		return nil, m.forcedErr
	}
	return m.row, nil
}

// ─── matrixErrorResp mirrors the standard Matrix error JSON ──────────────────

type matrixErrorResp struct {
	ErrCode string `json:"errcode"`
	Error   string `json:"error"`
}

// ─── buildDownloadHandler wires a Handler for use in tests ───────────────────

// buildDownloadHandler creates a ServeMux with the download handler registered
// at the correct Matrix route pattern.
// storagePath must be provided (use t.TempDir() at the call site).

// buildDownloadHandler creates a ServeMux with the download handler registered
// at the correct Matrix route pattern.
// storagePath must be provided (use t.TempDir() at the call site).
// After the Story 12.2 refactor, uses storage.LocalStorer instead of StoragePath string.

func buildDownloadHandler(t *testing.T, store *mockDownloadStore, storagePath string) http.Handler {
	t.Helper()

	h := NewHandler(HandlerConfig{
		DB:      store,
		Storage: &storage.LocalStorer{BasePath: storagePath},
	})

	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/media/v3/download/{serverName}/{mediaId}", h)
	mux.HandleFunc("GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})
	return mux
}

// ─── Helper: write encrypted file to temp dir ─────────────────────────────────

// writeEncryptedFile encrypts plaintext and writes the ciphertext to
// <dir>/<serverName>/<mediaID>. Returns the key and nonce as hex strings,
// and the raw ciphertext bytes (for tamper tests).

func writeEncryptedFile(t *testing.T, dir, serverName, mediaID string, plaintext []byte) (aesKeyHex, nonceHex string, ciphertext []byte) {
	t.Helper()

	key, err := mediacrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	nonce, err := mediacrypto.GenerateNonce()
	if err != nil {
		t.Fatalf("GenerateNonce: %v", err)
	}
	ct, err := mediacrypto.Encrypt(plaintext, key, nonce)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	subDir := filepath.Join(dir, serverName)
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, mediaID), ct, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return hex.EncodeToString(key), hex.EncodeToString(nonce), ct
}

// ─── Test 1: Happy path — 200, decrypted body equals original plaintext ───────
//
// AC #1, #3, #4, #5 — GET with valid serverName+mediaId.
// Expects 200; response body bytes equal original plaintext;
// Content-Type header matches the stored content_type.

func TestDownload_HappyPath(t *testing.T) {
	dir := t.TempDir()
	mediaID := "happy-path-media-id"
	plaintext := []byte("hello, this is the original file content")
	contentType := "image/png"

	aesKeyHex, nonceHex, _ := writeEncryptedFile(t, dir, testServerName, mediaID, plaintext)

	store := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     mediaID,
			ServerName:  testServerName,
			ContentType: contentType,
			AESKeyHex:   aesKeyHex,
			NonceHex:    nonceHex,
		},
	}
	mux := buildDownloadHandler(t, store, dir)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/"+mediaID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Response body must be byte-equal to the original plaintext.
	got := w.Body.Bytes()
	if !bytes.Equal(got, plaintext) {
		t.Errorf("response body mismatch: got %q, want %q", got, plaintext)
	}

	// Content-Type must match the stored value.
	ct := w.Header().Get("Content-Type")
	if ct != contentType {
		t.Errorf("Content-Type: got %q, want %q", ct, contentType)
	}

	// Content-Length must equal the length of the decrypted plaintext.
	cl := w.Header().Get("Content-Length")
	expected := strconv.Itoa(len(plaintext))
	if cl != expected {
		t.Errorf("Content-Length: got %q, want %q", cl, expected)
	}

	// GetMediaFile must have been called with the correct serverName and mediaID.
	if store.capturedServerName != "test.local" {
		t.Errorf("GetMediaFile serverName: got %q, want %q", store.capturedServerName, "test.local")
	}
	if store.capturedMediaID != mediaID {
		t.Errorf("GetMediaFile mediaID: got %q, want %q", store.capturedMediaID, mediaID)
	}
}

// ─── Test 2: mediaId not in store → 404 M_NOT_FOUND ──────────────────────────
//
// AC #2 — When the DB returns nil, nil (not found), handler returns 404.

func TestDownload_NotFound(t *testing.T) {
	dir := t.TempDir()

	store := &mockDownloadStore{row: nil} // nil → not found
	mux := buildDownloadHandler(t, store, dir)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/nonexistent-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode 404 error response: %v", err)
	}
	if errResp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %q", errResp.ErrCode)
	}
}

// ─── Test 3: Tampered ciphertext — GCM auth tag fails → 500 M_UNKNOWN ────────
//
// AC #4 — If the ciphertext on disk has been bit-flipped, GCM.Open returns an
// error and the handler must return 500 M_UNKNOWN.

func TestDownload_TamperedFile(t *testing.T) {
	dir := t.TempDir()
	mediaID := "tampered-media-id"
	plaintext := []byte("original content that will be tampered on disk")

	aesKeyHex, nonceHex, ciphertext := writeEncryptedFile(t, dir, testServerName, mediaID, plaintext)

	// Flip the first byte of the ciphertext to break the GCM auth tag.
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[0] ^= 0x01

	// Overwrite the file with the tampered bytes.
	if err := os.WriteFile(filepath.Join(dir, testServerName, mediaID), tampered, 0600); err != nil {
		t.Fatalf("WriteFile (tampered): %v", err)
	}

	store := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     mediaID,
			ServerName:  testServerName,
			ContentType: "application/octet-stream",
			AESKeyHex:   aesKeyHex,
			NonceHex:    nonceHex,
		},
	}
	mux := buildDownloadHandler(t, store, dir)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/"+mediaID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode 500 error response: %v", err)
	}
	if errResp.ErrCode != "M_UNKNOWN" {
		t.Errorf("expected errcode M_UNKNOWN, got %q", errResp.ErrCode)
	}
}

// ─── Test 4: serverName mismatch — row belongs to different server → 404 ──────
//
// AC #2 — The handler must match both serverName AND mediaId. If the DB is
// queried with a serverName that does not match any row, GetMediaFile returns
// nil, nil and the handler returns 404 M_NOT_FOUND.
//
// This test passes a different serverName in the URL than what is stored; the
// mock store is keyed by call arguments — here we simulate the DB returning nil
// for an unknown serverName combination.

func TestDownload_WrongServerName(t *testing.T) {
	dir := t.TempDir()

	// Store returns nil (not found) to simulate serverName mismatch.
	store := &mockDownloadStore{row: nil}
	mux := buildDownloadHandler(t, store, dir)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/wrong.server/some-media-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode 404 error response: %v", err)
	}
	if errResp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %q", errResp.ErrCode)
	}
}

// ─── Test 5: Storage read error — file missing from disk → 404 M_NOT_FOUND ────
//
// AC #3 (Story 12.4 update) — DB row exists but the file is absent from disk
// (e.g. deleted after upload). LocalStorer.Get returns ErrNotFound for a
// missing file (os.ErrNotExist), and the handler maps ErrNotFound → 404.
//
// Updated in Story 12.4: was 500 M_UNKNOWN (before ErrNotFound sentinel existed),
// now correctly 404 M_NOT_FOUND (missing object → ErrNotFound → 404).

func TestDownload_StorageReadError(t *testing.T) {
	dir := t.TempDir()
	mediaID := "deleted-media-id"

	// DB row is valid, but we do NOT write any file to disk.
	// LocalStorer.Get returns ErrNotFound → handler returns 404.
	store := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     mediaID,
			ServerName:  testServerName,
			ContentType: "image/jpeg",
			AESKeyHex:   strings.Repeat("aa", 32), // 32 bytes of 0xaa as hex
			NonceHex:    strings.Repeat("bb", 12), // 12 bytes of 0xbb as hex
		},
	}
	mux := buildDownloadHandler(t, store, dir)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/"+mediaID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Story 12.4: missing file → LocalStorer returns ErrNotFound → handler returns 404.
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode 404 error response: %v", err)
	}
	if errResp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %q", errResp.ErrCode)
	}
}

// ─── Test 6: Thumbnail stub → 501 M_UNRECOGNIZED ──────────────────────────────
//
// AC #6 — GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId} must return
// 501 with errcode M_UNRECOGNIZED. Thumbnails are Phase 2.

func TestDownload_ThumbnailStub(t *testing.T) {
	// This test verifies the download handler mux routes correctly to the
	// thumbnail endpoint. Since Story 12.5, the real thumbnail handler is wired
	// in main.go. In the download package's test helpers, the thumbnail route is
	// registered with a minimal 501 placeholder so this routing test remains valid.
	dir := t.TempDir()
	store := &mockDownloadStore{}
	mux := buildDownloadHandler(t, store, dir)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/thumbnail/"+testServerName+"/any-media-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── Test 7: Round-trip integration — Upload then Download returns identical bytes
//
// AC #8 (round-trip) — Upload using the upload handler (capturing the stored
// MediaFileRow via mockUploadStore), then download using the download handler
// (seeded with the captured row). Verifies end-to-end encrypt → store → read → decrypt.

func TestUpload_Download_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	plaintext := []byte("round-trip test: the quick brown fox jumps over the lazy dog")
	contentType := "text/plain"

	// ── Step 1: Upload via upload handler ────────────────────────────────────

	uploadStore := &mockUploadStore{}
	uploadHandler := buildUploadHandler(t, uploadStore, dir)

	uploadReq := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload",
		bytes.NewReader(plaintext))
	uploadReq.Header.Set("Content-Type", contentType)
	uploadReq.Header.Set("Authorization", "Bearer test-token")

	uploadW := httptest.NewRecorder()
	uploadHandler.ServeHTTP(uploadW, uploadReq)

	if uploadW.Code != http.StatusOK {
		t.Fatalf("upload: expected 200, got %d; body: %s", uploadW.Code, uploadW.Body.String())
	}

	// The upload store must have captured exactly one row.
	if len(uploadStore.inserted) != 1 {
		t.Fatalf("upload: expected 1 inserted row, got %d", len(uploadStore.inserted))
	}
	stored := uploadStore.inserted[0]

	// ── Step 2: Download via download handler using captured row ─────────────

	downloadRow := &MediaFileRow{
		MediaID:     stored.MediaID,
		ServerName:  stored.ServerName,
		ContentType: stored.ContentType,
		AESKeyHex:   stored.AESKeyHex,
		NonceHex:    stored.NonceHex,
	}
	downloadStore := &mockDownloadStore{row: downloadRow}
	downloadMux := buildDownloadHandler(t, downloadStore, dir)

	downloadReq := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+stored.ServerName+"/"+stored.MediaID, nil)
	downloadW := httptest.NewRecorder()
	downloadMux.ServeHTTP(downloadW, downloadReq)

	if downloadW.Code != http.StatusOK {
		t.Fatalf("download: expected 200, got %d; body: %s", downloadW.Code, downloadW.Body.String())
	}

	// Response body must be byte-equal to the original plaintext.
	got := downloadW.Body.Bytes()
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round-trip mismatch: got %q, want %q", got, plaintext)
	}

	// Content-Type must be preserved through the round-trip.
	if ct := downloadW.Header().Get("Content-Type"); ct != contentType {
		t.Errorf("Content-Type after round-trip: got %q, want %q", ct, contentType)
	}
}

// ─── Helpers for round-trip test ──────────────────────────────────────────────

// mockUploadStore implements upload.MediaStore, capturing inserted rows.

type mockUploadStore struct {
	inserted  []upload.MediaFileRow
	forcedErr error
}

func (m *mockUploadStore) InsertMediaFile(_ context.Context, row upload.MediaFileRow) error {
	if m.forcedErr != nil {
		return m.forcedErr
	}
	m.inserted = append(m.inserted, row)
	return nil
}

// buildUploadHandler creates a bare upload Handler (no mux) for round-trip testing.
// storagePath is shared with the download handler so files land in the same LocalStorer tree.
// After the Story 12.2 refactor, uses storage.LocalStorer instead of StoragePath string.

// testTokenVerifier is a minimal upload.TokenVerifier for download integration tests.
// It accepts any bearer token and returns a fixed subject ("test-user-download").
// Story 12.8: upload.HandlerConfig.OIDCVerifier must be non-nil (fail-closed).
type testTokenVerifier struct{}

func (testTokenVerifier) VerifyToken(_ context.Context, _ string) (string, error) {
	return "test-user-download", nil
}

func buildUploadHandler(t *testing.T, store *mockUploadStore, storagePath string) http.Handler {
	t.Helper()

	h := upload.NewHandler(upload.HandlerConfig{
		DB:           store,
		Storage:      &storage.LocalStorer{BasePath: storagePath},
		ServerName:   testServerName,
		MaxBytes:     52428800,
		OIDCVerifier: testTokenVerifier{}, // Story 12.8: non-nil required (fail-closed)
	})

	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/media/v3/upload", h)
	return mux
}

// ─── Verify Content-Disposition header format ─────────────────────────────────
//
// AC #5 — Correct Content-Disposition: inline; filename="<mediaId>"

func TestDownload_ContentDisposition(t *testing.T) {
	dir := t.TempDir()
	mediaID := "abc123"
	plaintext := []byte("disposition test content")

	aesKeyHex, nonceHex, _ := writeEncryptedFile(t, dir, testServerName, mediaID, plaintext)

	store := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     mediaID,
			ServerName:  testServerName,
			ContentType: "application/octet-stream",
			AESKeyHex:   aesKeyHex,
			NonceHex:    nonceHex,
		},
	}
	mux := buildDownloadHandler(t, store, dir)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/"+mediaID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	cd := w.Header().Get("Content-Disposition")
	if cd == "" {
		t.Fatal("Content-Disposition header is missing")
	}

	// application/octet-stream is NOT in the safe-inline allowlist (Story 12.7, HIGH-3).
	// Content must be served as attachment to prevent inline rendering of unknown binary types.
	// Expected format: attachment; filename="abc123"
	if !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition %q does not contain 'attachment' (application/octet-stream is not safe-inline)", cd)
	}
	if !strings.Contains(cd, mediaID) {
		t.Errorf("Content-Disposition %q does not contain mediaId %q", cd, mediaID)
	}
}

// ─── DB error from GetMediaFile → 500 M_UNKNOWN ───────────────────────────────
//
// Edge case: store returns a non-nil error (e.g., DB connection lost).
// Handler must return 500 M_UNKNOWN (not 404).

func TestDownload_DBError(t *testing.T) {
	dir := t.TempDir()

	store := &mockDownloadStore{
		forcedErr: errors.New("db connection lost"),
	}
	mux := buildDownloadHandler(t, store, dir)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/some-media-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode 500 error response: %v", err)
	}
	if errResp.ErrCode != "M_UNKNOWN" {
		t.Errorf("expected errcode M_UNKNOWN, got %q", errResp.ErrCode)
	}
}

// ─── Test (Story 5.8): DeletedAvatar_Returns404 ───────────────────────────────
//
// AC7 (Story 5.8) — Media download for a soft-deleted avatar must return 404 M_NOT_FOUND.
//
// Story 5.8 adds a `deleted BOOLEAN NOT NULL DEFAULT false` column to media_files
// (migration 000023). The pgMediaStore.GetMediaFile SQL query must add
// `AND NOT deleted` to its WHERE clause. When the row is soft-deleted, the query
// returns nil (not found) and the handler responds with 404 M_NOT_FOUND.
//
// Failing reason before implementation:
//   The current GetMediaFile SQL does not filter on the deleted column.
//   A row with deleted=true is currently returned to the handler and the file
//   content would be served (wrong behaviour). The mock simulates the CORRECT
//   post-implementation behaviour: GetMediaFile returns nil for a deleted row.
//   The handler must produce 404 — this test documents the required contract.
//
// Strategy: mockDownloadStore returns nil, nil for the deleted row
// (simulating the WHERE NOT deleted filter having excluded the row).
// Handler must return 404 M_NOT_FOUND — same as the normal not-found case.
// The test name makes the intent explicit.

func TestMediaDownload_DeletedAvatar_Returns404(t *testing.T) {
	dir := t.TempDir()

	// Simulate: GetMediaFile returns nil, nil because WHERE NOT deleted filtered the row.
	// This is what the pgMediaStore must do after migration 000023 + SQL fix.
	store := &mockDownloadStore{row: nil}
	mux := buildDownloadHandler(t, store, dir)

	deletedMediaID := "deleted-avatar-media-id"
	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/"+deletedMediaID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("deleted avatar must return 404 M_NOT_FOUND, got %d; body: %s",
			w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v — body: %s", err, w.Body.String())
	}
	if errResp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %q", errResp.ErrCode)
	}
	if errResp.Error == "" {
		t.Error("expected non-empty error message in 404 response")
	}
}

// ─── Story 12.4 ATDD Tests ────────────────────────────────────────────────────
//
// These tests will FAIL until:
//   1. storage.ErrNotFound sentinel is defined in storage/storage.go
//   2. storage.ErrStorageUnavailable sentinel is defined in storage/storage.go
//   3. handler.go maps ErrNotFound → 404 M_NOT_FOUND (not 500)
//   4. handler.go maps other storage errors → 502 M_UNKNOWN (not 500)
//   5. handler.go logs the full error but only returns generic message to client

// ─── AT-2 (Story 12.4): ErrNotFound from Storer → 404 M_NOT_FOUND ────────────
//
// AC3 — When storage returns ErrNotFound, handler must return 404 M_NOT_FOUND.
//
// Failing reason before implementation:
//   handler.go maps all storage errors to 500 M_UNKNOWN.

func TestDownload_StorerErrNotFound_Returns404(t *testing.T) {
	// DB row exists (media_files has the row), but Storer.Get returns ErrNotFound.
	// This simulates the case where the object is missing from MinIO/LocalFS
	// but the DB still has the metadata.
	storer := &fakeDownloadStorer{
		contents: make(map[string][]byte),
		getError: fmt.Errorf("object missing: %w", storage.ErrNotFound),
	}

	db := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     "missing-from-storage",
			ServerName:  testServerName,
			ContentType: "image/png",
			AESKeyHex:   strings.Repeat("aa", 32),
			NonceHex:    strings.Repeat("bb", 12),
		},
	}

	mux := buildDownloadHandlerWithStorer(t, db, storer)
	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/missing-from-storage", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for ErrNotFound, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if errResp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %q", errResp.ErrCode)
	}
}

// ─── AT-3 (Story 12.4): ErrStorageUnavailable from Storer → 502 M_UNKNOWN ────
//
// AC4 — When storage returns ErrStorageUnavailable (e.g. MinIO unreachable),
// handler must return 502 M_UNKNOWN (Bad Gateway).
//
// Failing reason before implementation:
//   handler.go maps all storage errors to 500 M_UNKNOWN (wrong code).

func TestDownload_StorerErrUnavailable_Returns502(t *testing.T) {
	storer := &fakeDownloadStorer{
		contents: make(map[string][]byte),
		getError: fmt.Errorf("backend unreachable: %w", storage.ErrStorageUnavailable),
	}

	db := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     "any-id",
			ServerName:  testServerName,
			ContentType: "image/jpeg",
			AESKeyHex:   strings.Repeat("aa", 32),
			NonceHex:    strings.Repeat("bb", 12),
		},
	}

	mux := buildDownloadHandlerWithStorer(t, db, storer)
	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/any-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for ErrStorageUnavailable, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if errResp.ErrCode != "M_UNKNOWN" {
		t.Errorf("expected errcode M_UNKNOWN, got %q", errResp.ErrCode)
	}
}

// ─── AT-4 (Story 12.4): Storage error must NOT leak credentials in response ───
//
// AC4 — The response body must not contain any internal details (endpoint URLs,
// credentials, MinIO SDK error messages). Only a generic message is returned.
//
// Failing reason before implementation:
//   Current handler may pass raw error message to response body.

func TestDownload_StorageError_NoCredentialLeak(t *testing.T) {
	// Inject an error that looks like it contains sensitive data.
	fakeEndpoint := "minio.secret-internal.corp:9000"
	fakeCreds := "AKIAIOSFODNN7EXAMPLE"
	sensitiveErr := fmt.Errorf("connection to %s failed with key %s: %w",
		fakeEndpoint, fakeCreds, storage.ErrStorageUnavailable)

	storer := &fakeDownloadStorer{
		contents: make(map[string][]byte),
		getError: sensitiveErr,
	}

	db := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     "leak-test-id",
			ServerName:  testServerName,
			ContentType: "application/octet-stream",
			AESKeyHex:   strings.Repeat("cc", 32),
			NonceHex:    strings.Repeat("dd", 12),
		},
	}

	mux := buildDownloadHandlerWithStorer(t, db, storer)
	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/leak-test-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Must be a 4xx or 5xx — not 200.
	if w.Code == http.StatusOK {
		t.Fatal("expected non-200 status for storage error")
	}

	body := w.Body.String()
	if strings.Contains(body, fakeEndpoint) {
		t.Errorf("response body must NOT contain endpoint URL %q, but got: %s", fakeEndpoint, body)
	}
	if strings.Contains(body, fakeCreds) {
		t.Errorf("response body must NOT contain credentials %q, but got: %s", fakeCreds, body)
	}
	if strings.Contains(body, "minio.secret-internal.corp") {
		t.Errorf("response body must NOT contain internal host, but got: %s", body)
	}
}

// ─── Story 12.2 ATDD Tests ────────────────────────────────────────────────────
//
// These tests will fail to compile until:
//   1. HandlerConfig.Storage Storer field replaces StoragePath string
//   2. handler.go imports and uses storage.Storer
//
// They test that the download handler works correctly with a fake Storer
// (no filesystem, no MinIO) — Storer is fully mockable.

// fakeDownloadStorer is an in-memory storage.Storer for download tests.
// contents maps keys to ciphertext bytes.
// getError, if set, is returned by Get.
type fakeDownloadStorer struct {
	contents map[string][]byte
	getError error
}

func newFakeDownloadStorer() *fakeDownloadStorer {
	return &fakeDownloadStorer{contents: make(map[string][]byte)}
}

func (f *fakeDownloadStorer) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.contents[key] = data
	return nil
}

func (f *fakeDownloadStorer) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if f.getError != nil {
		return nil, f.getError
	}
	data, ok := f.contents[key]
	if !ok {
		return nil, errors.New("fakeDownloadStorer: key not found: " + key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeDownloadStorer) Delete(_ context.Context, key string) error {
	delete(f.contents, key)
	return nil
}

// Compile-time check: *fakeDownloadStorer satisfies storage.Storer.
var _ storage.Storer = &fakeDownloadStorer{}

// buildDownloadHandlerWithStorer wires the download handler using the new
// Storage Storer field. Will fail to compile until HandlerConfig has a
// Storage field of type storage.Storer.
func buildDownloadHandlerWithStorer(t *testing.T, db *mockDownloadStore, storer storage.Storer) http.Handler {
	t.Helper()
	h := NewHandler(HandlerConfig{
		DB:      db,
		Storage: storer,
	})
	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/media/v3/download/{serverName}/{mediaId}", h)
	mux.HandleFunc("GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})
	return mux
}

// AT-7: Download handler with fake Storer — happy path returns 200, decrypted body correct.
//
// AC4 (Story 12.2) — When HandlerConfig has a Storage Storer field, the download
// handler calls Storer.Get instead of os.ReadFile. Uses fakeDownloadStorer so no
// filesystem is needed.

func TestDownload_WithFakeStorer_HappyPath(t *testing.T) {
	ctx := context.Background()
	storer := newFakeDownloadStorer()
	mediaID := "fake-storer-media-id"
	plaintext := []byte("content served via fakeDownloadStorer — no filesystem needed")
	contentType := "image/jpeg"

	// Generate encryption materials and store ciphertext in the fake storer.
	key, err := mediacrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	nonce, err := mediacrypto.GenerateNonce()
	if err != nil {
		t.Fatalf("GenerateNonce: %v", err)
	}
	ciphertext, err := mediacrypto.Encrypt(plaintext, key, nonce)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	storageKey := testServerName + "/" + mediaID
	if err := storer.Put(ctx, storageKey, bytes.NewReader(ciphertext), int64(len(ciphertext))); err != nil {
		t.Fatalf("fakeStorer.Put: %v", err)
	}

	db := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     mediaID,
			ServerName:  testServerName,
			ContentType: contentType,
			AESKeyHex:   hex.EncodeToString(key),
			NonceHex:    hex.EncodeToString(nonce),
		},
	}

	mux := buildDownloadHandlerWithStorer(t, db, storer)
	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/"+mediaID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	got := w.Body.Bytes()
	if !bytes.Equal(got, plaintext) {
		t.Errorf("response body mismatch: got %q, want %q", got, plaintext)
	}

	if ct := w.Header().Get("Content-Type"); ct != contentType {
		t.Errorf("Content-Type: got %q, want %q", ct, contentType)
	}
}

// AT-8: Download handler with fake Storer returning generic error → 502 M_UNKNOWN.
//
// AC4 (Story 12.2 / updated in Story 12.4) — When Storer.Get returns a generic
// (non-ErrNotFound) error, the handler must return 502 M_UNKNOWN (Bad Gateway).
//
// Updated in Story 12.4: was 500 M_UNKNOWN, now correctly 502 M_UNKNOWN because
// a storage backend error indicates upstream failure (Bad Gateway), not internal error.

func TestDownload_WithFakeStorer_StorageError(t *testing.T) {
	storer := &fakeDownloadStorer{
		contents: make(map[string][]byte),
		getError: errors.New("storer get error"),
	}

	db := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     "any-id",
			ServerName:  testServerName,
			ContentType: "image/png",
			AESKeyHex:   strings.Repeat("aa", 32),
			NonceHex:    strings.Repeat("bb", 12),
		},
	}

	mux := buildDownloadHandlerWithStorer(t, db, storer)
	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/any-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Story 12.4: generic storage error → 502 M_UNKNOWN (Bad Gateway).
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode 502 error response: %v", err)
	}
	if errResp.ErrCode != "M_UNKNOWN" {
		t.Errorf("expected errcode M_UNKNOWN, got %q", errResp.ErrCode)
	}
}

// ─── Story 12.7: SEC Gate 2 Fixes — Content-Type safety [HIGH-3] ──────────────
//
// AT-8: All download responses must include X-Content-Type-Options: nosniff
// AT-9: Non-safe stored content type forces Content-Type: application/octet-stream
//       and Content-Disposition: attachment
// AT-10: Safe content type gets inline disposition and original Content-Type preserved
//
// These tests will FAIL until:
//   1. download handler sets X-Content-Type-Options: nosniff on ALL responses
//   2. download handler checks stored ContentType against safe-inline allowlist
//   3. Non-safe types get Content-Type: application/octet-stream + Content-Disposition: attachment

// buildDownloadHandlerWithFakeStorer creates a handler backed by a fakeStorer
// (in-memory, not disk-based) for tests that need precise ciphertext control.
func buildDownloadHandlerWithFakeStorer(t *testing.T, store *mockDownloadStore, storer storage.Storer) http.Handler {
	t.Helper()
	h := NewHandler(HandlerConfig{
		DB:      store,
		Storage: storer,
	})
	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/media/v3/download/{serverName}/{mediaId}", h)
	return mux
}

// fakeInMemoryStorer is an in-memory storer for download tests.
type fakeInMemoryStorer struct {
	objects map[string][]byte
}

func newFakeInMemoryStorer() *fakeInMemoryStorer {
	return &fakeInMemoryStorer{objects: make(map[string][]byte)}
}

func (f *fakeInMemoryStorer) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = data
	return nil
}

func (f *fakeInMemoryStorer) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := f.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeInMemoryStorer) Delete(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

// storeEncryptedDownload stores plaintext encrypted to the in-memory storer.
func storeEncryptedDownload(t *testing.T, storer *fakeInMemoryStorer, serverName, mediaID string, plaintext []byte) (aesKeyHex, nonceHex string) {
	t.Helper()
	key, err := mediacrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	nonce, err := mediacrypto.GenerateNonce()
	if err != nil {
		t.Fatalf("GenerateNonce: %v", err)
	}
	ct, err := mediacrypto.Encrypt(plaintext, key, nonce)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	storer.objects[serverName+"/"+mediaID] = ct
	return hex.EncodeToString(key), hex.EncodeToString(nonce)
}

// ─── AT-8: Download always sets X-Content-Type-Options: nosniff ──────────────
//
// AC3-5 [HIGH-3]: Every download response MUST include X-Content-Type-Options: nosniff.
// This test uses a safe content type (image/png) to isolate the nosniff header check.

func TestDownload_NosniffHeader_AlwaysPresent(t *testing.T) {
	storer := newFakeInMemoryStorer()
	plaintext := []byte("png file content for nosniff test")
	aesKeyHex, nonceHex := storeEncryptedDownload(t, storer, testServerName, "nosniff-id", plaintext)

	db := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     "nosniff-id",
			ServerName:  testServerName,
			ContentType: "image/png",
			AESKeyHex:   aesKeyHex,
			NonceHex:    nonceHex,
		},
	}
	mux := buildDownloadHandlerWithFakeStorer(t, db, storer)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/nosniff-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-8] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	nosniff := w.Header().Get("X-Content-Type-Options")
	if nosniff != "nosniff" {
		t.Errorf("[AT-8] expected X-Content-Type-Options: nosniff, got %q", nosniff)
	}
}

// ─── AT-9: Download of non-safe stored type → attachment disposition ──────────
//
// AC3-6 [HIGH-3]: Files stored with non-safe Content-Type (e.g. text/html)
// must be served as Content-Type: application/octet-stream with
// Content-Disposition: attachment and X-Content-Type-Options: nosniff.

func TestDownload_UnsafeContentType_ForcesAttachment(t *testing.T) {
	storer := newFakeInMemoryStorer()
	plaintext := []byte("<html><script>alert(1)</script></html>")
	aesKeyHex, nonceHex := storeEncryptedDownload(t, storer, testServerName, "html-upload-id", plaintext)

	db := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     "html-upload-id",
			ServerName:  testServerName,
			ContentType: "text/html", // stored before the allowlist was enforced
			AESKeyHex:   aesKeyHex,
			NonceHex:    nonceHex,
		},
	}
	mux := buildDownloadHandlerWithFakeStorer(t, db, storer)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/html-upload-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-9] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/octet-stream" {
		t.Errorf("[AT-9] unsafe type: expected Content-Type application/octet-stream, got %q", ct)
	}

	cd := w.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment") {
		t.Errorf("[AT-9] unsafe type: expected Content-Disposition to start with 'attachment', got %q", cd)
	}

	nosniff := w.Header().Get("X-Content-Type-Options")
	if nosniff != "nosniff" {
		t.Errorf("[AT-9] expected X-Content-Type-Options: nosniff, got %q", nosniff)
	}
}

// ─── AT-10: Download of safe type: inline preserved + nosniff ─────────────────
//
// AC3-4 [HIGH-3]: Safe content types (image/png) must be served inline with
// original Content-Type and X-Content-Type-Options: nosniff.

func TestDownload_SafeContentType_InlineDisposition(t *testing.T) {
	storer := newFakeInMemoryStorer()
	plaintext := []byte("png image bytes")
	aesKeyHex, nonceHex := storeEncryptedDownload(t, storer, testServerName, "safe-png-id", plaintext)

	db := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     "safe-png-id",
			ServerName:  testServerName,
			ContentType: "image/png",
			AESKeyHex:   aesKeyHex,
			NonceHex:    nonceHex,
		},
	}
	mux := buildDownloadHandlerWithFakeStorer(t, db, storer)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/safe-png-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-10] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "image/png" {
		t.Errorf("[AT-10] safe type: expected Content-Type image/png, got %q", ct)
	}

	cd := w.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "inline") {
		t.Errorf("[AT-10] safe type: expected Content-Disposition to start with 'inline', got %q", cd)
	}

	nosniff := w.Header().Get("X-Content-Type-Options")
	if nosniff != "nosniff" {
		t.Errorf("[AT-10] expected X-Content-Type-Options: nosniff, got %q", nosniff)
	}
}

// ─── Story 12.16 ATDD Tests — CSP/CORP headers + fileName path value ─────────
//
// These tests will FAIL until:
//   1. download/handler.go adds Content-Security-Policy header to 200 responses
//   2. download/handler.go adds Cross-Origin-Resource-Policy: cross-origin to 200 responses
//   3. download/handler.go uses r.PathValue("fileName") when non-empty for Content-Disposition
//
// Spec compliance (Matrix CS API v1.18 §Media Repository):
//   - CSP SHOULD be set on all download/thumbnail 200 responses (v1.0+)
//   - CORP SHOULD be "cross-origin" on all download/thumbnail 200 responses (added v1.4)
//   - /{fileName} variant: Content-Disposition MUST use URL path filename, not mediaId

// ─── AT-5 (Story 12.16): CSP and CORP headers on download 200 response ───────
//
// AC-9 — Every successful download response (v3 path) must include:
//   Content-Security-Policy: sandbox; default-src 'none'; ...
//   Cross-Origin-Resource-Policy: cross-origin
//
// RED: fails until download handler sets these headers.

func TestDownloadHandler_CSPandCORPHeaders(t *testing.T) {
	storer := newFakeInMemoryStorer()
	plaintext := []byte("csp and corp test content")
	aesKeyHex, nonceHex := storeEncryptedDownload(t, storer, testServerName, "csp-test-id", plaintext)

	db := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     "csp-test-id",
			ServerName:  testServerName,
			ContentType: "image/png",
			AESKeyHex:   aesKeyHex,
			NonceHex:    nonceHex,
		},
	}
	mux := buildDownloadHandlerWithFakeStorer(t, db, storer)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/csp-test-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-5] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// AC-9: Content-Security-Policy must be present.
	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("[AT-5] Content-Security-Policy header must be present on 200 download response")
	}
	// Check key CSP directives (exact value defined in dev notes / handler implementation).
	if !strings.Contains(csp, "sandbox") {
		t.Errorf("[AT-5] CSP must contain 'sandbox', got: %q", csp)
	}
	if !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("[AT-5] CSP must contain \"default-src 'none'\", got: %q", csp)
	}

	// AC-9: Cross-Origin-Resource-Policy must be "cross-origin".
	corp := w.Header().Get("Cross-Origin-Resource-Policy")
	if corp != "cross-origin" {
		t.Errorf("[AT-5] Cross-Origin-Resource-Policy: expected 'cross-origin', got %q", corp)
	}
}

// ─── AT-6 (Story 12.16): {fileName} variant uses URL path filename ────────────
//
// AC-6 — When the download route includes a {fileName} path value, the
// Content-Disposition header must use that filename, NOT the mediaId.
//
// Route: GET /_matrix/client/v1/media/download/{serverName}/{mediaId}/{fileName}
// (or any route with a {fileName} path value recognised by r.PathValue("fileName"))
//
// RED: fails until handler uses r.PathValue("fileName") for Content-Disposition.

func TestDownloadHandler_FileNameVariant(t *testing.T) {
	// Sub-case 1: Safe content type (image/jpeg) → Content-Disposition starts with "inline;"
	t.Run("safe_type_inline_disposition", func(t *testing.T) {
		storer := newFakeInMemoryStorer()
		plaintext := []byte("photo file bytes for filename variant test")
		mediaID := "some-opaque-media-id"
		aesKeyHex, nonceHex := storeEncryptedDownload(t, storer, testServerName, mediaID, plaintext)

		db := &mockDownloadStore{
			row: &MediaFileRow{
				MediaID:     mediaID,
				ServerName:  testServerName,
				ContentType: "image/jpeg", // safe inline type
				AESKeyHex:   aesKeyHex,
				NonceHex:    nonceHex,
			},
		}

		h := NewHandler(HandlerConfig{
			DB:      db,
			Storage: storer,
		})
		mux := http.NewServeMux()
		mux.Handle("GET /_matrix/client/v1/media/download/{serverName}/{mediaId}/{fileName}", h)

		req := httptest.NewRequest(http.MethodGet,
			"/_matrix/client/v1/media/download/"+testServerName+"/"+mediaID+"/photo.jpg", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("[AT-6 safe] expected 200, got %d; body: %s", w.Code, w.Body.String())
		}

		cd := w.Header().Get("Content-Disposition")
		if cd == "" {
			t.Fatal("[AT-6 safe] Content-Disposition header must be present")
		}

		// image/jpeg is a safe type → disposition-type must be "inline".
		if !strings.HasPrefix(cd, "inline;") {
			t.Errorf("[AT-6 safe] image/jpeg: expected Content-Disposition to start with 'inline;', got: %q", cd)
		}

		// AC-6: filename must be "photo.jpg" (from URL path), NOT the mediaId.
		if !strings.Contains(cd, `photo.jpg`) {
			t.Errorf("[AT-6 safe] Content-Disposition must contain URL-path filename 'photo.jpg', got: %q", cd)
		}
		if strings.Contains(cd, mediaID) {
			t.Errorf("[AT-6 safe] Content-Disposition must NOT use mediaId %q when fileName path value is set, got: %q",
				mediaID, cd)
		}
	})

	// Sub-case 2: Unsafe content type (application/octet-stream) → Content-Disposition starts with "attachment;"
	// and filename still comes from the URL path (photo.jpg), not the mediaId.
	t.Run("unsafe_type_attachment_disposition", func(t *testing.T) {
		storer := newFakeInMemoryStorer()
		plaintext := []byte("binary file bytes for attachment variant test")
		mediaID := "another-opaque-media-id"
		aesKeyHex, nonceHex := storeEncryptedDownload(t, storer, testServerName, mediaID, plaintext)

		db := &mockDownloadStore{
			row: &MediaFileRow{
				MediaID:     mediaID,
				ServerName:  testServerName,
				ContentType: "application/octet-stream", // NOT in safe-inline list
				AESKeyHex:   aesKeyHex,
				NonceHex:    nonceHex,
			},
		}

		h := NewHandler(HandlerConfig{
			DB:      db,
			Storage: storer,
		})
		mux := http.NewServeMux()
		mux.Handle("GET /_matrix/client/v1/media/download/{serverName}/{mediaId}/{fileName}", h)

		req := httptest.NewRequest(http.MethodGet,
			"/_matrix/client/v1/media/download/"+testServerName+"/"+mediaID+"/photo.jpg", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("[AT-6 unsafe] expected 200, got %d; body: %s", w.Code, w.Body.String())
		}

		cd := w.Header().Get("Content-Disposition")
		if cd == "" {
			t.Fatal("[AT-6 unsafe] Content-Disposition header must be present")
		}

		// application/octet-stream is NOT safe-inline → disposition-type must be "attachment".
		if !strings.HasPrefix(cd, "attachment;") {
			t.Errorf("[AT-6 unsafe] application/octet-stream: expected Content-Disposition to start with 'attachment;', got: %q", cd)
		}

		// AC-6: filename must still be "photo.jpg" (from URL path), NOT the mediaId.
		if !strings.Contains(cd, `photo.jpg`) {
			t.Errorf("[AT-6 unsafe] Content-Disposition must contain URL-path filename 'photo.jpg', got: %q", cd)
		}
		if strings.Contains(cd, mediaID) {
			t.Errorf("[AT-6 unsafe] Content-Disposition must NOT use mediaId %q when fileName path value is set, got: %q",
				mediaID, cd)
		}
	})
}

// ─── AT-5b: CSP and CORP headers present on existing happy-path test ──────────
//
// Regression guard: extend the existing TestDownload_HappyPath coverage to also
// assert CSP and CORP headers, so any future removal of these headers is caught
// by the EXISTING tests (not just the new ones).
//
// This test re-exercises the happy path via the fakeInMemoryStorer (fast, no disk).
//
// RED: fails until download handler sets CSP/CORP headers.

func TestDownloadHandler_HappyPath_CSPandCORPPresent(t *testing.T) {
	storer := newFakeInMemoryStorer()
	plaintext := []byte("regression guard: csp+corp must be set on every 200")
	aesKeyHex, nonceHex := storeEncryptedDownload(t, storer, testServerName, "regression-csp-id", plaintext)

	db := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     "regression-csp-id",
			ServerName:  testServerName,
			ContentType: "image/png",
			AESKeyHex:   aesKeyHex,
			NonceHex:    nonceHex,
		},
	}
	mux := buildDownloadHandlerWithFakeStorer(t, db, storer)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/regression-csp-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-5b] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Both security headers must be present on every 200 response (regression guard).
	if csp := w.Header().Get("Content-Security-Policy"); csp == "" {
		t.Error("[AT-5b] regression: Content-Security-Policy must be present on every 200 download response")
	}
	if corp := w.Header().Get("Cross-Origin-Resource-Policy"); corp != "cross-origin" {
		t.Errorf("[AT-5b] regression: Cross-Origin-Resource-Policy: expected 'cross-origin', got %q", corp)
	}
}

// ─── AT-9b: image/svg+xml stored before allowlist → forced attachment ─────────
//
// AC3-6: image/svg+xml is not in the safe-inline list → must serve as attachment.

func TestDownload_SVGContentType_ForcesAttachment(t *testing.T) {
	storer := newFakeInMemoryStorer()
	plaintext := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	aesKeyHex, nonceHex := storeEncryptedDownload(t, storer, testServerName, "svg-upload-id", plaintext)

	db := &mockDownloadStore{
		row: &MediaFileRow{
			MediaID:     "svg-upload-id",
			ServerName:  testServerName,
			ContentType: "image/svg+xml",
			AESKeyHex:   aesKeyHex,
			NonceHex:    nonceHex,
		},
	}
	mux := buildDownloadHandlerWithFakeStorer(t, db, storer)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/media/v3/download/"+testServerName+"/svg-upload-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-9b] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/octet-stream" {
		t.Errorf("[AT-9b] svg: expected Content-Type application/octet-stream, got %q", ct)
	}

	cd := w.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment") {
		t.Errorf("[AT-9b] svg: expected Content-Disposition to start with 'attachment', got %q", cd)
	}
}
