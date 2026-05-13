# Epic 12 Security Review — Final5 (Definitive SEC Gate 2 after Story 12.14) — 2026-05-13

## Scope

- Epic: **12 — Media Gateway (MinIO + Thumbnails + Hardening + Graceful Shutdown)**
- Base: `5020ec8`
- HEAD: `fbe51e7` (Story 12.14 — Full Graceful Shutdown)
- Stories covered (post-base): **12.1 → 12.14** (full epic) — but this pass focuses on the **delta introduced by 12.14**, with epic-wide regression scan.
- Reviewer: Kassandra (adversarial security review)
- Trigger: definitive epic-end SEC Gate 2 after Story 12.14 closes the final shutdown gap (`srv.Shutdown` + DB pool + rate limiter goroutine).

## Stories in delta since previous final4 review

| Story | Description | Files touched |
|-------|-------------|---------------|
| 12.14 | Full Graceful Shutdown — HTTP drain via `srv.Shutdown(30s)`, ctx-aware rate limiter `cleanupLoop`, explicit `pool.Close()` after drain, exit 0 with `"media gateway stopped"` | `media/cmd/media/main.go`, `media/internal/ratelimit/ratelimit.go`, plus tests |

Previous SEC Gate 2 reviews carried 0 CRITICAL / 0 HIGH / 0 MEDIUM / 0 LOW after final4 (12.13). This review revisits 12.14 against the full epic-12 scope.

## Focus areas this pass

1. Shutdown sequence integrity — no request-drop window, no DB-leak race, no SIGKILL-before-drain.
2. Signal handler correctness — `signal.NotifyContext` + `defer stop()` semantics.
3. **New attack surfaces in the shutdown logic itself**: can an attacker exploit the shutdown phase to
   - bypass auth (e.g., handlers short-circuit on `ctx.Done()`),
   - reset rate-limit buckets (e.g., re-create sync.Map),
   - exhaust resources during drain (e.g., infinite drain window),
   - inject privileged state via a SIGTERM-triggered code path?
4. Goroutine / channel hygiene — leaks, deadlocks, double-close.

## Findings

| # | Severity | Story | Location | Vulnerability | Status |
|---|----------|-------|----------|---------------|--------|
| — | — | — | — | **No CRITICAL/HIGH/MEDIUM findings** | — |
| L-1 | LOW (carry, advisory) | 12.14 | `media/cmd/media/main.go:295-311` | Fatal-`serverErr` path skips `pool.Close()` — minor resource hygiene only (OS reclaims FDs on exit; postgres rolls back idle conns). | Carry / advisory |
| L-2 | LOW (carry, advisory) | 12.14 | `media/cmd/media/main.go:309-317` | Rate-limit `sync.Map` is not explicitly drained on shutdown — irrelevant in a single-process model (new process = fresh map), but means an in-process restart (hypothetical, not implemented) would inherit dirty state. | Carry / advisory |
| L-3 | LOW (carry, advisory) | 12.14 | `media/cmd/media/main.go:316` | `shutdownCtx` uses a hard-coded 30s timeout; not env-configurable. Docker `stop_grace_period` must be ≥ 30s or Docker will SIGKILL before drain completes. Operator footgun, not a security issue. | Carry / advisory |

Total: **0 CRITICAL · 0 HIGH · 0 MEDIUM · 3 LOW (advisory)**

## Detail — Why 12.14 introduces no new attack surface

### Shutdown sequence walkthrough (security-critical path)

1. **Process up, listening.** `srv.ListenAndServe()` runs in a background goroutine. Main blocks on `select { case <-serverErr: case <-ctx.Done(): }`.
2. **SIGTERM arrives** → `signal.NotifyContext` cancels `ctx` → main wakes on `<-ctx.Done()` → logs `"shutdown signal received, draining..."`.
3. `runShutdownSequence(shutdownCtx, srv, pool.Close)` is called with a fresh `context.WithTimeout(context.Background(), 30s)` — deliberately decoupled from the already-cancelled parent ctx so `srv.Shutdown` actually has 30s to drain.
4. **`srv.Shutdown(shutdownCtx)`** (Go stdlib semantics):
   - Listener is closed immediately → new TCP connections receive ECONNREFUSED.
   - Active connections finish their current request; idle keep-alive connections are closed.
   - Returns when all handlers finish OR shutdownCtx expires.
5. **`pool.Close()`** runs only after `srv.Shutdown` returns. pgxpool.Close waits for current acquisitions to release; in-flight queries complete or are cancelled cleanly.
6. **Log line + return from main** → exit 0.

### Auth bypass during shutdown — NO

- Upload handler (`media/internal/upload/upload.go:224-259`) performs Bearer + OIDC verification on every request unconditionally. Verification uses `r.Context()` — the per-request context, derived from `srv.BaseContext` which defaults to `context.Background()`. **`r.Context()` is NOT linked to the parent SIGTERM ctx** (no custom `BaseContext` is configured on the http.Server). In-flight requests during shutdown therefore complete their full auth stack with an uncancelled request context.
- The `uploadVerifier` instance is captured in the handler closure at startup and never reassigned. No nil-deref window opens during shutdown.
- Rate-limit middleware (`uploadRL` / `downloadRL`) is also captured pre-wrap; it does not branch on parent ctx. The cleanup goroutine exiting via `ctx.Done()` does NOT clear the `sync.Map` of limiters — so per-IP buckets remain enforced for the drain window.
- No code path bypasses auth based on shutdown state. Verified by reading the entire request pipeline.

### Rate-limit reset attack — NO

