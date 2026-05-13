# Epic 12 Security Review — Final SEC Gate 2 (after Story 12.13) — 2026-05-13

## Scope

- **Epic:** 12 — Media Gateway (MinIO backend, thumbnails, download, rate limits, OIDC, audit trail, graceful shutdown)
- **Base:** `5020ec8` (epic-12 base)
- **HEAD:** `e5da4ae` (branch `feature/epic-12-media`)
- **Story under definitive focus:** 12.13 — Media Gateway Graceful Shutdown: Signal-Aware OIDC Retry Loop
- **Prior epic-end reviews:**
  - `epic-12-security-review-final-2026-05-13.md` — BLOCKED (HIGH F-2 XFF spoof, MEDIUM F-1 canonical claim)
  - `epic-12-security-review-final2-2026-05-13.md` — **PASS** (F-1 + F-2 remediated; 3 LOW carried)
  - `epic-12-security-review-final3-2026-05-13.md` — **PASS** (F-3 + F-4 + F-5 all remediated, no regressions)
  - **This review (final4)** — verifies Story 12.13 introduces no new findings and addresses the two informational residual gaps noted in final3 (Background context in production, unconditional `time.Sleep`) cleanly.

This review focuses on the delta `f3344b6..e5da4ae` (Story 12.13 only). The wider epic surface was already cleared CLEAN in final3.

## Diff Surface (Story 12.13)

| File | Change | Lines |
|------|--------|-------|
| `media/cmd/media/main.go` | `signal.NotifyContext(SIGINT, SIGTERM)` + ctx-aware select replacing `time.Sleep` + log-message branch for ctx-cancel vs retry-exhausted | +26 −4 |
| `media/cmd/media/main_test.go` | 3 new tests (`AT-12-13-1`, `AT-12-13-3`, `AT-12-13-4`) covering sleep-interrupt and no-signal exhaustion paths | +147 |
| `docs/architecture/08-concepts.md` | Documentation note | +2 |
| Story file + sprint-status | Bookkeeping | +191 / +1 |

Production code touched is limited to ~15 lines of `main.go`. No other production file changed.

## Security Verification

### Signal handler registration (`main.go:196-197`)

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

- Canonical Go pattern. `signal.NotifyContext` returns a context that is cancelled on the first delivered signal; `stop()` deregisters the OS handler.
- `defer stop()` runs on normal `main()` return (server error path). The `os.Exit(1)` paths bypass defer, but `os.Exit` terminates the whole process — no goroutine leak, no leaked signal handler can survive.
- Captured signals: `SIGINT` (Ctrl-C from Docker `--init` and interactive use) and `SIGTERM` (`docker stop`). Both standard for containerised services.
- No `SIGKILL` registered — correct; SIGKILL is uncatchable by design.
- No `SIGHUP` — acceptable; the service does not implement config-reload semantics.

**Verdict:** Signal registration is correct. No double-signal hazard, no goroutine leak from the notifier.

### Context propagation into OIDC retry loop (`main.go:222`, `main.go:418-447`)

The signal-aware `ctx` is now threaded into `initOIDCVerifier(ctx, ...)`, which in turn passes it to `initOIDCVerifierWith`. Two cancellation observation points:

1. **Top of every retry iteration** (`main.go:418-420`) — `if ctx.Err() != nil { return nil, attempt-1, ctx.Err() }`. Pre-existing from 12.12.
2. **Inter-retry sleep** (`main.go:443-447`) — `select { case <-time.After(retryDelay): case <-ctx.Done(): return …, ctx.Err() }`. **New in 12.13.**

The select is correct. Both cases are independent; the `time.After` timer is GC-eligible after the select wakes (Go runtime cleanup of `time.After`-allocated timers in select is fine for short delays of 2s).

**Verdict:** Cooperative cancellation now fully covers both the network call (per-attempt timeout from 12.12) and the inter-retry sleep (12.13). Combined upper bound on SIGTERM-to-exit during startup: O(per-attempt-timeout) = ≤ 10 s, well inside the 10 s Docker grace window.

