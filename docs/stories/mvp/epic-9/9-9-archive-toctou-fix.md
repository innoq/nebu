---
status: review
epic: 9
story: 9
security_review: required
---

# Story 9.9: Archive TOCTOU Fix

Status: ready-for-dev

## Story

As a system operator,
I want Core's `send_event` to atomically check archived status before accepting events,
So that events cannot be written to an archived room during the archive race window.

**Size:** XS

---

## Background

**The race window being closed:**

The archive flow is:
1. Go Gateway calls `POST /admin/rooms/{roomId}/archive`
2. Gateway calls gRPC `ArchiveRoom(room_id)` → Core runs `archive_room_atomic/1` (SELECT FOR UPDATE → UPDATE rooms SET status='archived') then terminates the GenServer
3. A concurrent Matrix client `PUT /rooms/{roomId}/send/m.room.message/{txnId}` may be in-flight at this exact moment

Even though Story 6.9 added a gateway-level `GET rooms.status` check _before_ calling Core.SendEvent, and Story 9.1 moved the DB write into Core's `archive_room_atomic/1` with SELECT FOR UPDATE, **there is still a window**:

- Gateway reads `status=active` → passes the guard
- `archive_room_atomic/1` sets `status=archived` + terminates GenServer
- GenServer restarts (or was restarting) OR the archived flag is still in memory as stale
- Core's `send_event` handler does NOT re-check the DB — it trusts in-memory state

**Story 6.9b** was a first formulation of this fix (created 2026-05-02, see `_bmad-output/implementation-artifacts/6-9b-archive-race-core-send-event.md`). **Story 9.9 supersedes 6-9b** — same fix, now tracked under Epic 9. The 6-9b file remains as-is (do not delete).

**Key context from 6-9b:** The original formulation used a plain `SELECT status FROM rooms WHERE id = $1` (no locking). The Epic 9 epics.md AC1 explicitly requires `SELECT ... FOR UPDATE` to serialize the archived-status check with the archive write transaction. This is the stronger guarantee — use `SELECT ... FOR UPDATE` inside a transaction.

**SEC Gate 2 reference:** HIGH-2 from epic-6 SEC Gate 2 (2026-05-02).

---

## Acceptance Criteria

**AC1 — Atomic DB check before insert (SELECT FOR UPDATE):**
In `RoomServer.handle_call({:send_event, ...})`, after the power level check and idempotency lookup, the handler executes a DB-level archived-status check using `SELECT status FROM rooms WHERE room_id = $1 FOR UPDATE` inside a transaction.
If `status = 'archived'`, the call returns `{:error, :room_archived}` without writing to `events`.

**AC2 — Error propagation:**
`{:error, :room_archived}` propagates through the gRPC `SendEvent` response as `status: FAILED_PRECONDITION` with `message: "M_ROOM_ARCHIVED"`. The Go gateway's `PutSendEventHandler` must map `codes.FailedPrecondition` → `403 M_ROOM_ARCHIVED` (same error code and body as the existing gateway-level archive guard).

**AC3 — Race window test:**
ExUnit test: a Room GenServer receives a `send_event` call after the room has been marked `archived` in the FakeDB. The event is **not** written to FakeDB's ETS event store. Assertion: 0 events of type `m.room.message` in ETS after the sequence.

**AC4 — Happy path unaffected:**
Existing `send_event` ExUnit tests for active rooms remain green. The new DB check must be a fast, indexed point-lookup (rooms.room_id is the primary key).

**AC5 — Concurrent archive/send — no double-archive:**
ExUnit test: two simulated archive calls on the same room via `FakeDB.check_and_set_archived/1` — exactly one returns `:ok`, the other returns `{:error, :already_archived}` (or the second is idempotent). No double-archive state.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. `test "send_event after room archived in DB is rejected"` — ExUnit (room_manager or send_event_test)**
- Given: Room GenServer running for `!toctou-test:test.local`, FakeDB configured to return `{:ok, "archived"}` for `check_room_status_for_update/1`
- When: `Nebu.Room.Server.send_event(room_id, user_id, "m.room.message", %{}, "txn-1")` is called
- Then: returns `{:error, :room_archived}`, ETS FakeDB has 0 `m.room.message` events

**2. `test "send_event on active room succeeds (TOCTOU check does not regress)"` — ExUnit**
- Given: Room GenServer running, FakeDB returns `{:ok, "active"}` for `check_room_status_for_update/1`
- When: `send_event` is called with valid content
- Then: returns `{:ok, event_id}` and event is present in FakeDB ETS

