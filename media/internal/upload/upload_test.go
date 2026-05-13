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

	"github.com/nebu/nebu/media/internal/storage"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	testServerName  = "test.local"
	testBearerToken = "Bearer test-token-alice"
	defaultMaxBytes = int64(52428800) // 50 MiB
)

// ─── Mock TokenVerifier ───────────────────────────────────────────────────────

// mockTokenVerifier implements TokenVerifier for tests that don't specifically
// test OIDC validation. It accepts any token and returns a fixed subject identity.
//
// Story 12.8: buildHandler requires a non-nil OIDCVerifier (fail-closed).
// Tests focused on storage, size limits, CT blocks, DB errors, etc. use this mock.
// Tests that specifically test nil-verifier 503 behavior (AT-12-8-4) must NOT use it.
type mockTokenVerifier struct {
	// forcedErr, if non-nil, is returned by VerifyToken (simulates OIDC failure).
	forcedErr error
	// subject is the uploader identity returned on success.
	// Defaults to "test-user-alice" if empty.
	subject string
}

func (m *mockTokenVerifier) VerifyToken(_ context.Context, _ string) (string, error) {
	if m.forcedErr != nil {
		return "", m.forcedErr
	}
	if m.subject == "" {
		return "test-user-alice", nil
	}
	return m.subject, nil
}

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

// buildHandler wires a Handler with the provided mockMediaStore using a LocalStorer.
// storagePath defaults to a fresh t.TempDir() if empty string is passed.
// maxBytes defaults to defaultMaxBytes if 0 is passed.
//
// NOTE: After Story 12.2 refactor, this helper uses storage.LocalStorer instead of
// StoragePath string. Tests that need a failing storage should use buildHandlerWithStorer
// with a fakeStorer that returns an error.

