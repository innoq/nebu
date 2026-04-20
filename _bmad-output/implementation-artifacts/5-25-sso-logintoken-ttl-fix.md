---
security_review: required
---

# Story 5.25: SSO LoginToken TTL Correction (5 min → 30 s)

Status: ready-for-dev

## Story

As a security-conscious operator,
I want the SSO `loginToken` to live only 30 seconds as originally designed,
so that a harvested token (browser history, referrer, proxy log) expires before it can be replayed.

---

## Background / Motivation

Security audit (2026-04-20): `matrix/sso.go:258–266` calls `globalLoginTokens.save(opaqueToken, rawIDToken, 5*time.Minute)`. Adjacent code comments (and Story 2.17/2.18 AC) specify 30 seconds. This is a silent drift — tests for the value never existed, so the longer window went unnoticed.

A 5-minute window is an unnecessarily generous replay budget for a single-use bearer token passed in a URL.

---

## Acceptance Criteria

1. `matrix/sso.go` `loginTokenTTL` becomes a package-level `const` set to `30 * time.Second`.

2. `globalLoginTokens.save(...)` uses `loginTokenTTL`.

3. The divergent comment is removed.

4. Regression test asserts `const loginTokenTTL = 30*time.Second` (compile-time assertion via a small test).

5. Unit test asserts the token is rejected after TTL expires.

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestLoginToken_ExpiresAfter30s` — inject fake clock, save token, advance by 31s, pop → not found

2. `TestLoginToken_ValidWithin30s` — pop within 29s → returns stored ID token, entry removed

3. `TestLoginToken_TTLConstantIs30s` — compile-time assertion

---

## Implementation Notes

- Smallest change: one constant + remove comment
- If `save(opaque, raw, ttl)` has other callers with different TTLs, grep them and confirm they're intentional
