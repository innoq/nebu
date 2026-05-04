# Security Review — Story 9-1: Admin gRPC RPCs (Core User + Room Management) — 2026-05-04

**Agent:** Kassandra
**Diff base:** `git diff --staged` (16 files, +4764 / −79)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default)
**Story:** `_bmad-output/implementation-artifacts/9-1-admin-grpc-rpcs-core-user-room-management.md` (`security_review: required`)

---

## Executive Summary

Story 9-1 adds nine admin gRPC RPCs in the Elixir Core (`ListAdminUsers`, `GetAdminUser`, `DeactivateUser`, `ReactivateUser`, `UpdateUserRole`, `ListAdminRooms`, `GetAdminRoom`, `GetServerConfig`, `UpdateServerConfig`), replaces the `GetMetrics` stub with real counts, and changes the `ArchiveRoom` contract so Core now owns the atomic DB write. The bulk of changes is generated protobuf code, ExUnit fakes, and thin Go client wrappers; the new SQL surface is in `core/apps/event_dispatcher/lib/nebu/admin/db.ex` and the new handlers in `server.ex`.

The work passes the security checklist defined in the story:
- All SQL is parameterized with `$N` placeholders (no string interpolation).
- `oidc_client_secret` is filtered at both the DB layer (`WHERE key != 'oidc_client_secret'`) and the proto schema (no field exists in `ServerConfigProto`).
- Email is masked to `u***@domain` before leaving Core; the test suite refutes any plaintext-email pattern in the response.
- `ArchiveRoom` uses an explicit `SELECT ... FOR UPDATE` inside an Ecto transaction.
- `DeactivateUser` sequences DB commit before session destroy, matching the AC#3 invariant.
- `UpdateUserRole` validates role values against an allowlist and raises `invalid_argument` for unknown values.

No CRITICAL or HIGH findings. Three MEDIUM observations on defense-in-depth gaps (no Core-side `system_role` check on admin RPCs, swallowed `destroy_session` failure on deactivation, no audit-log emission inside Core for state-changing admin RPCs). One LOW (`String.to_integer/1` on DB-stored config) and two INFO entries (PSK interceptor coverage, attack-surface observation).

---

## Findings

### [MEDIUM-1] Core admin RPCs do not enforce `system_role == "instance_admin"` — defense-in-depth gap

- **CWE / OWASP:** CWE-862 (Missing Authorization) — defense-in-depth variant
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1716-2057` (every new admin handler)
- **Beschreibung:** None of the nine new admin handlers (`list_admin_users`, `get_admin_user`, `deactivate_user`, `reactivate_user`, `update_user_role`, `list_admin_rooms`, `get_admin_room`, `get_server_config`, `update_server_config`) call `Nebu.Grpc.Metadata.trusted_identity/1` to verify the caller carries `x-system-role: instance_admin`. The story's Dev Notes (line 418) acknowledge this explicitly: "the gRPC check is defense-in-depth" — but the implementation skips that defense layer entirely. The `Nebu.Grpc.AuthInterceptor` enforces only PSK presence; it does not bind to a system role.
- **Impact:** A bug in the Go gateway (e.g., a future route that forwards user-supplied metadata, an inverted `RequireRole` chain, or a misrouted internal handler) would be sufficient to call any admin RPC because no second check exists in Core. PSK alone proves "this is the gateway"; it does not prove "this caller had an admin OIDC session". Any single regression in gateway middleware becomes an immediate admin-RPC bypass.
- **Empfehlung:** Add a guard helper invoked at the top of every admin handler:
  ```elixir
  defp require_instance_admin!(stream) do
    {_, role} = Nebu.Grpc.Metadata.trusted_identity(stream)
    unless role == "instance_admin" do
      raise GRPC.RPCError,
        status: GRPC.Status.permission_denied(),
        message: "instance_admin role required"
    end
  end
  ```
  Call it as the first line of `list_admin_users/2` through `update_server_config/2`. The gateway is already populating `x-system-role` for other RPCs (see `server.ex:861, 1372, 1454, 1531`), so wiring is symmetric. This is a **MEDIUM** because the current threat model places gateway-and-core in one trust boundary (PSK), but the defense-in-depth value is non-trivial: it converts a single-component compromise (gateway) into a two-component compromise (gateway + correctly-forged metadata).
- **Referenz:** OWASP ASVS V4.1.5 ("server-side authorization decision is enforced at every required layer"), NIST SP 800-53 AC-3.

### [MEDIUM-2] `DeactivateUser` swallows `destroy_session` failure and reports success

- **CWE / OWASP:** CWE-755 (Improper Handling of Exceptional Conditions)
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1788-1810`
- **Beschreibung:** When the DB update succeeds but `destroy_session/1` returns `{:error, reason}`, the handler logs a warning and returns `%Core.DeactivateUserResponse{ok: true}`. The caller has no way to distinguish "fully deactivated" from "DB updated, sessions still alive in ETS". In practice, a subsequent `validate/4` call hits the DB (`session_manager/lib/nebu/session/token_validator/postgres.ex:18`) and rejects the user with `:deactivated`, so the runtime impact is bounded — but any code path that reads session state from ETS without re-validating would still see the user as live.
- **Impact:** A partial-failure scenario silently produces a `200 OK` from the gateway. The operator's audit log shows "deactivation succeeded" while ETS may still contain a live session pointer. Bounded by token-validation hitting DB on every check — but if a future change adds an ETS-only fast path (cache hit without DB confirmation), this becomes the seed of a real session-survival bug.
- **Empfehlung:** Either (a) propagate the destroy_session error as a gRPC `internal` error so the gateway can retry, or (b) record the partial failure in the response itself (e.g., `ok: true, session_invalidation_warning: true`). Option (a) is preferable because the gateway already retries gRPC errors; the operator never sees a silent partial state.
- **Referenz:** OWASP ASVS V7.4.1 (error handling reveals state correctly to caller).

