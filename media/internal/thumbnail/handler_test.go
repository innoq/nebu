package thumbnail_test

// ─── Story 12.5 ATDD Tests — Thumbnail HTTP handler unit tests ───────────────
//
// These tests will FAIL until:
//   1. media/internal/thumbnail/handler.go defines Handler, NewHandler, HandlerConfig
//   2. media/internal/thumbnail/thumbnail.go defines GenerateThumbnail, DetectMIMEType
//   3. main.go replaces thumbnailStub with thumbnail.NewHandler
//
// Test strategy:
//   - fakeThumbStore implements thumbnail.MediaStore.
//   - fakeThumbStorer implements storage.Storer.
//   - Tests use httptest.NewRecorder() + httptest.NewRequest().
//   - Images are stored encrypted (AES-256-GCM) to match the real storage pipeline.
//   - Route pattern: "GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}"
//
// Failing reason before implementation:
//   Package "github.com/nebu/nebu/media/internal/thumbnail" does not exist.
//   Handler, NewHandler, HandlerConfig are undefined.
//
// Matrix spec compliance (Oracle, v1.18):
//   - Content-Type: REQUIRED on 200 response; one of image/jpeg, image/png, image/apng, image/gif, image/webp
//   - Content-Disposition: REQUIRED on 200 response; MUST be "inline"; SHOULD include filename
//   - Cache-Control: max-age=86400 (project requirement, AC5)
//   - width + height: REQUIRED query params; missing → 400
//   - animated=false → MUST NOT return animated thumbnail
//   - method: [crop, scale] only

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mediacrypto "github.com/nebu/nebu/media/internal/crypto"
	"github.com/nebu/nebu/media/internal/storage"
	"github.com/nebu/nebu/media/internal/thumbnail"
)

// ─── Fake MediaStore ──────────────────────────────────────────────────────────

// fakeThumbStore implements thumbnail.MediaStore.
type fakeThumbStore struct {
	row       *thumbnail.MediaFileRow
	forcedErr error
}

func (f *fakeThumbStore) GetMediaFile(_ context.Context, _, _ string) (*thumbnail.MediaFileRow, error) {
	if f.forcedErr != nil {
		return nil, f.forcedErr
	}
	return f.row, nil
}

// ─── Fake Storer ─────────────────────────────────────────────────────────────

// fakeThumbStorer implements storage.Storer with in-memory map.
type fakeThumbStorer struct {
	objects  map[string][]byte
	getError error
}

func newFakeThumbStorer() *fakeThumbStorer {
	return &fakeThumbStorer{objects: make(map[string][]byte)}
}

func (f *fakeThumbStorer) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = data
	return nil
}

func (f *fakeThumbStorer) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if f.getError != nil {
		return nil, f.getError
	}
	data, ok := f.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeThumbStorer) Delete(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

// ─── Matrix JSON error shape ──────────────────────────────────────────────────

type thumbErrResp struct {
	ErrCode string `json:"errcode"`
	Error   string `json:"error"`
}

// ─── Helper: encrypt and store a JPEG ────────────────────────────────────────

// storeEncryptedJPEG creates a synthetic JPEG, encrypts it, stores it under key,
// and returns the MediaFileRow with the AES key/nonce hex values.
func storeEncryptedJPEG(t *testing.T, storer *fakeThumbStorer, serverName, mediaID string) *thumbnail.MediaFileRow {
	t.Helper()

	// Create synthetic JPEG.
	img := image.NewRGBA(image.Rect(0, 0, 200, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 100, A: 255})
		}
	}
	var imgBuf bytes.Buffer
	if err := jpeg.Encode(&imgBuf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("storeEncryptedJPEG: encode: %v", err)
	}
	plain := imgBuf.Bytes()

	key, err := mediacrypto.GenerateKey()
	if err != nil {
		t.Fatalf("storeEncryptedJPEG: GenerateKey: %v", err)
	}
	nonce, err := mediacrypto.GenerateNonce()
	if err != nil {
		t.Fatalf("storeEncryptedJPEG: GenerateNonce: %v", err)
	}
	ciphertext, err := mediacrypto.Encrypt(plain, key, nonce)
	if err != nil {
		t.Fatalf("storeEncryptedJPEG: Encrypt: %v", err)
	}

	storageKey := serverName + "/" + mediaID
	storer.objects[storageKey] = ciphertext

	return &thumbnail.MediaFileRow{
		MediaID:     mediaID,
		ServerName:  serverName,
		ContentType: "image/jpeg",
		AESKeyHex:   hex.EncodeToString(key),
		NonceHex:    hex.EncodeToString(nonce),
	}
}

