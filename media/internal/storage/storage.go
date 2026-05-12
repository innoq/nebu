package storage

import (
	"context"
	"errors"
	"io"
)

// Sentinel errors returned by Storer implementations.
// Callers use errors.Is to distinguish error categories:
//
//   errors.Is(err, ErrNotFound)           — object does not exist in the store
//   errors.Is(err, ErrStorageUnavailable) — backend is unreachable or degraded
var (
	// ErrNotFound is returned by Storer.Get when the requested object does not
	// exist. Maps to HTTP 404 M_NOT_FOUND in the download handler.
	ErrNotFound = errors.New("storage: object not found")

	// ErrStorageUnavailable is returned by Storer.Get when the backend is
	// unreachable (network error, connection refused, MinIO degraded).
	// Maps to HTTP 502 M_UNKNOWN in the download handler.
	ErrStorageUnavailable = errors.New("storage: storage backend unavailable")
)

// Storer is the consumer-defined interface for persisting and retrieving
// encrypted media objects. Implementations may use the local filesystem
// (LocalStorer) or an S3-compatible object store (MinIOStorer).
//
// Key format: "<serverName>/<mediaID>" — a single slash-separated string.
// The caller constructs the key; Storer implementations must not parse or
// validate it beyond using it as an opaque identifier.
//
// Error semantics:
//   - ErrNotFound:          object does not exist under the given key
//   - ErrStorageUnavailable: backend unreachable or degraded
//   - other errors:         implementation-specific, treated as internal errors
type Storer interface {
	// Put writes r (up to size bytes) under the given key.
	// size is a hint to the implementation (e.g. for S3 Content-Length);
	// passing -1 means unknown size.
	Put(ctx context.Context, key string, r io.Reader, size int64) error

	// Get returns a ReadCloser for the object stored under key.
	// The caller is responsible for closing the returned ReadCloser.
	// Returns ErrNotFound (wrapped) if the key does not exist.
	// Returns ErrStorageUnavailable (wrapped) if the backend is unreachable.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the object stored under key.
	// Returns nil if the object did not exist (idempotent).
	Delete(ctx context.Context, key string) error
}
