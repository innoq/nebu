# Security Review — Story 9.3: Admin UI Rooms API Integration — 2026-05-04

**Agent:** Kassandra
**Diff base:** `git diff --staged` (Story 9.3 Dev → SEC Gate 1)
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

The diff wires the Admin UI room-management pages to the real Elixir Core via gRPC — a clean, mechanical port of the Story 9-2 pattern. CSRF, body-size limits, sessionGuard, generic error responses, template auto-escape and input validation are all intact. One HIGH finding propagates from Story 9-2: state-changing room operations from the Admin UI never actually populate `audit_log` rows, because the gateway does not forward an `x-user-id` to the Core; the Core's `audit_writer` then rejects the row at `validate_required(:actor_user_id)`. The audit log of who archived/unarchived a room is silently dropped.

## Findings

### [HIGH] Admin-UI archive/unarchive actions never produce an audit_log row — actor_user_id arrives nil and the changeset rejects the entry

- **CWE / OWASP:** CWE-778 (Insufficient Logging), CWE-223 (Omission of Security-Relevant Information) / OWASP A09:2021 (Security Logging and Monitoring Failures)
- **Datei:**
  - `gateway/internal/admin/rooms.go:386` (ArchiveRoomHandler — gRPC call uses raw `r.Context()`, no `WithUserMetadata`)
  - `gateway/internal/admin/rooms.go:424` (UnarchiveRoomHandler — same pattern)
  - `gateway/internal/admin/rooms.go:359` (UpdateRoomSettingsHandler — same pattern; not yet audit-logged in Core, will fall into the same trap when audit is added)
  - `gateway/internal/grpc/client.go:308–325` (new `ArchiveRoom` / `UnarchiveRoom` / `UpdateRoomSettings` wrappers — only forward `ctx` unchanged; only the PSK interceptor adds metadata)
  - `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1654, 1697–1706, 1712, 1722–1731` (audit log emission with `actor_id` from `Nebu.Grpc.Metadata.trusted_identity(stream)`)
  - `core/apps/compliance/lib/compliance/audit_log_entry.ex:23` (`@required_fields [:actor_user_id, :action, :outcome]` — `validate_required` blocks insert)
  - `core/apps/compliance/lib/compliance/audit_writer.ex:69–107` (returns `{:error, :audit_write_invalid}` and writes nothing on nil actor)
- **Beschreibung:** The Admin-UI handlers added in this story call `h.core.ArchiveRoom(r.Context(), ...)`, `h.core.UnarchiveRoom(r.Context(), ...)`, and `h.core.UpdateRoomSettings(r.Context(), ...)` with the bare HTTP request context. No call site in `gateway/internal/admin/` ever invokes `coregrpc.WithUserMetadata(ctx, userID, systemRole)` — verified by `grep WithUserMetadata gateway/internal/admin/*.go` (zero hits). The PSK unary interceptor (`client.go:29–46`) only injects `x-nebu-node-token`. Consequently, `Nebu.Grpc.Metadata.user_id(stream)` returns `nil` on the Core side. The newly-added audit calls in `archive_room/2` (server.ex:1697) and `unarchive_room/2` (server.ex:1722) pass that `nil` to `Compliance.AuditWriter.log/6`. `AuditWriter` runs the changeset, `validate_required(:actor_user_id)` fails, the function returns `{:error, :audit_write_invalid}` and **no row is inserted** (test evidence: `audit_writer_test.exs:245–261` covers exactly this case). The room is still archived; the audit trail records nothing.
- **Impact:** Every archive / unarchive performed via the Admin UI succeeds silently with no record of the actor in `audit_log`. A compromised or malicious admin (or a confused operator) can churn room state with no forensic trail. This breaks Nebu's audit-log invariant for the most visible admin surface. Same gap was patched in Story 9-2 only on the Core side — the structural dependency on metadata that is never set was not noticed there either, so 9-2's `user_deactivated` / `user_reactivated` / `update_user_role` audit entries are likewise being silently dropped today. Story 9-3 doubles the surface.
- **Empfehlung:** Two fixes, applied together:
  1. **Gateway:** in each Admin-UI handler that calls a state-changing Core RPC, build the gRPC ctx with caller identity. Either (a) add a small helper in `admin/` that resolves `AdminSubFromContext(r.Context())` → Matrix user ID + `instance_admin` role and wraps with `coregrpc.WithUserMetadata(ctx, userID, "instance_admin")`, then pass that to `h.core.*`; or (b) add a UnaryClientInterceptor on the admin code path that reads the admin sub from a context key and sets `x-user-id` automatically.
  2. **Core:** harden `archive_room/2`, `unarchive_room/2`, `deactivate_user/2`, `reactivate_user/2`, `update_user_role/2` (and any analogous RPC) to refuse the call when `actor_id` is `nil` — return a `GRPC.RPCError` with `:unauthenticated`. This prevents the silent-drop failure mode permanently and surfaces the gap loudly the next time it is reintroduced.
  Add an integration test that exercises the full Admin-UI POST → Core RPC → audit_log path against a real DB so the `actor_user_id IS NOT NULL` invariant is enforced end-to-end.
