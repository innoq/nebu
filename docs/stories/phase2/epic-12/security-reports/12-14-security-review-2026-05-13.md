## Security Review — Story 12.14

**Diff scope:** Graceful HTTP shutdown pattern, rate limiter ctx-aware goroutine, DB pool explicit close  
**Date:** 2026-05-13  
**Reviewer:** Kassandra (nebu-agent-kassandra)

### Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | LOW | `main.go:srv.Shutdown` | 30s drain window: attacker holding a connection open delays shutdown by up to 30s | Inherent to graceful shutdown design (AC-2 requirement). WriteTimeout=120s bounds eventual response. Accept as design constraint. |

### Detail

**Finding #1 — Drain window delay** [LOW]

The 30s shutdown drain timeout (AC-2) means a malicious client holding a long-running connection open could delay container restart by up to 30s. This is intentional and specified by the acceptance criteria. The existing `WriteTimeout: 120 * time.Second` on the server ensures responses must be sent within 120s regardless. The 30s drain is well within that window. No remediation required — this is the correct trade-off for zero dropped requests during rolling deployments.

**No CRITICAL, HIGH, or MEDIUM findings.**

### Surface Reviewed

- `media/internal/ratelimit/ratelimit.go` — `cleanupLoop` ctx parameter addition, `newCore` and `NewIPRateLimiter` signature updates
- `media/cmd/media/main.go` — graceful shutdown goroutine pattern, `runShutdownSequence` helper
- `media/cmd/media/main_test.go` — AT-12-14-1 through AT-12-14-5 test additions
- `media/internal/ratelimit/ratelimit_test.go` — caller updates for ctx
- `media/internal/ratelimit/ratelimit_internal_test.go` — AT-12-14-3 goroutine exit test

### Summary

CRITICAL: 0  
HIGH: 0  
MEDIUM: 0  
LOW: 1 (drain window — accepted design constraint)

**Verdict: APPROVED**

No security-blocking findings. The shutdown sequence correctly orders srv.Shutdown → pool.Close to prevent in-flight DB query interruption. The ctx propagation to the rate limiter cleanup goroutine is safe and race-free. No new attack surface introduced.