### Distinguished log message (`main.go:370-377`)

```go
if ctx.Err() != nil {
    slog.Info("media: OIDC init aborted by signal — shutting down", ...)
} else {
    slog.Error("FATAL: media: OIDC provider unreachable after retries", ...)
}
```

- No sensitive data logged (`issuer` is operator-configured config, `reason` is the context error which is `context.Canceled` or `context.DeadlineExceeded` — both literal strings).
- No log-injection surface — `issuer` is not user-controllable at runtime; `ctx.Err()` returns a fixed sentinel error type.
- Severity choice (Info for signal abort, Error for unreachable) is operationally correct, not security-relevant.

**Verdict:** Logging is safe. No PII, no credentials, no injection.

### Context propagation to `http.Server` — examined and assessed

**Observation:** `srv.ListenAndServe()` is called on the same line as before (line 287). `ctx` is **not** wired into `srv.Shutdown(ctx)`. The story's title mentions "Graceful Shutdown" but the HTTP server is not gracefully drained on SIGTERM after OIDC initialisation has succeeded.

**Adversarial walk-through:**

- **Threat model for in-flight cancellation:** Could a SIGTERM during an in-flight request cause data corruption or partial state?
  - Upload path: `PutObject` to MinIO is atomic (S3 PUT); `INSERT INTO media_files` is transactional. Connection RST mid-upload leaves no orphan row (DB transaction did not commit) and no orphan object (MinIO discards incomplete uploads). No integrity issue.
  - Download/thumbnail path: read-only. No state to corrupt.
  - DB pool: `pgxpool` `Close()` is deferred and runs when `main()` returns. Since the server keeps running and `main()` does not return on SIGTERM (no observer), `pool.Close()` is not invoked — but `os.Exit` is never reached either; the process is killed by Docker SIGKILL at the 10 s grace boundary. Pool connections are closed by kernel TCP reset, server-side cleanup happens via `idle_in_transaction_session_timeout`. Operationally noisy, not a security issue.
- **Threat model for ignored signal:** Before 12.13, Go's default SIGTERM handler terminated the process immediately. After 12.13, the signal is intercepted and `ctx` is cancelled but no observer in the serving phase reacts — process hangs until Docker's 10 s SIGKILL. This is a **deployment-ops regression**, not a security finding. It does NOT widen the attack surface, does NOT bypass auth, does NOT leak data.

**Classification:** Functional/operational gap, out of scope for SEC Gate 2. Per Kassandra's CREED #2 ("Evidence-Based Findings… not 'this looks risky'") and the scope statement in `references/security-review.md` (SQL injection, XSS, CSRF, auth bypass, timing, crypto, headers, traversal, JWT), this does not match any in-scope finding category.

