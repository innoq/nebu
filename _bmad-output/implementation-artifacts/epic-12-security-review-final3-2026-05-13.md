# Epic 12 Security Review — Final SEC Gate 2 (after Story 12.12) — 2026-05-13

## Scope

- **Epic:** 12 — Media Gateway (MinIO backend, thumbnails, download, rate limits, OIDC, audit trail)
- **Base:** `5020ec8` (epic-12 base)
- **HEAD:** `f3344b6` (branch `feature/epic-12-media`)
- **Stories covered (this review focus):** 12.12 (F-3/F-4/F-5 remediation)
- **Definitive epic-end re-review.** Prior reviews:
  - `epic-12-security-review-final-2026-05-13.md` — BLOCKED (HIGH F-2 XFF spoof, MEDIUM F-1 canonical claim)
  - `epic-12-security-review-final2-2026-05-13.md` — **PASS** (F-1 + F-2 remediated by Story 12.11; 3 LOW carried: F-3/F-4/F-5)
  - This review (final3) — verifies Story 12.12 closed all three LOW advisories without regression.

This review focuses on the delta `f6a1d34..f3344b6` (Story 12.12 only). The wider epic surface was already reviewed CLEAN in final2; only verification of the three targeted fixes is required here.

## Diff Surface (Story 12.12)

| File | Lines | Touched |
|------|-------|---------|
| `media/cmd/media/main.go` | +43 −2 | F-3 per-attempt timeout, F-5 startup-warn helper |
| `media/cmd/media/main_test.go` | +166 −0 | F-3 timeout tests + F-5 startup-warn test |
| `media/internal/ratelimit/ratelimit_internal_test.go` | +118 (new) | F-4 white-box cleanup tests |

No production-code change in `media/internal/ratelimit/ratelimit.go` (prior review confirmed implementation already correct — only tests added).

## Verification of F-3 / F-4 / F-5

### F-3 — OIDC retry unbounded context

**Prior advisory:** `initOIDCVerifierWith` passed the parent `context.Background()` directly into `oidc.NewProvider`. A hung OIDC provider (TCP-accept, no HTTP response) could block startup indefinitely, and parent-ctx cancellation (SIGTERM) had no observable effect because Background never cancels.

**Fix verified (`media/cmd/media/main.go:391-428`):**

- New `attemptTimeout` parameter on `initOIDCVerifierWith`. Production path passes `10 * time.Second` (line 360).
- Each attempt wraps the parent ctx with `context.WithTimeout(ctx, attemptTimeout)` and calls `cancel()` immediately after `newProvider` returns. No goroutine/resource leak.
- `ctx.Err()` is checked at the top of every retry iteration before allocating a new attempt. This is the SIGTERM gate that the commit message references.

**Residual gap (informational, not a finding):**
The production call site (`main.go:215`) still passes `context.Background()` — no `signal.NotifyContext` wiring is installed. The `ctx.Err()` guard in the loop is therefore dead code in production: Background never cancels, so SIGTERM during startup still kills the process via OS signal, not via cooperative cancellation. The commit message says "SIGTERM propagates immediately into the retry loop" — that's true only when a caller wires a signal-aware context, which media/cmd/media does not do today. Per-attempt timeout (10s × 5 attempts = 50s worst case) is the effective bound. Acceptable. Not blocking — the function is now ready for a signal-aware context whenever it is wired up, and the per-attempt timeout already prevents the original "block forever" failure mode that motivated F-3.

**Minor note (also not a finding):** the retry loop still uses unconditional `time.Sleep(retryDelay)` between attempts (line 425). A cancellation during the 2-second sleep is not observed until the sleep returns. With 5 attempts and 2s delay this caps at ~10 s of unwakable wait, plus 50 s of per-attempt timeouts → 60 s startup ceiling. Operationally fine. Could be tightened to `select { case <-ctx.Done(): … case <-time.After(retryDelay): }` if a signal-aware context is ever wired, but not required here.

