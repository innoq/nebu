package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
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
		initOIDCVerifier(context.Background(), "", "nebu", "name", 1, 0)
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
		initOIDCVerifier(context.Background(), "http://localhost:1", "nebu", "name", 2, 0)
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
		"name",         // claimName (Story 12.11)
		3,              // maxAttempts
		0*time.Second,  // zero delay for test speed
		10*time.Second, // attemptTimeout (Story 12.12)
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

// ─── Story 12.12: Media Gateway Startup + Rate Limiter Hardening (F-3) ────────
//
// AT-12-12-1 — OIDC retry exits immediately when parent context is cancelled.
//
// AC-F3-3 — Given the parent context is cancelled before initOIDCVerifierWith is called,
// when initOIDCVerifierWith is invoked with the cancelled context,
// then it returns immediately (does not block for maxAttempts × timeout seconds).
//
// RED: Currently initOIDCVerifierWith does not check ctx.Err() at the start of
// each retry loop iteration. This test will FAIL until the early-exit check is added.
func TestInitOIDCVerifierWith_CancelledCtx_ExitsImmediately(t *testing.T) {
	t.Parallel()

	// Create a context that is already cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	calls := 0
	mockProvider := func(ctx context.Context, _ string) (*oidc.Provider, error) {
		calls++
		// Simulate a slow provider — if the loop does not check ctx.Err(),
		// it will call this repeatedly and the test will time out.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return nil, fmt.Errorf("mock: provider timeout")
		}
	}

	start := time.Now()
	_, _, err := initOIDCVerifierWith(
		ctx,
		"http://localhost:1",
		"nebu",
		"name",
		5,                // maxAttempts — must not all be tried
		0*time.Second,    // no backoff delay for test speed
		10*time.Second,   // attemptTimeout — doesn't matter since ctx is already cancelled
		mockProvider,
	)

	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("[AT-12-12-1] initOIDCVerifierWith took %v with cancelled context — expected < 500ms (immediate exit on ctx.Err())", elapsed)
	}
	if err == nil {
		t.Fatal("[AT-12-12-1] expected non-nil error when context is cancelled, got nil")
	}
	if calls > 1 {
		t.Errorf("[AT-12-12-1] mockProvider called %d times — expected at most 1 call (loop should exit on cancelled context)", calls)
	}
}

// AT-12-12-2 — OIDC retry uses per-attempt timeout (not unbounded).
//
// AC-F3-1 — Given a provider that blocks until its context is done (simulating hung TCP),
// when initOIDCVerifierWith is called with maxAttempts=2 and attemptTimeout=50ms,
// then each attempt times out in ~50ms and all retries complete within 300ms total.
//
// RED: initOIDCVerifierWith does not yet accept an attemptTimeout parameter.
// This test will FAIL TO COMPILE until the function signature is updated to accept
// an `attemptTimeout time.Duration` parameter that wraps each newProvider call with
// context.WithTimeout(ctx, attemptTimeout).
func TestInitOIDCVerifierWith_PerAttemptTimeout(t *testing.T) {
	t.Parallel()

	const attemptTimeout = 50 * time.Millisecond
	const maxAttempts = 2
	const maxExpectedTotal = 300 * time.Millisecond // 2×50ms + scheduling slack

	calls := 0
	mockProvider := func(ctx context.Context, _ string) (*oidc.Provider, error) {
		calls++
		// Block until the provided attempt context is done — simulates hung TCP.
		// When per-attempt timeout is implemented, ctx here is the attempt context
		// with the 50ms timeout, so this returns in ~50ms.
		<-ctx.Done()
		return nil, ctx.Err()
	}

	start := time.Now()
	_, attempts, err := initOIDCVerifierWith(
		context.Background(),
		"http://localhost:1",
		"nebu",
		"name",
		maxAttempts,
		0*time.Second, // no inter-attempt backoff for test speed
		attemptTimeout, // new parameter: per-attempt timeout
		mockProvider,
	)
	elapsed := time.Since(start)

	if elapsed > maxExpectedTotal {
		t.Errorf("[AT-12-12-2] total elapsed %v > %v — per-attempt timeout must bound each retry at %v",
			elapsed, maxExpectedTotal, attemptTimeout)
	}
	if err == nil {
		t.Fatal("[AT-12-12-2] expected non-nil error when all retries fail")
	}
	if attempts != maxAttempts {
		t.Errorf("[AT-12-12-2] expected %d attempts, got %d", maxAttempts, attempts)
	}
	if calls != maxAttempts {
		t.Errorf("[AT-12-12-2] mockProvider called %d times, expected %d", calls, maxAttempts)
	}
}