**Recommendation (non-blocking):** A follow-up cleanup story should wire `srv.Shutdown(ctx)` with a separate `signal.NotifyContext` listener goroutine and a `shutdownTimeout` (e.g. 8 s, leaving 2 s buffer inside Docker's 10 s grace). This is an operational/UX improvement, not a security control. It can be filed as a tech-debt story; it does not block Epic 12.

### Double-signal hazard

After the first SIGTERM, `signal.NotifyContext`'s installed handler remains registered until `stop()` is called. Subsequent signals are absorbed by the same handler (re-trigger the already-completed cancel — no effect). The Go runtime's default disposition is suppressed while the handler is in place. So:

- **First SIGTERM during OIDC retry:** ctx cancelled → select fires `ctx.Done()` → returns error → `os.Exit(1)`. Fast exit. Correct.
- **Second SIGTERM during OIDC retry:** Already on the `os.Exit(1)` path — irrelevant.
- **First SIGTERM during serving phase:** ctx cancelled, no observer, process hangs.
- **Second SIGTERM during serving phase:** Same — no escalation to default action while handler is registered. Process still hangs until Docker SIGKILL.

The "user can hit Ctrl-C twice to force-exit" pattern is **not** preserved (because `stop()` is deferred and only runs when `main()` returns, which it doesn't during ListenAndServe). This is the same functional-not-security issue as above. No security implication.

### No new attack surface introduced

- No new HTTP routes.
- No new authentication paths.
- No new request body parsing.
- No new SQL.
- No new file/path manipulation.
- No new crypto operations.
- No new environment variables consumed.
- No new external network calls.

The diff is purely a control-flow refinement around the existing OIDC startup retry. The only externally visible behavioural change is the log message during signal-driven aborts.

### Test code review

`main_test.go` adds three tests using a `mockProvider` injection (existing pattern from 12.12). The tests:

- Run with `t.Parallel()` — appropriate.
- Use `context.WithCancel` to simulate SIGTERM — correct.
- Assert on elapsed time (< 200 ms) — flaky risk acknowledged in `AT-12-13-1` comment with a 200 ms margin against a 500 ms baseline; comfortable headroom.
- No credentials or secrets in test data.
- No insecure test fixtures.

**Verdict:** Tests are sound. No security concern.

## New Issues Introduced by Story 12.13

| # | Severity | Description |
|---|----------|-------------|
| — | — | None. |

## Findings

| # | Severity | Story | Location | Vulnerability | Status |
|---|----------|-------|----------|---------------|--------|
| — | — | — | — | No findings. | Closed |

## Cross-Story Patterns

No new cross-story patterns. Story 12.13 finally closes the "informational note" from final3 about the `time.Sleep` not being ctx-aware — confirming the pattern observed there: when an epic-end review documents non-blocking advisories as informational notes, a single follow-up story addressing them is a clean way to close the loop without scope creep.

One observation worth noting (informational, not a recurring pattern yet):

| Observation | Description |
|-------------|-------------|
| Partial graceful-shutdown wiring | 12.13 wires signal-awareness into the OIDC startup phase but does not extend it to `http.Server.Shutdown`. The story name ("Graceful Shutdown") slightly oversells the change. Not a security issue, but if a future story claims to complete the graceful-shutdown surface, reviewers should verify it actually wires `srv.Shutdown(ctx)` and a `shutdownTimeout` — and adds a regression test that issues a request, sends SIGTERM, and asserts the request completes before exit. |

## Accepted Risks

None new. The two informational notes from final3 (Background context, unconditional `time.Sleep`) are now actively remediated — 12.13 wires the signal-aware context AND the ctx-aware sleep, so the OIDC startup phase is now fully cooperative on SIGTERM.

The remaining observation about `srv.Shutdown` not being wired is **not** a security accepted risk; it is a functional follow-up item with no security implication.

## Follow-up Stories Required

**Security follow-ups:** None.

**Optional operational follow-up (out of SEC Gate 2 scope, no security impact):**

- Wire `srv.Shutdown(ctx)` with a dedicated shutdown goroutine listening on a separate signal-aware context, plus a `shutdownTimeout` (e.g., 8 s) inside Docker's 10 s grace window. Add a regression test for in-flight request drain. This is a tech-debt / DX improvement, not a security control. It can be filed under Epic 13+ or carried as a pure ops cleanup.

## Summary

CRITICAL: 0 | HIGH: 0 | MEDIUM: 0 | LOW: 0
Follow-up stories required: 0
Accepted risks: 0

**Classification:** CLEAN

**Epic security gate:** **PASS — Epic 12 is cleared for `done`.**

Story 12.13's signal-handling additions are correct, minimal, and introduce no security regressions. The signal context is properly registered and cleaned up (no goroutine leak), the ctx-aware select correctly interrupts inter-retry sleep without double-fire hazards, and the log differentiation between signal-cancel and retry-exhaustion adds operator clarity without leaking sensitive data. The only residual observation — that `http.Server.Shutdown` is not wired — is a functional/operational gap that does not match any security finding category (no auth bypass, no data exposure, no widened attack surface) and therefore does not block the epic.

Epic 12 may be marked `done`.