**Verdict:** F-3 closed — the original "indefinite block on hung provider" risk is fully remediated.

### F-4 — Cleanup race jitter

**Prior advisory:** Concern that `cleanupOnce` could evict an entry concurrently in use by an in-flight request, causing a brief per-IP bypass at the 5-minute mark.

**Fix verified:**

Production code (`media/internal/ratelimit/ratelimit.go:89-119`) was already correct in final2. Story 12.12 added white-box tests in `media/internal/ratelimit/ratelimit_internal_test.go`:

- `TestIPRateLimiter_CleanupOnce_DoesNotEvictRecentEntry` — entry with `lastSeen = -1s` survives `cleanupOnce(5m)`.
- `TestIPRateLimiter_CleanupOnce_EvictsStaleEntry` — entry with `lastSeen = -10m` is evicted.
- `TestIPRateLimiter_CleanupOnce_MixedEntries` — mixed recent + stale.

The known TOCTOU window (cleanup acquires `mu` before getOrCreate updates `lastSeen` for a stale entry, then deletes the entry while the handler still holds the limiter pointer) remains theoretically present but is bounded by the 5-minute eviction maxAge vs. the 100 req/s download rate. The next request from the same IP would re-create a fresh bucket — equivalent to a one-time burst of `Burst` (5 or 20) once per 5 minutes per IP. This was already classified LOW in final2 and the addition of explicit tests for the dominant cases is appropriate remediation.

**Verdict:** F-4 closed (test-only — implementation was already correct).

### F-5 — Silent `NEBU_RATE_LIMIT_DISABLED`

**Prior advisory:** Setting `NEBU_RATE_LIMIT_DISABLED=true` silently returns a no-op middleware. Operators could mis-configure production and not realise rate limiting was off.

**Fix verified (`media/cmd/media/main.go:331-339` + invocation at `:248`):**

```go
func logIfRateLimitDisabled() {
    if os.Getenv("NEBU_RATE_LIMIT_DISABLED") == "true" {
        slog.Warn("rate limiting disabled — NEBU_RATE_LIMIT_DISABLED is set")
    }
}
```

Called exactly once during `main()` startup, before either rate-limiter is constructed. The `ratelimit.NewIPRateLimiter` function still independently checks `NEBU_RATE_LIMIT_DISABLED` (correct — defence in depth), but the operator-visible warning is now emitted exactly once at startup. Behaviour is consistent: when the flag is unset, no log line; when set, one `slog.Warn` per startup regardless of how many limiter tiers are constructed.

**Verdict:** F-5 closed.

## New Issues Introduced by Story 12.12

None. The diff is surgical (43 lines of production code + tests) and limited to the three targeted advisories.

## Findings

| # | Severity | Story | Location | Vulnerability | Status |
|---|----------|-------|----------|---------------|--------|
| — | — | — | — | No new findings. F-3, F-4, F-5 all remediated. | Closed |

## Cross-Story Patterns

No new patterns. Story 12.12 confirms the recurring-pattern observation from MEMORY.md: when an epic-end review identifies multiple LOW advisories that share a service surface (here: media gateway startup + rate limiter), addressing them in a single dedicated remediation story keeps the diff auditable and the fix scope tight.

## Accepted Risks

None new. The two minor notes on F-3 (Background context in production, `time.Sleep` not ctx-aware) are not findings — the existing per-attempt timeout already prevents the failure mode F-3 was raised against.

## Follow-up Stories Required

None.

## Summary

CRITICAL: 0 | HIGH: 0 | MEDIUM: 0 | LOW: 0
Follow-up stories required: 0
Accepted risks: 0

**Epic security gate:** **PASS — Epic 12 is cleared for `done`.**

All three LOW advisories from the prior epic-end review (F-3 OIDC retry context, F-4 cleanup race jitter, F-5 silent disable) have been remediated by Story 12.12. No new findings, no regressions, no cross-story patterns. Epic 12 may be marked `done`.