### [MEDIUM-3] No audit-log emission for state-changing admin RPCs at the Core layer

- **CWE / OWASP:** CWE-778 (Insufficient Logging) — partial; mitigated by Gateway-side logging (Story 5.2)
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1788, 1818, 1863, 1893, 2043`
  - `deactivate_user/2`, `reactivate_user/2`, `update_user_role/2`, `archive_room/2`, `update_server_config/2`
- **Beschreibung:** None of the five state-changing admin handlers emit a `WriteAuditLog` entry. Story Task 3 explicitly mentions `Ecto.Multi` to keep "the DB update and audit log atomic" but the implementation does the DB update with bare `Ecto.Adapters.SQL.query!/3` and writes no audit record. Today the audit trail is preserved because the Go gateway's existing handlers (`gateway/internal/api/server.go:632-720`, `rooms_archive_handler_test.go:138`) write the audit record before/after calling the RPC — those pre-9.1 paths still run because Stories 9.2-9.4 have not yet cut over to the new RPCs. The moment Story 9.2 routes the Admin UI through these new RPCs, the audit emission would silently disappear unless the Gateway keeps writing the log itself.
- **Impact:** **Today: none** — the new RPCs have no production caller yet. **After Story 9.2 cutover:** if the Gateway is changed to delegate to these RPCs and removes its local audit write (assuming Core handles it), every admin user-management action becomes invisible in the audit log. Audit immutability is a Nebu invariant; a silent gap in coverage is a regression vector.
- **Empfehlung:** Either (a) document explicitly in Story 9.2 dev notes that the Gateway must continue to write audit records around the new RPC calls; or (b) add `WriteAuditLog`-equivalent emission inside each Core handler (mirror the pattern from `delete_user_keys/2` which uses `Ecto.Multi` with a failure-invariant audit write). Option (b) is more durable because it cannot be accidentally dropped during refactoring. This finding is MEDIUM rather than HIGH because the immediate security posture is unchanged — the regression risk lives in the future cutover, not in the present diff.
- **Referenz:** OWASP ASVS V7.1.3 ("audit logging includes administrative actions"), Nebu invariant: audit-log immutability / coverage.

### [LOW] `String.to_integer/1` on DB-stored config will crash on malformed values

- **CWE / OWASP:** CWE-754 (Improper Check for Unusual or Exceptional Conditions)
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2050-2055` (`parse_int_config/2`)
- **Beschreibung:** `parse_int_config/2` calls `String.to_integer(val)` on any `:binary` value pulled from `server_config`. If a previous admin (or a manual DB edit) wrote a non-numeric string into `room_default_max_members` or `audit_log_retention_days`, the function raises `ArgumentError`, which surfaces as `internal` to the caller. Inputs are admin-controlled, not user-controlled, so this is hygiene rather than exploit.
- **Impact:** `GetServerConfig` fails for the entire instance until the bad row is repaired. No data leak; no privilege escalation.
- **Empfehlung:** Wrap with `Integer.parse/1` and treat `:error` as `0`:
  ```elixir
  val when is_binary(val) ->
    case Integer.parse(val) do
      {n, _} -> n
      :error -> 0
    end
  ```
