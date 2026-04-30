---
id: 7-32
type: fix
security_review: not-needed
created: 2026-04-30
sec_gate_ref: _bmad-output/implementation-artifacts/security-reports/epic-7b-security-review-2026-04-30.md
---

# Story 7.32: Fix moderation gRPC handlers — read caller_id from trusted gRPC metadata

Status: ready-for-dev

## Story

As the Nebu Core gRPC layer,
I want kick_user, ban_user, and unban_user to derive the caller's identity exclusively from trusted gRPC stream metadata (`x-user-id`),
so that a future gateway bug that forwards a client-supplied caller_id verbatim cannot let an attacker impersonate a higher-privileged actor and bypass power-level checks.

## Context / Background

**SEC Gate 2 finding (HIGH):** During the epic-7b security review (Kassandra, 2026-04-30), a HIGH finding was raised:

> `kick_user/2`, `ban_user/2` and `unban_user/2` extract the actor's identity from `request.caller_id` (a request-body field) rather than from `Nebu.Grpc.Metadata.trusted_identity(stream)`. Every other state-changing Core handler (`set_power_levels`, `get_messages`, `get_room_state`, etc.) uses the metadata path.

**Root cause:** Story 7-22 implemented these three handlers with `caller_id = request.caller_id` instead of the established metadata pattern. Today the Go gateway sets both the proto field and the `x-user-id` metadata header from the same JWT claim, so the values agree. However, the inconsistency erodes the architectural rule that *Core trusts only metadata from the gateway* and is one bug away from an auth bypass.

**Impact:** Any future gateway refactor that forwards a client-supplied `caller_id` verbatim would allow any authenticated user to execute kick/ban with arbitrary actor identity, including bypassing power-level checks by impersonating a higher-privileged room member.

**Reference:** [Source: `_bmad-output/implementation-artifacts/security-reports/epic-7b-security-review-2026-04-30.md` — Finding: "Moderation gRPC handlers trust request-body caller_id instead of trusted metadata"]

## Acceptance Criteria

1. In `kick_user/2`, the line `caller_id = request.caller_id` is replaced with `{caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)` and the `_stream` parameter is renamed to `stream`.

2. In `ban_user/2`, the same replacement is applied: `caller_id = request.caller_id` → metadata extraction from `stream`.

3. In `unban_user/2`, the same replacement is applied: `caller_id = request.caller_id` → metadata extraction from `stream`.

4. An ExUnit test in a new file `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_moderation_metadata_test.exs` verifies that when a kick request is sent with `request.caller_id = "@victim:nebu.local"` while the stream metadata identifies `@attacker:nebu.local` (who has insufficient power level), the handler uses the metadata identity (`@attacker`) — resulting in a `GRPC.RPCError` with `status: GRPC.Status.permission_denied()`, NOT a successful kick with `@victim`'s identity.

5. The same mismatched-identity test is written for `ban_user/2` (body says `@victim`, metadata says `@attacker` with insufficient power → permission_denied).

6. The existing Godog acceptance tests in `gateway/features/room_moderation.feature` continue to pass (no regression in the happy path or error paths).

7. `make test-unit-elixir` passes with all new tests green.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [kick_user — metadata identity used over body caller_id] — ExUnit
   - Given: Room `!sec-test:test.local` with `@victim:test.local` as admin (power level 100) and `@attacker:test.local` as regular member (power level 0, below kick threshold 50). `@target:test.local` is also a joined member.
   - When: `Server.kick_user/2` is called with `request.caller_id = "@victim:test.local"` (body) but stream metadata `x-user-id = "@attacker:test.local"` (metadata).
   - Then: raises `GRPC.RPCError` with `status: GRPC.Status.permission_denied()` — proving the handler uses `@attacker`'s (insufficient) power level from metadata, not `@victim`'s (sufficient) power level from the body.