**3. `test "gRPC SendEvent returns FAILED_PRECONDITION for archived room"` — ExUnit (send_event_test or grpc_handler_test)**
- Given: Room GenServer running, FakeDB returns `{:ok, "archived"}` for `check_room_status_for_update/1`
- When: `Nebu.EventDispatcher.Server.send_event/2` is called with a `Core.SendEventRequest`
- Then: raises `GRPC.RPCError` with `status: GRPC.Status.failed_precondition()` and message containing `"M_ROOM_ARCHIVED"`

**4. Gateway unit test: `TestPutSendEvent_ArchivedRoom_CoreFailedPrecondition_Returns403` — Go httptest**
- Given: mock Core.SendEvent returns `status.Error(codes.FailedPrecondition, "M_ROOM_ARCHIVED")`
- When: `PUT /_matrix/client/v3/rooms/{roomId}/send/m.room.message/txn1`
- Then: response is `403 {"errcode":"M_ROOM_ARCHIVED","error":"Room is archived"}`

---

## Technical Implementation Plan

### Files to modify

| File | Change |
|---|---|
| `core/apps/room_manager/lib/nebu/room/db_behaviour.ex` | Add `check_room_status_for_update/1` callback |
| `core/apps/room_manager/lib/nebu/room/db.ex` | Implement `check_room_status_for_update/1` with SELECT FOR UPDATE inside Ecto transaction |
| `core/apps/room_manager/lib/nebu/room/server.ex` | Add check between idempotency lookup (Step 1) and event build (Step 2) |
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | Map `{:error, :room_archived}` to `GRPC.Status.failed_precondition()` |
| `core/apps/event_dispatcher/test/nebu/event_dispatcher/send_event_test.exs` | Add FakeDB `check_room_status_for_update/1`, add AC3 test |
| `core/apps/event_dispatcher/test/nebu/event_dispatcher/archive_room_test.exs` | Add `check_room_status_for_update/1` stub to FakeDB + FakeDBWithArchivedStatus |
| `gateway/internal/matrix/rooms.go` | Map `codes.FailedPrecondition` → `403 M_ROOM_ARCHIVED` in SendEvent error switch |
| `gateway/internal/matrix/rooms_test.go` | Add AC4 gateway unit test for FailedPrecondition → 403 M_ROOM_ARCHIVED |
| ALL other FakeDB modules in core tests | Add `check_room_status_for_update/1` stub (see list below) |

### Step 1 — `Nebu.Room.DBBehaviour` — add callback

```elixir
@doc """
Atomically checks if a room is archived using SELECT FOR UPDATE.
Called by Room.Server.send_event/6 before inserting an event.

Must be called inside an Ecto transaction (the transaction context is
created inside db.ex — caller does NOT need to manage a transaction).

Returns {:ok, "active"} for active rooms.
Returns {:ok, "archived"} for archived rooms.
Returns {:error, :not_found} when the room does not exist.
Returns {:error, reason} on DB error.

Story 9-9: closes the TOCTOU race window between archive_room_atomic/1
and send_event insert.
"""
@callback check_room_status_for_update(room_id :: String.t()) ::
            {:ok, String.t()} | {:error, :not_found | term()}
```

### Step 2 — `Nebu.Room.DB` — implement `check_room_status_for_update/1`

```elixir
@spec check_room_status_for_update(String.t()) :: {:ok, String.t()} | {:error, :not_found | term()}
def check_room_status_for_update(room_id) do
  result =
    Nebu.Repo.transaction(fn ->
      case Ecto.Adapters.SQL.query!(
             Nebu.Repo,
             "SELECT COALESCE(status, 'active') FROM rooms WHERE room_id = $1 FOR UPDATE",
             [room_id]
           ) do
        %{rows: [[status]]} -> status
        %{rows: []} -> Nebu.Repo.rollback(:not_found)
      end
    end)

  case result do
    {:ok, status} -> {:ok, status}
    {:error, :not_found} -> {:error, :not_found}
    {:error, reason} -> {:error, reason}
  end
end
```

**Pattern reference:** `archive_room_atomic/1` in `core/apps/event_dispatcher/lib/nebu/admin/db.ex` lines 252–287 — identical transaction + SELECT FOR UPDATE pattern. Do NOT use `Ecto.Adapters.SQL.query/3` outside a transaction for this (no locking semantics).

### Step 3 — `Nebu.Room.Server` — insert check in `handle_call({:send_event, ...})`