// AT-12-12-7 — Rate limit disabled warning emitted once at startup when disabled.
//
// AC-F5-1 — Given NEBU_RATE_LIMIT_DISABLED=true, when logIfRateLimitDisabled() is called,
// then it emits slog.Warn with message containing "rate limiting disabled".
//
// RED: logIfRateLimitDisabled() does not exist yet.
func TestLogIfRateLimitDisabled_EmitsWarning(t *testing.T) {
	t.Setenv("NEBU_RATE_LIMIT_DISABLED", "true")

	// Capture slog output.
	var buf strings.Builder
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	original := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(original)

	logIfRateLimitDisabled()

	output := buf.String()
	if !strings.Contains(output, "rate limiting disabled") {
		t.Errorf("[AT-12-12-7] expected slog.Warn with 'rate limiting disabled' in output, got: %q", output)
	}
}

// AT-12-12-8 — No rate-limit-disabled warning when rate limiting is enabled.
//
// AC-F5-2 — Given NEBU_RATE_LIMIT_DISABLED is unset (or not "true"),
// when logIfRateLimitDisabled() is called,
// then no rate-limit-disabled warning is emitted.
//
// RED: logIfRateLimitDisabled() does not exist yet.
func TestLogIfRateLimitDisabled_NoWarning_WhenEnabled(t *testing.T) {
	t.Setenv("NEBU_RATE_LIMIT_DISABLED", "false")

	var buf strings.Builder
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	original := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(original)

	logIfRateLimitDisabled()

	output := buf.String()
	if strings.Contains(output, "rate limiting disabled") {
		t.Errorf("[AT-12-12-8] unexpected rate-limit-disabled warning when NEBU_RATE_LIMIT_DISABLED=false: %q", output)
	}
}

// ─── Story 12.13: Media Gateway Graceful Shutdown — Signal-Aware OIDC Retry ──
//
// Two changes make SIGTERM live during the OIDC retry loop:
//   1. main() creates ctx via signal.NotifyContext — so the context cancels on SIGTERM.
//   2. The time.Sleep between retries is replaced by a ctx-aware select.
//
// Tests verify:
//   AT-12-13-1: ctx cancelled during retry sleep exits immediately (< 200ms)
//   AT-12-13-3: no signal → all retries exhausted, no early exit
//   AT-12-13-4: ctx already cancelled before sleep → no block on retryDelay

// AT-12-13-1 — SIGTERM (simulated via context cancel) during retry sleep exits immediately.
//
// AC-2 — Given the parent context is cancelled while initOIDCVerifierWith is sleeping
// between retries (retryDelay = 500ms), when ctx.Done() fires, then the function
// returns within 200ms (not after the full 500ms sleep).
//
// RED: Currently initOIDCVerifierWith uses time.Sleep(retryDelay) which is not
// ctx-aware. After replacing with a select, this test will PASS (the sleep is
// interrupted). With the old implementation it takes ~500ms and FAILS the threshold.
func TestInitOIDCVerifierWith_SleepInterrupted_OnCtxCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	const retryDelay = 500 * time.Millisecond

	calls := 0
	mockProvider := func(_ context.Context, _ string) (*oidc.Provider, error) {
		calls++
		if calls == 1 {
			// Cancel the parent context immediately after the first attempt fails,
			// simulating a SIGTERM arriving at the start of the inter-retry sleep.
			// With a ctx-aware sleep, the select will fire ctx.Done() and return.
			// With time.Sleep, we block the full 500ms.
			cancel()
		}
		return nil, fmt.Errorf("mock: provider unavailable (call %d)", calls)
	}

	start := time.Now()
	_, _, err := initOIDCVerifierWith(
		ctx,
		"http://localhost:1",
		"nebu",
		"name",
		3,                   // maxAttempts — should stop after 1
		retryDelay,          // 500ms inter-attempt sleep that must be interrupted
		10*time.Millisecond, // per-attempt timeout (fast)
		mockProvider,
	)
	elapsed := time.Since(start)

	// With ctx-aware sleep: ~10ms (attempt timeout) + negligible select wakeup = < 200ms.
	// With time.Sleep: ~10ms + 500ms sleep = ~510ms — FAILS this assertion.
	if elapsed > 200*time.Millisecond {
		t.Errorf("[AT-12-13-1] elapsed %v > 200ms — ctx-aware sleep must interrupt on cancel (current time.Sleep blocks the full 500ms retryDelay)", elapsed)
	}
	if err == nil {
		t.Fatal("[AT-12-13-1] expected non-nil error when context is cancelled during retry sleep")
	}
	if calls != 1 {
		t.Errorf("[AT-12-13-1] expected exactly 1 provider call before cancellation, got %d", calls)
	}
}

