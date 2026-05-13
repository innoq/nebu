---
id: "12-12"
epic: 12
title: "Media Gateway Startup + Rate Limiter Hardening (F-3/F-4/F-5)"
status: ready-for-dev
security_review: required
ui: false
matrix: false
created: 2026-05-13
---

# Story 12.12 — Media Gateway Startup + Rate Limiter Hardening (F-3/F-4/F-5)

## User Story

As a system operator,
I want the media gateway's OIDC retry loop to use per-attempt timeouts, the rate limiter cleanup to be race-free, and rate-limit-disabled state to be logged at startup,
So that the gateway fails fast on hung OIDC discovery, has no jitter window where a throttled IP gets a fresh bucket, and audit logs capture when rate limiting is disabled.

## Acceptance Criteria

**[F-3] OIDC retry per-attempt timeout:**

- **AC-F3-1** — Given Dex hangs (accepts TCP but never responds), when `initOIDCVerifier` retries, then each attempt times out after 10 seconds and does not block indefinitely
- **AC-F3-2** — Given 5 retries all timeout at 10s, when the final retry fails, then the gateway exits with a non-zero exit code within 60 seconds total (5 × 10s + backoff)
- **AC-F3-3** — Given the parent context is cancelled (e.g. SIGTERM during startup), when a retry is in-flight, then the retry loop exits immediately without waiting for the per-attempt timeout

**[F-4] Race-free rate limiter cleanup:**

- **AC-F4-1** — Given a background cleanup tick fires while a request for the same IP is in-flight, when the cleanup runs, then it does not delete an entry that was accessed within the last 5 minutes (no fresh-bucket jitter for in-flight requests)
- **AC-F4-2** — Given an IP has not been seen for >5 minutes, when cleanup runs, then the entry is deleted (existing behaviour preserved)

**[F-5] Log when rate limiting disabled:**

- **AC-F5-1** — Given `NEBU_RATE_LIMIT_DISABLED=true`, when the media gateway starts, then it emits `slog.Warn("rate limiting disabled — NEBU_RATE_LIMIT_DISABLED is set")` exactly once at startup
- **AC-F5-2** — Given `NEBU_RATE_LIMIT_DISABLED` is unset or `false`, when the media gateway starts, then no rate-limit-disabled warning is emitted

## Acceptance Tests

### Tests written FIRST (before implementation code):

**AT-12-12-1** — OIDC retry exits immediately when parent context cancelled — ExUnit (Go: `TestInitOIDCVerifierWith_CancelledCtx_ExitsImmediately` in `media/cmd/media/main_test.go`)
- Given: parent context is cancelled before `initOIDCVerifierWith` is called
- When: `initOIDCVerifierWith` is invoked with the cancelled context
- Then: returns immediately with a non-nil error (does not block for `maxAttempts × timeout`)

**AT-12-12-2** — OIDC retry uses per-attempt timeout — Go unit test `TestInitOIDCVerifierWith_PerAttemptTimeout` in `media/cmd/media/main_test.go`
- Given: mock provider that blocks until its context is done
- When: `initOIDCVerifierWith` is called with maxAttempts=2, per-attempt timeout=50ms
- Then: each attempt returns within ~50ms and all retries complete within ~200ms total (not blocking indefinitely)

**AT-12-12-3** — Cleanup does not delete recently-accessed entry — Go unit test `TestIPRateLimiter_CleanupOnce_DoesNotEvictRecentEntry` in `media/internal/ratelimit/ratelimit_test.go`
- Given: an entry whose `lastSeen` is 1 second ago (within 5-minute window)
- When: `cleanupOnce(5 * time.Minute)` is called
- Then: the entry is NOT deleted from the sync.Map

**AT-12-12-4** — Cleanup deletes stale entry — Go unit test `TestIPRateLimiter_CleanupOnce_EvictsStaleEntry` in `media/internal/ratelimit/ratelimit_test.go`
- Given: an entry whose `lastSeen` is 10 minutes ago (outside 5-minute window)
- When: `cleanupOnce(5 * time.Minute)` is called
- Then: the entry IS deleted from the sync.Map

**AT-12-12-5** — Warning logged when rate limiting disabled — Go unit test `TestNewIPRateLimiter_LogsWarning_WhenDisabled` in `media/internal/ratelimit/ratelimit_test.go`
- Given: `NEBU_RATE_LIMIT_DISABLED=true`
- When: `NewIPRateLimiter` is called
- Then: a `slog.Warn` with message "rate limiting disabled — NEBU_RATE_LIMIT_DISABLED is set" is emitted exactly once

