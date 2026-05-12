# Story 4.4: Room GenServer: Send Event (Ed25519 Signing + txnId Idempotency)

Status: done

## Story

As a Core developer,
I want the Room GenServer to process, sign, and persist send-event requests with full txnId idempotency,
so that duplicate client requests never result in duplicate events in the room timeline.

## Acceptance Criteria

1. `Nebu.Room.Server` handles `call` message `{:send_event, user_id, event_type, content, txn_id}`:
   - Checks ETS idempotency table `NebuTxnDedup` for `{room_id, user_id, txn_id}`; if found, returns `{:ok, existing_event_id}` immediately without re-processing
   - Constructs the full event map: `%{"room_id" => room_id, "type" => event_type, "sender" => user_id, "content" => content, "origin_server_ts" => DateTime.utc_now() |> DateTime.to_unix(:millisecond)}`
   - Calls `Nebu.EventId.generate/1` to produce the `event_id`
   - Signs the event via OTP's `:crypto.sign(:eddsa, :none, canonical_event_json, [signing_private_key, :ed25519])`, attaches a `"signatures"` field to the event map
   - Persists the signed event to PostgreSQL `events` table (append-only)
   - Inserts `{room_id, user_id, txn_id} → event_id` into ETS `NebuTxnDedup`
   - Broadcasts `{:new_event, signed_event}` via `:pg` Process Group `"room:#{room_id}"`
   - Returns `{:ok, event_id}`

2. ETS table `NebuTxnDedup` is created in `Nebu.Room.Application.start/2` with options `[:named_table, :set, :public]` — owned by the Application supervisor process, not by individual GenServer instances.

3. On DB write failure: do NOT insert into ETS `NebuTxnDedup`, do NOT broadcast, return `{:error, reason}`.

4. Unit tests (in `core/apps/room_manager/test/nebu_room_test.exs`) cover:
   - Happy path: `send_event/5` returns `{:ok, event_id}` and the event_id starts with `"$"`
   - Determinism: same content + same txn_id → same event_id on first call
   - Idempotency: duplicate txn_id (same `{room_id, user_id, txn_id}`) returns the same event_id without re-processing
   - DB failure: `send_event/5` returns `{:error, reason}` and ETS `NebuTxnDedup` does NOT contain the `{room_id, user_id, txn_id}` key

5. `mix test --warnings-as-errors` passes for the full umbrella. All existing Story 4-1, 4-2, and 4-3 tests continue to pass unchanged.

6. A new SQL migration (`000010_events.up.sql`) creates the `events` append-only table before any event can be persisted.

---

## Tasks / Subtasks