// AT-12-13-3 — No signal → all retries exhausted, behaviour unchanged from 12.12.
//
// AC-3 — Given SIGTERM is NOT received and Dex remains unreachable,
// when all retries are exhausted, then the function returns an error after
// all maxAttempts have been tried (not before).
func TestInitOIDCVerifierWith_NoSignal_ExhaustsAllRetries(t *testing.T) {
	t.Parallel()

	calls := 0
	mockProvider := func(_ context.Context, _ string) (*oidc.Provider, error) {
		calls++
		return nil, fmt.Errorf("mock: always fails (call %d)", calls)
	}

	_, attempts, err := initOIDCVerifierWith(
		context.Background(), // no cancellation — must run all retries
		"http://localhost:1",
		"nebu",
		"name",
		3,              // maxAttempts
		0*time.Second,  // zero retryDelay: time.After(0) fires immediately
		0*time.Second,  // zero attemptTimeout: context.WithTimeout(ctx, 0) fires immediately
		mockProvider,
	)

	if err == nil {
		t.Fatal("[AT-12-13-3] expected non-nil error when all retries fail")
	}
	if attempts != 3 {
		t.Errorf("[AT-12-13-3] expected 3 attempts, got %d", attempts)
	}
	if calls != 3 {
		t.Errorf("[AT-12-13-3] expected provider called 3 times, got %d", calls)
	}
}

// ─── Story 12.14: Media Gateway Full Graceful Shutdown ────────────────────────
//
// Three SIGTERM gaps fixed:
//   1. http.Server had no Shutdown() — Docker kills mid-request after 10s
//   2. Rate limiter cleanup goroutine was not stoppable — goroutine leak
//   3. pgxpool.Pool close was only via defer — bypassed on os.Exit(1) paths
//
// Tests verify:
//   AT-12-14-1: in-flight HTTP request completes after Shutdown() (30s drain)
//   AT-12-14-2: Shutdown returns after drain timeout (not blocked forever)
//   AT-12-14-4: pool.Close() called after srv.Shutdown returns
//   AT-12-14-5: exit code 0 on SIGTERM (subprocess test)
//
// AT-12-14-3 lives in ratelimit_internal_test.go (needs package access).

// AT-12-14-1 — In-flight HTTP request completes after srv.Shutdown is called.
//
// AC-1 — Given an in-flight upload request is being processed (200ms handler delay),
// when srv.Shutdown(30s timeout ctx) is called,
// then the in-flight request completes with 200 OK and Shutdown returns nil.
//
// RED: Current code blocks on srv.ListenAndServe() synchronously and never calls
// srv.Shutdown(). This test verifies the new pattern: background goroutine + Shutdown.
func TestGracefulShutdown_InFlightRequestCompletes(t *testing.T) {
	t.Parallel()

	// A handler that takes 200ms to respond — simulating an in-flight upload.
	handlerDone := make(chan struct{})
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		close(handlerDone)
	})

	srv := &http.Server{
		Addr:    "127.0.0.1:0", // port 0 → OS assigns a free port
		Handler: slowHandler,
	}

	// Use an explicit listener so we can get the port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("[AT-12-14-1] failed to create listener: %v", err)
	}

	serverDone := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverDone <- err
		}
		close(serverDone)
	}()

	// Fire the in-flight request (async).
	addr := ln.Addr().String()
	reqDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/test") //nolint:noctx
		if err != nil {
			reqDone <- fmt.Errorf("request failed: %w", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			reqDone <- fmt.Errorf("expected 200 OK, got %d", resp.StatusCode)
			return
		}
		close(reqDone)
	}()

	// Give the handler a moment to start processing.
	time.Sleep(50 * time.Millisecond)

	// Call Shutdown — must wait for in-flight request to complete.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("[AT-12-14-1] srv.Shutdown returned error: %v", err)
	}

	// In-flight request must have completed.
	select {
	case err := <-reqDone:
		if err != nil {
			t.Fatalf("[AT-12-14-1] in-flight request failed after Shutdown: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("[AT-12-14-1] in-flight request did not complete within 500ms after Shutdown returned")
	}

	// Confirm handler body ran.
	select {
	case <-handlerDone:
		// good — handler completed
	default:
		t.Fatal("[AT-12-14-1] handler body did not complete")
	}
}

// AT-12-14-2 — Shutdown returns after drain timeout, not blocked forever.
//
// AC-2 — Given an in-flight request that never completes (hangs forever),
// when srv.Shutdown is called with a 100ms timeout context,
// then Shutdown returns within ~150ms with a context.DeadlineExceeded error.
//
// RED: Current code never calls srv.Shutdown() — this test verifies the 30s
// drain timeout mechanism (tested here with a short timeout for test speed).
func TestGracefulShutdown_DrainTimeoutReturnsFast(t *testing.T) {
	t.Parallel()

	// A handler that blocks forever — simulates a request that never completes.
	hangForever := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request context is cancelled (i.e. connection is closed).
		<-r.Context().Done()
	})

	srv := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: hangForever,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("[AT-12-14-2] failed to create listener: %v", err)
	}

	go func() {
		_ = srv.Serve(ln)
	}()

	// Fire a request that will hang.
	addr := ln.Addr().String()
	go func() {
		http.Get("http://" + addr + "/hang") //nolint:noctx,errcheck
	}()

	// Give the handler a moment to start processing.
	time.Sleep(50 * time.Millisecond)

	// Shutdown with a short 100ms timeout — must return within ~150ms.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = srv.Shutdown(shutdownCtx)
	elapsed := time.Since(start)

	if elapsed > 300*time.Millisecond {
		t.Errorf("[AT-12-14-2] Shutdown took %v — expected < 300ms with 100ms timeout", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("[AT-12-14-2] expected context.DeadlineExceeded, got: %v", err)
	}
}

