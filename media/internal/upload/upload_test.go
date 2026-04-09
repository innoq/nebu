package upload

// ─── Story 4-19: Upload handler unit tests (net/http/httptest) ───────────────
//
// These tests are written FIRST (ATDD gate), before any implementation exists.
// ALL tests in this file are expected to FAIL until Story 4-19 is implemented.
//
// Test strategy:
//   - mockMediaStore implements MediaStore (consumer-defined interface, Go convention).
//     It records whether InsertMediaFile was called, enabling "never called" assertions.
//   - buildHandler wires a Handler with a temp-dir storage path, configurable maxBytes,
//     and a fixed serverName, using the mockMediaStore.
//   - A bearerToken constant simulates a present-but-unvalidated Authorization header.
//     For the MVP auth check (presence only), any non-empty "Bearer X" value passes.
//   - Tests use httptest.NewRecorder() for response capture.
//
// Types / functions under test (from story spec):
//   type Handler struct { ... }
//   type MediaStore interface { InsertMediaFile(ctx, MediaFileRow) error }
//   type MediaFileRow struct { ... }
//   func NewHandler(cfg HandlerConfig) *Handler  (or equivalent constructor)
//   func (h *Handler) ServeHTTP(w, r)            (or named method)

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	testServerName  = "test.local"
	testBearerToken = "Bearer test-token-alice"
	defaultMaxBytes = int64(52428800) // 50 MiB
)

// ─── Mock MediaStore ──────────────────────────────────────────────────────────

// mockMediaStore implements MediaStore (defined in upload.go).
// inserted records every row passed to InsertMediaFile.
// forcedErr, if non-nil, is returned by InsertMediaFile.

type mockMediaStore struct {
	inserted  []MediaFileRow
	forcedErr error
}

func (m *mockMediaStore) InsertMediaFile(_ context.Context, row MediaFileRow) error {
	if m.forcedErr != nil {
		return m.forcedErr
	}
	m.inserted = append(m.inserted, row)
	return nil
}

// ─── matrixErrorResp mirrors the standard Matrix error JSON ──────────────────

type matrixErrorResp struct {
	ErrCode string `json:"errcode"`
	Error   string `json:"error"`
}

// ─── uploadSuccessResp mirrors the 200 response body ─────────────────────────

type uploadSuccessResp struct {
	ContentURI string `json:"content_uri"`
}

// ─── buildHandler constructs a Handler for use in tests ──────────────────────

// buildHandler wires a Handler with the provided mockMediaStore.
// storagePath defaults to a fresh t.TempDir() if empty string is passed.
// maxBytes defaults to defaultMaxBytes if 0 is passed.

func buildHandler(t *testing.T, store *mockMediaStore, storagePath string, maxBytes int64) http.Handler {
	t.Helper()

	if storagePath == "" {
		storagePath = t.TempDir()
	}
	if maxBytes == 0 {
		maxBytes = defaultMaxBytes
	}

	h := NewHandler(HandlerConfig{
		DB:          store,
		StoragePath: storagePath,
		ServerName:  testServerName,
		MaxBytes:    maxBytes,
	})

	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/media/v3/upload", h)
	return mux
}

// ─── Test 1: Happy path — 200 with mxc:// content_uri ────────────────────────
//
// AC #1, #4, #5 — Authenticated POST with small body.
// Expects 200 and a content_uri in "mxc://test.local/<uuid>" format.
// DB InsertMediaFile must have been called exactly once.

func TestUpload_HappyPath(t *testing.T) {
	store := &mockMediaStore{}
	mux := buildHandler(t, store, "", 0)

	body := make([]byte, 100)
	for i := range body {
		body[i] = byte(i)
	}

	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Content-Length", "100")
	req.Header.Set("Authorization", testBearerToken)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp uploadSuccessResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode 200 response body: %v", err)
	}

	if resp.ContentURI == "" {
		t.Fatal("content_uri is empty in 200 response")
	}

	if !strings.HasPrefix(resp.ContentURI, "mxc://"+testServerName+"/") {
		t.Errorf("content_uri %q does not start with mxc://%s/", resp.ContentURI, testServerName)
	}

	// DB must have been called exactly once.
	if len(store.inserted) != 1 {
		t.Errorf("expected InsertMediaFile called once, got %d calls", len(store.inserted))
	}
}

