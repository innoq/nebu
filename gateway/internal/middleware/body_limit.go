package middleware

// Story 5.20 — Request Body Size Limits + HTTP Server Timeouts
//
// AC 3: BodyLimitMiddleware wraps r.Body with http.MaxBytesReader so the Go
//       runtime enforces the limit at the transport layer.
// AC 4: When the limit is exceeded, the middleware intercepts the response and
//       returns HTTP 413 with a Matrix-compatible JSON error body (M_TOO_LARGE).

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// limitedBody wraps the MaxBytesReader and records whether the limit was hit.
type limitedBody struct {
	inner    io.ReadCloser
	exceeded bool
}

func (lb *limitedBody) Read(p []byte) (int, error) {
	n, err := lb.inner.Read(p)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			lb.exceeded = true
		}
	}
	return n, err
}

func (lb *limitedBody) Close() error {
	return lb.inner.Close()
}

// bufferedResponseWriter buffers the response so the middleware can substitute
// a 413 response if the body limit was exceeded before committing anything to
// the underlying http.ResponseWriter.
type bufferedResponseWriter struct {
	http.ResponseWriter
	buf    bytes.Buffer
	status int
}

func (b *bufferedResponseWriter) WriteHeader(status int) {
	b.status = status
}

func (b *bufferedResponseWriter) Write(p []byte) (int, error) {
	return b.buf.Write(p)
}

// flush commits the buffered response to the real writer.
func (b *bufferedResponseWriter) flush() {
	if b.status != 0 {
		b.ResponseWriter.WriteHeader(b.status)
	}
	_, _ = b.ResponseWriter.Write(b.buf.Bytes())
}

// BodyLimitMiddleware returns a middleware that enforces a maximum request body
// size of max bytes. When a request body exceeds the limit, the middleware
// returns HTTP 413 (Request Entity Too Large) with a Matrix error JSON body
// containing errcode "M_TOO_LARGE" before any handler response is committed.
//
// For bodies within the limit, the inner handler's response is passed through
// unchanged.
func BodyLimitMiddleware(max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lb := &limitedBody{inner: http.MaxBytesReader(w, r.Body, max)}
			r.Body = lb

			bw := &bufferedResponseWriter{ResponseWriter: w}
			next.ServeHTTP(bw, r)

			if lb.exceeded {
				// Discard buffered handler response; write 413 instead.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"errcode": "M_TOO_LARGE",
					"error":   "Request body exceeds the maximum allowed size",
				})
				return
			}

			// Body was within limits — commit the buffered response.
			bw.flush()
		})
	}
}