// ─── Helper: build handler + mux ─────────────────────────────────────────────

func buildThumbHandler(t *testing.T, store *fakeThumbStore, storer *fakeThumbStorer) http.Handler {
	t.Helper()
	h := thumbnail.NewHandler(thumbnail.HandlerConfig{
		DB:      store,
		Storage: storer,
	})
	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}", h)
	return mux
}

// ─── AT-4: Missing width or height → 400 M_BAD_JSON ─────────────────────────
//
// AC7 + Spec: both width and height are REQUIRED query params.
//
// Failing reason: thumbnail.Handler does not exist.

func TestThumbnailHandler_MissingWidth_Returns400(t *testing.T) {
	store := &fakeThumbStore{}
	storer := newFakeThumbStorer()
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/abc123?height=100", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("missing width: expected 400, got %d", w.Code)
	}
	var resp thumbErrResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode error response: %v", err)
	}
	if resp.ErrCode != "M_BAD_JSON" {
		t.Errorf("missing width: expected errcode M_BAD_JSON, got %q", resp.ErrCode)
	}
}

func TestThumbnailHandler_MissingHeight_Returns400(t *testing.T) {
	store := &fakeThumbStore{}
	storer := newFakeThumbStorer()
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/abc123?width=100", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("missing height: expected 400, got %d", w.Code)
	}
	var resp thumbErrResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode error response: %v", err)
	}
	if resp.ErrCode != "M_BAD_JSON" {
		t.Errorf("missing height: expected errcode M_BAD_JSON, got %q", resp.ErrCode)
	}
}

func TestThumbnailHandler_MissingBothParams_Returns400(t *testing.T) {
	store := &fakeThumbStore{}
	storer := newFakeThumbStorer()
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/abc123", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("missing both: expected 400, got %d", w.Code)
	}
}

// ─── AT-4b: Non-integer width/height → 400 (Spec edge case) ─────────────────
//
// Spec: "The request does not make sense to the server, or the server cannot
// thumbnail the content. For example, the client requested non-integer dimensions."
//
// Failing reason: thumbnail.Handler does not exist.

func TestThumbnailHandler_NonIntegerWidth_Returns400(t *testing.T) {
	store := &fakeThumbStore{}
	storer := newFakeThumbStorer()
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/abc123?width=abc&height=100", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("non-integer width: expected 400, got %d", w.Code)
	}
}

func TestThumbnailHandler_NegativeWidth_Returns400(t *testing.T) {
	store := &fakeThumbStore{}
	storer := newFakeThumbStorer()
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/abc123?width=-1&height=100", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("negative width: expected 400, got %d", w.Code)
	}
}

// ─── AT-5: Cache-Control header present on 200 response ──────────────────────
//
// AC5: Cache-Control: max-age=86400 MUST be set on all 2xx thumbnail responses.
// Also verifies: Content-Type and Content-Disposition are present (Spec v1.12 MUST).
//
// Failing reason: thumbnail.Handler does not exist.

