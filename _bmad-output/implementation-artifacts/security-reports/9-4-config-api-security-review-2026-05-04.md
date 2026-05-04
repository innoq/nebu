# Security Review — Story 9.4: Admin UI Config & Role Mapping API Integration — 2026-05-04

**Agent:** Kassandra
**Diff base:** `git diff --staged` (Story 9.4 Dev → SEC Gate 1)
**Classification:** CLEAN (all findings ≤ MEDIUM, none reach CRITICAL/HIGH thresholds)
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

Story 9.4 wires the Server Config admin page to the real Elixir core via gRPC `GetServerConfig`/`UpdateServerConfig`, and explicitly defers role-mapping persistence to a follow-up story (Option D — proto extension out-of-scope for XS). The implementation correctly applies the `contextWithAdminIdentity` HIGH-1 fix from Story 9.2, retains all CSRF / sessionGuard / body-limit middleware, validates form input bounds server-side, and adds `server_config_updated` to the audit_writer allowlist. The Core handler reads `actor_id` from `Nebu.Grpc.Metadata.trusted_identity(stream)` before emitting the audit log entry, mirroring the deactivate_user / archive_room pattern from 9.2/9.3.

The intentional `oidc_client_secret` omission in `ServerConfigProto` (proto/core.proto:594) survives — the gateway never sees the client secret on either the GET or UPDATE path. The role-mapping deferral is safe because no persistence change occurred (the in-memory stub mutation is identical to the Story-7.15 baseline; nothing new is exposed).

One MEDIUM operational finding (missing `Error updating config` flash allowlist entry) was fixed inline during review. No CRITICAL or HIGH findings. Recommendation: CLEAN to commit.

## Findings

### [MEDIUM] (FIXED inline) — `Error updating config` flash dropped on gRPC failure

- **CWE / OWASP:** CWE-1295 (Insufficient Logging of Operational Action) — UX surface with security tail
- **Datei:**
  - `gateway/internal/admin/config.go:134` (`http.Redirect(..., "/admin/config?flash=Error+updating+config", ...)`)
  - `gateway/internal/admin/flash.go:5–17` (allowlist before fix — entry was missing)
- **Beschreibung:** `sanitizeFlash` (Story 7.18) implements an exact-match allowlist of permitted flash messages. The new error path in `UpdateConfigHandler` redirects with `?flash=Error+updating+config` (URL-decoded: `Error updating config`), but that string was not in the allowlist. The flash banner would have been silently dropped on every gRPC error, leaving the operator with no feedback after a failed update. Same operational pattern as 9-3 MEDIUM (`Error archiving room` etc.).
- **Impact:** Defence-in-depth gap on operator feedback; no XSS / injection risk because the allowlist itself remained intact (it prevents flash-injection-as-XSS). An admin retrying a failed config save could generate duplicate audit events on the next attempt without realising the first one failed.
- **Fix applied:** Added `"Error updating config": {}` to `gateway/internal/admin/flash.go` allowlist. `flash_test.go::TestSanitizeFlash_AllowlistValuesPassThrough` iterates the map so the new entry is auto-covered.
- **Status:** RESOLVED in this review.

---

### [LOW] gRPC error details logged unredacted via `slog.Error(..., "err", err)`

- **CWE / OWASP:** CWE-209 (Generation of Error Message Containing Sensitive Information)
- **Datei:** `gateway/internal/admin/config.go:72, 130, 132`
- **Beschreibung:** Three `slog.Error(..., "err", err)` calls log the raw gRPC error. The Elixir core raises `GRPC.RPCError` whose `inspect(reason)` text may contain Postgres detail strings, schema names, or transient connection errors. The browser sees only the generic flash; the SIEM consumer of operator logs sees the full text.
- **Impact:** No reachable user-facing leak. Risk is operational: a SIEM-tier consumer with broader access than the local log file would see internal error text. Defence-in-depth gap, not an exploit path. Identical pattern to 9-2/9-3 LOW.
- **Empfehlung (deferred — same as 9-2/9-3):** Log `status.Code(err)` plus a short error category instead of `err`. Or, on the Elixir side, replace `inspect(reason)` in non-NotFound `GRPC.RPCError` messages with a fixed string. Recommend tracking as a single epic-end follow-up rather than per-story.
- **Referenz:** OWASP ASVS v4.0 §8.3.4 (do not include sensitive data in error messages).