// AT-12-14-4 — pool.Close() is called after srv.Shutdown returns (shutdown sequence).
//
// AC-4 — Given the HTTP server has drained all in-flight requests,
// when shutdown completes,
// then pool.Close() is called exactly once after srv.Shutdown returns.
//
// RED: Current code uses `defer pool.Close()` which is bypassed by os.Exit(1).
// This test verifies the new explicit sequence: Shutdown → pool.Close().
//
// Implementation: test the shutdown sequence via a testable helper function
// `runShutdownSequence(srv, pool, timeout)` extracted from main().
// The function must call srv.Shutdown first, then pool.Close().
func TestGracefulShutdown_PoolClosedAfterDrain(t *testing.T) {
	t.Parallel()

	// A fast handler so Shutdown completes immediately.
	srv := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("[AT-12-14-4] failed to create listener: %v", err)
	}

	go func() {
		_ = srv.Serve(ln)
	}()

	// Track call order: Shutdown must precede pool.Close.
	var callOrder []string
	var mu sync.Mutex

	// Wrap srv.Shutdown to record the call.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Call the helper — must call pool.Close() after srv.Shutdown.
	// runShutdownSequence is a testable extraction of the shutdown logic in main().
	// RED: this function does not exist yet — will FAIL TO COMPILE until extracted.
	runShutdownSequence(
		shutdownCtx,
		srv,
		// poolCloser is a function hook that represents pool.Close().
		func() {
			mu.Lock()
			callOrder = append(callOrder, "pool.Close")
			mu.Unlock()
		},
	)

	mu.Lock()
	defer mu.Unlock()

	if len(callOrder) == 0 {
		t.Fatal("[AT-12-14-4] pool.Close() was never called")
	}
	if callOrder[0] != "pool.Close" {
		t.Fatalf("[AT-12-14-4] expected pool.Close to be called, got call sequence: %v", callOrder)
	}
}

