# Security Review — Story 12.10

**Diff scope:** `media/internal/ratelimit/ratelimit.go` (new — per-IP token-bucket middleware), `media/cmd/media/main.go` (wiring), `media/go.mod` (add golang.org/x/time)
**Date:** 2026-05-13
**Reviewer:** Kassandra (SEC Gate 1)

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | LOW | `ratelimit.go:getOrCreate` | `NewIPRateLimiter` spawns a background goroutine that runs for process lifetime; called N times (once per tier in production) → N goroutines, each with a ticker | In production this is called exactly twice (upload + download tiers) — 2 goroutines is acceptable. If test code calls it repeatedly, goroutine leak accumulates. No production impact. |
| 2 | LOW | `ratelimit.go:extractClientIP` | When deployed without a reverse proxy that strips client-supplied `X-Forwarded-For` headers, a single-entry XFF (no 2nd entry) falls through to `RemoteAddr` — this is correct. However if deployed with a misbehaving proxy that forwards a single attacker-controlled entry, the spoofed IP could be used as the rate-limit key, allowing an attacker to rate-limit a different IP. | Operator responsibility: configure reverse proxy to strip XFF before forwarding. The same pattern + risk level is accepted in `gateway/internal/middleware/ratelimit.go` (Story 5.21). No code change needed; deployment documentation should note this. |

## Detail

**Finding #1 — Background goroutine per limiter instance** [LOW]
`NewIPRateLimiter` → `newCore` starts a `cleanupLoop` goroutine that runs until the process exits. In `main.go` this is called exactly twice (upload + download), producing 2 permanent goroutines. In test code that calls `NewIPRateLimiter` across multiple test functions, each call spawns a new goroutine that is never stopped. No security impact; negligible resource impact in production. Advisory: if the package ever supports graceful shutdown, a `context.Context`-cancellable loop would be cleaner.

**Finding #2 — X-Forwarded-For single-entry fallback** [LOW]
`extractClientIP` uses the rightmost-minus-1 strategy (same as gateway). When `X-Forwarded-For` has exactly 1 entry (or no header), `RemoteAddr` is used — this is the direct peer address and is trust-worthy. The risk is only present if: (a) no reverse proxy is deployed, AND (b) clients can freely forge `X-Forwarded-For` headers. In that case, a client could include a single XFF value and the code falls through to `RemoteAddr` anyway (correct — no spoofing possible). The only remaining edge: a 1-entry XFF that is from a proxy but the attacker controls that proxy. Out of scope for this deployment model. Accepted.

## Summary

CRITICAL: 0
HIGH: 0
MEDIUM: 0
LOW: 2 (advisory — both accepted, same patterns as story 5.21 and 12.7)

**Verdict:** APPROVED
