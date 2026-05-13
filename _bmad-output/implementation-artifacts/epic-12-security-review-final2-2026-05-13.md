# Epic 12 Security Review — Definitive SEC Gate 2 (post-12.11) — 2026-05-13

## Scope

- **Epic:** 12 — Media Gateway Phase 2 (MinIO backend, IAM, thumbnails, SEC Gate 2 hardening, OIDC fail-closed, canonical user ID, per-IP rate limiting, **12.11 SEC fixes**)
- **Base:** `5020ec8`
- **HEAD:** `f6a1d34` (branch `feature/epic-12-media`)
- **Reviewer:** Kassandra (adversarial security)
- **Trigger:** Definitive epic-end SEC Gate 2 after Story 12.11 landed remediations for prior-review blocking findings F-1 (MEDIUM) and F-2 (HIGH).
- **Stories covered (full diff range):**
  - 12.1 — MinIO Docker Compose + Docker Secrets
  - 12.2 — Storage interface refactor
  - 12.3 — MinIO backend wiring + IAM hardening
  - 12.4 — Media download error classification
  - 12.5 — On-demand thumbnail generation
  - 12.6 — Blurhash + animated thumbnail correctness
  - 12.7 — SEC Gate 2 fixes (closing 10 prior findings)
  - 12.8 — OIDC fail-open hardening (closing prior Residual-1)
  - 12.9 — Canonical Matrix user ID in upload audit trail
  - 12.10 — Per-IP rate limiting on media gateway
  - **12.11 — Media Gateway Rate Limit + Audit Trail SEC Fixes (F-1, F-2 remediations)**
  - Plus Epic-11 carry-over (11.7–11.11) and infra fixes from earlier review passes.

## Method

1. **Targeted re-verification** of the two blocking findings from the prior epic-end review (`epic-12-security-review-final-2026-05-13.md`):
   - F-2 (HIGH, XFF spoofing rate-limit bypass) → `NewIPRateLimiter` signature, `clientIP()` gating, compose default.
   - F-1 (MEDIUM, wrong OIDC claim for `uploader_user_id`) → `extractClaimFromMap`, `OIDCTokenVerifier` constructor, compose default.
2. **Adversarial check** of the new 12.11 code path for regressions, unsafe fallbacks, or new attack surface.
3. **Re-check of LOW-severity advisories** (F-3 unbounded ctx, F-4 cleanup race, F-5 silent disable, F-6 doc hygiene) — confirm none have escalated.
4. **Cross-call-site sweep**: `NewIPRateLimiter`, `NewOIDCTokenVerifier` signature changes propagated through all callers and tests.
5. **MEMORY.md scan** for recurring patterns — particularly "XFF rate-limit spoofing on directly-exposed services" and "`uploader_user_id` ≠ Matrix user ID".

## Findings — Prior-Blocking Verification

| Prior Blocking Finding | Story Intended to Fix | Status | Evidence |
|---|---|---|---|
| **F-2 (HIGH) — XFF spoofing bypass** | **12.11** | **FIXED** | `media/internal/ratelimit/ratelimit.go:59-76, 135-152, 168-202`: `NewIPRateLimiter(cfg, trustedProxy bool)` signature; `clientIP` gates the entire XFF parse path behind `rl.trustedProxy`; when `false`, code never reads `X-Forwarded-For` and always returns `RemoteAddr` (port-stripped). `media/cmd/media/main.go:245-253`: `trustedProxy := os.Getenv("NEBU_TRUSTED_PROXY") == "true"` (default false). `docker-compose.yml:234-237`: `NEBU_TRUSTED_PROXY: "false"` explicit in compose. Tests `TestRateLimit_TrustedProxyFalse_IgnoresXFF`, `TestRateLimit_TrustedProxyTrue_UsesXFF`, `TestRateLimit_TrustedProxyFalse_BypassNotPossible` cover the three AC-F2 cases including the active spoofing-attempt scenario (10 distinct XFF values exhaust the same RemoteAddr bucket → 429 on attempt 11). |
| **F-1 (MEDIUM) — Wrong OIDC claim for uploader_user_id** | **12.11** | **FIXED** | `media/internal/upload/upload.go:39-67`: `OIDCTokenVerifier` carries `claimName`; constructor accepts it (defaults to `"name"` matching migration 000044 + 3a5e305 gateway DB default). `extractClaimFromMap` (lines 82-98) is a pure function: returns configured claim when present, else falls back to `sub` with `slog.Warn`, else returns explicit error. `media/cmd/media/main.go:213`: `oidcUserIDClaim := getenv("NEBU_OIDC_USER_ID_CLAIM", "name")` wired through `initOIDCVerifier` → `NewOIDCTokenVerifier`. `docker-compose.yml:233`: `NEBU_OIDC_USER_ID_CLAIM: "name"`. Tests `TestExtractClaimFromMap_Sub_WhenPresent`, `TestExtractClaimFromMap_Name_WhenPresent`, `TestExtractClaimFromMap_FallsBackToSub_WhenClaimMissing`, `TestExtractClaimFromMap_Errors_WhenBothMissing` cover AC-F1-1..4. Now consistent with gateway's `FormatUserIDFromClaims` semantics — audit-correlation between `media_files.uploader_user_id` and `events.sender` works for the default `name`-claim deployment. |

