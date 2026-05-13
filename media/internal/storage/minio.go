package storage

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
)

// MinIOStorer implements Storer using the MinIO Go SDK (minio-go/v7).
// Objects are stored in the configured Bucket under the given key.
//
// The Client field must be a fully initialised *minio.Client (e.g. via
// minio.New or minio.NewWithOptions). The Bucket must exist before use;
// bucket creation is outside the scope of this type.
type MinIOStorer struct {
	Client *minio.Client
	Bucket string
}

// Compile-time assertion: *MinIOStorer satisfies Storer.
var _ Storer = &MinIOStorer{}

// Put uploads r as an object in the configured bucket under key.
// size is passed as ContentLength; pass -1 if the size is unknown.
func (s *MinIOStorer) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := s.Client.PutObject(ctx, s.Bucket, key, r, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	return err
}

// Get retrieves the object stored under key and returns it as an io.ReadCloser.
// The caller is responsible for closing the returned ReadCloser.
//
// Error semantics (Story 12.4):
//   - Returns ErrNotFound (wrapped) when the object does not exist in the bucket.
//   - Returns ErrStorageUnavailable (wrapped) for network or backend errors.
//
// Implementation note: minio.Client.GetObject does NOT return an error for missing
// objects at call time. The *minio.Object is returned immediately; errors surface
// on the first I/O operation. We call obj.Stat() eagerly to detect NoSuchKey before
// returning, so callers receive a classified error without needing to handle raw
// minio.ErrorResponse values.
func (s *MinIOStorer) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.Client.GetObject(ctx, s.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ClassifyMinIOError(err)
	}
	// Eagerly probe for existence — Stat() triggers the actual remote call.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, ClassifyMinIOError(err)
	}
	return obj, nil
}

// Delete removes the object stored under key.
// Returns nil if the object did not exist (MinIO RemoveObject is idempotent).
func (s *MinIOStorer) Delete(ctx context.Context, key string) error {
	return s.Client.RemoveObject(ctx, s.Bucket, key, minio.RemoveObjectOptions{})
}

// ClassifyMinIOError converts a raw MinIO SDK error into a storage sentinel error.
// It is exported so that tests can verify classification without a real MinIO server.
//
// Classification rules:
//   - minio.ErrorResponse (or wrapped) with Code "NoSuchKey" or StatusCode 404 → ErrNotFound
//   - all other errors (network errors, other MinIO codes)                       → ErrStorageUnavailable
//
// Implementation note: minio.ToErrorResponse uses a direct type switch, so it cannot
// unwrap errors wrapped with fmt.Errorf("%w", ...). We use errors.As instead, which
// correctly traverses the error chain.
func ClassifyMinIOError(err error) error {
	if err == nil {
		return nil
	}
	// Use errors.As to unwrap through any fmt.Errorf wrapping layers.
	var resp minio.ErrorResponse
	if errors.As(err, &resp) {
		if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
			return fmt.Errorf("%w: %s", ErrNotFound, resp.Code)
		}
		// Other MinIO error codes (e.g. Access Denied, Bucket Not Found) are
		// treated as storage unavailable — upstream dependency is degraded.
		return fmt.Errorf("%w: minio error code %s", ErrStorageUnavailable, resp.Code)
	}
	// Non-MinIO errors (network, DNS, TLS) → storage unavailable.
	return fmt.Errorf("%w: minio request failed", ErrStorageUnavailable)
}
