package thumbnail_test

// ─── Story 12.5 ATDD Tests — Thumbnail image processing unit tests ────────────
//
// These tests will FAIL until:
//   1. media/internal/thumbnail/thumbnail.go defines ThumbnailParams, GenerateThumbnail,
//      DetectMIMEType
//   2. github.com/disintegration/imaging is added to go.mod
//
// Test strategy:
//   - All image processing logic is tested without HTTP — pure function tests.
//   - Synthetic images are created programmatically using stdlib image/jpeg/png/gif.
//   - No real MinIO/storage interaction — these test the core transformation logic only.
//   - Tests are in package thumbnail_test (black-box) to test the public API.
//
// Failing reason before implementation:
//   Package "github.com/nebu/nebu/media/internal/thumbnail" does not exist.
//   ThumbnailParams, GenerateThumbnail, DetectMIMEType are undefined.

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/nebu/nebu/media/internal/thumbnail"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// makeSyntheticJPEG creates a minimal JPEG image of the given dimensions.
// Each pixel is colored to make the image non-trivially compressible.
func makeSyntheticJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("makeSyntheticJPEG: %v", err)
	}
	return buf.Bytes()
}

// makeSyntheticPNG creates a minimal PNG image of the given dimensions.
func makeSyntheticPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 0, G: uint8(x % 256), B: uint8(y % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("makeSyntheticPNG: %v", err)
	}
	return buf.Bytes()
}

// makeSyntheticGIF creates a minimal 2-frame animated GIF.
func makeSyntheticGIF(t *testing.T, w, h int) []byte {
	t.Helper()
	palette := color.Palette{
		color.RGBA{R: 255, G: 0, B: 0, A: 255},
		color.RGBA{R: 0, G: 0, B: 255, A: 255},
		color.RGBA{R: 255, G: 255, B: 255, A: 255},
	}

	makeFrame := func(c color.Color) *image.Paletted {
		frame := image.NewPaletted(image.Rect(0, 0, w, h), palette)
		idx := uint8(palette.Index(c))
		for i := range frame.Pix {
			frame.Pix[i] = idx
		}
		return frame
	}

	g := &gif.GIF{
		Image:     []*image.Paletted{makeFrame(palette[0]), makeFrame(palette[1])},
		Delay:     []int{10, 10},
		LoopCount: 0,
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatalf("makeSyntheticGIF: %v", err)
	}
	return buf.Bytes()
}

// ─── AT-1: Scale method preserves aspect ratio and fits within bounds ─────────
//
// AC1 — scale method: a 200×300 JPEG scaled to 100×100 must have
// aspect-ratio-preserved dimensions ≤ 100×100.
// For a 200×300 image: width/height = 2/3.
// Fitting within 100×100: height=100, width=67 (floor(100*200/300)).
//
// Failing reason: thumbnail.ThumbnailParams and thumbnail.GenerateThumbnail
// do not exist.

func TestGenerateThumbnail_Scale_PreservesAspectRatio(t *testing.T) {
	imgBytes := makeSyntheticJPEG(t, 200, 300)

	params := thumbnail.ThumbnailParams{
		Width:  100,
		Height: 100,
		Method: "scale",
	}

	result, contentType, err := thumbnail.GenerateThumbnail(imgBytes, params)
	if err != nil {
		t.Fatalf("GenerateThumbnail returned error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("GenerateThumbnail returned empty bytes")
	}
	if contentType != "image/jpeg" {
		t.Errorf("expected content type image/jpeg, got %q", contentType)
	}

	// Decode the output to check dimensions.
	decoded, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("could not decode result JPEG: %v", err)
	}
	bounds := decoded.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	// Must fit within 100×100.
	if w > 100 || h > 100 {
		t.Errorf("thumbnail exceeds requested bounds: got %d×%d, want ≤ 100×100", w, h)
	}
	// Aspect ratio must be preserved: original 200×300 → 2:3 ratio.
	// Expected: w=67, h=100 (or very close, allowing ±1 for rounding).
	// At minimum: neither dimension should be zero.
	if w == 0 || h == 0 {
		t.Errorf("thumbnail has zero dimension: %d×%d", w, h)
	}
	// Width must equal floor(h * 200/300), i.e. h * 2/3.
	expectedW := h * 200 / 300
	if w < expectedW-1 || w > expectedW+1 {
		t.Errorf("aspect ratio not preserved: got %d×%d, expected width ~%d for height %d", w, h, expectedW, h)
	}
}

// ─── AT-2: Crop method returns exactly the requested dimensions ───────────────
//
// AC2 — crop method: output MUST be exactly 100×100 pixels.
//
// Failing reason: thumbnail.ThumbnailParams and thumbnail.GenerateThumbnail
// do not exist.

