# Security Review — Story 12.11: Media Gateway Rate Limit + Audit Trail SEC Fixes

**Date:** 2026-05-13
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Story:** 12.11 — Media Gateway Rate Limit + Audit Trail SEC Fixes
**Classification: CLEAN**

---

## Summary

Story 12.11 directly remediates two findings from the epic-12 final security review
(`epic-12-security-review-final-2026-05-13.md`):

- **F-2 [HIGH]** — XFF rate-limit spoofing bypass on directly-exposed media gateway
- **F-1 [MEDIUM]** — `uploader_user_id` using hardcoded `sub → name` claim instead of operator-configured `NEBU_OIDC_USER_ID_CLAIM`

Both vectors are correctly closed by this story's implementation.

---

## Findings

| ID | Severity | Description | Status |
|----|----------|-------------|--------|
| — | — | No new findings | — |

**CRITICAL: 0 | HIGH: 0 | MEDIUM: 0 | LOW: 0**

---

## Detailed Analysis

### F-2 Fix — `media/internal/ratelimit/ratelimit.go`

**Bypass closure verified:** When `NEBU_TRUSTED_PROXY=false` (default), `clientIP()` never
reads `X-Forwarded-For`. The bucket key is always `RemoteAddr`. An attacker at
`RemoteAddr=5.6.7.8` cannot obtain a fresh rate-limit bucket by varying the XFF header value.
AT-12-11-3 explicitly tests 11 requests from same `RemoteAddr` with different XFF headers —
all 11 are correctly counted against `5.6.7.8`.

**trustedProxy=true path:** When enabled, the rightmost XFF entry (proxy-appended) is used.
The security note requiring the reverse proxy to strip incoming client-supplied XFF headers
is documented in code comments. This matches the gateway pattern
(`gateway/internal/middleware/ratelimit.go:85-99`).

**Single-entry XFF with trustedProxy=true:** Correctly falls through to `RemoteAddr` —
a single-entry XFF could be client-spoofed, so the fallback is the safe choice.

**No new attack surface:** The fix is purely restrictive — it disables a previously-always-on
behavior (XFF reading) behind a flag that defaults to `false`.

### F-1 Fix — `media/internal/upload/upload.go`

**Claim value injection:** The OIDC claim value flows through
`extractClaimFromMap` → `VerifyToken` → `formatMatrixUserID` → `sanitiseLocalpart`.
The sanitiser strips all non-`[a-z0-9._-]` characters. No injection vector.

**Type assertion safety:** `rawClaims[claimName].(string)` uses the two-value form
`if s, ok := val.(string); ok && s != ""`. No panic possible on non-string claims.

**Sub fallback:** When the configured claim is missing, falls back to `sub` with a
`slog.Warn`. The fallback value also goes through `sanitiseLocalpart`. The log line
emits only the claim name, not the token value — no secret leak.

**Empty claim value:** If the claim is present but empty, the code treats it as missing
and falls through to sub fallback. Correct — an empty string identity would cause
`uploader_user_id = @:server`, which is invalid.

**Default "name":** `getenv("NEBU_OIDC_USER_ID_CLAIM", "name")` and
`NewOIDCTokenVerifier(v, "")` both default to `"name"`, matching migration 000044.

### main.go

**`NEBU_TRUSTED_PROXY` parsing:** Only the string `"true"` enables proxy trust. Any
other value (empty, `"1"`, `"yes"`, unset) defaults to `false`. Intentionally strict.

**`NEBU_OIDC_USER_ID_CLAIM` parsing:** Used only as a map key for claim lookup in
`rawClaims`. Not interpolated into SQL, shell, or log output. No injection surface.

### docker-compose.yml

`NEBU_TRUSTED_PROXY: "false"` and `NEBU_OIDC_USER_ID_CLAIM: "name"` explicitly
document the secure defaults. No credentials or secrets added.

---

## Recurring Pattern Check

| Pattern (from MEMORY.md) | This Story |
|--------------------------|------------|
| XFF rate-limit spoofing on directly-exposed services | ✓ Fully remediated — `trustedProxy` flag added |
| `uploader_user_id` ≠ Matrix user ID | ✓ Partially closed (12.9 added format; 12.11 adds configurable claim) |

---

## Conclusion

Story 12.11 closes the two outstanding security findings from the epic-12 final review.
No new attack surface is introduced. Implementation is conservative (secure defaults,
explicit opt-in for proxy trust, operator-configurable claim with safe fallback).

**Recommendation: APPROVE for commit.**