2. [ban_user — metadata identity used over body caller_id] — ExUnit
   - Given: Same room setup with `@victim` (admin, power ≥ 50) and `@attacker` (regular user, power 0).
   - When: `Server.ban_user/2` is called with `request.caller_id = "@victim:test.local"` but stream metadata `x-user-id = "@attacker:test.local"`.
   - Then: raises `GRPC.RPCError` with `status: GRPC.Status.permission_denied()` — metadata identity enforced.

3. [kick_user — happy path still works with matching identity] — ExUnit
   - Given: Room with `@moderator:test.local` (power 50) and `@target:test.local` as members.
   - When: `Server.kick_user/2` called with both `request.caller_id` and stream metadata set to `@moderator:test.local`.
   - Then: returns `%Core.KickUserResponse{}` (no regression).

## Tasks / Subtasks

- [ ] Task 1: Fix `kick_user/2` handler (AC: #1)
  - [ ] Change function signature from `def kick_user(request, _stream) do` to `def kick_user(request, stream) do`
  - [ ] Replace `caller_id = request.caller_id` with `{caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)`
  - [ ] Verify the rest of the handler compiles and all `caller_id` usages are untouched (membership check, power-level check, event sender field)

- [ ] Task 2: Fix `ban_user/2` handler (AC: #2)
  - [ ] Change function signature from `def ban_user(request, _stream) do` to `def ban_user(request, stream) do`
  - [ ] Replace `caller_id = request.caller_id` with `{caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)`

- [ ] Task 3: Fix `unban_user/2` handler (AC: #3)
  - [ ] Change function signature from `def unban_user(request, _stream) do` to `def unban_user(request, stream) do`
  - [ ] Replace `caller_id = request.caller_id` with `{caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)`

- [ ] Task 4: Write failing ExUnit tests (AC: #4, #5, #6) — write BEFORE implementing Tasks 1–3
  - [ ] Create `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_moderation_metadata_test.exs`
  - [ ] Implement FakeRoomDB (ETS-backed, same pattern as `server_set_typing_test.exs`)
  - [ ] Implement FakeMessagesDB (captures insert_event calls)
  - [ ] Implement `build_stream/2` helper that sets `x-user-id` and `x-system-role` headers
  - [ ] Write `kick_user — metadata identity wins over body caller_id` test (AC #4)
  - [ ] Write `ban_user — metadata identity wins over body caller_id` test (AC #5)
  - [ ] Write `kick_user — happy path regression` test (AC #6 / no regression)
  - [ ] Run `make test-unit-elixir` — confirm tests FAIL (red phase)

- [ ] Task 5: Implement fixes (Tasks 1–3) and go green (AC: #7)
  - [ ] Apply the three handler fixes
  - [ ] Run `make test-unit-elixir` — confirm all new tests pass (green phase)

## Dev Notes

### What is being changed and why

**File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`

Three functions need a one-line fix each, plus a parameter rename:

| Function | Line (approx) | Current | Target |
|---|---|---|---|
| `kick_user/2` | ~1207 | `def kick_user(request, _stream) do` + `caller_id = request.caller_id` | `def kick_user(request, stream) do` + `{caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)` |
| `ban_user/2` | ~1289 | `def ban_user(request, _stream) do` + `caller_id = request.caller_id` | `def ban_user(request, stream) do` + `{caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)` |
| `unban_user/2` | ~1366 | `def unban_user(request, _stream) do` + `caller_id = request.caller_id` | `def unban_user(request, stream) do` + `{caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)` |

**Reference implementation:** `set_power_levels/2` (line ~564 in same file) uses the identical pattern:
```elixir
def set_power_levels(request, stream) do
  {user_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)
  ...
end
```
And `get_room_state/2` (line ~719) uses:
```elixir
{caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)
```

### Architecture rules enforced by this fix

- **"Auth principal arrives only via metadata"** — Core trusts the `x-user-id` gRPC header set by the Go gateway after OIDC validation. The proto body fields (`caller_id`) are informational (telemetry/logging) only.
- **`Nebu.Grpc.Metadata.trusted_identity/1`** — defined in `core/apps/event_dispatcher/lib/nebu/grpc/metadata.ex`. Returns `{user_id :: String.t() | nil, system_role :: String.t()}`. The `user_id` is the `x-user-id` header value.
- The `caller_id` proto field can remain in the proto messages for telemetry/logging — it must simply never be used as the auth principal. Do NOT remove it from the proto (unnecessary proto churn, no security gain).

### What must NOT change

- The `target_id` field continues to come from `request.target_id` — that is correct (the target of the kick/ban is supplied by the client and is not a privileged identity).
- The `reason` field continues to come from `request.reason` — correct.
- The `room_id` field continues to come from `request.room_id` — correct.
- All power-level checks, membership checks, and event emission logic remain unchanged.
- The Godog integration tests (`gateway/features/room_moderation.feature`) test the full stack end-to-end and must continue to pass — the Go gateway already sets `x-user-id` correctly from the JWT, so the change is transparent to the happy path.

### Test file pattern — use server_set_typing_test.exs as the model

The new test file `server_moderation_metadata_test.exs` must follow the exact same pattern as `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_set_typing_test.exs`:

1. `use ExUnit.Case, async: false` — Horde + ETS + Application.put_env are process-global.
2. Inner `FakeRoomDB` module (ETS-backed, same callbacks: `load_members/1`, `insert_room/1`, `insert_member/2`, `delete_member/2`, `insert_event/1`, `set_power_levels/2`).
3. Inner `FakeMessagesDB` module — same as FakeRoomDB for `insert_event/1` (used via `Application.put_env(:event_dispatcher, :messages_db_module, FakeRoomDB)` — can reuse FakeRoomDB as the messages module too, same as `join_room_test.exs`).
4. `setup` block that:
   - Creates ETS table (with guard for --watch reruns)
   - Injects `Application.put_env(:room_manager, :db_module, FakeRoomDB)`
   - Injects `Application.put_env(:event_dispatcher, :messages_db_module, FakeRoomDB)`
   - Injects NoOpAuditWriter for compliance: `Application.put_env(:compliance, :audit_writer, NoOpAuditWriter)`
   - Starts `:pg` (idempotent)
   - Clears `:NebuTxnDedup` ETS table
   - `on_exit` cleanup
5. `build_stream/2` helper:
   ```elixir
   defp build_stream(user_id, system_role \\ "user") do
     %{http_request_headers: %{"x-user-id" => user_id, "x-system-role" => system_role}}
   end
   ```
6. `start_and_track_room/1` helper (same as other test files).
7. Helper to set up a room with power levels — needed for the power-level tests:
   ```elixir
   defp setup_room_with_power_levels(room_id, members_and_levels) do
     {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
     :ok = start_and_track_room(room_id)
     # Insert members
     Enum.each(members_and_levels, fn {uid, _level} ->
       :ok = Nebu.Room.Server.join(room_id, uid)
     end)
     # Build power_levels JSON and set it
     power_levels_json = Jason.encode!(%{
       "users" => Map.new(members_and_levels, fn {uid, level} -> {uid, level} end),
       "kick" => 50,
       "ban" => 50
     })
     :ok = FakeRoomDB.set_power_levels(room_id, power_levels_json)
     # Force GenServer to reload power levels from ETS
     # (Room.Server reloads on next get_state after DB write)
     :ok
   end
   ```
   **Note:** After `set_power_levels`, you may need to restart the room GenServer or call `Nebu.Room.Server.reload_power_levels/1` if that function exists — check the Room.Server API. If there is no reload function, instead use `Nebu.Room.RoomSupervisor.start_room/1` after inserting the power levels to the ETS table before the GenServer starts. The simplest approach: insert into FakeRoomDB ETS *before* calling `start_room/1`, so the GenServer reads the correct power levels on init.

### Power level setup for the test

The critical test scenario:
- `@victim:test.local` — power level 100 (admin, would normally be allowed to kick)
- `@attacker:test.local` — power level 0 (regular user, NOT allowed to kick)
- `@target:test.local` — power level 0 (regular member, the kick/ban target)
- Kick threshold: 50 (Matrix default)
- Ban threshold: 50 (Matrix default)

Test assertion: calling `kick_user` with body `caller_id = "@victim:test.local"` but stream `x-user-id = "@attacker:test.local"` must raise `GRPC.RPCError` with `status: GRPC.Status.permission_denied()`. This proves the implementation uses metadata (attacker, level 0, rejected) not body (victim, level 100, would be allowed).

The `assert_raise` pattern to use:
```elixir
assert_raise GRPC.RPCError, fn ->
  Server.kick_user(request, stream)
end
```
Or capture the error for status assertion:
```elixir
error = assert_raise GRPC.RPCError, fn ->
  Server.kick_user(request, stream)
end
assert error.status == GRPC.Status.permission_denied()
```

### NoOpAuditWriter

The moderation handlers may call `Compliance.AuditWriter.log/6` (same as `join_room/2`). Include a no-op audit writer to avoid Nebu.Repo dependency:

```elixir
defmodule NoOpAuditWriter do
  def log(_, _, _, _, _, _, _ \\ nil), do: :ok
end
```
Then: `Application.put_env(:compliance, :audit_writer, NoOpAuditWriter)`.

Check if `kick_user/2` actually calls the audit writer — if not, the `NoOpAuditWriter` injection is harmless but may not be strictly necessary. Looking at the current implementation (lines 1207–1276), there is no explicit audit writer call in the kick/ban/unban handlers. Include the injection anyway for safety.

### Project Structure Notes

- The three handlers live at lines 1195–1420 of `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` under the `# ─── Story 7-22: Room Moderation ─────────────────────────` section.
- The `Nebu.Grpc.Metadata` module is at `core/apps/event_dispatcher/lib/nebu/grpc/metadata.ex`.
- New test file: `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_moderation_metadata_test.exs` — parallel to `server_set_typing_test.exs` and `server_receipts_test.exs`.
- No Go gateway changes needed — the gateway already sets both the body field and the `x-user-id` header from the JWT subject claim (verified in story 7-22 implementation).
- No proto changes needed — keep `caller_id` field in KickUserRequest/BanUserRequest/UnbanUserRequest for telemetry; just stop using it as the auth principal.
- No migration needed.

### References

- [Source: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1207-1276`] — `kick_user/2` current implementation
- [Source: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1289-1354`] — `ban_user/2` current implementation
- [Source: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1366-1420`] — `unban_user/2` current implementation
- [Source: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:563-564`] — `set_power_levels/2` reference pattern (uses `trusted_identity`)
- [Source: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:719`] — `get_room_state/2` reference pattern (uses `trusted_identity` with `caller_id` variable name)
- [Source: `core/apps/event_dispatcher/lib/nebu/grpc/metadata.ex`] — `Nebu.Grpc.Metadata.trusted_identity/1` implementation
- [Source: `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_set_typing_test.exs`] — test pattern to follow (FakeRoomDB, build_stream/2, setup block, on_exit cleanup)
- [Source: `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_receipts_test.exs`] — secondary test pattern reference
- [Source: `core/apps/event_dispatcher/test/nebu/event_dispatcher/join_room_test.exs`] — NoOpAuditWriter pattern, FakeInviteDB pattern
- [Source: `_bmad-output/implementation-artifacts/security-reports/epic-7b-security-review-2026-04-30.md` — Finding HIGH: "Moderation gRPC handlers trust request-body caller_id instead of trusted metadata"]
- [Source: `_bmad-output/implementation-artifacts/7-22-room-moderation-kick-ban-unban-forget.md`] — original story that introduced the handlers

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (UPDATE — 3 handler fixes)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_moderation_metadata_test.exs` (NEW — ExUnit test for metadata identity precedence)
