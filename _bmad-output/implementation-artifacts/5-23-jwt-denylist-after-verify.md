---
security_review: required
---

# Story 5.23: JWT Denylist Check After Signature Verification

Status: ready-for-dev

## Story

As a security-conscious operator,
I want the JWT denylist (revoked-token check) to run **after** signature verification,
so that an attacker cannot flood the DB with random unsigned strings and cannot probe denylist membership with arbitrary inputs.

---

## Background / Motivation

Security audit (2026-04-20): `middleware/auth.go:62–65` calls `store.IsInvalidated(rawToken)` (a DB lookup) **before** `verifier.Verify(ctx, rawToken)`. Every unauthenticated request triggers a DB roundtrip — trivial DB-DoS via unbounded random strings. Also, negative denylist responses implicitly confirm "this value is/is not in the denylist" — mildly useful for attackers reconstructing logout patterns.

---

## Acceptance Criteria

1. `JWTMiddleware` order:
   1. `verifier.Verify(ctx, rawToken)` — reject 401 if invalid signature/exp/aud
   2. After verify succeeds: `store.IsInvalidated(rawToken)` (or `idToken.JTI` if available) — reject 401 if in denylist
   3. Context population

2. `PostgresTokenStore.IsInvalidated` signature unchanged — but now called only on verified tokens.

3. Unit tests:
   - `TestJWT_DenylistCheckOnlyAfterVerify` — invalid-signature token does NOT produce a DB query (stub the store with a fail-fast assertion)
   - `TestJWT_ValidThenDenylisted_Returns401`
   - `TestJWT_ValidNotDenylisted_Returns200`

4. Metrics: `nebu_jwt_validation_total{stage="verify|denylist",result="pass|fail"}` to confirm production behavior matches design.

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestJWT_InvalidTokenSkipsDBLookup` — use a mock `TokenStore` that panics on any call; assert handler returns 401 without panic when given a malformed JWT

2. `TestJWT_ValidThenDenylisted_401` — valid JWT but `IsInvalidated` returns true → 401

---

## Implementation Notes

- Rearrange `middleware/auth.go` flow; small change, but carefully preserve context-propagation
- Update metrics instrumentation accordingly