func TestGenerateThumbnail_Crop_ReturnsExactDimensions(t *testing.T) {
	imgBytes := makeSyntheticJPEG(t, 200, 300)

	params := thumbnail.ThumbnailParams{
		Width:  100,
		Height: 100,
		Method: "crop",
	}

	result, contentType, err := thumbnail.GenerateThumbnail(imgBytes, params)
	if err != nil {
		t.Fatalf("GenerateThumbnail returned error: %v", err)
	}
	if contentType != "image/jpeg" {
		t.Errorf("expected content type image/jpeg, got %q", contentType)
	}

	decoded, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("could not decode result JPEG: %v", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != 100 || bounds.Dy() != 100 {
		t.Errorf("crop result is %d×%d, want exactly 100×100", bounds.Dx(), bounds.Dy())
	}
}

// ─── AT-3: DetectMIMEType identifies SVG and PDF as non-image types ───────────
//
// AC3 — MIME type detection from magic bytes:
//   - SVG bytes (starts with XML "<svg") → "image/svg+xml" or similar text type
//   - PDF bytes (starts with "%PDF") → "application/pdf"
//   Both must NOT be in the allowed image set.
//
// Failing reason: thumbnail.DetectMIMEType does not exist.

func TestDetectMIMEType_SVG_ReturnsSVGType(t *testing.T) {
	// SVG file: text/plain; charset=utf-8 (from http.DetectContentType) or image/svg+xml
	// Either way, it's not a supported image type.
	svgBytes := []byte(`<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect width="100" height="100" fill="red"/></svg>`)

	mimeType := thumbnail.DetectMIMEType(svgBytes)

	// SVG is not a valid thumbnail source — must NOT be in the allowed set.
	allowed := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
	}
	if allowed[mimeType] {
		t.Errorf("SVG detected as allowed MIME type %q — must not be in allowed image set", mimeType)
	}
}

func TestDetectMIMEType_PDF_ReturnsPDFType(t *testing.T) {
	// PDF magic bytes: starts with %PDF
	pdfBytes := []byte("%PDF-1.4 fake-pdf-content-for-mime-detection")

	mimeType := thumbnail.DetectMIMEType(pdfBytes)

	// PDF must not be in the allowed image set.
	allowed := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
	}
	if allowed[mimeType] {
		t.Errorf("PDF detected as allowed MIME type %q — must not be in allowed image set", mimeType)
	}
	// Must be classified as application/pdf
	if mimeType != "application/pdf" {
		t.Errorf("PDF magic bytes: expected application/pdf, got %q", mimeType)
	}
}

func TestDetectMIMEType_JPEG_ReturnsImageJPEG(t *testing.T) {
	// JPEG bytes must be correctly detected.
	jpegBytes := makeSyntheticJPEG(t, 10, 10)

	mimeType := thumbnail.DetectMIMEType(jpegBytes)

	if mimeType != "image/jpeg" {
		t.Errorf("JPEG: expected image/jpeg, got %q", mimeType)
	}
}

func TestDetectMIMEType_GIF_ReturnsImageGIF(t *testing.T) {
	// GIF bytes must be correctly detected.
	gifBytes := makeSyntheticGIF(t, 10, 10)

	mimeType := thumbnail.DetectMIMEType(gifBytes)

	if mimeType != "image/gif" {
		t.Errorf("GIF: expected image/gif, got %q", mimeType)
	}
}

func TestDetectMIMEType_PNG_ReturnsImagePNG(t *testing.T) {
	pngBytes := makeSyntheticPNG(t, 10, 10)

	mimeType := thumbnail.DetectMIMEType(pngBytes)

	if mimeType != "image/png" {
		t.Errorf("PNG: expected image/png, got %q", mimeType)
	}
}

// ─── AT-8b: Animated GIF — GenerateThumbnail preserves animation ─────────────
//
// AC6 — animated=true + GIF source: result must be an animated GIF.
// This is the pure-processing version (no HTTP).
//
// Failing reason: thumbnail.GenerateThumbnail does not exist.

func TestGenerateThumbnail_AnimatedGIF_AnimatedTrue_PreservesAnimation(t *testing.T) {
	gifBytes := makeSyntheticGIF(t, 200, 200)

	params := thumbnail.ThumbnailParams{
		Width:    100,
		Height:   100,
		Method:   "scale",
		Animated: true,
	}

	result, contentType, err := thumbnail.GenerateThumbnail(gifBytes, params)
	if err != nil {
		t.Fatalf("GenerateThumbnail returned error for animated GIF: %v", err)
	}
	if contentType != "image/gif" {
		t.Errorf("animated GIF: expected Content-Type image/gif, got %q", contentType)
	}
	// Result must be a valid GIF with at least 2 frames.
	g, err := gif.DecodeAll(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("could not decode result as GIF: %v", err)
	}
	if len(g.Image) < 2 {
		t.Errorf("animated GIF must have ≥ 2 frames, got %d", len(g.Image))
	}
}

// ─── AT-9: animated=false on GIF → static JPEG (spec MUST NOT animate) ───────
//
// Spec MUST: animated=false → server MUST NOT return an animated thumbnail.
//
// Failing reason: thumbnail.GenerateThumbnail does not exist.

func TestGenerateThumbnail_AnimatedGIF_AnimatedFalse_ReturnsStaticJPEG(t *testing.T) {
	gifBytes := makeSyntheticGIF(t, 200, 200)

	params := thumbnail.ThumbnailParams{
		Width:    100,
		Height:   100,
		Method:   "scale",
		Animated: false,
	}

	result, contentType, err := thumbnail.GenerateThumbnail(gifBytes, params)
	if err != nil {
		t.Fatalf("GenerateThumbnail returned error: %v", err)
	}
	// Must return static image — JPEG or PNG, NOT GIF.
	if contentType == "image/gif" {
		t.Errorf("animated=false must NOT return animated GIF — got Content-Type %q", contentType)
	}
	// Result must be a valid static JPEG.
	if contentType != "image/jpeg" {
		t.Errorf("expected image/jpeg for static GIF thumbnail, got %q", contentType)
	}
	// Must be decodable as a single-frame image.
	_, _, err = image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("result is not a valid image: %v", err)
	}
}