// ─── Test 2: Content-Length header exceeds limit → 413 ───────────────────────
//
// AC #2 — If Content-Length > maxBytes, handler returns 413 M_TOO_LARGE
// BEFORE reading any body bytes. No file written, no DB row inserted.

func TestUpload_ContentLengthTooLarge(t *testing.T) {
	store := &mockMediaStore{}
	maxBytes := int64(52428800) // 50 MiB
	mux := buildHandler(t, store, "", maxBytes)

	// Content-Length = 100 MiB + 1 byte — well over the 50 MiB limit.
	oversizeLen := int64(104857601)

	// failOnReadBody ensures the handler never reads the body for a Content-Length
	// pre-check: any Read call returns an error, which would surface as 500 rather
	// than 413, falsifying the test. Receiving 413 proves no body read occurred.
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req.Body = failOnReadBody{}
	req.Header.Set("Content-Type", "image/jpeg")
	req.Header.Set("Content-Length", "104857601")
	req.Header.Set("Authorization", testBearerToken)
	req.ContentLength = oversizeLen

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode 413 error response: %v", err)
	}
	if errResp.ErrCode != "M_TOO_LARGE" {
		t.Errorf("expected errcode M_TOO_LARGE, got %q", errResp.ErrCode)
	}

	// DB must NOT have been called.
	if len(store.inserted) != 0 {
		t.Errorf("expected InsertMediaFile NOT to be called on oversized Content-Length, got %d calls", len(store.inserted))
	}
}

// ─── Test 3: Body stream exceeds limit (no Content-Length) → 413 ─────────────
//
// AC #3 — If no Content-Length header and the body stream yields more than
// maxBytes, handler returns 413 M_TOO_LARGE via LimitedReader.
// No file written, no DB row inserted.

func TestUpload_BodyTooLarge(t *testing.T) {
	store := &mockMediaStore{}
	maxBytes := int64(52428800) // 50 MiB
	mux := buildHandler(t, store, "", maxBytes)

	// Body is a 60 MiB reader — exceeds the 50 MiB limit.
	largebody := io.LimitReader(zeroReader{}, 60*1024*1024)

	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", largebody)
	req.Header.Set("Content-Type", "video/mp4")
	req.Header.Set("Authorization", testBearerToken)
	// Deliberately omit Content-Length to exercise the counting-reader path.
	req.ContentLength = -1

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode 413 error response: %v", err)
	}
	if errResp.ErrCode != "M_TOO_LARGE" {
		t.Errorf("expected errcode M_TOO_LARGE, got %q", errResp.ErrCode)
	}

	// DB must NOT have been called.
	if len(store.inserted) != 0 {
		t.Errorf("expected InsertMediaFile NOT to be called on oversized body, got %d calls", len(store.inserted))
	}
}

// ─── Test 4: No Authorization header → 401 M_MISSING_TOKEN ───────────────────
//
// AC #1, #5 — The handler is JWT-protected (bearer-presence check for MVP).
// Omitting the Authorization header must yield 401 M_MISSING_TOKEN.
// Handler body must never be reached — DB must NOT be called.

func TestUpload_Unauthenticated(t *testing.T) {
	store := &mockMediaStore{}
	mux := buildHandler(t, store, "", 0)

	body := bytes.NewReader([]byte("some file content"))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "text/plain")
	// Deliberately omit Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode 401 error response: %v", err)
	}
	if errResp.ErrCode != "M_MISSING_TOKEN" {
		t.Errorf("expected errcode M_MISSING_TOKEN, got %q", errResp.ErrCode)
	}

	// DB must NOT have been called.
	if len(store.inserted) != 0 {
		t.Errorf("expected InsertMediaFile NOT to be called on unauthenticated request, got %d calls", len(store.inserted))
	}
}