## Findings — Prior LOW Advisories

| # | Prior Severity | Re-assessment | Notes |
|---|---|---|---|
| F-3 | LOW (OIDC retry uses `context.Background()`, unbounded) | **Unchanged LOW** | 12.11 did not address. `initOIDCVerifier` still passes `context.Background()` (`main.go:191, 215`). External attackers cannot trigger this; affects startup reliability only. Acceptable carry-forward — non-blocking. |
| F-4 | LOW (cleanupOnce race jitter) | **Unchanged LOW** | Code path unchanged in 12.11. No bypass — small fresh-burst-window jitter at cleanup boundary. Non-blocking. |
| F-5 | LOW (no log when `NEBU_RATE_LIMIT_DISABLED=true`) | **Unchanged LOW** | Code path unchanged. Operator audit hygiene gap, not exploitable. Non-blocking. |
| F-6 | INFO (doc hygiene "rightmost-minus-1" naming) | **Resolved** | The 12.11 rewrite of `clientIP` updated the inline documentation (`ratelimit.go:121-152`) to state "rightmost entry" without the misleading "minus-1" framing. The header doc block (lines 12-17) describes the actual semantics correctly. No remaining inconsistency. |

## New Findings

None at HIGH or CRITICAL.

### Adversarial Review of 12.11 Changes

I attempted the following exploitation patterns against the new code; all are mitigated:

1. **Spoofed XFF when `NEBU_TRUSTED_PROXY=true`** — operator-opt-in attack surface remains: when an operator turns on `true` without fronting :8009 with a header-stripping proxy, the rightmost XFF entry becomes spoofable again. Mitigation is in the documentation at `ratelimit.go:133-134` and the env-var name itself (operator must affirmatively choose `true`). This is the standard "trusted-proxy" semantics — same as the API gateway. Not a regression; not a finding.

2. **Empty `claimName` propagation** — `NewOIDCTokenVerifier("")` defaults to `"name"` (line 49-50). If `NEBU_OIDC_USER_ID_CLAIM` is set to an empty string in compose, `getenv` returns the default `"name"` (because `getenv` treats empty as unset). Two layers of defence — safe.

3. **Non-string claim value injection** — `extractClaimFromMap` does `val.(type-assert string)`; non-string values (e.g. number, array, object) silently miss the configured-claim branch and fall through to `sub`. No type confusion or panic. Acceptable.

4. **JWT containing `sub` claim with malicious local-part characters** — passes through `formatMatrixUserID` → `sanitiseLocalpart` which strips everything except `[a-z0-9._-]`. ✓ (unchanged from prior review).

5. **Combined: attacker sends spoofed XFF with no token** — auth check (`upload.go:226-247`) rejects with `401 M_MISSING_TOKEN` / `401 M_UNKNOWN_TOKEN` before the request hits the rate limiter side-effects. The rate limit still buckets correctly on RemoteAddr because trustedProxy=false. No amplification path.

6. **`extractClaimFromMap` log injection** — `slog.Warn(..., "configured_claim", claimName)` uses structured logging; `claimName` comes from the operator-controlled env var, not request input. Even via `claims[claimName]` lookup, the map key is the env-var value, not attacker-controlled. Safe.

### Migration Review

No new migrations beyond the previously-cleared 000043–000047 set. Compose-only changes for the env vars added by 12.11 (`NEBU_OIDC_USER_ID_CLAIM`, `NEBU_TRUSTED_PROXY`) — no schema impact.

## Cross-Story Pattern Status