**AT-12-12-6** — No warning when rate limiting enabled — Go unit test `TestNewIPRateLimiter_NoWarning_WhenEnabled` in `media/internal/ratelimit/ratelimit_test.go`
- Given: `NEBU_RATE_LIMIT_DISABLED` is unset
- When: `NewIPRateLimiter` is called
- Then: no rate-limit-disabled warning is emitted

## Technical Context

### Background: LOW advisories from Epic 12 SEC Gate 2 final review

The definitive SEC Gate 2 review (`_bmad-output/implementation-artifacts/epic-12-security-review-final2-2026-05-13.md`) identified three LOW advisories (F-3, F-4, F-5) that were carried as non-blocking. This story closes them as operational hardening:

- **F-3 [LOW]** — `initOIDCVerifier` passes `context.Background()` which makes each retry unbounded. A hung OIDC provider (accepts TCP, never responds) can block startup indefinitely.
- **F-4 [LOW]** — `cleanupOnce` in `ratelimit.go` already reads `entry.lastSeen` correctly (12.10 introduced the field) but the cleanup comparison path has a small jitter window: an entry may be deleted after its `lastSeen` was set by `getOrCreate` but before the request completes consuming the limiter.
- **F-5 [LOW]** — When `NEBU_RATE_LIMIT_DISABLED=true`, the gateway logs nothing. An operator reviewing startup logs cannot tell whether rate limiting is active.

### F-3 Fix — `media/cmd/media/main.go`

**Current code** (`initOIDCVerifierWith`):
```go
provider, err := newProvider(ctx, issuer)  // ctx is context.Background() — unbounded
```

**Required changes:**
- `initOIDCVerifier` and `initOIDCVerifierWith` must accept a parent `ctx context.Context` parameter (currently hardcoded to `context.Background()` in the caller)
- Per attempt: `attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)`, call `newProvider(attemptCtx, issuer)`, `cancel()` immediately after (defer or explicit)
- If `ctx.Err() != nil` at the top of the retry loop → return early with `ctx.Err()` (SIGTERM propagation)
- The existing 5-retry + 2s backoff structure stays unchanged
- The call in `main()` passes `ctx` (already a `context.Background()` there — no change to `main()` startup behaviour, but the parameter makes the function testable with a cancellable context)

**Per-attempt timeout constant:** 10 seconds (matches the `ReadHeaderTimeout` on the HTTP server and the existing gateway pattern).

### F-4 Fix — `media/internal/ratelimit/ratelimit.go`

**Current state** (from Story 12.10/12.11 implementation):
- `ipEntry` already has `lastSeen time.Time` field and `mu sync.Mutex` (lines 52-56)
- `getOrCreate` already updates `lastSeen` on every access (lines 107-118)
- `cleanupOnce` already reads `entry.lastSeen` under `entry.mu` (lines 90-102)

**Issue:** The current cleanup is functionally correct for the "race-free" requirement as written. However, re-read the security review finding: it notes that the `lastSeen` update happens in `getOrCreate`, which is called before `next.ServeHTTP(w, r)`. So by the time cleanup runs during the in-flight request, `lastSeen` is already fresh — the entry will not be evicted.

**Verification:** Read the cleanup code carefully:
```go
func (rl *IPRateLimiter) cleanupOnce(maxAge time.Duration) {
    threshold := time.Now().Add(-maxAge)
    rl.limiters.Range(func(key, val any) bool {
        entry := val.(*ipEntry)
        entry.mu.Lock()
        stale := entry.lastSeen.Before(threshold)
        entry.mu.Unlock()
        if stale {
            rl.limiters.Delete(key)
        }
        return true
    })
}
```

The existing implementation already satisfies AC-F4-1 and AC-F4-2 because `lastSeen` is updated in `getOrCreate` (which runs before the handler), and `cleanupOnce` uses `entry.lastSeen` (not a separate timer). The "race" the security review described is the window where `LoadOrStore` creates the entry with `lastSeen=time.Now()` but cleanup could theoretically fire before `lastSeen` is set by the second `getOrCreate` path — this is already addressed by the existing mutex.

**Conclusion:** The F-4 code is already correct. The acceptance tests (AT-12-12-3 and AT-12-12-4) are needed to make this explicit and guard against future regressions. Write tests that directly manipulate `ipEntry` state via the exported `cleanupOnce`-equivalent. Since `cleanupOnce` is unexported, tests must use the package or add a test-helper export.

**Option:** Export `CleanupOnce(maxAge time.Duration)` for testing purposes (capitalised — package-internal tests can use the lowercase version directly since `ratelimit_test.go` uses `package ratelimit_test`). The cleanest approach: add a `CleanupOnce` exported wrapper that calls `cleanupOnce`, or move the test to `package ratelimit` (internal test). Use internal test (`package ratelimit`).

### F-5 Fix — `media/internal/ratelimit/ratelimit.go`