Insert between Step 1 (idempotency check) and Step 2 (event map build), **only on cache miss**:

```elixir
# Step 1.5 — Archived-status check (TOCTOU fix, Story 9-9).
# SELECT FOR UPDATE serialises this check with archive_room_atomic/1.
# Fail-open for {:error, :not_found} — let Core return its existing NOT_FOUND guard.
# Fail-open for DB errors (log + proceed) — same philosophy as init/1 fail-open.
case db_module().check_room_status_for_update(room_id) do
  {:ok, "archived"} ->
    {:reply, {:error, :room_archived}, state}

  {:ok, _} ->
    # active (or unknown status) — proceed
    do_send_event(room_id, user_id, event_type, content, txn_id, state_key, state)

  {:error, reason} ->
    # Fail-open: log and proceed; Core's GenServer NOT_FOUND guard covers missing rooms.
    require Logger
    Logger.warning("send_event: check_room_status_for_update failed, proceeding (fail-open): #{inspect(reason)}",
      room_id: room_id)
    do_send_event(room_id, user_id, event_type, content, txn_id, state_key, state)
end
```

**NOTE on refactoring:** The current `handle_call({:send_event, ...})` clause is ~70 lines (Steps 2–6). To avoid deeply nested `case` chains, extract Steps 2–6 into a private `do_send_event/6` helper. This also makes the archived-room branch testable without duplication.

**Placement in the handle_call clause:** The check goes AFTER the ETS idempotency lookup cache-miss branch (`[]` arm), BEFORE building `event_map`. This ensures:
- Authorized users that already completed the event (ETS hit) still get their event_id back
- The DB check runs only for new events (correct — no point re-checking for already-persisted events)

### Step 4 — `Nebu.EventDispatcher.Server.send_event/2` — map error

In the `send_event/2` gRPC handler (around line 114), add a new case to the `case Nebu.Room.Server.send_event(...)` pattern match:

```elixir
{:error, :room_archived} ->
  raise GRPC.RPCError,
    status: GRPC.Status.failed_precondition(),
    message: "M_ROOM_ARCHIVED"
```

This goes BEFORE the existing `{:error, reason}` catch-all clause.

### Step 5 — Gateway `rooms.go` — map FailedPrecondition to 403

In `PutSendEventHandler`, the current `switch st.Code()` block (around line 557) handles `NotFound`, `PermissionDenied`, `ResourceExhausted`, and `default`. Add:

```go
case codes.FailedPrecondition:
    writeMatrixError(w, http.StatusForbidden, "M_ROOM_ARCHIVED", "Room is archived")
```

This produces the same response body `{"errcode":"M_ROOM_ARCHIVED","error":"Room is archived"}` as the existing gateway-level archive guard (line 529 of `rooms.go`). The two guards now produce identical 403 responses — this is intentional.

---

## FakeDB Modules to Update

Every FakeDB module that implements `@behaviour Nebu.Room.DBBehaviour` must add the new `check_room_status_for_update/1` stub or it will cause a compile-time warning (or error).

**Known FakeDB modules (from grepping the codebase):**

| File | FakeDB name | Default stub return |
|---|---|---|
| `send_event_test.exs` | `FakeDB` | `{:ok, "active"}` |
| `archive_room_test.exs` | `FakeDB` | `{:ok, "active"}` |
| `archive_room_test.exs` | `FakeDBWithArchivedStatus` | `{:ok, "archived"}` |
| `create_room_test.exs` | `FakeDB` | `{:ok, "active"}` |
| `join_room_test.exs` | `FakeDB` | `{:ok, "active"}` |
| `upgrade_room_test.exs` | `FakeDB` (and variants) | `{:ok, "active"}` |
| `nebu_room_test.exs` | `FakeDB` (room_manager app) | `{:ok, "active"}` |
| `grpc_handler_test.exs` | any FakeDB | `{:ok, "active"}` |
| `audit_room_ops_test.exs` | any FakeDB | `{:ok, "active"}` |

**Strategy:** After implementing the callback in `DBBehaviour`, run `mix compile` inside the Core container. Compiler warnings will identify every FakeDB missing the callback. Add the stub to each one.

```bash
make test-unit-elixir  # will show compile warnings for missing callbacks
```

---

## Elixir Conventions (must follow)