- Cleanup goroutine exit (Story 12.14 AC-3) only ends the periodic stale-entry sweep. The `IPRateLimiter.limiters` sync.Map is NOT explicitly cleared. Existing per-IP token buckets keep enforcing limits until the process exits.
- New process startup yields a fresh sync.Map — but that's the expected per-process semantics, NOT a 12.14 regression. The pattern documented in MEMORY ("XFF rate-limit spoofing on directly-exposed services") was remediated in 12.11 via `NEBU_TRUSTED_PROXY` gate; 12.14 does not regress it.
- An attacker forcing repeated container restarts to reset buckets would need control over the orchestrator (out of scope for media gateway threat model).

### DB leak / connection cleanup — NO

- `pool.Close()` runs **after** `srv.Shutdown`. Order is correct (Story 12.14 AC-4 comment in main.go:336).
- If drain times out (30s), `srv.Shutdown` returns `context.DeadlineExceeded`, the warning is logged, and `pool.Close()` still runs. pgxpool gracefully closes idle conns and cancels in-flight queries; postgres rolls back uncommitted transactions on connection close. No leak.
- The `os.Exit(1)` path on fatal serverErr (line 308) skips `pool.Close()`. Minor resource hygiene (L-1) — OS reclaims sockets/FDs on process exit; postgres detects abrupt disconnect and rolls back idle txns. Not a security issue.

### Signal-handler correctness — VERIFIED

- `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` is the canonical Go 1.16+ pattern.
- `defer stop()` is correctly placed at main.go:197. When main returns, signal registration is removed; subsequent signals follow Go's default handler (terminate). This is benign — operator double-SIGTERM forces faster exit.
- SIGKILL cannot be caught (kernel-level) — no handler needed.
- No SIGHUP/SIGUSR1 reload path exists, so no race between signal-driven config reload and shutdown.

### Goroutine / channel hygiene — VERIFIED

- Three goroutines created in main:
  1. HTTP server goroutine (line 296-302) — exits when `srv.Shutdown` closes the listener; writes to OR closes buffered `serverErr` channel (buffer 1 prevents send-block).
  2. `uploadRL.cleanupLoop` — exits cleanly on `ctx.Done()` (Story 12.14 AC-3).
  3. `downloadRL.cleanupLoop` — same.
- `serverErr` channel: buffered, written-once-then-closed. Even if both `serverErr <- err` and `close(serverErr)` execute, the buffer absorbs the send. No deadlock, no double-send (only one write site).
- TOCTOU between `serverErr` and `ctx.Done()`: select chooses one branch nondeterministically. Both lead to safe termination — serverErr branch exits with code 1 (no drain — acceptable, server is already broken), ctx.Done branch performs full drain.

### Shutdown-phase attack-surface scan — NO NEW ROUTES

- No new HTTP endpoint introduced by 12.14.
- No new env var introduced by 12.14 (shutdown timeout is hard-coded — L-3 advisory).
- No new file I/O introduced by 12.14.
- No new network listener (the `listenAddr` is the same; only its lifecycle changed).

### Cross-story regression scan (epic 12 wide)

I re-confirmed:
- MinIO secrets still loaded via `_FILE` form first (12.7, LOW-9 reversal) — preserved.
- OIDC fail-closed (12.8) — `oidcIssuer == ""` still aborts startup; `initOIDCVerifier` still exits on retry exhaustion.
- Canonical `@localpart:server` user ID format (12.9) — preserved.
- `NEBU_TRUSTED_PROXY` XFF gate (12.11) — preserved; 12.14 did not regress the `trustedProxy` field on `IPRateLimiter`.
- Per-attempt OIDC timeout + ctx-aware retry sleep (12.12 F-3, 12.13) — preserved.
- `NEBU_SERVER_NAME` mandatory (12.9 AC-3) — preserved at main.go:114.
- Slowloris-resistant `http.Server` timeouts (12.7 LOW-10) — preserved on the new `srv` struct (line 282-288).
- No SQL migrations in 12.14 → no RLS / migration regression.
- No `// MVP` / `// placeholder` / `// TODO` auth shortcuts introduced.

## Cross-Story Patterns

No new patterns this pass. 12.14 cleanly resolves the gap noted in MEMORY ("Story-title oversell on partial graceful-shutdown" — `srv.Shutdown` now IS wired). That pattern can be considered closed for epic 12.

## Accepted Risks

None new this pass.

## Follow-up Stories Required

**None.** Epic 12 SEC Gate 2 is **complete and clean**.

L-1, L-2, L-3 are operator/hygiene advisories with no exploit path; they do not require follow-up stories. If desired, they can be tracked as backlog tasks for a future operational-polish pass:

- L-1: emit `pool.Close()` also on serverErr fatal path (5 minute fix).
- L-3: make shutdown timeout configurable via `NEBU_MEDIA_SHUTDOWN_TIMEOUT` (5 minute fix; defaults remain 30s).
- L-2: not actionable — single-process semantics make it moot.

## Summary

CRITICAL: 0 | HIGH: 0 | MEDIUM: 0 | LOW: 3 (carry-advisory, no follow-up required)
Follow-up stories required: **0**
Accepted risks: 0

**Epic security gate: PASS / CLEAN**

Epic 12 is complete for security purposes. The full lifecycle of the media gateway — startup, request handling, shutdown — has been audited end-to-end across 14 stories. No CRITICAL or HIGH vulnerabilities remain. The shutdown path introduced by 12.14 does not open any new attack surface: requests in-flight at SIGTERM complete their full auth/rate-limit middleware stack on uncancelled request contexts; new connections are refused by the closed listener; DB pool is closed after drain; goroutines exit cleanly.

Classification: **CLEAN**

Report path: `_bmad-output/implementation-artifacts/epic-12-security-review-final5-2026-05-13.md`
