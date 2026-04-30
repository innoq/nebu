# Security Review: Stories 7.16a / 7.16b / 7.16c — Bugfix Triple (SEC Gate 1)

**Date:** 2026-04-30
**Reviewer:** Kassandra (Security Agent)
**Stories under review:**
- 7.16a — Bootstrap test fixture TRUNCATE permission fix (`security_review: not-needed` — courtesy review)
- 7.16b — Compliance rate limiter (`complianceRL`) extraction + revoke route move (`security_review: required`)
- 7.16c — Audit-log integration tests rewrite (`security_review: required`)

**Diff base:** `HEAD` (working-tree changes), branch `feature/github-readiness`
**Changed files in scope:**
- `gateway/cmd/gateway/main.go`
- `gateway/test/integration/audit_integration_test.go`
- `gateway/test/integration/main_test.go`
- `gateway/test/integration/grpc_auth_test.go`
- `gateway/test/integration/admin_bootstrap_steps_test.go`

**Classification:** **CLEAN**
**Severity counts:** CRITICAL: 0 · HIGH: 0 · MAJOR: 0 · MINOR: 1 · LOW: 2 · INFO: 3
**Blocking severity threshold:** CRITICAL/HIGH (per project policy — SEC Gate 1)
**Decision:** **APPROVED — no blocking issues, no follow-up story required**

---

## Executive Summary

Three small bugfix stories landed together. None of them ship new attack surface to production: 7.16a and 7.16c are test-only changes; 7.16b adds a separate `complianceRL` rate-limit instance with the **same** rate/burst as the previous `strictRL`-based wrapping (10 req/min, burst=10) and additionally **tightens** the compliance-session revoke endpoint by moving it from `adminRL` (burst=20) to `complianceRL` (burst=10) — a strict reduction in attacker budget. The forged-cookie / DB-seeded-session test pattern in 7.16c is gated by `NEBU_TEST_INTERNAL_SECRET` (the same PSK used for gRPC node auth) and reuses the existing `signTestCookie` mirror that has been in the test tree since Story 5.13/5.21 — it does not introduce a production backdoor. The new direct gRPC `WriteAuditLog` call from the integration test uses the production `x-nebu-node-token` PSK header path; production `Nebu.Grpc.AuthInterceptor` rejects unauthenticated calls, so this is exactly the same surface an authorised gateway has.

Findings are limited to one MINOR (action-string allowlist on the audit RPC), two LOW (ad-hoc DB connections in tests bypassing pooling, action-text not validated for log-injection at the Elixir side) and three INFO (cosmetic / future-hardening).

The original SEC Gate 1 questions:

1. **Does `complianceRL` create any rate-limiting bypass for compliance/admin operations?** — **No.** Rate/burst identical to the prior `strictRL` wrap (10/min, 10 burst); revoke endpoint goes from `adminRL` (60/min, burst 20) to `complianceRL` (10/min, burst 10), which is a strict tightening. The new instance gives compliance and login independent LRU buckets, so a legitimate compliance burst can no longer drain the login bucket — and vice-versa, a brute-force login storm can no longer exhaust the compliance bucket and force-shed valid GDPR/export traffic. This is a defensible isolation, not a relaxation. (See [INFO-1] for one observability note.)

2. **Does the forged-cookie approach in integration tests expose any test-only backdoor that could be misused in production?** — **No.** The forged cookie is signed with `internalSecret` read from `NEBU_TEST_INTERNAL_SECRET` — the **same** PSK used in production for gateway → core gRPC auth. There is no production code path that loosens cookie verification for tests. The `signTestCookie` helper is **a mirror** of `AdminAuth.signCookie`, not a privileged variant. `signTestCookie` is in `_test.go` files only and is not compiled into the gateway binary (Go build excludes `_test.go` from non-test builds). To forge a cookie an attacker must already own the gateway's HMAC secret — at which point they own the whole gateway. This invariant is identical to the pre-existing test pattern that landed with Story 5.13.

3. **Is the `coreGRPCAddr` gRPC connection in tests using proper auth (PSK header)?** — **Yes** in 7.16c (the `dialCoreGRPCForAudit` helper sets `x-nebu-node-token: <internalSecret>` via UnaryInterceptor on every outgoing RPC). **No** in 7.16b's unrelated `dialUnauthenticatedCore` (intentional — that helper is the negative test-fixture used by `grpc_auth_test.go` to verify the interceptor rejects unauthenticated calls; the 7.16b change there is a pure refactor of the address resolution, not the auth strategy). See [LOW-2].

