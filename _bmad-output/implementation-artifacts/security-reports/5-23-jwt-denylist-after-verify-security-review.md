# Security Review: Story 5.23 -- JWT Denylist Check After Signature Verification

**Date:** 2026-04-20
**Reviewer:** Kassandra (Security Agent)
**Story:** 5-23-jwt-denylist-after-verify
**Scope:** `gateway/internal/middleware/auth.go`, `gateway/internal/middleware/jwt_denylist_order_test.go`
**Classification:** PASS

---

## Summary

Story 5.23 reorders the JWT validation pipeline so that `verifier.Verify()` (signature + expiry + audience) runs **before** `store.IsInvalidated()` (denylist DB lookup). This eliminates a DB-DoS vector where attackers could flood the denylist with arbitrary unsigned strings, and prevents information leakage about denylist membership for unverified inputs. Prometheus counters `nebu_jwt_validation_total{stage,result}` are added to confirm production behavior.

---

## Findings

| # | Severity | Category | Finding | Status |
|---|----------|----------|---------|--------|
| 1 | INFO | Auth Bypass | Expired tokens (`exp` in the past) are rejected by `verifier.Verify()` at Step 1 via `oidc.TokenExpiredError`. `SkipExpiryCheck` is not set anywhere in the codebase (defaults to `false`). An attacker with an expired but signature-valid token **cannot** reach `IsInvalidated`. | OK |
| 2 | INFO | DoS | `IsInvalidated` is only reachable after `verifier.Verify()` succeeds. Random/malformed/unsigned strings are rejected at the crypto layer with zero DB queries. DB-DoS vector eliminated. | OK |
| 3 | INFO | Info Leak | Denylist membership is no longer queryable with arbitrary inputs. Only holders of a cryptographically valid, non-expired token can learn whether that specific token is denylisted -- which is information they could obtain anyway (their own logout). | OK |
| 4 | INFO | Metrics | `nebu_jwt_validation_total` counter uses `stage` and `result` labels. The `denylist` stage is only incremented after `verify/pass`. Verified: no counter increment path exists that reaches `denylist/*` without first passing through `verify/pass`. No sensitive data in label values. | OK |
| 5 | INFO | Error Messages | Three distinct 401 responses: "Token has expired" (expired), "Invalid token" (bad signature/audience), "Token has been logged out" (denylisted). The "Token has been logged out" message is only reachable for verified tokens, so it does not leak denylist state to unauthenticated callers. | OK |

---

## Detailed Analysis

### 1. Can an attacker with an expired but signature-valid token query the denylist?

**No.** The `go-oidc` verifier checks expiry as part of `Verify()`. When `exp` is in the past, it returns `*oidc.TokenExpiredError`, which is caught at line 96 and returns 401 immediately. The code never reaches line 107 (`store.IsInvalidated`). Confirmed: `SkipExpiryCheck` is not used anywhere in the gateway codebase.

### 2. Is `IsInvalidated` NEVER called for an unverified token?

**Correct.** The control flow is:

1. Lines 81-102: `verifier.Verify()` -- if error, return 401 immediately. No `IsInvalidated` call.
2. Line 107: `store.IsInvalidated()` -- only reachable if `Verify` returned `nil` error (token is cryptographically valid + not expired + correct audience).

The `panicStore` test (`TestJWT_InvalidTokenSkipsDBLookup`) structurally proves this: the test would panic if `IsInvalidated` were called on an unverified token.

### 3. Prometheus Counter Safety

- Global `var` registered via `init()` -- standard pattern, no double-registration risk (only one `auth.go` in the binary).
- Labels are static strings (`"verify"`, `"denylist"`, `"pass"`, `"fail"`), not user-controlled. No cardinality explosion risk.
- Counter is incremented before the response is written, ensuring metrics are consistent even if the write fails.

### 4. Nil Store Guard

Lines 107 and 112 both check `store != nil` before calling `IsInvalidated` or incrementing `denylist` counters. When `store` is nil (invalidation checking disabled), the denylist stage is entirely skipped -- no counter increment, no panic. Correct.

---

## Minor Issues Fixed During Review

None. Implementation is clean.

---

## Severity Counts

| Severity | Count |
|----------|-------|
| CRITICAL | 0 |
| HIGH | 0 |
| MEDIUM | 0 |
| LOW | 0 |
| INFO | 5 |

**Result: PASS -- no blocking findings.**
