package thumbnail

// thumbnail.go — Core image processing functions for Story 12.5.
//
// Library choice: github.com/disintegration/imaging (MIT, pure Go, no cgo).
// Rationale: pure Go satisfies AC4 (sandboxed by construction — no network access,
// no shell exec, no external process). Alpine-compatible, no glibc dependency.
//
// Supported MIME types (detected from magic bytes, not Content-Type header):
//   Allowed:  image/jpeg, image/png, image/gif, image/webp
//   Rejected: everything else (SVG, PDF, PS, EPS, etc.) → 400 M_BAD_JSON

import (
	"bytes"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"net/http"

	"github.com/disintegration/imaging"
)

// maxSourceMegapixels is the maximum decoded source image size in pixels.
// Images larger than this are rejected to prevent decompression-bomb DoS.
// 100 MP = 100,000,000 pixels (e.g. 10000×10000).
const maxSourceMegapixels = 100_000_000

// maxGIFFrames is the maximum number of GIF frames that will be resized.
// GIFs with more frames have the excess frames silently discarded.
const maxGIFFrames = 200

// ThumbnailParams holds the validated parameters for thumbnail generation.
type ThumbnailParams struct {
	// Width is the desired thumbnail width in pixels (required, > 0).
	Width int
	// Height is the desired thumbnail height in pixels (required, > 0).
	Height int
	// Method is the resizing method: "scale" (aspect-ratio-preserved, default) or "crop" (center-crop).
	Method string
	// Animated indicates whether to return an animated thumbnail for GIF sources.
	// When true and the source is a GIF, all frames are preserved.
	// When false, a static JPEG is returned even for GIF sources (spec MUST NOT animate).
	Animated bool
}

// AllowedMIMETypes is the set of MIME types accepted for thumbnail generation.
// Detected from magic bytes via DetectMIMEType; content-type header is NOT used.
var AllowedMIMETypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// DetectMIMEType returns the MIME type of data using magic bytes (first 512 bytes).
// Uses net/http.DetectContentType, which covers JPEG, PNG, GIF, WebP, PDF, and common types.
// SVG is XML-based and detected as "text/xml; charset=utf-8" or "text/plain; charset=utf-8".
func DetectMIMEType(data []byte) string {
	probe := data
	if len(probe) > 512 {
		probe = probe[:512]
	}
	return http.DetectContentType(probe)
}

// GenerateThumbnail generates a thumbnail from imgBytes using the given params.
//
// Behaviour:
//   - method="scale": aspect-ratio-preserved resize; output fits within Width×Height
//   - method="crop":  center-cropped to exactly Width×Height
//   - animated=true + GIF source: all frames resized, animated GIF returned
//   - animated=false (or non-GIF source): static JPEG returned
//
// Returns (thumbnailBytes, contentType, error).
// contentType is "image/jpeg" for static thumbnails, "image/gif" for animated GIF thumbnails.
func GenerateThumbnail(imgBytes []byte, params ThumbnailParams) ([]byte, string, error) {
	mimeType := DetectMIMEType(imgBytes)

	// Animated GIF path: only when animated=true AND source is GIF.
	if params.Animated && mimeType == "image/gif" {
		result, err := generateAnimatedGIFThumbnail(imgBytes, params.Width, params.Height)
		if err != nil {
			return nil, "", err
		}
		return result, "image/gif", nil
	}

	// Static path: decode with imaging (handles JPEG, PNG, GIF first-frame, WebP).
	// imaging.AutoOrientation applies EXIF orientation for JPEG if present.
	src, err := imaging.Decode(bytes.NewReader(imgBytes), imaging.AutoOrientation(true))
	if err != nil {
		return nil, "", err
	}

	// Decompression-bomb defence: reject source images larger than maxSourceMegapixels.
	// imaging allocates 4 * W * H bytes for the decoded image; without this check,
	// a maliciously crafted image could exhaust server memory.
	srcPixels := src.Bounds().Dx() * src.Bounds().Dy()
	if srcPixels > maxSourceMegapixels {
		return nil, "", fmt.Errorf("source image too large: %d pixels exceeds %d pixel limit", srcPixels, maxSourceMegapixels)
	}

	var dst image.Image
	switch params.Method {
	case "crop":
		// Fill = center-crop to exactly Width×Height.
		dst = imaging.Fill(src, params.Width, params.Height, imaging.Center, imaging.Lanczos)
	default: // "scale" or empty → aspect-ratio-preserved
		// Fit = scale down to fit within Width×Height, preserving aspect ratio.
		dst = imaging.Fit(src, params.Width, params.Height, imaging.Lanczos)
	}

	// Encode result as JPEG (quality 85 — good balance of size and quality).
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, dst, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

// generateAnimatedGIFThumbnail resizes all frames of an animated GIF to width×height.
// Each frame is resized individually and re-palettized. The original Delay and LoopCount
// are preserved in the output.
func generateAnimatedGIFThumbnail(imgBytes []byte, width, height int) ([]byte, error) {
	g, err := gif.DecodeAll(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, err
	}

	// Cap the number of frames to prevent resource exhaustion on large animated GIFs.
	// Frames beyond maxGIFFrames are silently discarded (no error returned).
	if len(g.Image) > maxGIFFrames {
		g.Image = g.Image[:maxGIFFrames]
		if len(g.Delay) > maxGIFFrames {
			g.Delay = g.Delay[:maxGIFFrames]
		}
		if len(g.Disposal) > maxGIFFrames {
			g.Disposal = g.Disposal[:maxGIFFrames]
		}
	}

	for i, frame := range g.Image {
		// Resize the frame using imaging.
		resized := imaging.Resize(frame, width, height, imaging.Lanczos)

		// GIF requires *image.Paletted. Convert the resized NRGBA back to paletted.
		// Use the original frame palette; fall back to Plan9 palette if missing.
		p := frame.Palette
		if len(p) == 0 {
			p = palette.Plan9
		}
		bounds := resized.Bounds()
		paletted := image.NewPaletted(bounds, p)
		draw.FloydSteinberg.Draw(paletted, bounds, resized, bounds.Min)
		g.Image[i] = paletted
	}

	// Update GIF config dimensions.
	g.Config.Width = width
	g.Config.Height = height

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