4. **Does the CSRF dance in the logout test correctly verify double-submit-cookie pattern?** — **Yes.** The test (1) fetches `/admin/dashboard` with the forged session cookie to obtain the server-issued `csrf_token` cookie, (2) replays both the `csrf_token` cookie and a matching `_csrf` form-field on `POST /admin/logout`. This exercises the full path through `CSRFMiddleware` (which uses `subtle.ConstantTimeCompare`) and `sessionGuard` (which queries the seeded `admin_sessions` row by SID). It does NOT shortcut by injecting an in-process token — exactly the production behaviour. See [INFO-2] for one observation.

5. **Are there any information leakage or timing attack risks in the new test helpers?** — **No new risks.** The tests run inside the project's docker-compose network and the helpers print no secret material. `forgeAdminSIDCookie` writes `internalSecret` (TrimSpace) into an HMAC and discards the cleartext. Timing-attack surface is unchanged: `subtle.ConstantTimeCompare` (CSRF) and `secure_compare` (Elixir PSK) are still in place — the test code paths do not introduce a non-constant-time string comparison.

---

## Detailed Findings

### [MINOR-1] WriteAuditLog accepts arbitrary action strings — log-injection / pollution risk

- **Severity:** MINOR
- **Mapping:** CWE-117 (Improper Output Neutralization for Logs), STRIDE-T (Tampering with audit data)
- **Status:** Pre-existing — exposed (not introduced) by 7.16c which is now testing the surface end-to-end
- **Where:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:168-193` (`write_audit_log/2`)
- **What:** `request.action`, `request.target_type`, `request.target_id`, `request.outcome`, `request.error_detail` and `request.metadata_json` are passed straight through `audit_writer_module().log/7` without an allowlist or length cap. A gateway with a stolen or leaked `internalSecret` can flood `audit_log` with arbitrary action strings (e.g. `"admin_login"` to forge a fake successful login, or oversized `error_detail` to bloat the table). The append-only RLS policy stops anyone from **deleting** the forged row, but it does not stop **writes** of bogus rows.
- **Why we are not blocking on this:** SEC Gate 1 scope is the diff under review. The 7.16c diff itself does not introduce this — it only **exercises** an existing RPC. The right home for the fix is a follow-up hardening story or Epic-end SEC Gate 2.
- **Recommendation (follow-up story candidate):**
  - Allowlist `action ∈ {admin_login, admin_logout, admin_login_failed, bootstrap_completed, room_joined, …}` either in `Server.write_audit_log` or in a `Compliance.AuditWriter` validator.
  - Cap `error_detail` and `metadata_json` length (≤4 KiB) before insertion to prevent a slow-burn DoS via row-bloat.
  - Reject `actor_user_id` containing newline / control characters (CWE-117) so log-aggregator pipelines (Loki/ELK) cannot be confused by forged line breaks.

### [LOW-1] Test helpers open ad-hoc `*sql.DB` per call — connection-pool exhaustion in long Godog runs

- **Severity:** LOW
- **Mapping:** Defensive (no direct CWE) — operational hygiene
- **Where:** `audit_integration_test.go:46`, `:124`; `admin_bootstrap_steps_test.go:90`
- **What:** Each call to `seedAdminSessionForAudit`, `countAuditLogRows`, `theServerHasNoBootstrapCompleted` opens and closes a fresh `sql.DB` (which is itself a connection pool). Inside Godog, `theServerHasNoBootstrapCompleted` fires for every scenario; with parallel `t.Run(... t.Parallel())` and the `audit_logout` test polling in a 50 ms loop for up to 3 s (60 iterations), the postgres connection accept-rate can briefly spike. Not a production concern (test-only file with `//go:build integration`), but it could mask flakiness as auth/race issues.
- **Recommendation:** Cache the two `*sql.DB` handles (one per role) in `TestMain` and reuse — see existing `openTestDB` pattern. Optional, non-blocking.

### [LOW-2] `dialUnauthenticatedCore` keeps insecure transport credentials — unrelated to 7.16b

- **Severity:** LOW (informational — by design)
- **Mapping:** CWE-319 (Cleartext Transmission)
- **Where:** `gateway/test/integration/grpc_auth_test.go:50` (after this diff)
- **What:** The diff only refactors `coreGRPCAddr()` from a function to a package-level `var` (initialised in `TestMain`). The `insecure.NewCredentials()` call is unchanged — it is **deliberately** unencrypted because the test exists to prove the Elixir `AuthInterceptor` rejects unauthenticated calls. Production gateway → core traffic also runs over plaintext gRPC inside a private docker-compose network plus PSK; per Story 5.29a-Block-B that posture is documented and accepted (mTLS deferred to post-MVP per ADR-008). The integration test stays correct.
- **Recommendation:** Track migration to mTLS in the existing ADR-008 Phase 2 backlog. No action for this story.

