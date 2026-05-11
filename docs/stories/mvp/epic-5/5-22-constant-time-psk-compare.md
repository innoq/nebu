---
security_review: required
---

# Story 5.22: Constant-Time PSK Comparison (Hash-then-Compare)

Status: ready-for-dev

## Story

As a security-conscious operator,
I want the node-registration PSK comparison to be fully length-agnostic in timing,
so that a timing attacker cannot learn the length of the configured PSK.

---

## Background / Motivation

Security audit (2026-04-20): `middleware/psk.go:16` uses `subtle.ConstantTimeCompare([]byte(authHeader), []byte(expected))`. Go's documentation is explicit: `ConstantTimeCompare` short-circuits and returns 0 immediately when lengths differ — so it leaks length. Attackers on the same network probing `POST /_internal/nodes/register` can learn PSK length byte-by-byte.

The fix is standard: hash both sides to a fixed-length digest first, then compare the digests.

---

## Acceptance Criteria

1. `middleware/psk.go` hashes both `authHeader` and `expected` with `sha256.Sum256` before `subtle.ConstantTimeCompare`.

2. Additionally, the hashing uses `hmac.New(sha256.New, domainSeparator)` with a compile-time domain-separation constant (e.g. `[]byte("nebu/psk/v1")`) so the digests are not usable as precomputed-hash attacks.

3. Same fix applied to any other secret/token comparison in the gateway — audit `grep "ConstantTimeCompare\|bytes.Equal" gateway/internal/` and verify each site is either OK (already fixed-length) or gets the same treatment.

4. Unit tests:
   - `TestPSK_TimingStable_OnLengthMismatch` — benchmark that compares 10k short-vs-long mismatches; assert median latency difference < 10% (loose bound, not strict timing oracle proof)
   - `TestPSK_AcceptsCorrect`
   - `TestPSK_RejectsWrong`

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestPSK_AcceptsCorrect` — exact match → 200
2. `TestPSK_RejectsWrong` — wrong PSK → 401
3. `TestPSK_TimingDistribution` — benchmark with `testing.B` showing no statistically significant timing diff between 1-byte-wrong and 50-byte-shorter mismatches

---

## Implementation Notes

- Extract helper `constantTimeEqualHashed(a, b []byte) bool` into a small internal package
- Consider using `hmac.Equal(hmac.Sum256(domain, a), hmac.Sum256(domain, b))` as a one-liner
- Do NOT log PSKs or authHeader contents even at debug level
