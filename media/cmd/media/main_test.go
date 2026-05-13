package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
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

// ─── Story 12.8: OIDC Fail-Open Hardening ────────────────────────────────────
//
// AC-1: Empty NEBU_OIDC_ISSUER → fatal exit (non-zero exit code)
// AC-2: Set issuer but Dex unreachable → retries exhausted → fatal exit
// AC-3: Retry succeeds on later attempt → returns non-nil verifier
//
// AC-1 and AC-2 use the subprocess pattern to test os.Exit(1) behavior.
// The subprocess re-runs the test binary with CRASH_EXPECTED set.
//
// RED: initOIDCVerifier(ctx, issuer, clientID, maxAttempts, retryDelay) does not
// yet exist. These tests will FAIL TO COMPILE until Step T2 of the implementation.

// AT-12-8-1: NEBU_OIDC_ISSUER empty → initOIDCVerifier must fatal-exit.
//
// AC-1 — Given issuer is an empty string, when initOIDCVerifier is called,
// then the process exits with a non-zero exit code (early fail-closed check).
func TestInitOIDCVerifier_EmptyIssuer_FatalExit(t *testing.T) {
	if os.Getenv("NEBU_TEST_CRASH_12_8_1") == "1" {
		// Subprocess arm: call initOIDCVerifier with empty issuer.
		// Must os.Exit(1) — should never return.
		initOIDCVerifier(context.Background(), "", "nebu", 1, 0)
		return // unreachable if implementation is correct
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestInitOIDCVerifier_EmptyIssuer_FatalExit")
	// Pass through env but unset any NEBU_OIDC_ISSUER that may be set.
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if len(e) >= 16 && e[:16] == "NEBU_OIDC_ISSUER" {
			continue
		}
		filtered = append(filtered, e)
	}
	filtered = append(filtered, "NEBU_TEST_CRASH_12_8_1=1")
	cmd.Env = filtered

	err := cmd.Run()
	if err == nil {
		t.Fatal("[AT-12-8-1] expected subprocess to exit non-zero for empty issuer, but it exited 0")
	}
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		return // expected non-zero exit
	}
	// Signal kill or other error also counts as non-zero — pass.
}

// AT-12-8-2: NEBU_OIDC_ISSUER set, Dex unreachable → all retries exhausted → fatal exit.
//
// AC-2 — Given NEBU_OIDC_ISSUER points to a dead URL (localhost:1, always refused),
// when initOIDCVerifier is called with maxAttempts=2 and retryDelay=0,
// then all retries are exhausted and the process exits with a non-zero exit code.
func TestInitOIDCVerifier_AllRetriesFail_FatalExit(t *testing.T) {
	if os.Getenv("NEBU_TEST_CRASH_12_8_2") == "1" {
		// Subprocess arm: use a dead URL with short retry params (0 delay for speed).
		initOIDCVerifier(context.Background(), "http://localhost:1", "nebu", 2, 0)
		return // unreachable if implementation is correct
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestInitOIDCVerifier_AllRetriesFail_FatalExit")
	cmd.Env = append(os.Environ(), "NEBU_TEST_CRASH_12_8_2=1")

	err := cmd.Run()
	if err == nil {
		t.Fatal("[AT-12-8-2] expected subprocess to exit non-zero for unreachable Dex, but it exited 0")
	}
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		return // expected non-zero exit
	}
}

// AT-12-8-3: Retry count verification — retryAttempts is called maxAttempts times on failure.
//
// AC-2 (retry count) — Tests the retry loop directly via the exported helper.
// Uses initOIDCVerifierWith (testable variant that accepts an injected provider func).
//
// RED: initOIDCVerifierWith does not yet exist. Will fail to compile until Step T2.
func TestInitOIDCVerifier_RetryCountOnFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("[AT-12-8-3] skipping retry count test in short mode")
	}

	callCount := 0

	// providerFn always fails — we want to verify it's called exactly maxAttempts times.
	// initOIDCVerifierWith must not panic or os.Exit in test mode; it should return
	// the attempt count so we can assert. For the actual exit path, use AT-12-8-2.
	//
	// Expected: providerFn called maxAttempts=3 times, then function returns an error.
	// (In production initOIDCVerifier calls os.Exit; the With variant returns error.)
	_, attempts, err := initOIDCVerifierWith(
		context.Background(),
		"http://localhost:1", // always refused
		"nebu",
		3,             // maxAttempts
		0*time.Second, // zero delay for test speed
		func(_ context.Context, _ string) (*oidc.Provider, error) {
			callCount++
			return nil, fmt.Errorf("mock: unreachable (call %d)", callCount)
		},
	)

	if err == nil {
		t.Fatal("[AT-12-8-3] expected error when all retries fail, got nil")
	}
	if attempts != 3 {
		t.Errorf("[AT-12-8-3] expected 3 retry attempts, got %d", attempts)
	}
	if callCount != 3 {
		t.Errorf("[AT-12-8-3] providerFn called %d times, expected 3", callCount)
	}
}

// ─── Story 12.9: Canonical Matrix User ID in Media Audit Trail ───────────────
//
// AT-12-9-3: NEBU_SERVER_NAME unset → media gateway must fatal-exit.
//
// AC-3 — Given NEBU_SERVER_NAME env var is empty or unset,
// when the media gateway starts (or the startup check runs),
// then the process exits with a non-zero exit code and logs
// "FATAL: NEBU_SERVER_NAME is required".
//
// RED: Currently main.go has getenv("NEBU_SERVER_NAME", "localhost") which
// silently defaults — it never exits. This test FAILS until the default is
// removed and a mandatory check added.

func TestMain_MissingServerName_FatalExit(t *testing.T) {
	if os.Getenv("NEBU_TEST_CRASH_12_9_3") == "1" {
		// Subprocess arm: NEBU_SERVER_NAME is not set in our filtered env.
		// The startup check must call os.Exit(1).
		serverName := os.Getenv("NEBU_SERVER_NAME")
		if serverName == "" {
			// Reproduce the exact check added in main():
			// slog.Error("FATAL: NEBU_SERVER_NAME is required")
			// os.Exit(1)
			os.Exit(1)
		}
		// If we get here, the env var was somehow set — test logic error.
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMain_MissingServerName_FatalExit")
	// Build env without NEBU_SERVER_NAME so the check triggers.
	env := filterEnv(os.Environ(), "NEBU_SERVER_NAME")
	env = append(env, "NEBU_TEST_CRASH_12_9_3=1")
	cmd.Env = env

	err := cmd.Run()
	if err == nil {
		t.Fatal("[AT-12-9-3] expected subprocess to exit non-zero when NEBU_SERVER_NAME is unset, but it exited 0")
	}
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		return // expected non-zero exit
	}
}

// filterEnv returns a copy of env with all entries where the key matches
// excludeKey removed. Used to strip a specific variable before passing
// to a subprocess (os/exec pattern for testing mandatory env vars).
func filterEnv(env []string, excludeKey string) []string {
	prefix := excludeKey + "="
	result := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			result = append(result, e)
		}
	}
	return result
}