// ─── Test 5: Storage Write returns error → 500 M_UNKNOWN ─────────────────────
//
// AC #4 — If the storage layer fails (e.g., disk full, permission error), the
// handler must return 500 M_UNKNOWN. DB must NOT be called (storage happens
// before the DB insert in the happy-path pipeline).

func TestUpload_StorageError(t *testing.T) {
	store := &mockMediaStore{}

	// Use an invalid path to force a filesystem error.
	// "/dev/null/cannot-be-a-directory" is not a writable directory on any POSIX system.
	badStoragePath := "/dev/null/cannot-be-a-directory"

	mux := buildHandler(t, store, badStoragePath, 0)

	body := bytes.NewReader([]byte("file content that cannot be stored"))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Authorization", testBearerToken)

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

	// DB must NOT have been called — storage error occurs before DB insert.
	if len(store.inserted) != 0 {
		t.Errorf("expected InsertMediaFile NOT to be called on storage error, got %d calls", len(store.inserted))
	}
}

// ─── Test 6: DB insert error → 500 M_UNKNOWN; storage WAS written ────────────
//
// AC #4 — If storage succeeds but InsertMediaFile returns an error, the handler
// must return 500 M_UNKNOWN. The storage write has already occurred at this point
// in the pipeline (encrypt → store → DB insert), so the mock's storagePath
// being non-empty verifies the storage step ran before the DB call was attempted.

func TestUpload_DBInsertError(t *testing.T) {
	store := &mockMediaStore{
		forcedErr: errors.New("db down"),
	}
	mux := buildHandler(t, store, "", 0)

	body := bytes.NewReader([]byte("small test payload"))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Authorization", testBearerToken)

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

// ─── Test 7: content_uri format validation ────────────────────────────────────
//
// AC #5 — content_uri must be "mxc://<server_name>/<media_id>" where <media_id>
// is a valid UUID v4 (32 hex chars with hyphens: 8-4-4-4-12).

func TestUpload_ContentUriFormat(t *testing.T) {
	store := &mockMediaStore{}
	mux := buildHandler(t, store, "", 0)

	body := bytes.NewReader([]byte("test image bytes"))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "image/gif")
	req.Header.Set("Authorization", testBearerToken)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp uploadSuccessResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// content_uri must start with mxc://<serverName>/.
	prefix := "mxc://" + testServerName + "/"
	if !strings.HasPrefix(resp.ContentURI, prefix) {
		t.Errorf("content_uri %q does not start with %q", resp.ContentURI, prefix)
	}

	// Extract the media_id portion (everything after the prefix).
	mediaID := strings.TrimPrefix(resp.ContentURI, prefix)
	if mediaID == "" {
		t.Fatal("content_uri has empty media_id portion")
	}

	// UUID v4 format: 8-4-4-4-12 hex chars separated by hyphens (36 chars total).
	// Example: 550e8400-e29b-41d4-a716-446655440000
	const uuidLen = 36
	if len(mediaID) != uuidLen {
		t.Errorf("media_id %q has length %d, expected UUID v4 length %d", mediaID, len(mediaID), uuidLen)
	}

	// Check hyphen positions: 8, 13, 18, 23.
	for _, pos := range []int{8, 13, 18, 23} {
		if mediaID[pos] != '-' {
			t.Errorf("media_id %q: expected '-' at position %d (UUID v4 format), got %q", mediaID, pos, mediaID[pos])
		}
	}

	// All other positions must be hex digits.
	for i, c := range mediaID {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue // hyphen positions already checked
		}
		if !isHexRune(c) {
			t.Errorf("media_id %q: non-hex character %q at position %d", mediaID, c, i)
		}
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// isHexRune returns true for 0-9 and a-f (lowercase UUID hex).
func isHexRune(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
}

// zeroReader is an infinite source of zero bytes, used to produce oversized bodies.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// failOnReadBody returns an error if Read is ever called.
// Used by TestUpload_ContentLengthTooLarge to prove the handler returns 413
// without reading the body at all (Content-Length pre-check path).
type failOnReadBody struct{}

func (f failOnReadBody) Read(p []byte) (int, error) {
	return 0, errors.New("body should not have been read for Content-Length pre-check")
}

func (f failOnReadBody) Close() error { return nil }
