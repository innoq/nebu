package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorer implements Storer using the local filesystem.
// Keys are split on the first "/" into (subDir, name); files are stored at
// BasePath/<subDir>/<name>.
type LocalStorer struct {
	BasePath string
}

// Compile-time assertion: *LocalStorer satisfies Storer.
var _ Storer = &LocalStorer{}

// Put reads all bytes from r and writes them to BasePath/<subDir>/<name>,
// creating the subdirectory if it does not exist.
// The size parameter is accepted for interface compatibility but is not used.
func (s *LocalStorer) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	subDir, name := splitStorageKey(key)
	dir := filepath.Join(s.BasePath, subDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o600)
}

// Get opens BasePath/<subDir>/<name> and returns its contents as an io.ReadCloser.
// Returns a non-nil error if the file does not exist.
func (s *LocalStorer) Get(_ context.Context, key string) (io.ReadCloser, error) {
	subDir, name := splitStorageKey(key)
	filePath := filepath.Join(s.BasePath, subDir, name)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// Delete removes the file at BasePath/<subDir>/<name>.
// Returns nil if the file does not exist (idempotent).
func (s *LocalStorer) Delete(_ context.Context, key string) error {
	subDir, name := splitStorageKey(key)
	filePath := filepath.Join(s.BasePath, subDir, name)
	err := os.Remove(filePath)
	if os.IsNotExist(err) {
		return nil // idempotent
	}
	return err
}

// splitStorageKey splits "subDir/name" into its two components.
// If no "/" is present, returns ("", key).
func splitStorageKey(key string) (subDir, name string) {
	idx := strings.IndexByte(key, '/')
	if idx < 0 {
		return "", key
	}
	return key[:idx], key[idx+1:]
}
