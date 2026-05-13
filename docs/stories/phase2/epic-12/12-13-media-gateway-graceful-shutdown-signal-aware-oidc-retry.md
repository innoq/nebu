---
id: "12-13"
epic: 12
title: "Media Gateway Graceful Shutdown: Signal-Aware OIDC Retry Loop"
status: ready-for-dev
security_review: not-needed
ui: false
matrix: false
created: 2026-05-13
---

# Story 12.13 — Media Gateway Graceful Shutdown: Signal-Aware OIDC Retry Loop

## User Story

As a system operator,
I want the media gateway to react to SIGTERM immediately during OIDC discovery retries,
So that `docker compose stop` completes within Docker's 10-second grace window instead of being hard-killed.

## Acceptance Criteria

**AC-1** — Given the media gateway is in the OIDC retry loop (Dex unreachable),
when SIGTERM is received,
then the retry loop exits within 100ms and the process terminates with a non-zero exit code and a log message indicating shutdown.

**AC-2** — Given the media gateway is sleeping between OIDC retries (2s backoff),
when SIGTERM is received during the sleep,
then the sleep is interrupted immediately (not after 2s) and the process exits.

**AC-3** — Given SIGTERM is NOT received and Dex remains unreachable,
when all 5 retries are exhausted,
then behaviour is unchanged from 12.12 (exits with fatal log after ~60s).

**AC-4** — Given the media gateway starts successfully (Dex reachable),
when SIGTERM is received after startup,
then the http.Server performs graceful shutdown (existing behaviour — not in scope of this story, but must not regress).

## Acceptance Tests

### Tests written FIRST (before implementation code):

**AT-12-13-1** — SIGTERM received during OIDC retry loop exits immediately — Go unit test `TestInitOIDCVerifierWith_SleepInterrupted_OnCtxCancel` in `media/cmd/media/main_test.go`
- Given: parent context is cancelled (simulating SIGTERM) while `initOIDCVerifierWith` is sleeping between retries
- When: the sleep select fires the `ctx.Done()` branch
- Then: the function returns immediately (within 200ms) with a non-nil context-cancellation error

**AT-12-13-2** — SIGTERM received when ctx is signalled — Go unit test `TestInitOIDCVerifier_SigtermContext_ExitsImmediately` in `media/cmd/media/main_test.go`
- Given: `initOIDCVerifierWith` is called with a context created via `signal.NotifyContext` and we send SIGTERM to the process
- When: SIGTERM arrives
- Then: the context is cancelled and the function returns error within 500ms (subprocess test pattern using `exec.Command`)

**AT-12-13-3** — Behaviour unchanged when SIGTERM is NOT received — Go unit test `TestInitOIDCVerifierWith_NoSignal_ExhaustsAllRetries` in `media/cmd/media/main_test.go`
- Given: all retries fail (mock always returns error), no context cancellation
- When: `initOIDCVerifierWith` runs with maxAttempts=3, zero retryDelay, zero-duration sleep select
- Then: all 3 attempts are made, error is returned, process does not terminate early

**AT-12-13-4** — ctx-aware sleep does not block when context is already cancelled — Go unit test `TestInitOIDCVerifierWith_CancelledCtxDuringSleep_NoBlockOnSleep` in `media/cmd/media/main_test.go`
- Given: context is cancelled after the first provider attempt fails (before the sleep fires)
- When: the select fires the `ctx.Done()` branch instead of `time.After`
- Then: the function returns within 200ms without waiting for the full retryDelay

## Technical Context

### Background

Story 12.12 added `ctx.Err()` guard at the top of the retry loop in `initOIDCVerifierWith`. That check runs **before each attempt** — but after a failed attempt, the code calls `time.Sleep(retryDelay)` (2 seconds) before the next `ctx.Err()` check. If SIGTERM arrives during this sleep, the process stays alive for up to 2s before noticing the cancellation.

Additionally, `main()` currently passes `context.Background()` to `initOIDCVerifier` — a context that is never cancelled. Even though the per-attempt timeout and ctx.Err() guard exist, they are dead code for SIGTERM during the retry loop because the parent context never signals.

This story makes both paths live:

1. `main()` creates a SIGTERM-aware context using `signal.NotifyContext`.
2. The retry sleep is replaced by a `select` that wakes on either `time.After(retryDelay)` or `ctx.Done()`.

### Two Changes in `media/cmd/media/main.go`

**Change 1 — Signal-aware context in `main()`:**

```go
import "os/signal"
import "syscall"

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

verifier := initOIDCVerifier(ctx, oidcIssuer, oidcClientID, oidcUserIDClaim, 5, 2*time.Second)
```

Replace the `ctx := context.Background()` declaration (line ~191) and update the `initOIDCVerifier` call to pass this `ctx`.