- **Referenz:** OWASP ASVS v4.0 §7.1 (security-relevant events logged), NIST AU-2 (audit events), NIST AU-12 (audit generation completeness). Re-opens Story 9-2 HIGH-1.

---

### [MEDIUM] New flash strings emitted by the new handlers are not in the `allowedFlashMessages` allowlist — user sees no banner on success or error

- **CWE / OWASP:** CWE-1295 (Insufficient Logging of Operational Action) — UX surface with security tail
- **Datei:**
  - `gateway/internal/admin/rooms.go:317` (`?flash=Name+update+not+yet+available`)
  - `gateway/internal/admin/rooms.go:365, 369, 375` (`Room+not+found`, `Error+updating+settings`, `Settings+updated`)
  - `gateway/internal/admin/rooms.go:389, 393` (`Room+not+found`, `Error+archiving+room`)
  - `gateway/internal/admin/rooms.go:427, 431` (`Room+not+found`, `Error+unarchiving+room`)
  - `gateway/internal/admin/flash.go:5–17` (allowlist — none of the above strings is present)
- **Beschreibung:** `sanitizeFlash` (Story 7.18) implements an exact-match allowlist of permitted flash messages. The seven new strings introduced by Story 9-3 are not in it. They will be silently dropped from the rendered banner. `Room archived` and `Room unarchived` happen to already be on the allowlist, so the success path of archive/unarchive still works; everything else does not.
- **Impact:** Operationally identical to 9-2's LOW-1: an admin who triggers an error (room not found, gRPC failure, settings save) sees no feedback. The action may have failed completely, or partially succeeded — the operator may retry, generate duplicate audit events (when Finding HIGH-1 is fixed) or duplicate Core writes. The allowlist itself is correct (it prevents flash-injection-as-XSS); the security control is intact, but its *coverage* is incomplete.
- **Empfehlung:** Add the seven strings to `allowedFlashMessages` in `flash.go` (`Settings updated`, `Error updating settings`, `Error archiving room`, `Error unarchiving room`, `Room not found`, `Name update not yet available`) and extend `flash_test.go` to assert each is permitted. Mirrors the same fix recommended for Story 9-2.
- **Referenz:** ASVS v4.0 §7.4.3 (graceful error handling); internal Story 7.18 invariant.

---

### [MEDIUM] gRPC error details are written to logs unredacted via `slog.Warn(..., "err", err)`

- **CWE / OWASP:** CWE-209 (Generation of Error Message Containing Sensitive Information)
- **Datei:** `gateway/internal/admin/rooms.go:99, 210, 219, 368, 392, 430`
- **Beschreibung:** Each gRPC error path logs the raw `err` returned by the Core. The Elixir side raises `GRPC.RPCError`s whose messages may contain `inspect(reason)` of internal DB errors (Postgres detail strings, schema names, room IDs). The browser sees only generic flash messages, so there is no client-facing leak — but the operator log destination receives the full text.
- **Impact:** No reachable user-facing leak. Risk is operational: a SIEM-tier consumer with broader access than the local log file would see internal error text. Defence-in-depth gap, not an exploit path.
- **Empfehlung:** Log `status.Code(err)` plus a short error category instead of `err`. Or, on the Elixir side, replace `inspect(reason)` in non-NotFound `GRPC.RPCError` messages with a fixed string. Same approach as 9-2 LOW-2.
- **Referenz:** OWASP ASVS v4.0 §8.3.4 (do not include sensitive data in error messages).

---

### [LOW] `update_room_settings/2` in Core has no audit-log call — `max_members` changes are not recorded

