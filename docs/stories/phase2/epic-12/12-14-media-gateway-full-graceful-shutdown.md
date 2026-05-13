---
id: "12-14"
epic: 12
title: "Media Gateway Full Graceful Shutdown (HTTP + DB Pool + Rate Limiter)"
status: ready-for-dev
security_review: required
ui: false
matrix: false
created: 2026-05-13
---

# Story 12.14 — Media Gateway Full Graceful Shutdown (HTTP + DB Pool + Rate Limiter)

## User Story

As a system operator running in a cloud container environment (Kubernetes, Cloud Run, ECS),
I want the media gateway to drain all in-flight HTTP requests, stop background goroutines, and close the DB connection pool cleanly on SIGTERM,
so that rolling deployments and container restarts do not cause dropped requests, connection leaks, or dirty DB state.

## Background

Story 12.13 fixed SIGTERM handling in the OIDC startup retry loop. Three remaining SIGTERM gaps exist at runtime:

1. `http.Server` has timeouts (12.7) but no `Shutdown()` call — Docker kills the process mid-request after 10s
2. Rate limiter cleanup goroutine is explicitly marked "not stoppable" (`ratelimit.go:80-81`) — leaks on graceful exit
3. `pgxpool.Pool` is never explicitly closed — pool.Close() is only registered via `defer`, which doesn't run before `os.Exit(1)` error paths

**Critical observation about the existing code:** `main.go` currently has `defer pool.Close()` which only runs if `main()` returns normally. If `ListenAndServe` returns an error (non-`ErrServerClosed`), it calls `os.Exit(1)` which bypasses all defers. This story replaces that exit path with proper shutdown sequencing.

## Acceptance Criteria

**[AC-1] HTTP drain:**
Given an in-flight upload request is being processed,
When SIGTERM arrives,
Then the gateway stops accepting new connections immediately and the in-flight request completes before the process exits (up to 30s drain timeout)

**[AC-2] Shutdown timeout:**
Given in-flight requests do not complete within 30 seconds of SIGTERM,
When the drain timeout expires,
Then `srv.Shutdown` returns, the process logs a warning and exits — Docker SIGKILL is never needed for normal deployments

**[AC-3] Rate limiter goroutine stops:**
Given the cleanup goroutine is running,
When SIGTERM arrives,
Then the goroutine exits cleanly (no goroutine leak after process shutdown)

**[AC-4] DB pool closes after drain:**
Given the HTTP server has drained all in-flight requests,
When shutdown completes,
Then `pool.Close()` is called and DB connections are released before process exit

**[AC-5] Clean exit code:**
Given a normal SIGTERM shutdown (no errors),
When the process exits,
Then the exit code is 0 and the final log line is `"media gateway stopped"`

## Acceptance Tests

### Tests written FIRST (before implementation code):

**AT-12-14-1** — HTTP drain: in-flight request completes after SIGTERM — Go test in `media/cmd/media/main_test.go`
- Given: `http.Server` is running and processing an in-flight request (simulated with `httptest.Server` that holds a response for 200ms)
- When: `srv.Shutdown(ctx)` is called with a 30s timeout context
- Then: the in-flight request completes with 200 OK and `Shutdown` returns nil

**AT-12-14-2** — Shutdown timeout: Shutdown returns after drain timeout — Go test in `media/cmd/media/main_test.go`
- Given: an in-flight request that never completes (holds open), 100ms drain timeout
- When: `srv.Shutdown(shutdownCtx)` is called
- Then: returns `context.DeadlineExceeded` within ~110ms (not blocked indefinitely)

**AT-12-14-3** — Rate limiter goroutine stops on ctx cancellation — Go test in `media/internal/ratelimit/ratelimit_test.go` (or `ratelimit_internal_test.go`)
- Given: `newCore` is called with a cancellable context
- When: the context is cancelled (simulating SIGTERM)
- Then: `cleanupLoop` exits within 100ms (verified via goroutine count or channel signal)

**AT-12-14-4** — DB pool close after drain — integration-style test in `media/cmd/media/main_test.go`
- Given: a `shutdownSequence` helper (testable extraction of the shutdown logic)
- When: shutdown completes normally
- Then: `pool.Close()` is called exactly once after `srv.Shutdown` returns (verified via mock/spy)

**AT-12-14-5** — Exit code 0 on clean SIGTERM — subprocess test in `media/cmd/media/main_test.go`
- Given: the gateway is running and healthy
- When: SIGTERM is sent to the process
- Then: the process exits with code 0 and last log line contains "media gateway stopped"

## Tasks / Subtasks