**Location:** In `NewIPRateLimiter`, in the `NEBU_RATE_LIMIT_DISABLED=true` branch, add:

```go
slog.Warn("rate limiting disabled — NEBU_RATE_LIMIT_DISABLED is set")
```

**Placement:** Before the `return func(next http.Handler) http.Handler { return next }` line.

**Single emission:** The warning fires once per `NewIPRateLimiter` call. In `main()`, `NewIPRateLimiter` is called twice (once for upload, once for download/thumbnail). Both calls emit the warning. This is acceptable — two lines at startup, both audit trail. If the story description says "exactly once", consolidate the `NEBU_RATE_LIMIT_DISABLED` check in `main()` instead:

```go
// In main(), after reading NEBU_RATE_LIMIT_DISABLED:
rateLimitDisabled := os.Getenv("NEBU_RATE_LIMIT_DISABLED") == "true"
if rateLimitDisabled {
    slog.Warn("rate limiting disabled — NEBU_RATE_LIMIT_DISABLED is set")
}
```

The story AC says "exactly once at startup" — implement in `main()` (not in `NewIPRateLimiter`) to guarantee exactly one log line regardless of how many limiter instances are created.

## Tasks / Subtasks

- [ ] **F-3: Per-attempt OIDC timeout** (AC-F3-1, AC-F3-2, AC-F3-3)
  - [ ] Write AT-12-12-1 (cancelled ctx) and AT-12-12-2 (per-attempt timeout) in `media/cmd/media/main_test.go` — failing RED
  - [ ] Update `initOIDCVerifierWith` signature: add parent `ctx` parameter (no longer implicit `context.Background()`)
  - [ ] Per-attempt: `attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)` + `defer cancel()`
  - [ ] At top of retry loop: `if ctx.Err() != nil { return nil, 0, ctx.Err() }`
  - [ ] Update `initOIDCVerifier` to pass `ctx` through to `initOIDCVerifierWith`
  - [ ] Update `main()` call: `initOIDCVerifier(ctx, oidcIssuer, oidcClientID, oidcUserIDClaim, 5, 2*time.Second)` — no change to call site needed (ctx is already defined)

- [ ] **F-4: Cleanup race test coverage** (AC-F4-1, AC-F4-2)
  - [ ] Write AT-12-12-3 (recent entry not evicted) and AT-12-12-4 (stale entry evicted) in `media/internal/ratelimit/ratelimit_test.go` as `package ratelimit` (internal) to access `cleanupOnce`
  - [ ] Verify existing implementation satisfies both ACs (no code change needed, tests only)

- [ ] **F-5: Startup log for disabled rate limiting** (AC-F5-1, AC-F5-2)
  - [ ] Write AT-12-12-5 (warning emitted) and AT-12-12-6 (no warning when enabled) in `media/internal/ratelimit/ratelimit_test.go`
  - [ ] Add `slog.Warn("rate limiting disabled — NEBU_RATE_LIMIT_DISABLED is set")` in `main()` after reading `NEBU_RATE_LIMIT_DISABLED` (not in `NewIPRateLimiter`), or add it to `NewIPRateLimiter` if per-limiter logging is acceptable — coordinate with AC "exactly once"
  - [ ] Update `main()` to emit the warning if `os.Getenv("NEBU_RATE_LIMIT_DISABLED") == "true"` before wiring the mux

- [ ] Run `make test-unit-go` — all tests green

## Dev Notes

### Files to Modify

| File | Change |
|------|--------|
| `media/cmd/media/main.go` | F-3: add per-attempt timeout to `initOIDCVerifierWith`; F-5: add startup warn when rate limiting disabled |
| `media/internal/ratelimit/ratelimit.go` | F-5: optionally add warn here (alternative to main.go); no code change for F-4 |
| `media/cmd/media/main_test.go` | F-3: add AT-12-12-1 and AT-12-12-2 |
| `media/internal/ratelimit/ratelimit_test.go` | F-4+F-5: add AT-12-12-3..6 |

### Critical: Test file package for F-4

The existing `ratelimit_test.go` uses `package ratelimit_test` (black-box testing). `cleanupOnce` is unexported. For AT-12-12-3 and AT-12-12-4, either:
1. Add a second test file `ratelimit_internal_test.go` with `package ratelimit` to access `cleanupOnce` directly
2. Export `CleanupOnce` with the exact same semantics as `cleanupOnce`

**Recommended:** Option 1 — add `ratelimit_internal_test.go` with `package ratelimit`. Keeps the public API surface unchanged.

### Critical: F-5 log location — "exactly once"

`main()` calls `NewIPRateLimiter` twice (upload + download). If the warn is emitted from inside `NewIPRateLimiter`, it fires twice. The story says "exactly once". Therefore emit the warning in `main()` directly, not inside `NewIPRateLimiter`.

