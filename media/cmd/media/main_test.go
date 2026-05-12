package main

import (
	"testing"

	"github.com/nebu/nebu/media/internal/storage"
)

// AT-1 — TestMain_StorageBackend_Local_Default
// RED: selectStorer function does not exist yet.
// When NEBU_STORAGE_BACKEND is unset (or "local"), selectStorer must return *storage.LocalStorer.
func TestMain_StorageBackend_Local_Default(t *testing.T) {
	t.Setenv("NEBU_STORAGE_BACKEND", "")
	t.Setenv("NEBU_MEDIA_STORAGE_PATH", "/tmp/test-media")

	cfg := mediaConfig{
		storageBackend: "local",
		storagePath:    "/tmp/test-media",
	}

	storer, err := selectStorer(cfg)
	if err != nil {
		t.Fatalf("selectStorer returned unexpected error: %v", err)
	}

	if _, ok := storer.(*storage.LocalStorer); !ok {
		t.Fatalf("expected *storage.LocalStorer, got %T", storer)
	}
}

// AT-2 — TestMain_StorageBackend_Minio_EnvVars
// RED: selectStorer and mediaConfig do not exist yet.
// When NEBU_STORAGE_BACKEND=minio and all required env vars are set,
// selectStorer must return *storage.MinIOStorer with correct Bucket and non-nil Client.
func TestMain_StorageBackend_Minio_EnvVars(t *testing.T) {
	cfg := mediaConfig{
		storageBackend: "minio",
		minioEndpoint:  "localhost:9000",
		minioAccessKey: "testkey",
		minioSecretKey: "testsecret",
		minioBucket:    "nebu-media",
		minioUseSSL:    false,
	}

	storer, err := selectStorer(cfg)
	if err != nil {
		t.Fatalf("selectStorer returned unexpected error: %v", err)
	}

	ms, ok := storer.(*storage.MinIOStorer)
	if !ok {
		t.Fatalf("expected *storage.MinIOStorer, got %T", storer)
	}
	if ms.Bucket != "nebu-media" {
		t.Errorf("expected Bucket=%q, got %q", "nebu-media", ms.Bucket)
	}
	if ms.Client == nil {
		t.Error("expected non-nil MinIO Client")
	}
}

// AT-3 — TestMain_StorageBackend_Minio_MissingEndpoint
// When NEBU_STORAGE_BACKEND=minio but NEBU_MINIO_ENDPOINT is empty,
// selectStorer must return an error (not nil).
func TestMain_StorageBackend_Minio_MissingEndpoint(t *testing.T) {
	cfg := mediaConfig{
		storageBackend: "minio",
		minioEndpoint:  "", // missing — must trigger error
		minioAccessKey: "testkey",
		minioSecretKey: "testsecret",
		minioBucket:    "nebu-media",
	}

	_, err := selectStorer(cfg)
	if err == nil {
		t.Fatal("expected error when NEBU_MINIO_ENDPOINT is empty, got nil")
	}
}

// TestMain_StorageBackend_Minio_MissingAccessKey
// When NEBU_STORAGE_BACKEND=minio but NEBU_MINIO_ACCESS_KEY is empty,
// selectStorer must return an error — no silent anonymous access.
func TestMain_StorageBackend_Minio_MissingAccessKey(t *testing.T) {
	cfg := mediaConfig{
		storageBackend: "minio",
		minioEndpoint:  "localhost:9000",
		minioAccessKey: "", // missing — must trigger error
		minioSecretKey: "testsecret",
		minioBucket:    "nebu-media",
	}

	_, err := selectStorer(cfg)
	if err == nil {
		t.Fatal("expected error when NEBU_MINIO_ACCESS_KEY is empty, got nil")
	}
}

// TestMain_StorageBackend_Minio_MissingSecretKey
// When NEBU_STORAGE_BACKEND=minio but NEBU_MINIO_SECRET_KEY is empty,
// selectStorer must return an error — no silent anonymous access.
func TestMain_StorageBackend_Minio_MissingSecretKey(t *testing.T) {
	cfg := mediaConfig{
		storageBackend: "minio",
		minioEndpoint:  "localhost:9000",
		minioAccessKey: "testkey",
		minioSecretKey: "", // missing — must trigger error
		minioBucket:    "nebu-media",
	}

	_, err := selectStorer(cfg)
	if err == nil {
		t.Fatal("expected error when NEBU_MINIO_SECRET_KEY is empty, got nil")
	}
}
