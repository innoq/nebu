package storage

import (
	"context"
	"io"
)

// Storer is the consumer-defined interface for persisting and retrieving
// encrypted media objects. Implementations may use the local filesystem
// (LocalStorer) or an S3-compatible object store (MinIOStorer).
//
// Key format: "<serverName>/<mediaID>" — a single slash-separated string.
// The caller constructs the key; Storer implementations must not parse or
// validate it beyond using it as an opaque identifier.
type Storer interface {
	// Put writes r (up to size bytes) under the given key.
	// size is a hint to the implementation (e.g. for S3 Content-Length);
	// passing -1 means unknown size.
	Put(ctx context.Context, key string, r io.Reader, size int64) error

	// Get returns a ReadCloser for the object stored under key.
	// The caller is responsible for closing the returned ReadCloser.
	// Returns a non-nil error if the key does not exist.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the object stored under key.
	// Returns nil if the object did not exist (idempotent).
	Delete(ctx context.Context, key string) error
}