- [ ] **Task 1: Refactor `cleanupLoop` to accept `ctx context.Context`** (AC-3)
  - [ ] 1.1 Change `cleanupLoop(interval, maxAge time.Duration)` to `cleanupLoop(ctx context.Context, interval, maxAge time.Duration)` in `ratelimit.go`
  - [ ] 1.2 Replace `for range ticker.C` with `select { case <-ticker.C: ... case <-ctx.Done(): return }` 
  - [ ] 1.3 Update `newCore` to accept `ctx context.Context` as first parameter
  - [ ] 1.4 Pass `ctx` when calling `go rl.cleanupLoop(ctx, ...)` in `newCore`
  - [ ] 1.5 Update `NewIPRateLimiter` to accept `ctx context.Context` as first parameter and thread it to `newCore`
  - [ ] 1.6 Update all callers in `main.go` (`uploadRL`, `downloadRL`) to pass `ctx`
  - [ ] 1.7 Update all test callers in `ratelimit_test.go` and `ratelimit_internal_test.go`

- [ ] **Task 2: Replace blocking `ListenAndServe` with background goroutine + signal-aware select** (AC-1, AC-2, AC-5)
  - [ ] 2.1 Remove `defer pool.Close()` (pool is now closed explicitly in shutdown sequence)
  - [ ] 2.2 Launch `srv.ListenAndServe()` in a goroutine, sending errors to `serverErr chan error`
  - [ ] 2.3 Add `select { case err := <-serverErr: ...; case <-ctx.Done(): }` to block until shutdown or error
  - [ ] 2.4 Create `shutdownCtx` with `context.Background()` (30s timeout) — separate from cancelled parent ctx
  - [ ] 2.5 Call `srv.Shutdown(shutdownCtx)`; on error log warning; on success continue
  - [ ] 2.6 After `srv.Shutdown` returns, call `pool.Close()`
  - [ ] 2.7 Log `slog.Info("media gateway stopped")` and let `main()` return (exit code 0)

- [ ] **Task 3: Write failing acceptance tests** (TDD — before implementation)
  - [ ] 3.1 Write AT-12-14-1 in `main_test.go` (HTTP drain with real httptest.Server)
  - [ ] 3.2 Write AT-12-14-2 in `main_test.go` (drain timeout with hanging handler)
  - [ ] 3.3 Write AT-12-14-3 in `ratelimit_internal_test.go` (goroutine exits on ctx cancel)
  - [ ] 3.4 Write AT-12-14-4 in `main_test.go` (mock pool.Close called after drain)
  - [ ] 3.5 Write AT-12-14-5 in `main_test.go` (subprocess: exit 0 + last log line)

- [ ] **Task 4: Verify existing tests still pass** (no regressions)
  - [ ] 4.1 `TestUploadRateLimit_BlocksAfterBurst` and friends — signature change only, logic unchanged
  - [ ] 4.2 `TestIPRateLimiter_CleanupOnce_*` in `ratelimit_internal_test.go` — internal struct tests unchanged
  - [ ] 4.3 `TestInitOIDCVerifierWith_*` in `main_test.go` — unaffected by shutdown changes

## Dev Notes

### Files to Modify

| File | Change Type | Description |
|------|------------|-------------|
| `media/internal/ratelimit/ratelimit.go` | UPDATE | `cleanupLoop` + `newCore` + `NewIPRateLimiter` signature change |
| `media/internal/ratelimit/ratelimit_test.go` | UPDATE | Add `context.Background()` as first arg to `NewIPRateLimiter` callers |
| `media/internal/ratelimit/ratelimit_internal_test.go` | UPDATE | Add AT-12-14-3 goroutine-stops test |
| `media/cmd/media/main.go` | UPDATE | Replace blocking `ListenAndServe` with goroutine+shutdown pattern |
| `media/cmd/media/main_test.go` | UPDATE | Add AT-12-14-1, AT-12-14-2, AT-12-14-4, AT-12-14-5 |

### Critical Code Context

**Current `main.go` (lines to replace):**

```go
// Current — BLOCKING, no drain:
slog.Info("Nebu Media Gateway listening", "addr", listenAddr)
if err := srv.ListenAndServe(); err != nil {
    slog.Error("server error", "err", err)
    os.Exit(1)
}
```

**Target pattern:**

```go
serverErr := make(chan error, 1)
go func() {
    slog.Info("media gateway listening", "addr", listenAddr)
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

shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
if err := srv.Shutdown(shutdownCtx); err != nil {
    slog.Warn("graceful shutdown timed out", "err", err)
}
pool.Close()
slog.Info("media gateway stopped")
// main() returns → exit code 0
```

**Note:** Remove `defer pool.Close()` — it is replaced by explicit `pool.Close()` after `srv.Shutdown`. The `defer` form fails to run if `os.Exit(1)` is called; explicit sequencing is correct and required.

**Current `ratelimit.go` `newCore` (lines to change):**

```go
// Current — goroutine not stoppable:
func newCore(cfg Config, trustedProxy bool) *IPRateLimiter {
    rl := &IPRateLimiter{...}
    go rl.cleanupLoop(1*time.Minute, 5*time.Minute)
    return rl
}

func (rl *IPRateLimiter) cleanupLoop(interval, maxAge time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for range ticker.C {
        rl.cleanupOnce(maxAge)
    }
}
```

**Target:**

