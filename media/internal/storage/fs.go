package storage

import (
	"os"
	"path/filepath"
)

// Store writes data to <basePath>/<subDir>/<name>, creating subdirectories as needed.
// Returns the full file path on success.
func Store(basePath, subDir, name string, data []byte) (string, error) {
	dir := filepath.Join(basePath, subDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fullPath := filepath.Join(dir, name)
	if err := os.WriteFile(fullPath, data, 0o600); err != nil {
		return "", err
	}
	return fullPath, nil
}