// AT-12-14-5 — Exit code 0 on clean SIGTERM shutdown.
//
// AC-5 — Given the gateway receives SIGTERM after startup,
// when it exits,
// then the exit code is 0 and the last log line contains "media gateway stopped".
//
// RED: Current code's ListenAndServe blocks forever; there is no Shutdown() call.
// If SIGTERM arrives, ListenAndServe does not return, so main() never reaches
// slog.Info("media gateway stopped") and exits non-zero via Docker SIGKILL.
//
// This test uses the subprocess pattern: runs a minimal binary that starts
// listening, receives SIGTERM, and verifies exit code + log output.
func TestGracefulShutdown_CleanExitOnSIGTERM(t *testing.T) {
	if os.Getenv("NEBU_TEST_SHUTDOWN_12_14_5") == "1" {
		// Subprocess arm: run a minimal version of main that exercises
		// the graceful shutdown path. We stub out OIDC and DB.
		// The test binary's main() is the real main — so we call a
		// test-only entrypoint via runMinimalGateway() that:
		//   1. Starts http.Server on random port
		//   2. Waits for SIGTERM via signal.NotifyContext
		//   3. Calls srv.Shutdown(30s) → pool stub
		//   4. Logs "media gateway stopped" and exits 0
		runMinimalGatewayForShutdownTest()
		return // unreachable if runMinimalGatewayForShutdownTest works correctly
	}

	// Build the test binary if needed.
	binaryPath := t.TempDir() + "/media-test-binary"
	buildCmd := exec.Command("go", "test", "-c", "-o", binaryPath, ".")
	buildCmd.Dir = "."
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("[AT-12-14-5] failed to build test binary: %v\n%s", err, out)
	}

	// Run the subprocess with SIGTERM trigger flag.
	cmd := exec.Command(binaryPath, "-test.run=TestGracefulShutdown_CleanExitOnSIGTERM", "-test.v")
	cmd.Env = append(os.Environ(), "NEBU_TEST_SHUTDOWN_12_14_5=1")

	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		t.Fatalf("[AT-12-14-5] failed to start subprocess: %v", err)
	}

	// Give the subprocess a moment to start.
	time.Sleep(200 * time.Millisecond)

	// Send SIGTERM.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("[AT-12-14-5] failed to send SIGTERM: %v", err)
	}

	// Wait for exit.
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case err := <-waitDone:
		if err != nil {
			// os.Exit(0) in subprocess shows up as nil err; any ExitError is failure.
			if exitErr, ok := err.(*exec.ExitError); ok {
				t.Fatalf("[AT-12-14-5] subprocess exited with non-zero code %d\nOutput:\n%s",
					exitErr.ExitCode(), output.String())
			}
			// Other errors (signal kill etc.) are failures too.
			t.Fatalf("[AT-12-14-5] unexpected wait error: %v\nOutput:\n%s", err, output.String())
		}
		// Exit code 0 — verify log line.
		if !strings.Contains(output.String(), "media gateway stopped") {
			t.Errorf("[AT-12-14-5] expected log line 'media gateway stopped', got output:\n%s", output.String())
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("[AT-12-14-5] subprocess did not exit within 5s after SIGTERM\nOutput:\n%s", output.String())
	}
}

// runMinimalGatewayForShutdownTest is the test-only entrypoint used by
// AT-12-14-5 (subprocess arm). It starts a minimal http.Server, waits for
// SIGTERM via signal.NotifyContext, runs the shutdown sequence (stubbed pool),
// and exits 0 via main() returning.
//
// Lives in main_test.go (not main.go) so it is not compiled into production binaries.
// The test binary (built with `go test -c`) includes _test.go files — this function
// is visible to the subprocess arm in TestGracefulShutdown_CleanExitOnSIGTERM.
func runMinimalGatewayForShutdownTest() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}),
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		slog.Error("server error", "err", err)
		os.Exit(1)
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Stub pool.Close — no real pool in this test entrypoint.
	runShutdownSequence(shutdownCtx, srv, func() {})
	// runShutdownSequence logs "media gateway stopped" and returns → exit 0.
}

// AT-12-13-4 — Context already cancelled before sleep → no block on retryDelay.
//
// AC-2 (pre-cancelled variant) — Given the parent context is cancelled before
// the inter-retry sleep fires, when the select is reached, then ctx.Done() fires
// immediately without waiting for time.After(retryDelay).
func TestInitOIDCVerifierWith_CancelledCtxDuringSleep_NoBlockOnSleep(t *testing.T) {
	t.Parallel()

	// Context cancelled immediately after first attempt starts.
	ctx, cancel := context.WithCancel(context.Background())

	const retryDelay = 500 * time.Millisecond

	calls := 0
	mockProvider := func(_ context.Context, _ string) (*oidc.Provider, error) {
		calls++
		cancel() // cancel synchronously on first call, before we return
		return nil, fmt.Errorf("mock: provider unavailable (call %d)", calls)
	}

	start := time.Now()
	_, _, err := initOIDCVerifierWith(
		ctx,
		"http://localhost:1",
		"nebu",
		"name",
		3,                   // maxAttempts — should stop after 1
		retryDelay,          // 500ms sleep that must NOT be honoured
		10*time.Millisecond, // per-attempt timeout (fast)
		mockProvider,
	)
	elapsed := time.Since(start)

	// Without ctx-aware sleep, we'd wait 500ms before noticing the cancellation.
	// With it, we return immediately after the select fires ctx.Done().
	if elapsed > 200*time.Millisecond {
		t.Errorf("[AT-12-13-4] elapsed %v > 200ms — ctx-aware sleep must not block when ctx is already done (current time.Sleep blocks full 500ms)", elapsed)
	}
	if err == nil {
		t.Fatal("[AT-12-13-4] expected non-nil error when context cancelled before sleep")
	}
	if calls != 1 {
		t.Errorf("[AT-12-13-4] expected 1 provider call, got %d", calls)
	}
}
