package storage

import (
	"context"
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
// Returns a non-nil error if the object does not exist.
func (s *MinIOStorer) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.Client.GetObject(ctx, s.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// Delete removes the object stored under key.
// Returns nil if the object did not exist (MinIO RemoveObject is idempotent).
func (s *MinIOStorer) Delete(ctx context.Context, key string) error {
	return s.Client.RemoveObject(ctx, s.Bucket, key, minio.RemoveObjectOptions{})
}