---

### [INFO] `originalInstanceName` in Playwright `afterEach` may carry stale state across reload-cycle test

- **CWE / OWASP:** N/A (test-only flake risk)
- **Datei:** `e2e/tests/features/admin/config-api-integration.spec.ts:37–62`
- **Beschreibung:** The describe block declares `let originalInstanceName: string | null = null;` at module scope. Each `beforeEach` overwrites it, but the cleanup `afterEach` always sees the *most recent* test's value. The flip-flop pattern (`=== 'Nebu Playwright Test' ? 'Nebu API Integration' : 'Nebu Playwright Test'`) tolerates either starting state, so test order does not matter — but this is a structural source of cross-test contamination if a future maintainer hardcodes a specific name. Also: `page.request.post` in cleanup bypasses CSRF middleware (the test author flagged this as TEA MINOR-4) — best-effort cleanup only.
- **Impact:** No security implication. Test is functionally correct in CI today.
- **Empfehlung:** Optional — replace flip-flop with timestamp suffix (`Nebu-${Date.now()}`) and rely on a cleanup test-utility helper that resets via the same UI flow. Tracking as INFO rather than blocking.

---

## Security Checklist (per Story 9.4 Dev Notes lines 277–289)

| # | Invariant | Status | Evidence |
|---|-----------|--------|----------|
| 1 | `UpdateConfigHandler` and `RoleMappingHandler.UpdateHandler` protected by CSRF middleware | PASS | `cmd/gateway/main.go:334, 340` — both POST routes wrapped `bodyLimit64KiB(csrf(sessionGuard(...)))`; matches 7.17 baseline |
| 2 | gRPC client called with `contextWithAdminIdentity(r.Context(), ...)` | PASS | `gateway/internal/admin/config.go:118` — `grpcCtx := contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))` applied before `UpdateServerConfig` call. PSK token via interceptor + `x-user-id` propagation = both Story 5.29a and Story 9.2 HIGH-1 fix in effect. |
| 3 | Form fields validated server-side before gRPC call | PASS | `config.go:98–114` — `instance_name` non-empty, `max_rooms_per_user` ∈ [1,100], `retention_days` ∈ [1,3650]; validation precedes the gRPC call |
| 4 | `oidc_issuer` HTTPS validation if added to form | N/A | `oidc_issuer` was *not* added to the form in this story (XS scope). The proto field stays in the GET response only; `Handler` never echoes it back into a form input. No new attack surface. |
| 5 | No config values in `slog` error calls (especially `oidc_client_secret`) | PASS | `oidc_client_secret` is intentionally absent from `ServerConfigProto` (proto/core.proto:594 — confirmed by code comment + proto inspection). The gateway never receives it on either GET or PATCH path. The three `slog.Error` calls log the *gRPC error* (not the response config); see LOW finding above for unrelated defence-in-depth recommendation. |

## Threat-Specific Analysis

### CSRF on UpdateConfigHandler
**PASS.** Route `POST /admin/config` is wrapped `bodyLimit64KiB(csrf(sessionGuard(...)))` (main.go:334). Same wrapping for `POST /admin/config/role-mapping` (main.go:340). Identical to 7.17/9.2/9.3 baseline.