func TestThumbnailHandler_CacheControlAndHeaders_OnSuccess(t *testing.T) {
	storer := newFakeThumbStorer()
	row := storeEncryptedJPEG(t, storer, "test.local", "cache-test-001")
	store := &fakeThumbStore{row: row}
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/cache-test-001?width=100&height=100&method=scale", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// AC5: Cache-Control
	cc := w.Header().Get("Cache-Control")
	if cc != "max-age=86400" {
		t.Errorf("Cache-Control: expected max-age=86400, got %q", cc)
	}

	// Spec v1.12 MUST: Content-Type must be one of the allowed image types.
	ct := w.Header().Get("Content-Type")
	allowed := map[string]bool{
		"image/jpeg": true, "image/png": true, "image/apng": true,
		"image/gif": true, "image/webp": true,
	}
	if !allowed[ct] {
		t.Errorf("Content-Type must be a valid thumbnail type, got %q", ct)
	}

	// Spec v1.12 MUST: Content-Disposition must be inline.
	cd := w.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "inline") {
		t.Errorf("Content-Disposition must start with 'inline', got %q", cd)
	}
}

// ─── AT-6: Unsupported MIME type → 400 M_BAD_JSON ────────────────────────────
//
// AC3: SVG/PDF/PS/EPS stored media → handler detects from magic bytes → 400.
//
// Failing reason: thumbnail.Handler does not exist.