### [INFO-1] Prometheus tier label `compliance` should be added to dashboards / alerts

- **Where:** `gateway/cmd/gateway/main.go:211` introduces `"compliance"` as a fourth value of the `nebu_rate_limit_total{tier,…}` label.
- **Why it matters:** Existing Grafana dashboards / alert rules that aggregate by `tier="strict|admin|medium|loose"` now silently drop compliance traffic. Add the new label to any saturation-alert join.
- **Action:** Update SRE dashboards (operational, not blocking).

### [INFO-2] CSRF token rotated only on `/admin/callback` — logout test relies on dashboard GET

- **Where:** `audit_integration_test.go:153` (`getCSRFTokenWithSession(t, "/admin/dashboard", …)`)
- **What:** `CSRFMiddleware` issues a fresh token only when the cookie is missing or the path is `/admin/callback` (line 246). The test fetches the dashboard once with no `csrf_token` cookie → middleware mints one → test replays it on logout. Behaviour is correct, but if a future change makes CSRF token issuance lazier (e.g. only on `/admin/callback`), this test will break with a confusing `no csrf_token cookie in response` error rather than pointing at a CSRF regression. Recommend adding a `t.Logf` or assertion comment in the helper noting the dependency.

### [INFO-3] `forgeAdminSIDCookie` does NOT include `iat`/`exp` — test mirrors current production cookie payload exactly

- **Where:** `audit_integration_test.go:39-67`
- **What:** `auditSIDCookie{SID string}` mirrors `adminSessionSIDCookie` (auth.go:300-302) — there is no expiry inside the cookie itself; expiry lives in the `admin_sessions` DB row (`expires_at` column, seeded at `NOW() + 2h`). `sessionGuard` reads the row, so a bare-SID cookie cannot be replayed past server-side revocation. This is the **intended** Story 5.12 model (server-side session revocation). The test correctly does NOT bake an `exp` into the cookie. No change required; recording for traceability.

---

## Cross-cutting checks (Nebu invariants)

| Invariant | Verdict | Notes |
|---|---|---|
| Rate-limit tiers preserved (no class downgraded) | OK | Compliance: 10/min/burst-10 unchanged. Revoke: tightened (60/20 → 10/10). Login: unchanged. |
| CSRF double-submit on state-changing admin POST | OK | `revoke` keeps `csrf(sessionGuard(…))`; logout test exercises the real CSRF dance. |
| OIDC Auth Code + PKCE only (no ROPC) | OK | No OIDC code touched. |
| gRPC PSK enforced (`x-nebu-node-token`) | OK | New test helper sends production header; Elixir `AuthInterceptor` unchanged. |
| Audit-log immutability (FORCE RLS, no DELETE) | OK | No migration changes; only test rows added by privileged `nebu_migrate` for `admin_sessions` (separate table). |
| Cookie HMAC secret hygiene | OK | `internalSecret` only consumed via `signTestCookie` in `_test.go`; not compiled into prod. |
| Constant-time secret compare | OK | `subtle.ConstantTimeCompare` (CSRF) and Elixir `secure_compare` (PSK) unchanged. |
| TRUNCATE privilege change (5.29a regression) | OK | Test fixture now uses `nebu_migrate` (BYPASSRLS, table owner) — `nebu_app` correctly remains without TRUNCATE. No production role grant change. |
| Body-size limits | OK | `bodyLimit64KiB` retained on every state-changing compliance route. |
| JWT validation flaws | n/a | No JWT code touched. |
| SQL injection | OK | All test SQL is parametrised (`$1..$4`); table names are static literals. |
| XSS / SSRF / open redirect | n/a | No URL-handling or template change. |
| Secrets in logs | OK | Test helpers do not `slog`/`t.Logf` the PSK or signed cookie payload. |

---

## Conclusion

**Classification: CLEAN.** No blocking findings. SEC Gate 1 PASSED for all three stories.

The single MINOR is a pre-existing audit-log input-validation gap exposed (not introduced) by 7.16c testing the path end-to-end. It is a candidate follow-up story but does not gate this commit.

**Report path:** `_bmad-output/implementation-artifacts/security-reports/7-16a-b-c-security-review-2026-04-30.md`

---

**Reviewer signature:** Kassandra — automated SEC Gate 1
**Next gate:** SEC Gate 2 (Epic-7 closure security review) — covers full epic diff including these three stories.