- **CWE / OWASP:** CWE-778 (Insufficient Logging)
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:281–297`
- **Beschreibung:** Story 9-3 wires the Admin UI to call `UpdateRoomSettings`, but the Core handler does not emit an audit-log entry for the change. Archive/unarchive got the audit hook in this diff; settings update did not. AC scope acknowledges this implicitly (`Story 9.3` description: "audit logging added to Core for archive/unarchive"), so this is a known limitation rather than a missed AC. Listed for completeness — `max_members` changes have lower forensic value than archive state, but a hostile admin can still drain a room by setting `max_members` to a small number, and that action would leave no trace.
- **Impact:** Operator can change room capacity without being logged. Together with Finding HIGH-1, the entire Admin-UI surface for rooms has incomplete audit coverage.
- **Empfehlung:** When HIGH-1 is fixed, add an `audit_writer_module().log(actor_id, "room_settings_updated", "room", room_id, %{max_members: max_members}, "success")` call after the Room.Server cast in `update_room_settings/2`, and add `room_settings_updated` to the `@known_actions` allowlist in `audit_writer.ex`.
- **Referenz:** ASVS v4.0 §7.1.3.

---

### [INFO] CSRF, body-size, sessionGuard wiring on the new POST route is correct

- **Datei:** `gateway/cmd/gateway/main.go:329`
- **Outcome:** No vulnerability.
- **Beschreibung:** The newly registered `POST /admin/rooms/{roomId}/settings` is wrapped — outside-in — `bodyLimit64KiB(csrf(sessionGuard(...)))`, identical to the existing archive/unarchive routes and the analogous user routes from Story 9-2. The form template at `templates/rooms.html:91–106` includes `<input type="hidden" name="_csrf" value="{{ .PageData.CSRFToken }}">` and the action attribute uses Go template auto-escape (HTML context). No CSRF gap, no body-flood vector, no XSS via `ActiveItemID`.

---

### [INFO] `roomID` redirect concatenation does not enable open redirect or header injection

- **Datei:** `gateway/internal/admin/rooms.go:317, 365, 369, 375, 389, 393, 412, 427, 431, 450`
- **Outcome:** No vulnerability.
- **Beschreibung:** Several handlers concatenate `roomID` (from `r.PathValue("roomId")`) into a `Location` URL. Go's net/http rejects request URIs with control characters at parse time, and `http.Header.Set` rejects CRLF in header values. A malicious roomID cannot redirect to a different origin — the path always begins with `/admin/rooms/`, and even a `..%2F..` segment is contained within the gateway's same-origin space. The `UpdateRoomNameHandler` deferred-redirect added in this story (line 317) inherits this property.

---

### [INFO] `max_members` validation bound is sane

- **Datei:** `gateway/internal/admin/rooms.go:346–354`
- **Outcome:** No vulnerability.
- **Beschreibung:** Server-side `max_members` is bounded to `[0, 1_000_000]` and clamped to int32. Mirrors the `min`/`max` HTML attributes on the form input. Sufficient to prevent integer-overflow surprises in proto field (`int32 max_members = 2`).

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ — diff does not touch compliance tables |
| `reason` field on compliance access         | ✅ — n/a, no compliance access added |
| Audit-log immutability                      | ✅ — no DDL change to `audit_log`; immutability preserved |
| Audit-log **completeness** for admin actions | ❌ — see HIGH-1 (silent drop on nil actor) |
| `instance_admin` notification (if in-scope) | ✅ — n/a |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ — login path unchanged; sessionGuard enforces existing session |
| Matrix Power Level checks                   | ✅ — n/a, admin RPCs are instance-scoped, not room-power-scoped |
| No hardcoded secrets                        | ✅ — diff contains no secret material |
| TLS 1.3 enforcement                         | ✅ — diff does not touch TLS config |
| AES-256-GCM correctness                     | ✅ — n/a |
| Ed25519 verify-before-accept                | ✅ — n/a |
| No secrets in logs / error messages         | ⚠️ — `slog.Warn(..., "err", err)` may carry inspect-formatted Core error strings; see MEDIUM-2. No tokens/passwords. |

The audit-completeness invariant (a Nebu-specific extension of the immutability rule) is violated — see HIGH-1.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 1 |
| MEDIUM    | 2 |
| LOW       | 1 |
| INFO      | 3 |

## Pipeline Decision

**HIGH findings present, `blocking_severity: CRITICAL` (default)** — Pipeline proceeds with warning. The HIGH-1 finding must be scheduled as a follow-up story (it re-opens Story 9-2 HIGH-1, since the gateway-side metadata propagation was never added) or explicitly accepted with written justification before the Epic-9 SEC Gate 2 review. The defect is structural and the same shape now affects user-deactivate/reactivate, role-update, room-archive, and room-unarchive — five admin surfaces with no audit row in production.

User decision required: **fix, accept with written justification, or convert to follow-up story?**

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