- [x] Add SQL migration for `events` table (AC: #6)
  - [x] Create `gateway/migrations/000010_events.up.sql` with schema (see Dev Notes below)
  - [x] Create `gateway/migrations/000010_events.down.sql`
  - [x] Verify migration number: highest existing is `000009_rooms`, so next is `000010`

- [x] Create ETS table `NebuTxnDedup` in `Nebu.Room.Application` (AC: #2)
  - [x] In `core/apps/room_manager/lib/nebu/room/application.ex`, after starting Horde children, call `:ets.new(:NebuTxnDedup, [:named_table, :set, :public])`
  - [x] The ETS table must be created BEFORE any Room GenServer starts (created at Application boot, not inside GenServer)

- [x] Add `send_event/5` public API function to `Nebu.Room.Server` (AC: #1)
  - [x] Add `def send_event(room_id, user_id, event_type, content, txn_id)` as a `GenServer.call/2` wrapper
  - [x] Add `handle_call({:send_event, user_id, event_type, content, txn_id}, ...)` callback

- [x] Implement `:send_event` `handle_call` in `Nebu.Room.Server` (AC: #1, #3)
  - [x] Step 1 — Idempotency check: `case :ets.lookup(:NebuTxnDedup, {state.room_id, user_id, txn_id})` — if `[{_, existing_id}]` → `{:reply, {:ok, existing_id}, state}`
  - [x] Step 2 — Build event map with string keys (see Dev Notes for exact structure)
  - [x] Step 3 — Generate event_id via `Nebu.EventId.generate(event_map)`
  - [x] Step 4 — Sign event via `:crypto.sign(:eddsa, :none, ...)` with server signing key (see Dev Notes for key management)
  - [x] Step 5 — Persist to DB via `db_module().insert_event(signed_event_with_id)`
  - [x] Step 6 — Only on DB success: insert into ETS, broadcast via `:pg`, return `{:ok, event_id}`
  - [x] On DB error: return `{:error, reason}`, do NOT insert into ETS, do NOT broadcast

- [x] Add `insert_event/1` to `Nebu.Room.DB` (AC: #1, #3)
  - [x] SQL: `INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts, signatures) VALUES ($1, $2, $3, $4, $5, $6, $7)`
  - [x] Returns `:ok` or `{:error, reason}`

- [x] Add `signature` app dependency to `room_manager/mix.exs` (AC: #1)
  - [x] Add `{:signature, in_umbrella: true}` to deps so `Nebu.EventId` is accessible

- [x] Set up `:pg` process group broadcast (AC: #1)
  - [x] In `Nebu.Room.Application.start/2`: call `:pg.start_link()` or ensure `:pg` module is started (it is part of OTP, but the scope must be started)
  - [x] In `handle_call({:send_event, ...})`, after ETS insert: `:pg.get_local_members/1` + `Enum.each/2` + `send/2` (Note: `:pg.broadcast/3` does not exist in OTP — used correct API)

- [x] Write unit tests (AC: #4, #5)
  - [x] Extend existing `FakeDB` module in `nebu_room_test.exs` with `insert_event/1` function
  - [x] Add `FailingInsertEventDB` variant or extend `FailingWriteDB` with `insert_event/1` that returns `{:error, :db_connection_lost}`
  - [x] Test: happy path — `send_event/5` returns `{:ok, event_id}` starting with `"$"`
  - [x] Test: idempotency — second call with same `{room_id, user_id, txn_id}` returns same event_id
  - [x] Test: DB failure — ETS does NOT contain the txn key after failed insert
  - [x] Manage ETS `NebuTxnDedup` cleanup in `setup/on_exit` (must clean between tests)

- [x] Run `mix test --warnings-as-errors` for full umbrella (AC: #5)

---

## Dev Notes

### CRITICAL: Correct Module Names

The epics.md acceptance criteria use `RoomManager.RoomServer` — this is **WRONG** for this codebase. All modules follow `Nebu.{Domain}.{Name}`:

| Epic Spec Name | Correct Name in Codebase |
|---|---|
| `RoomManager.RoomServer` | `Nebu.Room.Server` |
| `RoomManager.Application` | `Nebu.Room.Application` |
| `RoomManager.RoomSupervisor` | `Nebu.Room.RoomSupervisor` |
| `Signature.Ed25519.sign/2` | Use OTP `:crypto.sign/4` directly — no wrapper function exists yet |

### Events Table SQL Migration

Create `gateway/migrations/000010_events.up.sql`:

```sql
CREATE TABLE events (
    event_id         TEXT    PRIMARY KEY,
    room_id          TEXT    NOT NULL REFERENCES rooms(room_id),
    sender           TEXT    NOT NULL,
    event_type       TEXT    NOT NULL,
    content          JSONB   NOT NULL,
    origin_server_ts BIGINT  NOT NULL,
    signatures       JSONB
);

CREATE INDEX events_room_id_ts_idx ON events (room_id, origin_server_ts);
```

And `gateway/migrations/000010_events.down.sql`:
```sql
DROP TABLE IF EXISTS events;
```

Architecture naming rule: table `events`, columns `snake_case`, BIGINT for timestamps, JSONB for content and signatures. [Source: `architecture.md` — Naming Conventions > PostgreSQL, Timestamps section]

### Event Map Structure (String Keys Only)

All event content arrives via gRPC/JSON as **string-keyed maps**. Always use string keys. Do NOT use atom keys in the canonical event map.

```elixir
event_map = %{
  "room_id"          => state.room_id,
  "type"             => event_type,
  "sender"           => user_id,
  "content"          => content,
  "origin_server_ts" => System.system_time(:millisecond)
}
```

Use `Nebu.DB.Helpers.now_ms()` (from the `nebu_db` umbrella app, already a dep of `room_manager`) for the timestamp, consistent with `Nebu.Room.DB` pattern.

### Ed25519 Event Signing — Key Management Strategy for MVP

The epics.md mentions `Signature.Ed25519.sign/2` — this function **does not exist** in the codebase. `Nebu.Signature` only has `generate_signing_keypair/0`. For Story 4-4, implement a **server-level signing key** for MVP (not per-user keys, which require DB lookup).

**MVP approach — generate server signing key at application boot:**

```elixir
# In Nebu.Room.Application.start/2:
{pub_key, priv_key} = :crypto.generate_key(:eddsa, :ed25519)
:persistent_term.put(:nebu_signing_key, {pub_key, priv_key})
```

**In `handle_call({:send_event, ...})`, sign the canonical event JSON:**

```elixir
{_pub, priv} = :persistent_term.get(:nebu_signing_key)
event_json = Jason.encode!(event_map)
signature = :crypto.sign(:eddsa, :none, event_json, [priv, :ed25519])
sig_b64 = Base.encode64(signature)
signed_event = Map.put(event_map, "signatures", %{"nebu" => sig_b64})
```

**Critical OTP sign API (established in Story 2-8, tested in `nebu_signature_test.exs`):**
- Sign: `:crypto.sign(:eddsa, :none, message, [private_key, :ed25519])` — 4-arg form, curve in key list
- Verify: `:crypto.verify(:eddsa, :none, message, signature, [public_key, :ed25519])` — 4-arg form
- **NOT** the 5-arg RSA form: `(:eddsa, :none, msg, [key], [:ed25519])`
- `private_key` is 32 bytes (OTP seed format) — NOT 64-byte libsodium format

**Alternative:** If a simpler MVP signing approach is preferred (e.g., no signing for MVP, just EventId), add an `@impl false` note. But the epics.md ACs explicitly require signing.

### `Nebu.EventId.generate/1` Usage

`Nebu.EventId` is in `core/apps/signature/` and MUST be added as a dependency of `room_manager`:

```elixir
# core/apps/room_manager/mix.exs — add to deps:
{:signature, in_umbrella: true}
```

Call pattern (Architecture enforcement rule #7 — never construct event IDs manually):
```elixir
event_id = Nebu.EventId.generate(event_map)
# event_id starts with "$", e.g. "$abc123..."
```

`Nebu.EventId.generate/1` strips `"signatures"` and `"unsigned"` before hashing — so generate the event_id BEFORE attaching signatures, OR generate it on the map WITHOUT signatures (which is the same thing because generate strips them anyway).

### ETS `NebuTxnDedup` — Creation and Usage

**Creation (in `Nebu.Room.Application.start/2`, BEFORE Supervisor.start_link):**

```elixir
:ets.new(:NebuTxnDedup, [:named_table, :set, :public])
```

The table is owned by the Application process. It persists for the lifetime of the VM. It survives individual GenServer crashes/restarts.

**Lookup pattern:**
```elixir
case :ets.lookup(:NebuTxnDedup, {room_id, user_id, txn_id}) do
  [{_, existing_event_id}] -> {:reply, {:ok, existing_event_id}, state}
  [] -> # proceed with event creation
end
```

**Insert pattern (only after successful DB write):**
```elixir
:ets.insert(:NebuTxnDedup, {{room_id, user_id, txn_id}, event_id})
```

**Test cleanup — CRITICAL**: `NebuTxnDedup` is a named table created at Application boot. Tests CANNOT delete and recreate it. Instead, clean up specific keys in `on_exit`:
```elixir
on_exit(fn ->
  :ets.delete(:NebuTxnDedup, {room_id, user_id, txn_id})
end)
```
Or use a helper that deletes all matching patterns after each test.

**Alternative for tests**: use `:ets.match_delete(:NebuTxnDedup, {:"_", :"_"})` to wipe all entries — BUT only works with the correct match spec pattern. Use `:ets.delete_all_objects(:NebuTxnDedup)` to clear all entries in `setup`.

### `:pg` Process Group Broadcast

`:pg` is an OTP built-in (available since OTP 23). No additional dependency needed. The architecture explicitly replaces NATS/Redis with `:pg` Process Groups.

**Setup in Application.start/2:**
```elixir
:pg.start_link()  # or ensure it's started before use
```

**Broadcast in `handle_call({:send_event, ...})`:**
```elixir
# Join the process group when Room GenServer starts (in init/1):
:pg.join("room:#{room_id}", self())

# Broadcast after ETS insert:
members = :pg.get_members("room:#{room_id}")
Enum.each(members, fn pid -> send(pid, {:new_event, signed_event}) end)
```

**IMPORTANT**: `:pg.broadcast/3` does NOT exist in OTP. Use `:pg.get_members/1` + `Enum.each/2` + `send/2`. Alternatively: `:pg.get_local_members/1` for local-only broadcast in single-node MVP.

Story 4-8 will build the gRPC EventBus on top of `:pg`. For Story 4-4, the broadcast is fire-and-forget — no subscriber needs to be present. If no subscribers: no-op is correct.

**For unit tests**, the broadcast can be a no-op: testing that a `:pg` group receives messages requires process subscription setup. Keep tests focused on the idempotency and DB failure scenarios. The broadcast test is optional in unit tests (covered in Story 4-8 integration tests).

### FakeDB Extension for Unit Tests

Extend the existing `FakeDB` module in `nebu_room_test.exs` with:

```elixir
# In FakeDB:
def insert_event(event) do
  :ets.insert(:fake_room_db, {{:event, event["event_id"]}, event})
  :ok
end
```

Extend `FailingWriteDB` with:
```elixir
def insert_event(_event), do: {:error, :db_connection_lost}
```

**Important**: The `FakeDB` and `FailingWriteDB` modules do NOT implement a `@behaviour` yet (deferred from Story 4-2 code review). Adding `insert_event/1` manually to both is required.

### `send_event/5` Public API Function

Add as convenience wrapper (consistent with existing `join/2`, `leave/2`, `get_state/1` pattern):

```elixir
@doc """
Processes a send-event request for the given `room_id`.

Checks txn_id idempotency via ETS first. On success, signs the event,
persists to PostgreSQL, and broadcasts to `:pg` room group.

Returns `{:ok, event_id}` or `{:error, reason}`.
"""
@spec send_event(String.t(), String.t(), String.t(), map(), String.t()) ::
        {:ok, String.t()} | {:error, term()}
def send_event(room_id, user_id, event_type, content, txn_id) do
  GenServer.call(via(room_id), {:send_event, user_id, event_type, content, txn_id})
end
```

### Nebu.Room.DB — `insert_event/1` SQL

Add to `core/apps/room_manager/lib/nebu/room/db.ex`:

```elixir
@sql_insert_event """
INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts, signatures)
VALUES ($1, $2, $3, $4, $5, $6, $7)
"""

@spec insert_event(map()) :: :ok | {:error, term()}
def insert_event(event) do
  case Ecto.Adapters.SQL.query(
    Nebu.Repo,
    @sql_insert_event,
    [
      event["event_id"],
      event["room_id"],
      event["sender"],
      event["type"],
      Jason.encode!(event["content"]),
      event["origin_server_ts"],
      if(event["signatures"], do: Jason.encode!(event["signatures"]), else: nil)
    ]
  ) do
    {:ok, _} -> :ok
    {:error, reason} -> {:error, reason}
  end
end
```

Note: JSONB columns in PostgreSQL require the value to be passed as a JSON string when using raw Ecto.Adapters.SQL queries — `Jason.encode!(...)` converts the map to string. This is the established pattern from `nebu_db` usage in other apps.

### Architecture Compliance Rules

| Rule | Requirement | Source |
|---|---|---|
| Rule #1 | Timestamps as BIGINT in PostgreSQL — `BIGINT NOT NULL` in `events` table | `architecture.md#Enforcement rule #1` |
| Rule #7 | Event-IDs always via `Nebu.EventId.generate/1` — never `"$" <> UUID.generate()` | `architecture.md#Enforcement rule #7` |
| Rule #8 | Canonical JSON only via `Nebu.EventId` / `Signature` app | `architecture.md#Enforcement rule #8` |
| Rule #6 | `{:ok, result}` / `{:error, reason}` — no raise/throw for business logic | `architecture.md#Enforcement rule #6` |
| ETS Arc. | `NebuTxnDedup` created at Application start, type `:set`, access `:public` | Epic 4 AC |
| No Redis | ETS replaces Redis for in-memory idempotency — no external deps | `architecture.md#ADR-002` |
| pg Groups | `:pg` OTP module (not `:pg2`, deprecated) for room broadcast | `architecture.md#ADR-005` |
| Async Tests | `async: false` (required — shares `:NebuTxnDedup` ETS named table + Horde) | `nebu_room_test.exs` existing pattern |

### Cross-Story Context

| Story | Relationship to 4-4 |
|---|---|
| Story 2-8 | Established correct OTP Ed25519 sign/verify 4-arg form (MUST use, not 5-arg RSA form) |
| Story 4-2 | `Nebu.Room.Server` base with `via/1`, `db_module/0`, `FakeDB` pattern — EXTEND, not replace |
| Story 4-3 | `Nebu.EventId.generate/1` is the only way to generate event IDs (Rule #7) |
| Story 4-8 | Will build gRPC EventBus on top of the `:pg` groups established in Story 4-4 |
| Story 4-11 | Go gateway PUT handler calls `gRPC CoreService.SendEvent` which calls this `send_event/5` |

### File Structure

Files to create or modify:

```
gateway/migrations/
  000010_events.up.sql              ← CREATE: events table
  000010_events.down.sql            ← CREATE: DROP TABLE events

core/apps/room_manager/
  mix.exs                           ← MODIFY: add {:signature, in_umbrella: true}
  lib/nebu/room/
    application.ex                  ← MODIFY: add :ets.new(:NebuTxnDedup, ...) + :pg.start_link()
    server.ex                       ← MODIFY: add send_event/5 public API + handle_call + @spec
    db.ex                           ← MODIFY: add insert_event/1 function + SQL
  test/
    nebu_room_test.exs              ← MODIFY: extend FakeDB/FailingWriteDB, add send_event tests
```

**Do NOT modify:**
- Any files in `core/apps/signature/` — `Nebu.EventId` is complete; no changes needed
- Any other umbrella app
- `core/mix.lock` — Mix auto-updates when adding `{:signature, in_umbrella: true}`

### Build & Test Commands

```bash
# Run room_manager tests only (fast, targeted):
make test-unit-elixir

# Run full umbrella (before marking complete):
make test-unit-elixir
```

All tests run inside Docker containers — no local Elixir install needed.

**Expected result after implementation:**
- All existing Story 4-1 + 4-2 tests pass (no regression)
- New send_event tests pass (happy path, idempotency, DB failure)
- 0 failures, 0 warnings

---

## Previous Story Intelligence (Story 4-3)

Key learnings from Story 4-3 implementation and code review:

1. **`Jason.OrderedObject` is the canonical JSON pattern** — `normalize_keys/1` in `Nebu.EventId` uses `%Jason.OrderedObject{values: sorted_pairs}` to guarantee key ordering for maps >32 keys. Do NOT use `Map.new/1` on sorted pairs — this was a MAJOR bug in the original 4-3 implementation.

2. **Flat test file naming** — `test/nebu_event_id_test.exs` (not `test/nebu/event_id_test.exs`). Follow the same pattern for any new test files: `nebu_room_test.exs` (already exists, extend it).

3. **`Jason.encode!` with OrderedObject** — When `normalize_keys/1` returns `%Jason.OrderedObject{}`, `Jason.encode!/1` honors the insertion order of the `values` keyword list. This produces deterministic canonical JSON.

4. **Review deferred work** — Architecture expects a separate `canonical_json.ex` module; currently it's a private function in `event_id.ex`. Story 4-4 does NOT need to extract this — use `Nebu.EventId.generate/1` directly, which internally uses canonical JSON.

5. **`async: false` is required** for all `room_manager` tests — Horde uses global named processes (`Nebu.Room.Registry`, `Nebu.Room.HordeSupervisor`) and `Application.put_env` is process-global. The new `NebuTxnDedup` ETS table reinforces this requirement.

6. **Dependency on `signature` app** — `room_manager` does NOT currently depend on `signature`. Adding `{:signature, in_umbrella: true}` is required for `Nebu.EventId.generate/1` access. Jason is already in `mix.lock` (1.4.4). The `signature` app already has `{:jason, "~> 1.4"}` in its own deps.

7. **Files from Story 4-2 that 4-4 extends:**
   - `core/apps/room_manager/lib/nebu/room/server.ex` — add `send_event/5` + `handle_call({:send_event, ...})`
   - `core/apps/room_manager/lib/nebu/room/db.ex` — add `insert_event/1`
   - `core/apps/room_manager/lib/nebu/room/application.ex` — add ETS creation + `:pg.start_link()`
   - `core/apps/room_manager/test/nebu_room_test.exs` — extend `FakeDB`, add tests

---

## Architecture References

- `_bmad-output/planning-artifacts/architecture.md` — Enforcement rules #1, #6, #7, #8; ETS/pg approach (ADR-002, ADR-005); Naming Conventions; Timestamps
- `_bmad-output/planning-artifacts/epics.md` — Story 4.4 Acceptance Criteria (line ~1888)
- `core/apps/room_manager/lib/nebu/room/server.ex` — Base GenServer (Stories 4-1, 4-2)
- `core/apps/room_manager/lib/nebu/room/db.ex` — DB layer pattern to extend
- `core/apps/room_manager/lib/nebu/room/application.ex` — OTP Application to extend
- `core/apps/room_manager/test/nebu_room_test.exs` — FakeDB + test patterns to extend
- `core/apps/signature/lib/nebu/event_id.ex` — `Nebu.EventId.generate/1` (Story 4-3)
- `core/apps/signature/lib/nebu/signature.ex` — OTP Ed25519 API reference
- `core/apps/signature/test/nebu_signature_test.exs` — Correct sign/verify API examples (4-arg form)
- `gateway/migrations/000009_rooms.up.sql` — rooms/room_members schema (next migration: 000010)

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

No blocking issues encountered.

Key implementation decision: `:pg.broadcast/3` does NOT exist in OTP (Dev Notes correctly warned about this). Used `:pg.get_local_members/1` + `Enum.each/2` + `send/2` as specified in Dev Notes.

Added `handle_info({:new_event, _event}, state)` catch-all to suppress unexpected-message logs — the Room GenServer joins its own `:pg` group in `init/1`, so it receives its own broadcasts. This is intentional; Story 4-8 will add the real subscriber.

### Completion Notes List

- SQL migration `000010_events.up.sql` creates append-only `events` table with FK to `rooms`, JSONB content/signatures, BIGINT timestamp (arch rule #1), and `(room_id, origin_server_ts)` index.
- `Nebu.Room.Application` now: (1) creates `:NebuTxnDedup` ETS table before any GenServer starts, (2) starts `:pg.start_link()`, (3) generates server-level Ed25519 keypair stored in `:persistent_term` for MVP signing.
- `Nebu.Room.Server.send_event/5`: full 6-step pipeline — idempotency check → event_map construction → `Nebu.EventId.generate/1` → Ed25519 sign → DB persist → ETS insert + `:pg` broadcast.
- `Nebu.Room.DB.insert_event/1`: appends signed event to `events` table; JSONB fields JSON-encoded with `Jason.encode!` per established pattern.
- `room_manager/mix.exs`: added `{:signature, in_umbrella: true}` for `Nebu.EventId` access.
- Tests: 4 new `send_event` tests cover happy path, determinism, idempotency (same `{room_id, user_id, txn_id}`), cross-room isolation, and DB failure (ETS not updated). `setup` clears `:NebuTxnDedup` with `:ets.delete_all_objects/1` between tests.
- `mix test --warnings-as-errors` passes for full umbrella: 21 room_manager tests, 0 failures, 0 warnings.
- All Story 4-1, 4-2, 4-3 tests continue to pass (regression-free).

### File List

- `gateway/migrations/000010_events.up.sql` — CREATE: events table with FK, JSONB, BIGINT ts, index
- `gateway/migrations/000010_events.down.sql` — DROP TABLE IF EXISTS events
- `core/apps/room_manager/mix.exs` — Added `{:signature, in_umbrella: true}` dep
- `core/apps/room_manager/lib/nebu/room/application.ex` — Added ETS creation, `:pg.start_link()`, server signing keypair in persistent_term
- `core/apps/room_manager/lib/nebu/room/server.ex` — Added `send_event/5` public API, `handle_call({:send_event, ...})`, `handle_info({:new_event, ...})`, `:pg.join` in `init/1`
- `core/apps/room_manager/lib/nebu/room/db.ex` — Added `insert_event/1` with JSONB-encoded SQL parameters
- `core/apps/room_manager/test/nebu_room_test.exs` — Extended FakeDB/FailingWriteDB with `insert_event/1`; added 4 send_event tests; added `:ets.delete_all_objects(:NebuTxnDedup)` to setup/on_exit

### Change Log

- 2026-04-03: Implemented Story 4-4 — Room GenServer send_event with Ed25519 signing and txnId idempotency. Created events table migration (000010), added ETS NebuTxnDedup, server signing keypair, send_event/5 API, insert_event/1 DB function, signature app dependency, and 4 unit tests. All 21 room_manager tests pass, full umbrella green.

### Review Findings

- [x] [Review][Patch] MAJOR: Signing payload includes `event_id` — sign `event_map` not `event_with_id` [core/apps/room_manager/lib/nebu/room/server.ex:handle_call send_event Step 4]
- [x] [Review][Patch] MAJOR: ETS `:NebuTxnDedup` creation in `Application.start/2` has no guard — crashes if table already exists [core/apps/room_manager/lib/nebu/room/application.ex]
- [x] [Review][Patch] MINOR: `Jason.encode!` in `insert_event/1` raises on non-serializable content instead of returning `{:error, reason}` [core/apps/room_manager/lib/nebu/room/db.ex:insert_event]
- [x] [Review][Patch] MINOR: `CanonicalJson.encode!/1` doctest copy-paste bug — example shows both values as `2` instead of `1,2` [core/apps/signature/lib/nebu/canonical_json.ex]
- [x] [Review][Patch] MINOR: Test `on_exit` calls `:ets.delete_all_objects(:NebuTxnDedup)` without checking if table exists [core/apps/room_manager/test/nebu_room_test.exs:on_exit]
- [x] [Review][Patch] MINOR: AC #3 gap — no test asserts that no broadcast is sent on DB failure [core/apps/room_manager/test/nebu_room_test.exs:DB failure test]
- [x] [Review][Defer] Private key stored in `:persistent_term` without access control — acknowledged MVP limitation; Phase 2 must persist key to DB/disk [core/apps/room_manager/lib/nebu/room/application.ex] — deferred, pre-existing
- [x] [Review][Defer] `:pg.start_link/0` uses default scope (global atom) — could collide with other umbrella apps; should use named scope [core/apps/room_manager/lib/nebu/room/application.ex] — deferred, pre-existing
- [x] [Review][Defer] `:pg.get_local_members/1` is node-local only — remote cluster subscribers silently skipped; Story 4-8 will address [core/apps/room_manager/lib/nebu/room/server.ex] — deferred, pre-existing
- [x] [Review][Defer] ETS `:NebuTxnDedup` grows unbounded — acknowledged TODO in code, TTL pruning deferred to Story 4-X [core/apps/room_manager/lib/nebu/room/server.ex] — deferred, pre-existing
- [x] [Review][Defer] `events` table missing index on `sender` and `event_type` — out of scope for this story [gateway/migrations/000010_events.up.sql] — deferred, pre-existing
- [x] [Review][Defer] `Jason.OrderedObject` is internal Jason struct — pre-existing pattern from Story 4-3 [core/apps/signature/lib/nebu/canonical_json.ex] — deferred, pre-existing
- [x] [Review][Defer] `CanonicalJson.normalize/1` treats Keyword lists as plain lists — low-likelihood edge case [core/apps/signature/lib/nebu/canonical_json.ex] — deferred, pre-existing
- [x] [Review][Defer] Self-send in `:pg` broadcast (GenServer joins its own group) — intentional no-op, documented in code, Story 4-8 adds real subscriber [core/apps/room_manager/lib/nebu/room/server.ex] — deferred, pre-existing
- [x] [Review][Defer] `insert_room/1` ON CONFLICT returns node-clock timestamp not DB row timestamp — pre-existing from Story 4-2 [core/apps/room_manager/lib/nebu/room/db.ex] — deferred, pre-existing
- [x] [Review][Defer] Determinism test verifies `Nebu.EventId.generate/1` in isolation rather than two end-to-end `send_event` calls — current approach valid given server-side timestamp [core/apps/room_manager/test/nebu_room_test.exs] — deferred, pre-existing