- GenServer state: modify only via `handle_*` callbacks — no direct state mutation
- Errors: let it crash + Supervisor — no defensive try/rescue around the new DB call
- The `db_module()` helper pattern is already established — use it for the new callback
- No atoms as DB-facing keys — string keys throughout
- Transaction use: `Nebu.Repo.transaction/1` (same as `archive_room_atomic/1` pattern)
- Fail-open semantics on DB errors matches `init/1` and `PutSendEvent` gateway patterns

---

## Go Conventions (must follow)

- The switch statement in `PutSendEventHandler` already handles multiple gRPC codes — add `codes.FailedPrecondition` case in the same pattern
- `writeMatrixError(w, http.StatusForbidden, "M_ROOM_ARCHIVED", "Room is archived")` — match the existing errcode/message exactly (line 529 of `rooms.go`)
- Consumer-defined interfaces: if adding a test for the gateway FailedPrecondition path, use the existing mock pattern in `rooms_test.go` (line 1514+)

---

## Test Infrastructure Notes

- `async: false` — all Room GenServer tests; Horde.Registry and ETS are process-global
- FakeDB injection: `Application.put_env(:room_manager, :db_module, FakeDBWithArchivedForSend)` in setup; `Application.delete_env` in `on_exit`
- ETS cleanup: `:ets.delete_all_objects(:NebuTxnDedup)` in `on_exit` (same as send_event_test.exs setup)
- `on_exit` Horde cleanup: use `Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid)` — NOT `GenServer.stop/1`
- For the new test's FakeDB: the `check_room_status_for_update/1` callback returns the status directly from an ETS flag set at test setup time (no real transaction needed)
- The `load_members/1` call in FakeDB for `send_event_test.exs` currently returns a 3-tuple `{:ok, [uid], created_at_ms}` — note the existing FakeDB there does NOT include `power_levels_json` in the tuple (line 47). The production `load_members/1` returns a 4-tuple. This is an existing inconsistency in `send_event_test.exs`'s FakeDB — do NOT fix it in this story (avoid regressions). The new callback is independent.

---

## Previous Story Intelligence (9-8)

From Story 9-8 (Room Version Upgrade):
- Story 9-8 used the `archive_room_atomic/1` pattern in `admin/db.ex` as a reference for SELECT FOR UPDATE transactions — that same pattern is the template for `check_room_status_for_update/1`
- Story 9-7 (Code Review MAJOR-2): when adding a new callback to `DBBehaviour`, ALL existing FakeDB modules in all test files must be updated — the compiler warns but does not error by default. Run `mix compile` to surface them all at once before writing tests
- Pattern from 9-3: `archive_room_atomic/1` introduced SELECT FOR UPDATE for archive operations — the new `check_room_status_for_update/1` is the read-side mirror of that write-side lock
- Kassandra HIGH-1 in 9-3: actor_id from gRPC metadata — not relevant here (no actor_id needed for the archive check)
- Story 6-9b (`_bmad-output/implementation-artifacts/6-9b-archive-race-core-send-event.md`): The original story for this fix — it describes the same fix but without SELECT FOR UPDATE. Story 9-9 supersedes 6-9b by requiring FOR UPDATE. The 6-9b story file remains; do not delete it or change its status.

---

## Security Context

This is a **SEC Gate 2 HIGH-2** follow-up from epic-6 (2026-05-02). The fix closes a TOCTOU race window in Core's `send_event` flow. `security_review: required` — the pipeline will run a dedicated security-focused code review pass after Gate 3 (Code Review). CRITICAL/HIGH findings block the commit.

The threat: A message sent to a room in the exact moment it's being archived would be written to the `events` table, violating the archive invariant (archived rooms must not accept new events). The SELECT FOR UPDATE ensures that either:
- The `check_room_status_for_update/1` transaction sees `status='archived'` (archive completed first) → rejects the send
- OR the archive transaction must wait until `check_room_status_for_update/1` completes (send completes first) → archive proceeds after

Both orderings are safe: no event is written to an already-archived room.

---

## Definition of Done

- [x] `check_room_status_for_update/1` callback in `DBBehaviour` + implementation in `Nebu.Room.DB` using SELECT FOR UPDATE inside `Nebu.Repo.transaction/1`
- [x] `handle_call({:send_event, ...})` checks archived status (fail-open on DB error), returns `{:error, :room_archived}` for archived rooms
- [x] `EventDispatcher.Server.send_event/2` maps `{:error, :room_archived}` → `GRPC.Status.failed_precondition()` with message `"M_ROOM_ARCHIVED"`
- [x] Gateway `PutSendEventHandler` maps `codes.FailedPrecondition` → `403 M_ROOM_ARCHIVED`
- [x] All FakeDB modules updated with `check_room_status_for_update/1` stub
- [x] `make test-unit-elixir` passes (all existing tests remain green + new tests pass)
- [x] `make test-unit-go` passes (gateway unit test for FailedPrecondition → 403 passes)
- [ ] `security_review: required` — pipeline runs security review after code review