```go
// After reading env vars in main(), before selectStorer():
rateLimitDisabled := os.Getenv("NEBU_RATE_LIMIT_DISABLED") == "true"
if rateLimitDisabled {
    slog.Warn("rate limiting disabled — NEBU_RATE_LIMIT_DISABLED is set")
}
```

The warning must appear before the mux is wired (startup audit trail, not per-request).

### Critical: F-3 context propagation

The parent `ctx` in `main()` is `context.Background()`. The change makes the function testable with `context.WithCancel(context.Background())` in tests. The actual runtime behaviour is unchanged (parent context is never cancelled in `main()` since there's no signal handler).

**If a signal handler is added in a future story:** the parent context cancellation will propagate to the OIDC retry loop automatically. This is the reason for AC-F3-3.

### Testing AT-12-12-2: Per-attempt timeout

Use a mock `newProvider` function that:
1. Blocks until `attemptCtx.Done()` is closed (context deadline exceeded)
2. Returns `attemptCtx.Err()`

This proves each attempt is bounded by the timeout and does not block beyond it.

```go
mockProvider := func(ctx context.Context, issuer string) (*oidc.Provider, error) {
    <-ctx.Done() // block until context cancelled/timed out
    return nil, ctx.Err()
}
```

With `maxAttempts=2`, `perAttemptTimeout=50ms`, `retryDelay=10ms`:
- Total time ≤ 2×50ms + 1×10ms + slack = 120ms
- Test `time.Since(start) < 200*time.Millisecond` to allow for scheduler jitter

### Testing AT-12-12-3/4: Direct cleanupOnce access

In `ratelimit_internal_test.go` (`package ratelimit`):
```go
func TestIPRateLimiter_CleanupOnce_DoesNotEvictRecentEntry(t *testing.T) {
    rl := &IPRateLimiter{rate: rate.Limit(1), burst: 1}
    entry := &ipEntry{
        limiter:  rate.NewLimiter(1, 1),
        lastSeen: time.Now().Add(-1 * time.Second), // 1s ago — within 5m window
    }
    rl.limiters.Store("192.0.2.1", entry)
    rl.cleanupOnce(5 * time.Minute)
    if _, ok := rl.limiters.Load("192.0.2.1"); !ok {
        t.Fatal("recently-accessed entry must not be evicted by cleanup")
    }
}
```

### Testing AT-12-12-5/6: slog.Warn capture

Since the warning is in `main()` (not `NewIPRateLimiter`), tests for F-5 should be in `media/cmd/media/main_test.go`. Use `slog.SetDefault` with a custom handler to capture log output:

```go
func TestMain_RateLimitDisabledWarning(t *testing.T) {
    t.Setenv("NEBU_RATE_LIMIT_DISABLED", "true")
    // Replace slog handler with a capturing handler
    var buf bytes.Buffer
    handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
    slog.SetDefault(slog.New(handler))
    // Call the function that emits the warning (or test the check logic directly)
    // ...
    if !strings.Contains(buf.String(), "rate limiting disabled") {
        t.Error("expected rate-limit-disabled warning in startup log")
    }
}
```

Alternatively: extract the startup-warn check into a testable helper:
```go
func logIfRateLimitDisabled() {
    if os.Getenv("NEBU_RATE_LIMIT_DISABLED") == "true" {
        slog.Warn("rate limiting disabled — NEBU_RATE_LIMIT_DISABLED is set")
    }
}
```

**Recommended:** Extract helper — easier to test without mocking main() setup.

### Previous Story Intelligence

- **Story 12.11** introduced `trustedProxy bool` to `NewIPRateLimiter`. The `ratelimit_test.go` already has `AT-12-11-1..3` for XFF gating. New tests for 12.12 must not break these.
- **Story 12.10** introduced `ipEntry` with `lastSeen time.Time` and the `cleanupLoop`. The F-4 tests verify this existing behavior is correct.
- **Story 12.8** introduced `initOIDCVerifierWith` with the injectable `newProvider` function. AT-12-12-1 and AT-12-12-2 use the same injection pattern — the test infrastructure is already there.

### Source References

- `media/cmd/media/main.go` — `initOIDCVerifier`, `initOIDCVerifierWith` (lines ~300-365)
- `media/internal/ratelimit/ratelimit.go` — `ipEntry`, `cleanupOnce`, `NewIPRateLimiter` (lines 52-202)
- `media/internal/ratelimit/ratelimit_test.go` — existing test patterns to follow
- `_bmad-output/implementation-artifacts/epic-12-security-review-final2-2026-05-13.md` — F-3/F-4/F-5 finding descriptions
- `docs/stories/phase2/epic-12/12-8-oidc-fail-open-hardening-media-gateway.md` — `initOIDCVerifierWith` background

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List
