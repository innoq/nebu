# Security Review — Story 9.18: Admin UI Room Detail — Member List — 2026-05-05

**Agent:** Kassandra
**Diff base:** `git diff --staged` (Story 9.18 Dev → SEC Gate 1)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

The diff adds a read-only Admin RPC (`ListAdminRoomMembers`) plus a small Admin-UI panel that lists current members of a room, and folds in a defect fix for `dedup_member_state_events/2` (state_key vs sender_id). The new query is parameterized, decryption uses the shared `decrypt_display_name/1` helper that fails closed to `""`, and the new HTTP surface inherits the existing `csrf(sessionGuard(...))` chain on `GET /admin/rooms/{roomId}`. No new attack surface beyond the existing 9-1 / 9-3 admin-RPC pattern. Audit-log gap from 9-3 (HIGH-1, archive/unarchive without `actor_user_id`) does **not** re-emerge here — `ListAdminRoomMembers` is read-only and not audited by design. One INFO observation on the proto change (`Core.Event.state_key` is now wire-visible to all Event consumers).

## Findings

### [INFO] `Core.Event.state_key` field added (proto field 8) — broadens the wire payload of every Event RPC

- **Datei:** `proto/core.proto:156`, `core/apps/event_dispatcher/lib/pb/core.pb.ex:13`, `gateway/internal/grpc/pb/core.pb.go` (regenerated)
- **Outcome:** No vulnerability.
- **Beschreibung:** `state_key` is added to `Core.Event` so the dedup logic in `get_initial_sync` / `get_sync_delta` / `get_messages` can compare timeline events by their state subject (the user whose membership changed). For `m.room.member`, `state_key` equals the target user_id — already public room-state. For `m.room.power_levels`, `m.room.create`, etc. it is `""`. No PII or session token is carried in this field. The new field is correctly populated from `Map.get(event_map, "state_key", "")` in both `map_to_proto_event/1` (server.ex:1416) and the second helper at server.ex:471. Worth recording because every Event consumer (EventBus stream, GetMessages, GetEventContext) now also emits `state_key` — confirmed correct, no leakage.
- **Empfehlung:** None. Logged for audit completeness.

---

### [INFO] `dedup_member_state_events/2` correctness fix — keying changed from `sender_id` to `state_key`

- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1230–1245`
- **Outcome:** No vulnerability — this is a defect fix, not a regression.
- **Beschreibung:** The dedup function previously keyed on `sender_id`, which incorrectly removed the inviter's state membership instead of the invitee's whenever an invite/kick was in the timeline window (sender ≠ state_key). The corrected logic keys on `state_key` — the user whose membership actually changed. Test coverage in `server_dedup_test.exs` (394 lines, new) explicitly exercises Self-Join, Invite, Kick, and the empty-state edge case. No information disclosure: state events for users not in the timeline window are still returned; state events for users **in** the timeline are deduplicated, which matches Matrix sync semantics. Element's "No membership changes detected" symptom was a UX bug, not a security bug.
- **Empfehlung:** None. Defect fix verified.

---

### [INFO] New gRPC RPC `ListAdminRoomMembers` follows the established read-only Admin pattern

- **Datei:**
  - `proto/core.proto:141–144, 643–653` (RPC + AdminRoomMemberProto + Request/Response)
  - `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2055–2082` (handler)
  - `core/apps/event_dispatcher/lib/nebu/admin/db.ex:339–382` (SQL query)
  - `gateway/internal/grpc/client.go:347–352` (Go wrapper)
  - `gateway/internal/admin/rooms.go:206–245` (DetailHandler integration)
- **Outcome:** No vulnerability.
- **Beschreibung:**
  1. **SQL injection:** The query uses `Ecto.Adapters.SQL.query!` with positional parameter `$1` for `room_id`; no string interpolation (db.ex:357–369). `room_id` itself is forwarded as the proto `string` from the gRPC request — Postgres parameter binding is the canonical mitigation.
  2. **Authn / Authz:** The HTTP route `GET /admin/rooms/{roomId}` is wrapped in `csrf(sessionGuard(...))` (`gateway/cmd/gateway/main.go:332`). The Core RPC is reachable only via the Gateway's mTLS/PSK-protected gRPC channel. Read-only listing of a room's members for an authenticated `instance_admin` is exactly the role's scope. ADR G2: Core trusts the Gateway's authentication.
  3. **PII handling:** `display_name` is decrypted via the shared `decrypt_display_name/1` helper (server.ex:2164–2178) which fails closed to `""` on AES-GCM decrypt error. No raw ciphertext, nonce, or ephemeral pubkey leaves the Core process — only the decoded plaintext (Tier 1 PII, which `instance_admin` is permitted to see). The proto schema intentionally omits `email` (Tier 2 — out-of-scope for this view) — verified at `proto/core.proto:645–649`.
  4. **Error handling — gateway:** `ListAdminRoomMembers` failure is non-fatal (`rooms.go:222–225`): the panel renders an empty member section with `slog.Warn` only. The user-visible response is unchanged (200 with the room detail). No internal error text reaches the browser.
  5. **Error handling — core:** On `{:error, reason}` from the DB, the handler raises `GRPC.RPCError{status: internal, message: "list_admin_room_members failed: #{inspect(reason)}"}`. `inspect(reason)` may include Postgres detail strings — same defence-in-depth gap as 9-3 MEDIUM-1; not new in this story. The Gateway logs the gRPC error (`slog.Warn`) but does not propagate it to the HTTP body (the handler renders an empty member list instead). Risk is operational-log-only, not user-facing.
  6. **No audit log:** Read operations are intentionally not audit-logged — consistent with `list_admin_users`, `list_admin_rooms`, `get_admin_user`, `get_admin_room`. Only state-changing admin actions audit-log.
  7. **No Power Level check:** Not required — read-only operation, gated by `instance_admin` role at the gateway. Power levels protect room-scoped state mutation; admin viewing is global, not room-scoped, by design (ADR-permissions / system_role).
- **Empfehlung:** None. Mirrors the established 9-1 / 9-3 pattern.

---

### [INFO] Test stubs for `ListAdminRoomMembers` correctly added to all `mockCoreClient` types

- **Datei:** `gateway/internal/admin/admin_grpc_actor_identity_test.go`, `gateway/internal/admin/auth_audit_test.go`, `gateway/internal/audit/writer_test.go`, `gateway/internal/compliance/handler_test.go`, `gateway/internal/grpc/stream_test.go`
- **Outcome:** No vulnerability.
- **Beschreibung:** Each mock returns `&pb.ListAdminRoomMembersResponse{}` and no error — preserves the contract used by the other Admin RPC mocks. No production code paths in tests; no security impact.

---

## Nebu Invariants

| Invariant                                    | Status | Note |
| -------------------------------------------- | ------ | ---- |
| Compliance RSP coverage                      | ✅     | `room_members` and `users` tables are not subject to user-scoped RSP (admin tables, accessed by `instance_admin`); query reads only joined-and-not-left members for one room. Same pattern as `list_users` / `list_rooms`. |
| `reason` field on compliance access          | ✅     | N/A — this is room/admin metadata, not compliance access. |
| Audit-log immutability                       | ✅     | No change to audit grants; no new migration. Read-only RPC by design does not audit-log. |
| `instance_admin` notification                | ✅     | Not applicable — listing is admin-internal, no scope escalation. |
| OIDC token validation (`iss`/`aud`/`exp`)    | ✅     | No new HTTP surface; gateway route inherits `sessionGuard` (which validated the OIDC session at login). |
| Matrix Power Levels                          | ✅     | Read-only, no state-changing operation. |
| No hardcoded secrets                         | ✅     | None introduced. |
| TLS 1.3 enforcement                          | ✅     | No TLS config changes. |
| AES-256-GCM correctness                      | ✅     | Decryption uses existing `Nebu.Signature.decrypt_operational_pii/3`; failure path returns `""` (no plaintext fallback, no oracle leak). |
| Ed25519 verify-before-accept                 | ✅     | No signed-event ingest path touched. |
| No secrets in logs / error messages          | ⚠️     | `slog.Warn(..., "err", err)` and `inspect(reason)` in `GRPC.RPCError` mirror the 9-3 MEDIUM-1 finding (operational-log defence-in-depth gap; no client-facing leak). Carried forward, not newly introduced — not blocking. |

---

## Triage Summary

| Severity  | Count |
| --------- | ----- |
| CRITICAL  | 0     |
| HIGH      | 0     |
| MEDIUM    | 0     |
| LOW       | 0     |
| INFO      | 4     |

**Classification:** CLEAN

## Decision

No CRITICAL or HIGH findings. Pipeline proceeds. The carried-forward 9-3 MEDIUM-1 (`inspect(reason)` in gRPC error messages) and 9-3 HIGH-1 (`actor_user_id` not forwarded on state-changing admin RPCs) are not in scope for this read-only story; both remain tracked under their original epic-level review.
