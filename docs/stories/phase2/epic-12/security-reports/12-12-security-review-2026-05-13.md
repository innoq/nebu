# Security Review — Story 12.12: Media Gateway Startup + Rate Limiter Hardening (F-3/F-4/F-5)

**Date:** 2026-05-13
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Story:** 12.12 — Media Gateway Startup + Rate Limiter Hardening
**Classification: CLEAN**

---

## Summary

Story 12.12 closes three LOW advisories from the epic-12 definitive SEC Gate 2 review
(`epic-12-security-review-final2-2026-05-13.md`):

- **F-3 [LOW]** — `initOIDCVerifierWith` now adds a 10-second per-attempt timeout via
  `context.WithTimeout(ctx, attemptTimeout)`, preventing startup hangs on a TCP-accepting
  but never-responding OIDC provider. Parent ctx cancellation check added at loop entry.
- **F-4 [LOW]** — Existing `cleanupOnce` is confirmed correct (already uses `entry.lastSeen`).
  New white-box tests (`ratelimit_internal_test.go`) guard against regression.
- **F-5 [LOW]** — `logIfRateLimitDisabled()` emits a single `slog.Warn` at startup when
  `NEBU_RATE_LIMIT_DISABLED=true`. Exactly one call in `main()`.

---

## Findings

| ID | Severity | Description | Status |
|----|----------|-------------|--------|
| — | — | No new findings | — |

**CRITICAL: 0 | HIGH: 0 | MEDIUM: 0 | LOW: 0**

---

## Detailed Analysis

### F-3 — `initOIDCVerifierWith` per-attempt timeout

- `context.WithTimeout(ctx, attemptTimeout)` → child context with 10s deadline.
  Parent deadline takes precedence if earlier (correct Go semantics). ✓
- `cancel()` called immediately after `newProvider` returns — no context leak. ✓
- `ctx.Err()` check at loop entry catches SIGTERM-style parent cancellation before each
  attempt — the check is on the parent, not the attempt context. ✓
- No credential or token data in `slog.Warn` output — only error type and attempt count. ✓
- `initOIDCVerifier` still calls `os.Exit(1)` when all retries fail. Fail-closed guarantee
  not weakened by adding the per-attempt timeout. ✓

### F-5 — `logIfRateLimitDisabled()`

- Reads `NEBU_RATE_LIMIT_DISABLED` env var; compared to `"true"` only — value never logged. ✓
- `slog.Warn` message has no structured fields with sensitive data. ✓
- Called once in `main()` before mux wiring — no per-request execution path. ✓
- Exactly one log line at startup regardless of how many `NewIPRateLimiter` instances created. ✓

### F-4 — Test-only changes

No production code changes to `ratelimit.go`. Tests verify `cleanupOnce` correctness
directly via `package ratelimit` (internal test access). No new attack surface. ✓

### Regression Check (MEMORY.md patterns)

| Pattern | Status |
|---------|--------|
| XFF rate-limit spoofing | No change to `clientIP()` or `trustedProxy`. Not regressed. |
| OIDC fail-open at startup | `initOIDCVerifier` still calls `os.Exit(1)`. Not weakened. |
| `uploader_user_id` canon | No change to claim extraction path. Not regressed. |
| Per-story CLEAN stacking into combined HIGH | Single-story scope; no new combined surface. |

---

## Conclusion

Story 12.12 closes all three LOW advisories from the epic-12 definitive SEC Gate 2 review
without introducing new attack surface or weakening existing security controls.

No follow-ups required.

**Recommendation: APPROVE for commit.**
