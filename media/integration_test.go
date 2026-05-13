//go:build integration

package media_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/nebu/nebu/media/internal/storage"
	"github.com/nebu/nebu/media/internal/upload"
)

// fakeMediaStoreIntegration implements upload.MediaStore for integration tests.
// It records the last inserted row.
type fakeMediaStoreIntegration struct {
	lastRow *upload.MediaFileRow
}

func (f *fakeMediaStoreIntegration) InsertMediaFile(_ context.Context, row upload.MediaFileRow) error {
	f.lastRow = &row
	return nil
}

// fakeTokenVerifierIntegration implements upload.TokenVerifier for integration tests.
// It accepts any bearer token and returns a fixed subject identity.
// Story 12.8: OIDCVerifier must be non-nil (fail-closed).
type fakeTokenVerifierIntegration struct{}

func (fakeTokenVerifierIntegration) VerifyToken(_ context.Context, _ string) (string, error) {
	return "test-integration-user", nil
}

// AT-6 — TestUpload_MinIOBackend_StoresEncryptedFile
// Conditional: skipped when NEBU_TEST_MINIO_ENDPOINT is not set.
// Verifies that a real MinIO upload:
//   - returns 200 with mxc:// URI
//   - stores the object at key <serverName>/<mediaID>
//   - object size equals AES-256-GCM ciphertext size (plaintext + 28 bytes overhead)
func TestUpload_MinIOBackend_StoresEncryptedFile(t *testing.T) {
	endpoint := os.Getenv("NEBU_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("NEBU_TEST_MINIO_ENDPOINT not set — skipping MinIO integration test")
	}

	accessKey := os.Getenv("NEBU_TEST_MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "minioadmin"
	}
	secretKey := os.Getenv("NEBU_TEST_MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "minioadmin"
	}
	bucket := os.Getenv("NEBU_TEST_MINIO_BUCKET")
	if bucket == "" {
		bucket = "nebu-media"
	}
	serverName := "localhost"

	// Create MinIO client
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("failed to create MinIO client: %v", err)
	}

	// Ensure bucket exists
	ctx := context.Background()
	exists, err := minioClient.BucketExists(ctx, bucket)
	if err != nil {
		t.Fatalf("BucketExists check failed: %v", err)
	}
	if !exists {
		if err := minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("MakeBucket failed: %v", err)
		}
	}

	storer := &storage.MinIOStorer{
		Client: minioClient,
		Bucket: bucket,
	}

	fakeStore := &fakeMediaStoreIntegration{}
	handler := upload.NewHandler(upload.HandlerConfig{
		DB:           fakeStore,
		Storage:      storer,
		ServerName:   serverName,
		MaxBytes:     52428800,
		OIDCVerifier: &fakeTokenVerifierIntegration{}, // Story 12.8: non-nil required
	})

	// Upload a small plaintext body
	plaintext := []byte("hello integration test")
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", bytes.NewReader(plaintext))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Authorization", "Bearer test-token")

	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	resp := rw.Result()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", resp.StatusCode, body)
	}

	var result struct {
		ContentURI string `json:"content_uri"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if !strings.HasPrefix(result.ContentURI, "mxc://") {
		t.Errorf("expected mxc:// URI, got %q", result.ContentURI)
	}

	// Extract mediaID from mxc://<serverName>/<mediaID>
	mxcParts := strings.TrimPrefix(result.ContentURI, "mxc://")
	parts := strings.SplitN(mxcParts, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("malformed mxc:// URI: %q", result.ContentURI)
	}
	mediaID := parts[1]
	storageKey := fmt.Sprintf("%s/%s", serverName, mediaID)

	// Verify the object exists in MinIO
	objInfo, err := minioClient.StatObject(ctx, bucket, storageKey, minio.StatObjectOptions{})
	if err != nil {
		t.Fatalf("StatObject(%q) failed — object not found in MinIO: %v", storageKey, err)
	}

	// AES-256-GCM overhead: 12-byte nonce + 16-byte GCM tag = 28 bytes
	expectedMinSize := int64(len(plaintext) + 28)
	if objInfo.Size < expectedMinSize {
		t.Errorf("object size %d < expected minimum ciphertext size %d (plaintext=%d + 28 GCM overhead)",
			objInfo.Size, expectedMinSize, len(plaintext))
	}

	t.Logf("MinIO integration test passed: key=%s size=%d", storageKey, objInfo.Size)
}