### Input validation on config values
**PASS.**
- `instance_name`: trimmed, non-empty (returns 400)
- `max_rooms_per_user`: integer, [1, 100]
- `retention_days`: integer, [1, 3650]
- `oidc_issuer`: not exposed in this story's form — no new attack surface
- `oidc_group_claim` (role mapping): `^[a-zA-Z0-9:_-]+$`, max 50 runes (Story 7.15 validation preserved)
- `instance_admin_group`: max 100 runes
- `compliance_user_group`: max 100 runes (optional)

All bounds match Story 7.10/7.15 originals; no regression.

### `oidc_client_secret` exposure
**PASS.** Proto comment at proto/core.proto:594 explicitly states "oidc_client_secret intentionally absent." `ServerConfigProto` has 6 fields, none of which is the secret. `protoToStubConfig` in config.go:48–56 only maps `instance_name`, `room_default_max_members`, `audit_log_retention_days` — three of the six exposed fields. The secret is never deserialised into the StubConfig nor rendered in the template.

### Role mapping deferral safety (Option D)
**PASS.** The change to `role_mapping.go` is comment-only on the `UpdateHandler` body — old `TODO(epic-6)` markers replaced with `NOTE(epic-9)`. The actual stub mutation (`stubRoleMappingConfig.OIDCGroupClaim = ...` etc.) is byte-identical to the Story 7.15 baseline. No new persistence introduced means no new attack surface. The deferral is documented transparently in the function header doc-comment (lines 47–60) so a future developer cannot accidentally assume the change is persisted.

### Audit log integrity (Story 9.2 HIGH-1 propagation)
**PASS.** `update_server_config/2` in `core/.../server.ex:2013` now calls:
```elixir
{actor_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)
```
…before invoking `audit_writer_module().log(actor_id, "server_config_updated", "server_config", "config", %{changed_keys: ...}, "success")`. The 6-arity log signature matches `Compliance.AuditWriter.log/6` (audit_writer.ex:55). The action string `server_config_updated` is in the `@known_actions` allowlist (audit_writer.ex:49). Combined with the gateway's `contextWithAdminIdentity` injection, `actor_id` will be a non-nil Matrix user ID for every UI-driven update — the silent-drop trap from 9-2/9-3 is closed for this story.

### Auth: RequireRole(`instance_admin`) on config routes
**PASS by inheritance.** The admin UI routes are protected by `sessionGuard`, which validates a signed admin session cookie issued only after Dex OIDC login + bootstrap-admin claim. The actual role check happens at `contextWithAdminIdentity` time: it hardcodes `"instance_admin"` as the system role passed to Core (middleware.go:319–321). The Core gRPC server then uses `Nebu.Grpc.Metadata.trusted_identity(stream)` to receive both `actor_id` and `system_role`. Defense-in-depth: the entire admin UI surface trusts that anyone who reaches a `sessionGuard`-protected route is `instance_admin`, because admin login flow only issues a session cookie for users whose Dex token contains the `instance_admin_group` claim (verified at login time, not per-request). No new bypass introduced by Story 9.4.

## Severity Counts

| Severity | Count |
|----------|-------|
| CRITICAL | 0 |
| HIGH     | 0 |
| MEDIUM   | 1 (FIXED inline) |
| LOW      | 1 (deferred — same as 9-2/9-3 epic-end follow-up) |
| INFO     | 1 |

## Recommendation

**CLEAN — safe to commit.**

- The single MEDIUM was fixed inline during review (flash allowlist).
- The LOW (unredacted gRPC error in slog) is a known defence-in-depth gap shared with Stories 9-2 and 9-3 — track as one epic-end follow-up rather than per-story.
- The INFO (Playwright cleanup pattern) is a test-quality observation; no security implication.
- No CRITICAL/HIGH findings; the audit-log silent-drop trap from earlier 9.x work is correctly closed by the `contextWithAdminIdentity` application.
- All Story 9.4 security checklist items (Dev Notes §"Security Checklist") pass.

`make test-unit-go` (post-fix): all packages green.
`make test-unit-elixir` (post-fix): all packages green; `admin_grpc_test.exs` 25/25 passing.