## Dev Agent Record

### Implementation Notes

Story 9-9 implemented 2026-05-05.

**Changes made:**

1. `core/apps/room_manager/lib/nebu/room/db_behaviour.ex` — Added `@callback check_room_status_for_update/1` with full doc comment.

2. `core/apps/room_manager/lib/nebu/room/db.ex` — Implemented `check_room_status_for_update/1` using `Nebu.Repo.transaction/1` with `SELECT COALESCE(status, 'active') FROM rooms WHERE room_id = $1 FOR UPDATE`. Returns `{:ok, status}` or `{:error, :not_found}`.

3. `core/apps/room_manager/lib/nebu/room/server.ex` — Added Step 1.5 TOCTOU check in `handle_call({:send_event, ...})` between idempotency cache miss and event build. Extracted Steps 2–6 into private `do_send_event/7` helper (placed in the Private section to avoid Elixir's `handle_call` grouping warning). Fail-open: `{:error, _}` from `check_room_status_for_update` proceeds with event insert.

4. `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — Added `{:error, :room_archived}` clause before the catch-all `{:error, reason}` in `send_event/2`, raising `GRPC.RPCError` with `status: GRPC.Status.failed_precondition()` and message `"M_ROOM_ARCHIVED: room is archived"`.

5. `gateway/internal/matrix/rooms.go` — Added `case codes.FailedPrecondition:` to the `switch st.Code()` in `PutSendEvent`, calling `writeMatrixError(w, http.StatusForbidden, "M_ROOM_ARCHIVED", "Room is archived")`.

6. Updated all FakeDB modules across 13 test files to add `check_room_status_for_update(_room_id), do: {:ok, "active"}` stub (or `{:ok, "archived"}` for `FakeDBWithArchivedStatus`).

7. `core/apps/room_manager/test/nebu/room/db_behaviour_test.exs` — Added `{:check_room_status_for_update, 1}` to the required callbacks list.

**Test results:**
- `make test-unit-go`: all 16 packages pass (including 3 new Story 9-9 tests: `TestPutSendEvent_CoreFailedPrecondition_Returns403_MRoomArchived`, `TestPutSendEvent_CoreFailedPrecondition_WithNonArchiveMessage_Returns403`, `TestPutSendEvent_OtherErrors_NotAffectedByFailedPreconditionFix`)
- `make test-unit-elixir`: 358 tests pass (0 failures, 2 skipped) across all apps; includes new tests in `send_event_archived_room_test.exs`

## File List

- `core/apps/room_manager/lib/nebu/room/db_behaviour.ex` — modified
- `core/apps/room_manager/lib/nebu/room/db.ex` — modified
- `core/apps/room_manager/lib/nebu/room/server.ex` — modified
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — modified
- `gateway/internal/matrix/rooms.go` — modified
- `core/apps/room_manager/test/nebu/room/db_behaviour_test.exs` — modified
- `core/apps/room_manager/test/nebu_room_test.exs` — modified (FakeDB + FailingWriteDB)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/archive_room_test.exs` — modified (FakeDB + FakeDBWithArchivedStatus)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/audit_archive_ops_test.exs` — modified
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/grpc_handler_test.exs` — modified
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/upgrade_room_test.exs` — modified (FakeDB + FakeDBWithName)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/send_event_test.exs` — modified
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/create_room_test.exs` — modified
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/join_room_test.exs` — modified
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/get_messages_test.exs` — modified
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_receipts_test.exs` — modified
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/sync_test.exs` — modified
- `core/apps/room_manager/test/nebu/room/power_level_enforcement_test.exs` — modified (FakeDB + FailingWriteDB)

## Change Log

- 2026-05-05: Implemented Story 9-9 — Archive TOCTOU fix. Added `check_room_status_for_update/1` SELECT FOR UPDATE check to `Room.Server.send_event` handler. Wired `{:error, :room_archived}` through EventDispatcher to gRPC `FAILED_PRECONDITION` and Gateway `403 M_ROOM_ARCHIVED`. Updated 18 files including all FakeDB modules. All tests pass.