**NOTE:** `ctx` is already used for `pgxpool.New(ctx, dbURL)` at line ~194. After this change, both the pool creation and the OIDC verifier initialization share the same SIGTERM-aware context. This is intentional: a SIGTERM before startup completes should abort both.

**Change 2 — ctx-aware sleep in `initOIDCVerifierWith`:**

Replace:
```go
time.Sleep(retryDelay)
```

With:
```go
select {
case <-time.After(retryDelay):
case <-ctx.Done():
    return nil, 0, fmt.Errorf("OIDC verifier init aborted: %w", ctx.Err())
}
```

Note the return signature: `initOIDCVerifierWith` returns `(upload.TokenVerifier, int, error)`. The int is `attempt - 1` (attempts completed before abort), consistent with the existing ctx.Err() early-exit at the top of the loop.

### Current State of `initOIDCVerifierWith` (12.12 implementation)

```go
func initOIDCVerifierWith(
    ctx context.Context,
    issuer, clientID, claimName string,
    maxAttempts int,
    retryDelay time.Duration,
    attemptTimeout time.Duration,
    newProvider oidcNewProviderFunc,
) (upload.TokenVerifier, int, error) {
    var lastErr error
    for attempt := 1; attempt <= maxAttempts; attempt++ {
        if ctx.Err() != nil {
            return nil, attempt - 1, ctx.Err()
        }
        attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
        provider, err := newProvider(attemptCtx, issuer)
        cancel()
        if err == nil {
            // ... success path
            return verifier, attempt, nil
        }
        lastErr = err
        slog.Warn("media: OIDC provider unavailable, retrying", ...)
        if attempt < maxAttempts {
            time.Sleep(retryDelay)  // ← REPLACE THIS
        }
    }
    return nil, maxAttempts, lastErr
}
```

### What NOT to Change

- The `http.Server.ListenAndServe` path and graceful shutdown wiring (AC-4 is a non-regression requirement, not a new implementation task). The `srv` is unchanged.
- The 5-retry, 2s backoff, 10s per-attempt timeout values — only the sleep becomes ctx-aware.
- The `initOIDCVerifier` public signature (it already accepts `ctx context.Context`).
- No new imports are required for `initOIDCVerifierWith` — `fmt`, `context`, and `time` are already imported.

For `main()`, two new imports are required:
- `"os/signal"` (standard library)
- `"syscall"` (standard library)

### Existing Tests That Must Remain Green

- `TestInitOIDCVerifierWith_CancelledCtx_ExitsImmediately` (AT-12-12-1) — already tests ctx cancellation at loop top; still passes
- `TestInitOIDCVerifierWith_PerAttemptTimeout` (AT-12-12-2) — per-attempt timeout; still passes (sleep is only reached if `attempt < maxAttempts`)
- `TestInitOIDCVerifier_EmptyIssuer_FatalExit` (AT-12-8-1) — empty issuer; still passes
- `TestInitOIDCVerifier_AllRetriesFail_FatalExit` (AT-12-8-2) — unreachable Dex; still passes
- `TestInitOIDCVerifier_RetryCountOnFailure` (AT-12-8-3) — uses `context.Background()`, `retryDelay=0` → sleep select fires `time.After(0)` immediately, all 3 attempts run; still passes

### File Locations

- Implementation: `media/cmd/media/main.go`
- Tests: `media/cmd/media/main_test.go`

No other files need to change.

### Security Considerations

`signal.NotifyContext` is the idiomatic Go stdlib pattern (introduced Go 1.16) for signal-aware contexts. No new dependencies. The SIGTERM context is scoped to `main()` and does not leak to HTTP handlers.

The `stop()` deferred call ensures the OS signal registration is cleaned up when `main()` returns (standard pattern).

### Previous Story Learnings (from 12.12)

- `initOIDCVerifierWith` accepts an injected `newProvider` func — keep this for testability in new tests.
- Tests use `context.Background()` with `retryDelay=0` where timing is not the concern.
- The subprocess pattern (`exec.Command` + `NEBU_TEST_CRASH_*` env var) is used for tests that exercise `os.Exit`. New tests for SIGTERM behaviour should use `signal.NotifyContext` directly without subprocess (no os.Exit involved).
- `t.Parallel()` is used on all non-env-mutating tests.

## Dev Notes

Implementation is mechanical — two changes totalling ~10 lines of modified code:

1. In `main()`: add `os/signal` + `syscall` imports, replace `ctx := context.Background()` with `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` + `defer stop()`.
2. In `initOIDCVerifierWith`: replace `time.Sleep(retryDelay)` with the `select { case <-time.After(retryDelay): case <-ctx.Done(): return nil, 0, ... }` block.

Run `make test-unit-go` (builds in Docker) to verify. All existing tests must stay green.