```go
func newCore(ctx context.Context, cfg Config, trustedProxy bool) *IPRateLimiter {
    rl := &IPRateLimiter{...}
    go rl.cleanupLoop(ctx, 1*time.Minute, 5*time.Minute)
    return rl
}

func (rl *IPRateLimiter) cleanupLoop(ctx context.Context, interval, maxAge time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            rl.cleanupOnce(maxAge)
        case <-ctx.Done():
            return
        }
    }
}
```

**`NewIPRateLimiter` signature change:**

```go
// Before:
func NewIPRateLimiter(cfg Config, trustedProxy bool) func(http.Handler) http.Handler

// After:
func NewIPRateLimiter(ctx context.Context, cfg Config, trustedProxy bool) func(http.Handler) http.Handler
```

Pass `ctx` through to `newCore`.

### Shutdown Order (must be preserved)

1. SIGTERM → `ctx` cancels (already wired via `signal.NotifyContext` in 12.13)
2. `srv.Shutdown(shutdownCtx)` — stops new connections, drains in-flight HTTP requests (30s timeout)
3. Rate limiter goroutine exits via `ctx.Done()` (concurrent with step 2 — ctx already cancelled)
4. `pool.Close()` — after HTTP drain so in-flight DB queries can complete
5. `slog.Info("media gateway stopped")` + `main()` returns → exit code 0

### Testing Strategy

**AT-12-14-1 and AT-12-14-2** can be written against `http.Server` directly using `httptest.NewServer` + `httptest.NewUnstartedServer`. No need to call `main()` — test the `Shutdown` pattern directly.

**AT-12-14-3** test approach for goroutine exit:
- Create `newCore` with a cancellable context and a short `cleanupInterval` (e.g. 10ms)
- Call `cancel()` and wait at most 200ms for goroutine exit
- Detect goroutine exit: use a done channel injected via test-only variant, or use `runtime.NumGoroutine()` before/after with a short `time.Sleep` to let the goroutine drain

**AT-12-14-5** subprocess test:
- Build the binary with `go build ./media/cmd/media/...`
- Set `NEBU_RATE_LIMIT_DISABLED=true`, mock or skip OIDC/DB (the subprocess can exit via OIDC init if no real Dex — test only verifies SIGTERM path not blocked before OIDC)
- Alternative: use a `TestMain` integration approach with `httptest.Server` injecting a dummy OIDC verifier

**Note on AT-12-14-3 goroutine detection:** Since `newCore` is unexported, the test lives in `ratelimit_internal_test.go` (package `ratelimit`) to access the struct. Add a done channel to `cleanupLoop` during test, or use `runtime.NumGoroutine` delta approach.

### ratelimit_test.go Caller Updates

Every call to `NewIPRateLimiter` in `ratelimit_test.go` must add `context.Background()` as first arg:

```go
// Before:
handler := ratelimit.NewIPRateLimiter(cfg, false)(okHandler)

// After:
handler := ratelimit.NewIPRateLimiter(context.Background(), cfg, false)(okHandler)
```

Similarly for any `newCore` calls in `ratelimit_internal_test.go`:

```go
// The internal tests construct IPRateLimiter directly (not via newCore),
// so cleanupLoop is not called — no change needed for AT-12-12-3/4/5 tests.
// But AT-12-14-3 must call newCore with ctx.
```

### imports to add in main.go

`"errors"` is already imported. No new imports needed for the shutdown pattern itself (all packages already imported: `context`, `net/http`, `os`, `time`).

In `ratelimit.go`, add `"context"` to imports (not yet present).

### Previous Story Context (12.13)

Story 12.13 added:
- `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` — ctx is already signal-aware
- `defer stop()` for signal registration cleanup
- `defer pool.Close()` — this defer will be removed in 12.14 and replaced with explicit close

The ctx wiring is complete. Story 12.14 only needs to:
1. Use the existing `ctx` in the server goroutine select
2. Pass `ctx` to `NewIPRateLimiter`
3. Replace the blocking `ListenAndServe` with the background goroutine pattern

### Security Review Note

`security_review: required` because this story modifies signal handling and process exit paths. Kassandra should check:
- Shutdown timeout is bounded (30s) — no indefinite block
- `pool.Close()` called before process exit — no connection leak
- No new race conditions in goroutine stop (ctx.Done is safe)

### References

- [Source: media/cmd/media/main.go] — current `ListenAndServe` call (line ~280) and `defer pool.Close()`
- [Source: media/internal/ratelimit/ratelimit.go] — `newCore`, `cleanupLoop` (lines 68-88)
- [Source: media/internal/ratelimit/ratelimit_test.go] — callers of `NewIPRateLimiter` (all must add ctx)
- [Source: media/internal/ratelimit/ratelimit_internal_test.go] — white-box tests for `cleanupOnce`
- [Source: docs/stories/phase2/epic-12/12-13-media-gateway-graceful-shutdown-signal-aware-oidc-retry.md] — signal.NotifyContext wiring (must not regress)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List