func TestThumbnailHandler_UnsupportedSVG_Returns400(t *testing.T) {
	// Store an SVG as if it were uploaded (encrypted).
	storer := newFakeThumbStorer()
	svgBytes := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><rect width="100" height="100"/></svg>`)

	key, _ := mediacrypto.GenerateKey()
	nonce, _ := mediacrypto.GenerateNonce()
	ciphertext, err := mediacrypto.Encrypt(svgBytes, key, nonce)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	storer.objects["test.local/svg-file-001"] = ciphertext

	row := &thumbnail.MediaFileRow{
		MediaID:     "svg-file-001",
		ServerName:  "test.local",
		ContentType: "image/svg+xml", // stored content type (irrelevant — magic bytes used)
		AESKeyHex:   hex.EncodeToString(key),
		NonceHex:    hex.EncodeToString(nonce),
	}
	store := &fakeThumbStore{row: row}
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/svg-file-001?width=100&height=100", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("SVG: expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp thumbErrResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode error response: %v", err)
	}
	if resp.ErrCode != "M_BAD_JSON" {
		t.Errorf("SVG: expected errcode M_BAD_JSON, got %q", resp.ErrCode)
	}
}

func TestThumbnailHandler_UnsupportedPDF_Returns400(t *testing.T) {
	storer := newFakeThumbStorer()
	pdfBytes := []byte("%PDF-1.4 fake pdf bytes for testing thumbnail rejection")

	key, _ := mediacrypto.GenerateKey()
	nonce, _ := mediacrypto.GenerateNonce()
	ciphertext, err := mediacrypto.Encrypt(pdfBytes, key, nonce)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	storer.objects["test.local/pdf-file-001"] = ciphertext

	row := &thumbnail.MediaFileRow{
		MediaID:     "pdf-file-001",
		ServerName:  "test.local",
		ContentType: "application/pdf",
		AESKeyHex:   hex.EncodeToString(key),
		NonceHex:    hex.EncodeToString(nonce),
	}
	store := &fakeThumbStore{row: row}
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/pdf-file-001?width=100&height=100", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("PDF: expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp thumbErrResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode error response: %v", err)
	}
	if resp.ErrCode != "M_BAD_JSON" {
		t.Errorf("PDF: expected errcode M_BAD_JSON, got %q", resp.ErrCode)
	}
}

func TestThumbnailHandler_UnsupportedPS_Returns400(t *testing.T) {
	// PostScript magic bytes: starts with %!PS
	storer := newFakeThumbStorer()
	psBytes := []byte("%!PS-Adobe-3.0 fake postscript content for thumbnail rejection test")

	key, _ := mediacrypto.GenerateKey()
	nonce, _ := mediacrypto.GenerateNonce()
	ciphertext, err := mediacrypto.Encrypt(psBytes, key, nonce)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	storer.objects["test.local/ps-file-001"] = ciphertext

	row := &thumbnail.MediaFileRow{
		MediaID:     "ps-file-001",
		ServerName:  "test.local",
		ContentType: "application/postscript",
		AESKeyHex:   hex.EncodeToString(key),
		NonceHex:    hex.EncodeToString(nonce),
	}
	store := &fakeThumbStore{row: row}
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/ps-file-001?width=100&height=100", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("PS: expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp thumbErrResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode error response: %v", err)
	}
	if resp.ErrCode != "M_BAD_JSON" {
		t.Errorf("PS: expected errcode M_BAD_JSON, got %q", resp.ErrCode)
	}
}

// ─── AT-7: MediaID not in DB → 404 M_NOT_FOUND ───────────────────────────────
//
// AC: media not in DB → 404 M_NOT_FOUND (consistent with download handler).
//
// Failing reason: thumbnail.Handler does not exist.

func TestThumbnailHandler_NotFound_Returns404(t *testing.T) {
	store := &fakeThumbStore{row: nil} // nil means not found
	storer := newFakeThumbStorer()
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/notexist?width=100&height=100", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp thumbErrResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode error response: %v", err)
	}
	if resp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %q", resp.ErrCode)
	}
}

// ─── AT-8: Animated GIF with animated=true → 200 image/gif ───────────────────
//
// AC6: animated=true + GIF source → 200 with Content-Type: image/gif.
//
// Failing reason: thumbnail.Handler does not exist.

func TestThumbnailHandler_AnimatedGIF_Returns200WithGIFContentType(t *testing.T) {
	// Store an animated GIF.
	storer := newFakeThumbStorer()
	gifBytes := makeSyntheticGIF(t, 200, 200)

	key, _ := mediacrypto.GenerateKey()
	nonce, _ := mediacrypto.GenerateNonce()
	ciphertext, err := mediacrypto.Encrypt(gifBytes, key, nonce)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	storer.objects["test.local/animated-gif-001"] = ciphertext

	row := &thumbnail.MediaFileRow{
		MediaID:     "animated-gif-001",
		ServerName:  "test.local",
		ContentType: "image/gif",
		AESKeyHex:   hex.EncodeToString(key),
		NonceHex:    hex.EncodeToString(nonce),
	}
	store := &fakeThumbStore{row: row}
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/animated-gif-001?width=100&height=100&animated=true", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("animated GIF: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if ct != "image/gif" {
		t.Errorf("animated GIF: expected Content-Type image/gif, got %q", ct)
	}
}

// ─── AT-10: animated=false on GIF → static JPEG, NOT image/gif (Spec MUST) ───
//
// Spec v1.18 MUST: animated=false → server MUST NOT return animated thumbnail.
//
// Failing reason: thumbnail.Handler does not exist.

func TestThumbnailHandler_AnimatedFalse_GIFSource_ReturnsStaticJPEG(t *testing.T) {
	storer := newFakeThumbStorer()
	gifBytes := makeSyntheticGIF(t, 200, 200)

	key, _ := mediacrypto.GenerateKey()
	nonce, _ := mediacrypto.GenerateNonce()
	ciphertext, err := mediacrypto.Encrypt(gifBytes, key, nonce)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	storer.objects["test.local/animated-gif-002"] = ciphertext

	row := &thumbnail.MediaFileRow{
		MediaID:     "animated-gif-002",
		ServerName:  "test.local",
		ContentType: "image/gif",
		AESKeyHex:   hex.EncodeToString(key),
		NonceHex:    hex.EncodeToString(nonce),
	}
	store := &fakeThumbStore{row: row}
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/animated-gif-002?width=100&height=100&animated=false", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("animated=false GIF: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if ct == "image/gif" {
		t.Errorf("animated=false: MUST NOT return animated GIF — got Content-Type image/gif")
	}
	if ct != "image/jpeg" {
		t.Errorf("animated=false GIF: expected image/jpeg, got %q", ct)
	}
}

// ─── AT-11: Storage unavailable → 502 M_UNKNOWN ──────────────────────────────
//
// Consistent with download handler: ErrStorageUnavailable → 502.
//
// Failing reason: thumbnail.Handler does not exist.

func TestThumbnailHandler_StorageUnavailable_Returns502(t *testing.T) {
	row := &thumbnail.MediaFileRow{
		MediaID:     "unavail-001",
		ServerName:  "test.local",
		ContentType: "image/jpeg",
		AESKeyHex:   "0000000000000000000000000000000000000000000000000000000000000000",
		NonceHex:    "000000000000000000000000",
	}
	store := &fakeThumbStore{row: row}
	storer := newFakeThumbStorer()
	storer.getError = storage.ErrStorageUnavailable
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/unavail-001?width=100&height=100", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("storage unavailable: expected 502, got %d", w.Code)
	}
	var resp thumbErrResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode error response: %v", err)
	}
	if resp.ErrCode != "M_UNKNOWN" {
		t.Errorf("storage unavailable: expected M_UNKNOWN, got %q", resp.ErrCode)
	}
	// Response must NOT contain storage error details (no credential leak).
	bodyStr := w.Body.String()
	if strings.Contains(bodyStr, "minio") || strings.Contains(bodyStr, "endpoint") {
		t.Errorf("502 response must not contain storage internals: %s", bodyStr)
	}
}

// ─── AT-12: mediaId path traversal → 400 (Spec MUST sanitize) ────────────────
//
// Spec v1.18 security section MUST: homeservers must sanitize mxc:// URIs.
// mediaId must only allow alphanumeric, _ and - characters.
// Requests with .. or / in mediaId must be rejected.
//
// Failing reason: thumbnail.Handler does not exist.

func TestThumbnailHandler_PathTraversalInMediaID_Returns400(t *testing.T) {
	store := &fakeThumbStore{}
	storer := newFakeThumbStorer()
	mux := buildThumbHandler(t, store, storer)

	// Note: Go's HTTP router will normalize "/../" before it reaches the handler.
	// We test with a URL-encoded traversal that doesn't get normalized.
	// The handler must validate the raw mediaId value and return 400 M_BAD_JSON.
	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/..%2Fetc%2Fpasswd?width=100&height=100", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Must be exactly 400 M_BAD_JSON — the handler validates charset proactively.
	if w.Code != http.StatusBadRequest {
		t.Errorf("path traversal in mediaId: expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp thumbErrResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode error response: %v", err)
	}
	if resp.ErrCode != "M_BAD_JSON" {
		t.Errorf("path traversal: expected errcode M_BAD_JSON, got %q", resp.ErrCode)
	}
}

// ─── AT-13: Content-Disposition must be 'inline' on 200 (Spec v1.12 MUST) ────
//
// Spec v1.18 MUST (v1.12+): Content-Disposition MUST be inline.
// This is verified as part of AT-5 (CacheControlAndHeaders) above, but added
// here as an explicit named test for traceability.
//
// Failing reason: thumbnail.Handler does not exist.

func TestThumbnailHandler_ContentDisposition_IsInline(t *testing.T) {
	storer := newFakeThumbStorer()
	row := storeEncryptedJPEG(t, storer, "test.local", "cd-test-001")
	store := &fakeThumbStore{row: row}
	mux := buildThumbHandler(t, store, storer)

	req := httptest.NewRequest("GET", "/_matrix/media/v3/thumbnail/test.local/cd-test-001?width=100&height=100", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "inline") {
		t.Errorf("Content-Disposition MUST be 'inline' per spec v1.12, got %q", cd)
	}
}