func buildHandler(t *testing.T, store *mockMediaStore, storagePath string, maxBytes int64) http.Handler {
	t.Helper()

	if storagePath == "" {
		storagePath = t.TempDir()
	}
	if maxBytes == 0 {
		maxBytes = defaultMaxBytes
	}

	h := NewHandler(HandlerConfig{
		DB:           store,
		Storage:      &storage.LocalStorer{BasePath: storagePath},
		ServerName:   testServerName,
		MaxBytes:     maxBytes,
		OIDCVerifier: &mockTokenVerifier{}, // Story 12.8: non-nil required (fail-closed)
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
//
// After the Story 12.2 refactor, this test uses a fakeStorer that returns an
// error from Put instead of relying on an invalid filesystem path. This makes
// the test backend-agnostic and faster (no OS-level syscalls needed).

func TestUpload_StorageError(t *testing.T) {
	db := &mockMediaStore{}
	storer := &fakeStorer{putError: errors.New("simulated storage failure")}
	mux := buildHandlerWithStorer(t, db, storer, 0)

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
	if len(db.inserted) != 0 {
		t.Errorf("expected InsertMediaFile NOT to be called on storage error, got %d calls", len(db.inserted))
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

// ─── Story 12.2 ATDD Tests ────────────────────────────────────────────────────
//
// These tests will fail to compile until:
//   1. HandlerConfig.Storage Storer field replaces StoragePath string
//   2. upload.go imports and uses storage.Storer
//
// They test that the upload handler works correctly with a fake Storer
// (no filesystem, no MinIO) — Storer is fully mockable.

// Compile-time check: *fakeStorer satisfies storage.Storer.
// This will fail until storage.Storer is defined with the correct Put/Get/Delete signatures.
var _ storage.Storer = &fakeStorer{}

// fakeStorer is an in-memory implementation of storage.Storer for upload tests.
// putCalled records keys passed to Put. putError, if set, is returned by Put.
type fakeStorer struct {
	putCalled []string
	putError  error
}

func (f *fakeStorer) Put(_ context.Context, key string, _ io.Reader, _ int64) error {
	if f.putError != nil {
		return f.putError
	}
	f.putCalled = append(f.putCalled, key)
	return nil
}

func (f *fakeStorer) Get(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, errors.New("fakeStorer.Get not implemented")
}

func (f *fakeStorer) Delete(_ context.Context, _ string) error {
	return errors.New("fakeStorer.Delete not implemented")
}

// buildHandlerWithStorer constructs an upload handler using the new Storage Storer
// field instead of StoragePath. Used by Story 12.2 tests only.
// Will fail to compile until HandlerConfig has a Storage field of type Storer (interface).
func buildHandlerWithStorer(t *testing.T, db *mockMediaStore, storer *fakeStorer, maxBytes int64) http.Handler {
	t.Helper()
	if maxBytes == 0 {
		maxBytes = defaultMaxBytes
	}
	h := NewHandler(HandlerConfig{
		DB:           db,
		Storage:      storer,
		ServerName:   testServerName,
		MaxBytes:     maxBytes,
		OIDCVerifier: &mockTokenVerifier{}, // Story 12.8: non-nil required (fail-closed)
	})
	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/media/v3/upload", h)
	return mux
}

// AT-5: Upload handler with fake Storer — happy path returns 200 and calls Put once.
//
// AC4 + AC5 (Story 12.2) — When HandlerConfig has a Storage Storer field, the
// upload handler calls Storer.Put instead of writing to disk. The test uses a
// fakeStorer so no filesystem or MinIO is needed.

func TestUpload_WithFakeStorer_HappyPath(t *testing.T) {
	db := &mockMediaStore{}
	storer := &fakeStorer{}
	mux := buildHandlerWithStorer(t, db, storer, 0)

	body := []byte("fake ciphertext payload from fakeStorer test")
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Authorization", testBearerToken)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// DB must have been called.
	if len(db.inserted) != 1 {
		t.Errorf("expected InsertMediaFile called once, got %d", len(db.inserted))
	}

	// Storer.Put must have been called exactly once.
	if len(storer.putCalled) != 1 {
		t.Errorf("expected Storer.Put called once, got %d", len(storer.putCalled))
	}

	// Put key must be non-empty.
	if len(storer.putCalled) > 0 && storer.putCalled[0] == "" {
		t.Error("Storer.Put called with empty key")
	}

	// Verify content_uri format.
	var resp struct {
		ContentURI string `json:"content_uri"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !strings.HasPrefix(resp.ContentURI, "mxc://"+testServerName+"/") {
		t.Errorf("content_uri %q does not start with mxc://%s/", resp.ContentURI, testServerName)
	}
}

// AT-6: Upload handler with fake Storer returning error — returns 500, DB NOT called.
//
// AC4 (Story 12.2) — When Storer.Put returns an error, the handler must return
// 500 M_UNKNOWN and must NOT call InsertMediaFile.

func TestUpload_WithFakeStorer_StorageError(t *testing.T) {
	db := &mockMediaStore{}
	storer := &fakeStorer{putError: errors.New("storer put error")}
	mux := buildHandlerWithStorer(t, db, storer, 0)

	body := []byte("data that cannot be stored")
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", bytes.NewReader(body))
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
	if len(db.inserted) != 0 {
		t.Errorf("expected InsertMediaFile NOT to be called on storage error, got %d calls", len(db.inserted))
	}
}

// ─── Story 12.7: SEC Gate 2 Fixes ─────────────────────────────────────────────
//
// AT-5/6/7: Blocked Content-Type at upload [HIGH-3]
// AT-11/12: JWT validation on upload [HIGH-2]
//
// These tests will FAIL until:
//   1. HandlerConfig gains an OIDCVerifier field
//   2. Upload handler blocks dangerous Content-Types
//   3. Upload handler performs real JWT verification (not just bearer-presence)

// ─── AT-5: Upload with text/html blocked → 400 M_BAD_JSON ────────────────────
//
// AC3-1 [HIGH-3]: text/html Content-Type must be rejected at upload with 400.

func TestUpload_BlockedContentType_TextHTML(t *testing.T) {
	store := &mockMediaStore{}
	mux := buildHandler(t, store, "", 0)

	body := bytes.NewReader([]byte("<html><script>alert(1)</script></html>"))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "text/html")
	req.Header.Set("Authorization", testBearerToken)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AT-5] text/html upload: expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[AT-5] could not decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("[AT-5] expected errcode M_BAD_JSON, got %q", errResp.ErrCode)
	}
	// DB must NOT have been called.
	if len(store.inserted) != 0 {
		t.Errorf("[AT-5] InsertMediaFile must NOT be called on blocked content type, got %d calls", len(store.inserted))
	}
}

// ─── AT-6: Upload with image/svg+xml blocked → 400 M_BAD_JSON ────────────────
//
// AC3-2 [HIGH-3]: image/svg+xml Content-Type must be rejected at upload with 400.

func TestUpload_BlockedContentType_SVG(t *testing.T) {
	store := &mockMediaStore{}
	mux := buildHandler(t, store, "", 0)

	body := bytes.NewReader([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "image/svg+xml")
	req.Header.Set("Authorization", testBearerToken)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AT-6] svg upload: expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[AT-6] could not decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("[AT-6] expected errcode M_BAD_JSON, got %q", errResp.ErrCode)
	}
}

// ─── AT-7: Upload with application/javascript blocked → 400 M_BAD_JSON ────────
//
// AC3-3 [HIGH-3]: application/javascript Content-Type must be rejected at upload.

func TestUpload_BlockedContentType_JavaScript(t *testing.T) {
	store := &mockMediaStore{}
	mux := buildHandler(t, store, "", 0)

	body := bytes.NewReader([]byte("alert('xss')"))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "application/javascript")
	req.Header.Set("Authorization", testBearerToken)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AT-7] javascript upload: expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[AT-7] could not decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("[AT-7] expected errcode M_BAD_JSON, got %q", errResp.ErrCode)
	}
}

// ─── AT-7b: Upload with text/javascript blocked → 400 M_BAD_JSON ─────────────
//
// AC3-3 [HIGH-3]: text/javascript Content-Type (alias) must also be blocked.

func TestUpload_BlockedContentType_TextJavaScript(t *testing.T) {
	store := &mockMediaStore{}
	mux := buildHandler(t, store, "", 0)

	body := bytes.NewReader([]byte("alert('xss')"))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "text/javascript")
	req.Header.Set("Authorization", testBearerToken)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AT-7b] text/javascript upload: expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[AT-7b] could not decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("[AT-7b] expected errcode M_BAD_JSON, got %q", errResp.ErrCode)
	}
}

// ─── AT-7c: Content-Type with charset param: normalize before check ──────────
//
// AC3 [HIGH-3]: "text/html; charset=utf-8" must also be blocked (param stripping).

func TestUpload_BlockedContentType_TextHTMLWithCharset(t *testing.T) {
	store := &mockMediaStore{}
	mux := buildHandler(t, store, "", 0)

	body := bytes.NewReader([]byte("<html></html>"))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "text/html; charset=utf-8")
	req.Header.Set("Authorization", testBearerToken)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AT-7c] text/html;charset upload: expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[AT-7c] could not decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("[AT-7c] expected errcode M_BAD_JSON, got %q", errResp.ErrCode)
	}
}

// ─── Story 12.8: OIDC Fail-Open Hardening ─────────────────────────────────────
//
// AT-12-8-4: Upload handler with nil OIDC verifier → 503 M_UNAVAILABLE.
//
// AC-4 — Given an upload Handler created with OIDCVerifier: nil,
// when a POST /_matrix/media/v3/upload request arrives with a valid Bearer token,
// then the handler must return 503 with errcode M_UNAVAILABLE (fail-closed).
// No file must be written, no DB row must be inserted.
//
// RED: The current code has a nil-verifier fallback that accepts any Bearer token
// as the uploaderUserID. This test FAILS until the else branch is replaced with 503.

// ─── Story 12.9: Canonical Matrix User ID in Media Audit Trail ───────────────
//
// AT-12-9-1: Upload stores canonical @localpart:server in uploader_user_id.
//
// AC-1 — Given a user with OIDC sub=alice and server name "localhost",
// when the upload is accepted,
// then media_files.uploader_user_id must be "@alice:localhost" (not "alice").
//
// RED: The current code sets uploaderUserID = subject (raw claim).
// This test FAILS until formatMatrixUserID is added and called in ServeHTTP.

func TestUpload_StoresCanonicalMatrixUserID(t *testing.T) {
	store := &mockMediaStore{}
	storer := &fakeStorer{}

	h := NewHandler(HandlerConfig{
		DB:           store,
		Storage:      storer,
		ServerName:   "localhost",
		MaxBytes:     defaultMaxBytes,
		OIDCVerifier: &mockTokenVerifier{subject: "alice"},
	})
	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/media/v3/upload", h)

	body := bytes.NewReader([]byte("test file content"))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Authorization", "Bearer mock-token")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-12-9-1] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if len(store.inserted) != 1 {
		t.Fatalf("[AT-12-9-1] expected InsertMediaFile called once, got %d calls", len(store.inserted))
	}

	got := store.inserted[0].UploaderUserID
	want := "@alice:localhost"
	if got != want {
		t.Errorf("[AT-12-9-1] UploaderUserID = %q, want %q (canonical Matrix format)", got, want)
	}
}

// AT-12-9-2: Upload uses NEBU_SERVER_NAME as server part of the Matrix user ID.
//
// AC-2 — Given an OIDC token with sub=alice and server name "myserver.example.com",
// when the upload is accepted,
// then media_files.uploader_user_id must be "@alice:myserver.example.com".
//
// RED: Current code does not format the user ID at all — raw claim is stored.
// This test FAILS until formatMatrixUserID is added and the handler uses ServerName.

func TestUpload_UsesServerNameInMatrixUserID(t *testing.T) {
	store := &mockMediaStore{}
	storer := &fakeStorer{}

	h := NewHandler(HandlerConfig{
		DB:           store,
		Storage:      storer,
		ServerName:   "myserver.example.com",
		MaxBytes:     defaultMaxBytes,
		OIDCVerifier: &mockTokenVerifier{subject: "alice"},
	})
	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/media/v3/upload", h)

	body := bytes.NewReader([]byte("test file content"))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Authorization", "Bearer mock-token")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-12-9-2] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if len(store.inserted) != 1 {
		t.Fatalf("[AT-12-9-2] expected InsertMediaFile called once, got %d calls", len(store.inserted))
	}

	got := store.inserted[0].UploaderUserID
	want := "@alice:myserver.example.com"
	if got != want {
		t.Errorf("[AT-12-9-2] UploaderUserID = %q, want %q", got, want)
	}
}

// ─── Story 12.8: OIDC Fail-Open Hardening ─────────────────────────────────────

func TestUpload_NilVerifier_Returns503(t *testing.T) {
	store := &mockMediaStore{}
	storer := &fakeStorer{}

	// Build handler with nil OIDCVerifier (explicitly no OIDC configured).
	h := NewHandler(HandlerConfig{
		DB:           store,
		Storage:      storer,
		ServerName:   testServerName,
		MaxBytes:     defaultMaxBytes,
		OIDCVerifier: nil, // nil → must return 503 M_UNAVAILABLE after Story 12.8
	})
	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/media/v3/upload", h)

	body := bytes.NewReader([]byte("some file content"))
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", body)
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Authorization", "Bearer somevalidlookingtoken")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// AC-4: nil verifier must be fail-closed — 503 M_UNAVAILABLE.
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("[AT-12-8-4] nil verifier: expected 503, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrorResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[AT-12-8-4] could not decode 503 response: %v", err)
	}
	if errResp.ErrCode != "M_UNAVAILABLE" {
		t.Errorf("[AT-12-8-4] expected errcode M_UNAVAILABLE, got %q", errResp.ErrCode)
	}

	// Neither storage nor DB must have been touched.
	if len(storer.putCalled) != 0 {
		t.Errorf("[AT-12-8-4] Storer.Put must NOT be called on 503 response, got %d calls", len(storer.putCalled))
	}
	if len(store.inserted) != 0 {
		t.Errorf("[AT-12-8-4] InsertMediaFile must NOT be called on 503 response, got %d calls", len(store.inserted))
	}
}

// ---------------------------------------------------------------------------
// Story 12.11 — SEC Fix F-1: Configurable OIDC claim for audit trail
//
// These tests validate the extractClaimFromMap pure function and the
// OIDCTokenVerifier claim configuration. They are RED until:
//   - extractClaimFromMap(rawClaims map[string]interface{}, claimName string) (string, error) exists
//   - OIDCTokenVerifier accepts claimName in its constructor
// ---------------------------------------------------------------------------

// AT-12-11-4 — extractClaimFromMap returns configured claim ("sub") when present
//
// AC-F1-1: Given claimName="sub" and rawClaims={"sub":"alice-uuid","name":"alice"},
//          when extractClaimFromMap is called,
//          then it returns "alice-uuid".
//
// RED: fails until extractClaimFromMap is defined in upload.go.
func TestExtractClaimFromMap_Sub_WhenPresent(t *testing.T) {
	t.Parallel()

	rawClaims := map[string]interface{}{
		"sub":  "alice-uuid",
		"name": "alice",
	}

	got, err := extractClaimFromMap(rawClaims, "sub")
	if err != nil {
		t.Fatalf("[AT-12-11-4] unexpected error: %v", err)
	}
	if got != "alice-uuid" {
		t.Errorf("[AT-12-11-4] claimName=sub: got %q, want %q", got, "alice-uuid")
	}
}

// AT-12-11-5 — extractClaimFromMap returns configured claim ("name") when present
//
// AC-F1-2: Given claimName="name" and rawClaims={"sub":"uuid-123","name":"alice"},
//          when extractClaimFromMap is called,
//          then it returns "alice".
//
// RED: fails until extractClaimFromMap is defined.
func TestExtractClaimFromMap_Name_WhenPresent(t *testing.T) {
	t.Parallel()

	rawClaims := map[string]interface{}{
		"sub":  "uuid-123",
		"name": "alice",
	}

	got, err := extractClaimFromMap(rawClaims, "name")
	if err != nil {
		t.Fatalf("[AT-12-11-5] unexpected error: %v", err)
	}
	if got != "alice" {
		t.Errorf("[AT-12-11-5] claimName=name: got %q, want %q", got, "alice")
	}
}

// AT-12-11-6 — extractClaimFromMap falls back to sub when configured claim is missing
//
// AC-F1-4: Given claimName="email" and token has no "email" claim but has sub="uuid-123",
//          when extractClaimFromMap is called,
//          then it returns "uuid-123" (sub fallback).
//
// RED: fails until extractClaimFromMap implements the fallback logic.
func TestExtractClaimFromMap_FallsBackToSub_WhenClaimMissing(t *testing.T) {
	t.Parallel()

	rawClaims := map[string]interface{}{
		"sub":  "uuid-123",
		"name": "alice",
		// no "email" claim
	}

	got, err := extractClaimFromMap(rawClaims, "email")
	if err != nil {
		t.Fatalf("[AT-12-11-6] unexpected error: %v", err)
	}
	if got != "uuid-123" {
		t.Errorf("[AT-12-11-6] claimName=email (missing): got %q, want sub fallback %q", got, "uuid-123")
	}
}

// AT-12-11-7 — extractClaimFromMap returns error when both configured claim and sub are missing
//
// Given claimName="email" and rawClaims has neither "email" nor "sub",
// then an error is returned.
func TestExtractClaimFromMap_ErrorWhenBothMissing(t *testing.T) {
	t.Parallel()

	rawClaims := map[string]interface{}{
		"name": "alice",
		// no "email", no "sub"
	}

	got, err := extractClaimFromMap(rawClaims, "email")
	if err == nil {
		t.Fatalf("[AT-12-11-7] expected error when both configured claim and sub are missing, got %q", got)
	}
}