- **Referenz:** Hygiene; defensive parsing.

### [INFO-1] PSK interceptor automatically covers all new RPCs

- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex:7`, `core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex`
- **Beschreibung:** The `Nebu.Grpc.AuthInterceptor` is mounted at the `GRPC.Endpoint` level, so all nine new admin RPCs are automatically protected by the `x-nebu-node-token` PSK check with constant-time comparison (HMAC equality, line 106-114). No per-RPC registration is required. This is a positive observation — confirms Story 7-16d's hardening still holds.

### [INFO-2] New attack surface: nine admin RPCs reachable from any process holding the gateway PSK

- **Datei:** `proto/core.proto:108-134`, `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1716-2057`
- **Beschreibung:** This story expands the Core gRPC attack surface by 9 RPCs, all of which are admin-privilege operations (mutate users, mutate rooms, mutate server config, return PII). The existing PSK gates the surface, but anyone with read access to `.secrets/internal_secret` (or a process running on a node that can mount it) can call these RPCs without an admin OIDC session. Document this in the threat model: "PSK = gateway authority; gateway authority includes admin-RPC authority". Combined with MEDIUM-1, this is a defense-in-depth observation rather than a finding.

---

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅     |
| `reason` field on compliance access         | ✅     |
| Audit-log immutability                      | ✅     |
| `instance_admin` notification (if in-scope) | ⚠️     |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅     |
| Matrix Power Level checks                   | ✅     |
| No hardcoded secrets                        | ✅     |
| TLS 1.3 enforcement                         | ✅     |
| AES-256-GCM correctness                     | ✅     |
| Ed25519 verify-before-accept                | ✅     |
| No secrets in logs / error messages         | ✅     |
| **Nebu-9.1 specific:** `oidc_client_secret` excluded from `GetServerConfig` | ✅ |
| **Nebu-9.1 specific:** Email masked, never plaintext | ✅ |
| **Nebu-9.1 specific:** `ArchiveRoom` uses `SELECT FOR UPDATE` | ✅ |
| **Nebu-9.1 specific:** `DeactivateUser` sequences DB → session-destroy | ✅ |
| **Nebu-9.1 specific:** `UpdateUserRole` validates role allowlist | ✅ |
| **Nebu-9.1 specific:** Admin-RPC role guard at Core layer | ❌ (MEDIUM-1) |
| **Nebu-9.1 specific:** Audit emission for state-changing admin RPCs | ⚠️ (MEDIUM-3) |

Notes:
- `instance_admin notification` ⚠️: not directly in scope for this story (no compliance escalation paths added). Listed as ⚠️ rather than ✅ because future stories that route admin actions through these RPCs would need to verify the notification hooks remain wired in the Gateway pre-call.
- `oidc_client_secret excluded`: verified at three layers — proto field omitted (`core.proto:594-602`), DB filter (`db.ex:303` `WHERE key != 'oidc_client_secret'`), and ExUnit assertion (`admin_grpc_test.exs:880-905`).
- `ArchiveRoom atomic`: `db.ex:252-287` opens an Ecto transaction, runs `SELECT status FROM rooms WHERE room_id = $1 FOR UPDATE`, and dispatches on three states (not_found / archived / other). Idempotent for already-archived rooms.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 3 |
| LOW       | 1 |
| INFO      | 2 |

## Pipeline Decision

**CLEAN** — no CRITICAL or HIGH findings. Pipeline may proceed.

The three MEDIUM items are defense-in-depth gaps and a forward-looking audit-coverage concern that becomes load-bearing only after Story 9.2 cuts over to these RPCs. They should be filed as follow-up tasks (or addressed in the Story 9.2 dev notes) before the new RPCs replace the existing Gateway-local handlers in production.

Recommended follow-ups:
1. **MEDIUM-1**: Add `require_instance_admin!/1` guard to all nine admin handlers (1-2 line change per handler).
2. **MEDIUM-3**: Decide explicitly in Story 9.2 whether audit emission lives in Gateway-around-RPC or Core-inside-RPC. Document the choice. Add Core-side audit if the answer is "Core".
3. **MEDIUM-2**: Treat `destroy_session` failure as gRPC `internal` rather than swallowing it.
4. **LOW**: Replace `String.to_integer/1` with `Integer.parse/1` for config robustness.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