| Pattern (from MEMORY.md) | Status at HEAD |
|---|---|
| `uploader_user_id` ≠ Matrix user ID | **Closed.** Configurable claim now mirrors the gateway. |
| XFF rate-limit spoofing on directly-exposed services | **Closed.** Gated by `NEBU_TRUSTED_PROXY=false` default; spoof tests prove non-bypassable. |
| Per-story CLEAN reviews stacking into combined HIGH at epic-end | **Validated.** This very pattern is what the per-story → epic-end SEC Gate 2 split was designed to catch. The pattern now has its corrective workflow proven end-to-end (12.8/12.9/12.10 CLEAN → epic-end HIGH found → 12.11 remediation → epic-end CLEAN). |
| OIDC fail-open at startup | **Closed** (in 12.8); confirmed not regressed. |
| Permissive RLS UPDATE without key-scope | **Closed** (in 000046); confirmed not regressed. |
| Other migration/RLS patterns | **No new tables; no new RLS gaps.** |

## Accepted Risks (pre-existing, carried)

| Risk | Justification | Accepted by | Date |
|---|---|---|---|
| Download / thumbnail endpoints intentionally unauthenticated per Matrix v3 spec | Matrix CS API v3 download is unauthenticated by design. Authenticated `_matrix/client/v1/media/*` is a separate epic. Mitigated in MVP by dimension caps (12.7) and per-IP rate limits with non-bypassable IP keying (12.10 + 12.11). | Project policy (CLAUDE.md "Matrix API Scope") | 2026-05-12 |
| F-3, F-4, F-5 LOW advisories carried | Non-exploitable; operational/defence-in-depth nature; deferred to non-blocking follow-ups. | Phil (implicit — accepted in prior review) | 2026-05-13 |

## Follow-up Stories Required (BLOCKING for epic-done)

**None.** All HIGH and MEDIUM findings from the prior SEC Gate 2 review are remediated by Story 12.11 with verified test coverage and correct compose-level defaults.

## Recommended Follow-ups (non-blocking, optional)

- **12-FU-12** (LOW) — Bound `oidc.NewProvider` startup retries with `context.WithTimeout(10s)` per attempt. (F-3 from prior review.)
- **12-FU-13** (LOW) — Emit `slog.Warn` when `NEBU_RATE_LIMIT_DISABLED=true` is honoured. (F-5 from prior review.)
- **12-FU-14** (LOW) — Document the operator hardening requirement: when setting `NEBU_TRUSTED_PROXY=true` in production, the reverse proxy in front of :8009 MUST strip `X-Forwarded-For` from external clients before adding its own entry. Currently noted only in code comments; should be in operator-facing deployment docs.

## Summary

| Severity | Count |
|---|---|
| CRITICAL | **0** |
| HIGH | **0** |
| MEDIUM | **0** |
| LOW | 3 (F-3, F-4, F-5 — carried, accepted) |
| INFO | 0 (F-6 resolved by 12.11 doc rewrite) |

- Follow-up stories required (blocking): **0**
- Recommended follow-ups (non-blocking): 3
- Accepted risks: 2 (one carried policy risk + one LOW-bundle acceptance)

**Epic security gate: PASS — Epic 12 may be marked done.**

## Classification

**CLEAN.**

The definitive SEC Gate 2 review confirms that Story 12.11 successfully remediates both blocking findings from the prior epic-end review:

- **F-2 (HIGH)** — closed by `trustedProxy bool` parameter, RemoteAddr-only default, and explicit `NEBU_TRUSTED_PROXY=false` compose value. Active-spoofing test proves non-bypassable.
- **F-1 (MEDIUM)** — closed by `NEBU_OIDC_USER_ID_CLAIM=name` (default) + `extractClaimFromMap` pure function with safe `sub` fallback. Audit-correlation now functions for default deployments.

No new findings introduced by 12.11. The three LOW advisories (F-3/F-4/F-5) remain in the non-blocking bucket and are appropriate as recommended follow-ups, not gate blockers.

This is the third epic-end review pass on Epic 12. The first found 10 issues (closed by 12.7). The second found 2 blocking issues (F-1, F-2). This pass finds zero blocking issues. The per-story → epic-end SEC Gate 2 split worked as designed: each per-story review was correctly CLEAN, but composed-topology blind spots (XFF-spoof + ports-exposed; canonical-claim mismatch with gateway) only surfaced at epic boundary. The team responded with a tightly-scoped remediation story (12.11) that closed both at the right architectural layer.

---

*Reviewed by Kassandra, 2026-05-13. The work is honest. The fixes are real. The team did this right.*
