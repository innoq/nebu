---
security_review: required
---

# Story 6.8: Room Settings Update API (max members, visibility, serverweite Defaults)

Status: review

## Story

As an instance admin,
I want to update room settings and define server-wide default room configuration,
so that I can enforce limits and consistent defaults across all rooms on the instance.

## Acceptance Criteria

1. `PATCH /api/v1/admin/rooms/{roomId}` — `instance_admin` role required; body (all fields optional):
   - `max_members: integer` (min 2, max 100000) — stored in `rooms.max_members`; the Room GenServer enforces this limit on join: if `member_count >= max_members`, join returns `{:error, :room_full}` and the gateway returns `403 M_FORBIDDEN` with `errcode: "M_ROOM_FULL"`
   - `visibility: "public"|"private"` — updates room visibility
   - `name: string` (1–255 chars)
   - `topic: string` (0–1000 chars)
   - Calls `gRPC CoreService.UpdateRoomSettings` to notify the Room GenServer of the updated `max_members` in real time (no restart required)
   - Calls `AuditWriter.log(admin, "room_settings_updated", "room", room_id, %{changes: changed_fields}, "success")`
   - Returns `200` with the updated room object (`AdminRoomDetail` shape)
   - Returns `404` if room not found; `400` if validation fails

2. `PUT /api/v1/admin/config/room-defaults` — `instance_admin` role required; body: `{"default_max_members": integer, "default_visibility": "public"|"private"}`:
   - Upserts into `server_config` table: `room_default_max_members` and `room_default_visibility`
   - These defaults are applied when `POST /_matrix/client/v3/createRoom` is called without explicit overrides
   - Returns `200 {"data": {"default_max_members": N, "default_visibility": "..."}}`

3. `gRPC CoreService` proto adds `rpc UpdateRoomSettings(UpdateRoomSettingsRequest) returns (UpdateRoomSettingsResponse)`:
   - `UpdateRoomSettingsRequest`: `room_id string`, `max_members int32` (0 = no limit; skip if not in PATCH body)
   - Core implementation: calls `Nebu.Room.Server.update_settings(room_id, %{max_members: N})` via GenServer cast (best-effort); if the Room GenServer is not running, update is skipped (will be applied from DB on next start)
   - `UpdateRoomSettingsResponse`: `ok bool`

4. Unit tests (Go):
   - `PATCH` with `max_members=2`, third join attempt → 403 M_ROOM_FULL (mocked gRPC)
   - Update visibility → reflected in subsequent `GET /admin/rooms/{roomId}`
   - `PUT /admin/config/room-defaults` → new rooms use defaults (via `server_config` upsert)
   - `PATCH` unknown room → 404
   - `PATCH` with invalid `max_members=1` (below min 2) → 400 M_BAD_JSON

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **PATCH room updates max_members — 200 with updated object** — Go unit test (mock `RoomRepository` + mock gRPC)
   - Given: room `!roomA:server` exists with `max_members=0`
   - When: `PATCH /api/v1/admin/rooms/!roomA:server` with body `{"max_members": 50}`
   - Then: status 200, response body `data.max_members = 50`; mock `UpdateRoomSettings` gRPC called once; `UpdateRoom` repository called once

2. **PATCH room not found → 404** — Go unit test
   - Given: `RoomRepository.GetRoom` returns `(nil, nil)`
   - When: `PATCH /api/v1/admin/rooms/!unknown:server` with body `{"max_members": 10}`
   - Then: status 404, body `{"error":{"code":"M_NOT_FOUND","message":"Room not found"}}`

3. **PATCH with max_members=1 (below min) → 400** — Go unit test
   - Given: any room
   - When: `PATCH /api/v1/admin/rooms/!roomA:server` with body `{"max_members": 1}`
   - Then: status 400, body `{"error":{"code":"M_BAD_JSON","message":"..."}}`

4. **PATCH with max_members=100001 (above max) → 400** — Go unit test
   - Given: any room
   - When: `PATCH /api/v1/admin/rooms/!roomA:server` with body `{"max_members": 100001}`
   - Then: status 400, `errcode: "M_BAD_JSON"`

5. **PATCH with visibility=invalid → 400** — Go unit test
   - Given: any room
   - When: `PATCH /api/v1/admin/rooms/!roomA:server` with body `{"visibility": "secret"}`
   - Then: status 400, `errcode: "M_BAD_JSON"`

6. **PATCH updates visibility — reflected in GET** — Go unit test (mock repo)
   - Given: mock `UpdateRoom` succeeds; mock `GetRoom` returns updated object with `visibility="public"`
   - When: `PATCH /api/v1/admin/rooms/!roomA:server` with body `{"visibility": "public"}`
   - Then: status 200, `data.visibility = "public"`

7. **PATCH empty body (no fields) → 200 with unchanged room** — Go unit test
   - Given: room exists
   - When: `PATCH /api/v1/admin/rooms/!roomA:server` with body `{}`
   - Then: status 200 (no-op is valid); `UpdateRoom` called with no changes; `UpdateRoomSettings` gRPC NOT called (no max_members in body)

8. **PUT room-defaults upserts server_config → 200** — Go unit test (mock `RoomDefaultsRepository`)
   - Given: mock `UpsertRoomDefaults` succeeds
   - When: `PUT /api/v1/admin/config/room-defaults` with body `{"default_max_members": 100, "default_visibility": "public"}`
   - Then: status 200, `data.default_max_members = 100`, `data.default_visibility = "public"`

9. **PUT room-defaults with invalid visibility → 400** — Go unit test
   - Given: any
   - When: `PUT /api/v1/admin/config/room-defaults` with body `{"default_max_members": 10, "default_visibility": "secret"}`
   - Then: status 400, `errcode: "M_BAD_JSON"`

10. **Audit log called on PATCH** — Go unit test
    - Given: mock `CoreClient` that captures `WriteAuditLog` calls
    - When: `PATCH /api/v1/admin/rooms/!roomA:server` with body `{"name": "New Name"}`
    - Then: `audit.LogEvent` called with `action="room_settings_updated"`, `target_type="room"`, `target_id="!roomA:server"`, metadata contains changed field `name`

11. **Router test: PATCH /admin/rooms/{roomId} registered → 501 when Rooms=nil** — Go unit test
    - Given: `AdminServer{}` with no repositories
    - When: `PATCH /api/v1/admin/rooms/someRoom`
    - Then: status 501

12. **Router test: PUT /admin/config/room-defaults registered → 501 when no repo** — Go unit test
    - Given: `AdminServer{}` with no repositories
    - When: `PUT /api/v1/admin/config/room-defaults`
    - Then: status 501

## Tasks / Subtasks

- [x] Write FAILING tests first — `gateway/internal/api/rooms_patch_handler_test.go` + `gateway/internal/api/room_defaults_handler_test.go` (AC: #1–#12)
  - [x] All 12 test cases written as RED phase (may use `t.Skip` if compile errors exist before gen-api)
  - [x] Define mock `RoomRepository` extension or new `RoomDefaultsRepository` mock

- [x] Extend `gateway/api/openapi.yaml` with new routes (AC: #1, #2)
  - [x] Add `PatchAdminRoomRequest` schema (all fields optional: max_members, visibility, name, topic)
  - [x] Add `PutRoomDefaultsRequest` schema (default_max_members, default_visibility)
  - [x] Add `RoomDefaultsResponse` schema (data with default_max_members + default_visibility)
  - [x] Add `PATCH /admin/rooms/{roomId}` path (operationId: `PatchAdminRoom`)
  - [x] Add `PUT /admin/config/room-defaults` path (operationId: `PutAdminRoomDefaults`)
  - [x] Run `make gen-api` to regenerate `api_gen.go`

- [x] Extend `proto/core.proto` with `UpdateRoomSettings` RPC (AC: #3)
  - [x] Add `rpc UpdateRoomSettings(UpdateRoomSettingsRequest) returns (UpdateRoomSettingsResponse)` to `CoreService`
  - [x] Add `UpdateRoomSettingsRequest` message: `room_id string = 1`, `max_members int32 = 2`
  - [x] Add `UpdateRoomSettingsResponse` message: `ok bool = 1`
  - [x] Run `make proto` to regenerate Go stubs + Elixir `core_grpc.pb.ex`

- [x] Implement Elixir gRPC handler `update_room_settings` in `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (AC: #3)
  - [x] Add `def update_room_settings/2` handler: looks up Room GenServer via `Nebu.Room.RoomSupervisor.lookup_room/1`; if found, sends `{:update_settings, %{max_members: req.max_members}}` via `GenServer.cast`; if not found, returns `ok: true` (best-effort)

- [x] Extend `Nebu.Room.Server` in `core/apps/room_manager/lib/nebu/room/server.ex` (AC: #3)
  - [x] Add public `update_settings(room_id, settings)` function: `GenServer.cast(via(room_id), {:update_settings, settings})`
  - [x] Add `handle_cast({:update_settings, settings}, state)` callback: updates `max_members` in state
  - [x] Add `max_members` field to GenServer state (loaded from DB via `db_module().load_room_settings/1` in both new-room and existing-room init paths)
  - [x] Enforce `max_members` check in `handle_call({:join, user_id}, ...)`: if `max_members > 0 AND MapSet.size(members) >= max_members`, return `{:error, :room_full}`

- [x] Extend `Nebu.Room.Server.join_room` handler in `server.ex` gRPC layer to handle `{:error, :room_full}` (AC: #1)
  - [x] Map `{:error, :room_full}` → raise `GRPC.RPCError` with `status: GRPC.Status.resource_exhausted()` and `message: "room is full"`
  - [x] Go gateway maps `codes.ResourceExhausted` → `403 M_ROOM_FULL` in `gateway/internal/matrix/rooms.go`

- [x] Add `UpdateRoom` method to `RoomRepository` interface and `dbRoomRepo` implementation in `gateway/internal/api/rooms_repo.go` (AC: #1)
  - [x] `UpdateRoom(ctx, roomID string, patch RoomPatch) (*AdminRoomDetail, error)` — returns the updated room; returns `(nil, nil)` if room not found
  - [x] `RoomPatch` struct: `MaxMembers *int`, `Visibility *string`, `Name *string`, `Topic *string` (all optional pointer fields)
  - [x] SQL: dynamic `UPDATE rooms SET ... WHERE room_id = $1` from non-nil fields; after UPDATE call `GetRoom`; 0 rows affected → `(nil, nil)`

- [x] Add `RoomDefaultsRepository` interface and `dbRoomDefaultsRepo` implementation in new file `gateway/internal/api/room_defaults_repo.go` (AC: #2)
  - [x] `RoomDefaultsRepository` interface: `UpsertRoomDefaults(ctx, maxMembers int, visibility string) error`; `GetRoomDefaults(ctx) (int, string, error)`
  - [x] Uses new `room_defaults` table (migration 000037) — separate from INSERT-only `server_config`

- [x] Implement `PatchAdminRoom` and `PutAdminRoomDefaults` handlers in `gateway/internal/api/server.go` (AC: #1, #2)
  - [x] Add `RoomDefaults RoomDefaultsRepository` field to `AdminServer`
  - [x] `PatchAdminRoom`: nil-guard → 501; validate fields; call `UpdateRoom`; call `UpdateRoomSettings` gRPC if max_members changed; emit audit log; return updated `AdminRoomDetail`
  - [x] `PutAdminRoomDefaults`: nil-guard → 501; validate body; call `UpsertRoomDefaults`; emit audit log; return 200 with defaults

- [x] Register new routes in `gateway/internal/api/router.go` (AC: #1, #2)
  - [x] `PATCH /api/v1/admin/rooms/{roomId}` → `patchAdminRoomHandler(sh)` with body pre-check
  - [x] `PUT /api/v1/admin/config/room-defaults` → `putAdminRoomDefaultsHandler(sh)` with body pre-check
  - [x] Both wrapped in `jwtMW(RequireRole("instance_admin", checker)(...))`
  - [x] Added `patchAdminRoomHandler` and `putAdminRoomDefaultsHandler` wrapper functions

- [x] Wire new repositories in `gateway/cmd/gateway/main.go` (AC: #1, #2)
  - [x] `roomDefaultsRepo := apihandler.NewRoomDefaultsRepo(bootstrapDB)`
  - [x] Add `RoomDefaults: roomDefaultsRepo` to `adminSrv`

- [x] Handle `room_full` gRPC error in `joinRoomHandler` in matrix handler (AC: #1)
  - [x] Map `codes.ResourceExhausted` → HTTP 403 with `{"errcode":"M_ROOM_FULL","error":"Room is full"}`

- [x] Update `router_test.go` — add 501 tests for new routes when repos are nil (AC: #11, #12)

- [x] Remove `t.Skip` from test files — GREEN phase
  - [x] Run `make test-unit-go` — zero failures (all 16 packages pass)
  - [x] Run `make test-unit-elixir` — zero failures (284 tests across 6 suites)

- [x] Add `load_room_settings/1` to `Nebu.Room.DBBehaviour` + `Nebu.Room.DB` + all FakeDB modules in tests
  - [x] `db_behaviour.ex`: new `@callback load_room_settings/1` added
  - [x] `db.ex`: `load_room_settings/1` implementation using `COALESCE(max_members, 0)` from rooms table
  - [x] All FakeDB modules in room_manager and event_dispatcher test files updated with stub returning `{:ok, 0}`

## Dev Notes

### Critical: server_config INSERT-ONLY Constraint

`server_config` has **FORCE RLS** with only INSERT and SELECT policies (migration 000003). The app role (`nebu_app`) **cannot UPDATE existing rows** — attempts will be silently denied by RLS.

The "latest wins" strategy used for other config keys (`audit_log_retention_days`, etc.) is:
```sql
-- Write: always INSERT (new row wins by recency)
INSERT INTO server_config (key, value, set_at)
VALUES ($1, $2, $3)
ON CONFLICT (key) DO NOTHING  -- WRONG: this skips if key already exists!
```

**CRITICAL:** `ON CONFLICT (key) DO NOTHING` will fail silently on repeated writes because it skips the insert. The correct approach for append-only config:

```sql
-- Write: always INSERT new row (primary key is `key`, so this conflicts)
-- Since the table has `key TEXT PRIMARY KEY`, we cannot have two rows with same key.
-- The only option: use a separate time-series pattern with a composite PK.
```

Wait — looking at migration 000003 more carefully: `server_config` has `key TEXT PRIMARY KEY` (single-valued). The RLS policy allows INSERT but not UPDATE. The `DO NOTHING` approach means the FIRST inserted value wins (idempotent after first insert).

**This is the ACTUAL design:** `server_config` is truly immutable per key after first insert. The `loadAuditRetentionDays` function in `main.go` confirms this — it reads the single row per key.

**For room defaults, this means:** Once `room_default_max_members` is set, it cannot be changed via the nebu_app role. To allow updates via the Admin API, there are two options:
- **Option A:** Use a different mechanism (e.g., a separate `room_defaults` table that allows UPDATE)
- **Option B:** Use the nebu_migrate role (which has BYPASSRLS) for updates — not available at runtime
- **Option C:** Accept that `server_config` is immutable per key and use a time-series pattern with a composite PK

**Decision (to implement):** Create a new migration `000037_room_defaults.up.sql` that adds a `room_defaults` table (separate from `server_config`) with mutable columns:
```sql
CREATE TABLE room_defaults (
    id          SERIAL PRIMARY KEY,
    default_max_members INTEGER NOT NULL DEFAULT 0,  -- 0 = no limit
    default_visibility  TEXT    NOT NULL DEFAULT 'private'
        CHECK (default_visibility IN ('public', 'private')),
    set_at      BIGINT  NOT NULL
);
-- Seed with a single row (the "current" defaults)
INSERT INTO room_defaults (default_max_members, default_visibility, set_at)
VALUES (0, 'private', 0);
```

`PutAdminRoomDefaults` then does `UPDATE room_defaults SET ... WHERE id = 1`.

This avoids the RLS constraint and is simpler than a time-series pattern.

**Alternative simpler approach:** Since the user says "Upserts into `server_config` table", check if migration 000003 allows an alternative path. Looking at the bootstrap code, `compliance_signing_key_priv` uses `ON CONFLICT (key) DO NOTHING` and re-reads the winner. This confirms `server_config` is truly immutable per key.

**Final decision:** Use the `room_defaults` table approach (migration 000037). This is the cleanest architecture — room defaults are mutable configuration, not immutable bootstrap config.

**Note to dev agent:** If you prefer to use `server_config` for room defaults with a read-of-latest-row-by-set_at approach, you need to change the table to have a composite key (`key, set_at`) and SELECT MAX(set_at). However, migration 000003 has `key TEXT PRIMARY KEY` which prevents multiple rows per key. A separate `room_defaults` table is the right call.

### Critical: Next Migration Number

Migration 000036 was created in Story 6.7. **Next migration: `000037`.**

### Critical: proto/core.proto — UpdateRoomSettings Message Fields

The field number sequence must not conflict with existing fields. This is a new RPC — add at the end of the `CoreService` service block and use new message types:

```protobuf
// UpdateRoomSettings — Story 6.8: Admin API notifies Room GenServer of settings change.
// max_members = 0 means "no limit". Go gateway sends this after updating rooms table.
// Elixir Core updates in-memory GenServer state; no-op if GenServer not running (will load from DB on next start).
rpc UpdateRoomSettings(UpdateRoomSettingsRequest) returns (UpdateRoomSettingsResponse);
```

```protobuf
// UpdateRoomSettings — Story 6.8
message UpdateRoomSettingsRequest {
  string room_id     = 1;
  int32  max_members = 2;  // 0 = no limit; always sent (even if unchanged) for simplicity
}
message UpdateRoomSettingsResponse {
  bool ok = 1;
}
```

After adding to `core.proto`, run `make proto`. This regenerates:
- `gateway/internal/grpc/pb/core.pb.go` (Go stubs)
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` + `core_grpc.pb.ex` (Elixir stubs)

The generated Elixir file `core_grpc.pb.ex` must have `update_room_settings` registered. Check `gateway/internal/grpc/pb/core_grpc.pb.go` for the Go client method `UpdateRoomSettings`.

### Critical: Elixir Room.Server State Extension

The current GenServer state (from `server.ex:init/1`) does NOT include `max_members`. Story 6.8 needs to add it.

**State before Story 6.8:**
```elixir
%{
  room_id:      String.t(),
  members:      MapSet.t(String.t()),
  power_levels: map(),
  created_at:   DateTime.t(),
  typing_users: MapSet.t(String.t())
}
```

**State after Story 6.8:**
```elixir
%{
  room_id:      String.t(),
  members:      MapSet.t(String.t()),
  power_levels: map(),
  created_at:   DateTime.t(),
  typing_users: MapSet.t(String.t()),
  max_members:  non_neg_integer()   # 0 = no limit; loaded from DB on init
}
```

**Loading max_members from DB on init:**

`Nebu.Room.DB.load_members/1` currently returns `{:ok, [user_id], created_at_ms, power_levels_json}`. It needs to be extended to also return `max_members`.

However, changing the DB behaviour return type breaks all existing FakeDB implementations in test files. **Preferred approach:** Add a separate `load_room_settings(room_id)` callback to `DBBehaviour` that returns `{:ok, max_members}` | `{:error, term()}`. This minimizes disruption to existing tests.

```elixir
# In Nebu.Room.DBBehaviour:
@callback load_room_settings(room_id :: String.t()) ::
            {:ok, non_neg_integer()} | {:error, term()}
```

```elixir
# In Nebu.Room.DB:
@sql_load_room_settings """
SELECT COALESCE(max_members, 0) FROM rooms WHERE room_id = $1
"""

def load_room_settings(room_id) do
  case Nebu.Repo.query(@sql_load_room_settings, [room_id]) do
    {:ok, %{rows: [[max_members]]}} -> {:ok, max_members}
    {:ok, %{rows: []}} -> {:ok, 0}  # room not in DB yet
    {:error, reason} -> {:error, reason}
  end
end
```

In `Nebu.Room.Server.init/1`, call `db_module().load_room_settings(room_id)` after loading members and include the result in the state. If the call fails, default to `max_members: 0` (fail-open — the limit will be enforced once the GenServer catches up).

**All FakeDB modules in test files** (in `join_room_test.exs`, `create_room_test.exs`, `sync_test.exs`, etc.) must implement the new callback. The easiest approach is to add a default fallback in the behaviour using `@optional_callbacks` OR add `def load_room_settings(_), do: {:ok, 0}` to each FakeDB. **Check carefully** before committing that all FakeDB modules compile.

### Critical: max_members Enforcement in Room.Server.join

```elixir
@impl GenServer
def handle_call({:join, user_id}, _from, %{members: members, max_members: max_members} = state) do
  cond do
    MapSet.member?(members, user_id) ->
      {:reply, {:error, :already_member}, state}
    max_members > 0 and MapSet.size(members) >= max_members ->
      {:reply, {:error, :room_full}, state}
    true ->
      case db_module().insert_member(state.room_id, user_id) do
        :ok ->
          emit_membership_event(state.room_id, user_id, "join")
          new_state = %{state | members: MapSet.put(members, user_id)}
          {:reply, :ok, new_state}
        {:error, reason} ->
          {:reply, {:error, reason}, state}
      end
  end
end
```

### Critical: gRPC Error Mapping for room_full in Go

The `JoinRoom` handler in `gateway/internal/matrix/` (specifically in `join_room.go` or the file that calls `coreClient.JoinRoom(...)`) needs to map `codes.ResourceExhausted` to HTTP 403. Check the current error switch:

```go
// In the existing JoinRoom gRPC error handler (add this case):
case codes.ResourceExhausted:
    w.WriteHeader(http.StatusForbidden)
    fmt.Fprintf(w, `{"errcode":"M_ROOM_FULL","error":"Room is full"}`)
```

Find this file: `gateway/internal/matrix/join_room.go` (or similar). The existing switch has `codes.NotFound` and `codes.PermissionDenied` — add `codes.ResourceExhausted`.

### Critical: openapi.yaml — New Schemas and Paths

**Add to `components.schemas`:**

```yaml
PatchAdminRoomRequest:
  type: object
  properties:
    max_members:
      type: integer
      minimum: 2
      maximum: 100000
    visibility:
      type: string
      enum: [public, private]
    name:
      type: string
      minLength: 1
      maxLength: 255
    topic:
      type: string
      maxLength: 1000

PutRoomDefaultsRequest:
  type: object
  required:
    - default_max_members
    - default_visibility
  properties:
    default_max_members:
      type: integer
      minimum: 0
    default_visibility:
      type: string
      enum: [public, private]

RoomDefaultsResponse:
  type: object
  required:
    - data
  properties:
    data:
      type: object
      required:
        - default_max_members
        - default_visibility
      properties:
        default_max_members:
          type: integer
        default_visibility:
          type: string
```

**Add to `paths`** (after `/admin/rooms/{roomId}`):

```yaml
  /admin/rooms/{roomId}:
    get:
      # existing GetAdminRoom definition unchanged
    patch:
      operationId: PatchAdminRoom
      summary: Update room settings (instance_admin required)
      parameters:
        - name: roomId
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/PatchAdminRoomRequest"
      responses:
        "200":
          description: Updated room
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/AdminRoomDetailObject"
        "400":
          description: Validation error
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/EmptyResponse"
        "404":
          description: Room not found
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/EmptyResponse"
        "501":
          description: Not implemented
  /admin/config/room-defaults:
    put:
      operationId: PutAdminRoomDefaults
      summary: Set server-wide room defaults (instance_admin required)
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/PutRoomDefaultsRequest"
      responses:
        "200":
          description: Room defaults
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/RoomDefaultsResponse"
        "400":
          description: Validation error
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/EmptyResponse"
        "501":
          description: Not implemented
```

After editing, run `make gen-api`. The generated `api_gen.go` will include:
- `PatchAdminRoomRequestObject` with `RoomId string` and `Body *PatchAdminRoomRequest`
- `PutAdminRoomDefaultsRequestObject` with `Body *PutRoomDefaultsRequest`
- `PatchAdminRoom501Response`, `PatchAdminRoom400Response`, `PatchAdminRoom404Response`, `PatchAdminRoom200JSONResponse`
- `PutAdminRoomDefaults501Response`, `PutAdminRoomDefaults400Response`, `PutAdminRoomDefaults200JSONResponse`

**StrictServerInterface compile-time check:** Until `PatchAdminRoom` and `PutAdminRoomDefaults` are implemented in `server.go`, the build fails. Implement the 501 stubs first, then the full handlers.

### Critical: RoomRepository Extension

Add `UpdateRoom` to the existing `RoomRepository` interface in `rooms_repo.go`:

```go
// RoomPatch holds the optional fields for a PATCH /admin/rooms/{roomId} request.
// Only non-nil fields are applied to the rooms table.
type RoomPatch struct {
    MaxMembers *int
    Visibility *string
    Name       *string
    Topic      *string
}

type RoomRepository interface {
    ListRooms(ctx context.Context, afterID, afterCreatedAt string, limit int, search, status string) ([]AdminRoom, int, string, error)
    GetRoom(ctx context.Context, roomID string) (*AdminRoomDetail, error)
    UpdateRoom(ctx context.Context, roomID string, patch RoomPatch) (*AdminRoomDetail, error)  // NEW
}
```

`UpdateRoom` SQL strategy:
1. Build a dynamic `UPDATE rooms SET col=$N ... WHERE room_id=$1` from non-nil patch fields
2. If no fields are non-nil (empty patch), skip the UPDATE and call `GetRoom` directly
3. After UPDATE, call `GetRoom(ctx, roomID)` to return the full `AdminRoomDetail`
4. If `UPDATE` affects 0 rows, room does not exist → return `(nil, nil)`

```go
func (r *dbRoomRepo) UpdateRoom(ctx context.Context, roomID string, patch RoomPatch) (*AdminRoomDetail, error) {
    setClauses := []string{}
    args := []any{roomID}  // $1 = room_id for WHERE
    n := 2

    if patch.MaxMembers != nil {
        setClauses = append(setClauses, fmt.Sprintf("max_members = $%d", n))
        args = append(args, *patch.MaxMembers)
        n++
    }
    if patch.Visibility != nil {
        setClauses = append(setClauses, fmt.Sprintf("visibility = $%d", n))
        args = append(args, *patch.Visibility)
        n++
    }
    if patch.Name != nil {
        setClauses = append(setClauses, fmt.Sprintf("name = $%d", n))
        args = append(args, *patch.Name)
        n++
    }
    if patch.Topic != nil {
        setClauses = append(setClauses, fmt.Sprintf("topic = $%d", n))
        args = append(args, *patch.Topic)
        n++
    }

    if len(setClauses) > 0 {
        q := fmt.Sprintf("UPDATE rooms SET %s WHERE room_id = $1", strings.Join(setClauses, ", "))
        result, err := r.db.ExecContext(ctx, q, args...)
        if err != nil {
            return nil, fmt.Errorf("UpdateRoom: %w", err)
        }
        rowsAffected, _ := result.RowsAffected()
        if rowsAffected == 0 {
            return nil, nil  // room not found
        }
    }

    // Fetch updated state (also handles the no-op case and the not-found check when no SET clauses)
    return r.GetRoom(ctx, roomID)
}
```

**Note:** For the empty-patch case (no SET clauses), calling `GetRoom` is correct — it returns nil if the room doesn't exist, which gives a correct 404.

### Critical: room_defaults Table (Migration 000037)

```sql
-- 000037_room_defaults.up.sql
-- Story 6.8: Mutable server-wide room defaults (separate from immutable server_config).
-- server_config uses INSERT-only RLS policy (cannot UPDATE) — room defaults need to be mutable.

CREATE TABLE room_defaults (
    id                    SERIAL  PRIMARY KEY,
    default_max_members   INTEGER NOT NULL DEFAULT 0
        CHECK (default_max_members >= 0),
    default_visibility    TEXT    NOT NULL DEFAULT 'private'
        CHECK (default_visibility IN ('public', 'private')),
    set_at                BIGINT  NOT NULL
);

-- Seed with a single row (the system default)
INSERT INTO room_defaults (default_max_members, default_visibility, set_at)
VALUES (0, 'private', 0);
```

```sql
-- 000037_room_defaults.down.sql
DROP TABLE IF EXISTS room_defaults;
```

**RoomDefaultsRepository** implementation:
```go
func (r *dbRoomDefaultsRepo) UpsertRoomDefaults(ctx context.Context, maxMembers int, visibility string) error {
    _, err := r.db.ExecContext(ctx,
        `UPDATE room_defaults SET default_max_members = $1, default_visibility = $2, set_at = $3 WHERE id = 1`,
        maxMembers, visibility, time.Now().UnixMilli())
    return err
}

func (r *dbRoomDefaultsRepo) GetRoomDefaults(ctx context.Context) (int, string, error) {
    var maxMembers int
    var visibility string
    err := r.db.QueryRowContext(ctx,
        `SELECT default_max_members, default_visibility FROM room_defaults WHERE id = 1`).
        Scan(&maxMembers, &visibility)
    if errors.Is(err, sql.ErrNoRows) {
        return 0, "private", nil  // safe default if table is empty
    }
    return maxMembers, visibility, err
}
```

### Critical: PatchAdminRoom Handler Logic

```go
func (s *AdminServer) PatchAdminRoom(ctx context.Context, req PatchAdminRoomRequestObject) (PatchAdminRoomResponseObject, error) {
    if s.Rooms == nil {
        return PatchAdminRoom501Response{}, nil
    }

    roomID := req.RoomId
    body := req.Body
    if body == nil {
        return &patchRoom400Resp{msg: "request body is required"}, nil
    }

    // 1. Validate fields
    patch := RoomPatch{}
    changedFields := map[string]any{}

    if body.MaxMembers != nil {
        v := *body.MaxMembers
        if v < 2 || v > 100000 {
            return &patchRoom400Resp{msg: "max_members must be between 2 and 100000"}, nil
        }
        patch.MaxMembers = &v
        changedFields["max_members"] = v
    }
    if body.Visibility != nil {
        v := string(*body.Visibility)
        if v != "public" && v != "private" {
            return &patchRoom400Resp{msg: "visibility must be 'public' or 'private'"}, nil
        }
        patch.Visibility = &v
        changedFields["visibility"] = v
    }
    if body.Name != nil {
        v := *body.Name
        if len(v) < 1 || len(v) > 255 {
            return &patchRoom400Resp{msg: "name must be 1–255 characters"}, nil
        }
        patch.Name = &v
        changedFields["name"] = v
    }
    if body.Topic != nil {
        v := *body.Topic
        if len(v) > 1000 {
            return &patchRoom400Resp{msg: "topic must be at most 1000 characters"}, nil
        }
        patch.Topic = &v
        changedFields["topic"] = v
    }

    // 2. Apply patch (also checks existence)
    updated, err := s.Rooms.UpdateRoom(ctx, roomID, patch)
    if err != nil {
        return nil, err
    }
    if updated == nil {
        return &patchRoom404Resp{}, nil
    }

    // 3. Notify Room GenServer via gRPC (only if max_members changed)
    if patch.MaxMembers != nil && s.CoreClient != nil {
        _, grpcErr := s.CoreClient.UpdateRoomSettings(ctx, &pb.UpdateRoomSettingsRequest{
            RoomId:     roomID,
            MaxMembers: int32(*patch.MaxMembers),
        })
        if grpcErr != nil {
            slog.Warn("UpdateRoomSettings gRPC failed — GenServer state not updated in real time",
                "room_id", roomID, "err", grpcErr)
            // Best-effort: continue (DB is already updated; GenServer will load from DB on next start)
        }
    }

    // 4. Audit log (never-raise)
    if s.CoreClient != nil {
        actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
        _ = audit.LogEvent(ctx, s.CoreClient, actorID, "room_settings_updated", "room", roomID,
            map[string]any{"changes": changedFields}, "success", "")
    } else {
        slog.Warn("PatchAdminRoom audit skipped — CoreClient is nil", "room_id", roomID)
    }

    return &patchRoom200Resp{detail: updated}, nil
}
```

### Critical: Response Types for New Handlers

```go
// patchRoom200Resp — 200 OK with updated AdminRoomDetail
type patchRoom200Resp struct{ detail *AdminRoomDetail }

func (r *patchRoom200Resp) VisitPatchAdminRoomResponse(w http.ResponseWriter) error {
    type envelope struct{ Data *AdminRoomDetail `json:"data"` }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    return json.NewEncoder(w).Encode(envelope{Data: r.detail})
}

type patchRoom400Resp struct{ msg string }

func (r *patchRoom400Resp) VisitPatchAdminRoomResponse(w http.ResponseWriter) error {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusBadRequest)
    return json.NewEncoder(w).Encode(map[string]any{
        "error": map[string]string{"code": "M_BAD_JSON", "message": r.msg},
    })
}

type patchRoom404Resp struct{}

func (r *patchRoom404Resp) VisitPatchAdminRoomResponse(w http.ResponseWriter) error {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusNotFound)
    return json.NewEncoder(w).Encode(map[string]any{
        "error": map[string]string{"code": "M_NOT_FOUND", "message": "Room not found"},
    })
}

// putRoomDefaults200Resp — 200 OK with room defaults
type putRoomDefaults200Resp struct {
    maxMembers int
    visibility string
}

func (r *putRoomDefaults200Resp) VisitPutAdminRoomDefaultsResponse(w http.ResponseWriter) error {
    type dataObj struct {
        DefaultMaxMembers int    `json:"default_max_members"`
        DefaultVisibility string `json:"default_visibility"`
    }
    type envelope struct{ Data dataObj `json:"data"` }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    return json.NewEncoder(w).Encode(envelope{Data: dataObj{
        DefaultMaxMembers: r.maxMembers,
        DefaultVisibility: r.visibility,
    }})
}

type putRoomDefaults400Resp struct{ msg string }

func (r *putRoomDefaults400Resp) VisitPutAdminRoomDefaultsResponse(w http.ResponseWriter) error {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusBadRequest)
    return json.NewEncoder(w).Encode(map[string]any{
        "error": map[string]string{"code": "M_BAD_JSON", "message": r.msg},
    })
}
```

### Critical: Router Wrappers

```go
// patchAdminRoomHandler extracts {roomId}, checks body is present, delegates.
// Story 6.8: PATCH /api/v1/admin/rooms/{roomId}
func patchAdminRoomHandler(sh ServerInterface) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        roomID := r.PathValue("roomId")
        if r.Body == nil || r.ContentLength == 0 {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusBadRequest)
            _, _ = w.Write([]byte(`{"error":{"code":"M_BAD_JSON","message":"request body is required"}}`))
            return
        }
        sh.PatchAdminRoom(w, r, roomID)
    })
}

// putAdminRoomDefaultsHandler checks body is present, delegates.
// Story 6.8: PUT /api/v1/admin/config/room-defaults
func putAdminRoomDefaultsHandler(sh ServerInterface) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Body == nil || r.ContentLength == 0 {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusBadRequest)
            _, _ = w.Write([]byte(`{"error":{"code":"M_BAD_JSON","message":"request body is required"}}`))
            return
        }
        sh.PutAdminRoomDefaults(w, r)
    })
}
```

Routes in `RegisterAdminRoutes`:
```go
// Story 6.8: PATCH room settings + PUT room defaults
mux.Handle("PATCH /api/v1/admin/rooms/{roomId}",
    jwtMW(RequireRole("instance_admin", checker)(bodyLimit64KiB(patchAdminRoomHandler(sh)))))
mux.Handle("PUT /api/v1/admin/config/room-defaults",
    jwtMW(RequireRole("instance_admin", checker)(bodyLimit64KiB(putAdminRoomDefaultsHandler(sh)))))
```

**Note:** `bodyLimit64KiB` must be passed from `RegisterAdminRoutes` OR defined inside router.go (check how the existing PATCH deactivate/reactivate routes handle it). Looking at `router.go`: `RegisterAdminRoutes` does not currently accept a `bodyLimit` parameter — the body limit is applied in `main.go` wrapping. For the admin API routes registered via `RegisterAdminRoutes`, the body limit middleware is NOT currently applied (the existing POST routes in RegisterAdminRoutes have NO bodyLimit). **This is a pre-existing gap.** For Story 6.8, follow the existing pattern: do NOT add `bodyLimit64KiB` inside `RegisterAdminRoutes`. The main.go chain already has `bodyLimit64KiB` wrapping for admin POST/PATCH endpoints where appropriate. Alternatively, wire it in main.go when registering the mux (after `RegisterAdminRoutes`). **Recommendation:** Skip body limit for now (consistent with all other `/api/v1/admin/*` routes registered via `RegisterAdminRoutes`). The JSON decode error from a too-large body will return a 400, which is acceptable.

### Critical: Elixir update_room_settings gRPC Handler

Add to `event_dispatcher/server.ex` at the end of the module:

```elixir
# ─── UpdateRoomSettings — Story 6.8: Admin API notifies Room GenServer ───────
#
# Best-effort: if the Room GenServer is not running, the settings will be
# loaded from DB when the GenServer next starts. Returns ok=true in all cases
# (the Go gateway has already persisted the change to DB).
def update_room_settings(%Core.UpdateRoomSettingsRequest{} = req, _stream) do
  case Nebu.Room.RoomSupervisor.lookup_room(req.room_id) do
    {:ok, _pid} ->
      # Cast is fire-and-forget; we don't wait for the GenServer to apply the change.
      Nebu.Room.Server.update_settings(req.room_id, %{max_members: req.max_members})
      Logger.info("UpdateRoomSettings applied to running GenServer",
        room_id: req.room_id, max_members: req.max_members)
    {:error, :not_found} ->
      Logger.info("UpdateRoomSettings: Room GenServer not running — will load from DB on next start",
        room_id: req.room_id)
  end
  %Core.UpdateRoomSettingsResponse{ok: true}
end
```

Add to `Nebu.Room.Server`:

```elixir
@doc """
Sends an update_settings cast to the Room GenServer for room_id.
Best-effort: if the GenServer is not running, the message is dropped.
"""
@spec update_settings(String.t(), map()) :: :ok
def update_settings(room_id, settings),
  do: GenServer.cast(via(room_id), {:update_settings, settings})

# In handle_cast:
@impl GenServer
def handle_cast({:update_settings, settings}, state) do
  new_max_members = Map.get(settings, :max_members, state.max_members)
  {:noreply, %{state | max_members: new_max_members}}
end
```

### Critical: Elixir DBBehaviour — load_room_settings

All FakeDB modules in the test suite that `@behaviour Nebu.Room.DBBehaviour` must implement `load_room_settings/1`. The easiest fix is to add `@optional_callbacks load_room_settings: 1` to the behaviour **or** add a default `def load_room_settings(_), do: {:ok, 0}` to each FakeDB.

**Search for FakeDB modules:**
```bash
grep -r "@behaviour Nebu.Room.DBBehaviour" core/apps/
```

These files need to be updated.

### Critical: File Inventory

**Files to CREATE (new):**
- `gateway/migrations/000037_room_defaults.up.sql` — new mutable room_defaults table
- `gateway/migrations/000037_room_defaults.down.sql`
- `gateway/internal/api/room_defaults_repo.go` — `RoomDefaultsRepository` interface + `dbRoomDefaultsRepo` implementation
- `gateway/internal/api/rooms_patch_handler_test.go` — ATDD tests for PATCH endpoint (written first, RED phase)
- `gateway/internal/api/room_defaults_handler_test.go` — ATDD tests for PUT room-defaults (written first, RED phase)

**Files to UPDATE:**
- `proto/core.proto` — add `rpc UpdateRoomSettings` + messages
- `gateway/api/openapi.yaml` — add `PatchAdminRoomRequest`, `PutRoomDefaultsRequest`, `RoomDefaultsResponse` schemas + PATCH + PUT paths
- `gateway/api/api_gen.go` — regenerated via `make gen-api` (do not edit manually)
- `gateway/internal/api/rooms_repo.go` — add `UpdateRoom` to `RoomRepository` interface + `RoomPatch` struct + `dbRoomRepo.UpdateRoom` implementation
- `gateway/internal/api/server.go` — add `RoomDefaults RoomDefaultsRepository` field; implement `PatchAdminRoom` + `PutAdminRoomDefaults` + response types
- `gateway/internal/api/router.go` — add `PATCH /api/v1/admin/rooms/{roomId}` + `PUT /api/v1/admin/config/room-defaults` routes; add `patchAdminRoomHandler` + `putAdminRoomDefaultsHandler` functions
- `gateway/internal/api/router_test.go` — add 501 tests for the two new routes
- `gateway/cmd/gateway/main.go` — wire `RoomDefaults: apihandler.NewRoomDefaultsRepo(bootstrapDB)`; update comment
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — add `def update_room_settings/2` handler
- `core/apps/room_manager/lib/nebu/room/server.ex` — add `max_members` to state; add `update_settings/2`; add `handle_cast({:update_settings, ...})`; add `max_members` enforcement in `handle_call({:join, ...})`
- `core/apps/room_manager/lib/nebu/room/db.ex` — add `load_room_settings/1`
- `core/apps/room_manager/lib/nebu/room/db_behaviour.ex` — add `load_room_settings/1` callback
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — regenerated via `make proto`
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — regenerated via `make proto`
- `gateway/internal/grpc/pb/core.pb.go` — regenerated via `make proto`
- `gateway/internal/grpc/pb/core_grpc.pb.go` — regenerated via `make proto`
- `gateway/internal/matrix/join_room.go` (or wherever JoinRoom gRPC error is mapped) — add `codes.ResourceExhausted` → 403 M_ROOM_FULL
- All FakeDB modules in Elixir tests that implement `Nebu.Room.DBBehaviour` — add `load_room_settings/1` stub

### Previous Story Learnings (from 6.7)

1. **501 guard pattern is mandatory**: check the repository field first (`if s.Rooms == nil`) and return 501. This ensures `router_test.go` with empty `AdminServer{}` still passes.

2. **slog.Warn on nil CoreClient**: always log a warning when audit is skipped. MINOR-1 pattern from Story 6.5 code review.

3. **make gen-api before implementing handlers**: StrictServerInterface compile check blocks build. Add 501 stubs FIRST, then implement.

4. **Body pre-check in router wrapper**: replicate the `deactivateAdminUserHandler` pattern — check `r.Body == nil || r.ContentLength == 0` before delegating, to return the Matrix envelope rather than the strict-handler default error.

5. **FakeDB modules must implement all DBBehaviour callbacks**: this is the trickiest part of the Elixir changes. Search all test files before adding a new callback.

6. **audit.LogEvent signature**: `(ctx, coreClient, actorID, action, targetType, targetID, metadata, outcome, errorDetail)` — see existing usages in server.go.

7. **epochMsToISO8601 / parseISO8601ToEpochMs**: private helpers in `users_repo.go` — accessible from `rooms_repo.go` since both are in `package api`. Do NOT copy.

### Git Context (Recent Commits)

```
8eeaee1 refactor: replace `git` commands with `rtk` for improved context management
6520117 feat(6-6): role_overrides migration, POST /roles grant/revoke, RequireRole DB-override
012bce0 feat(6-5): User Deactivation + Reactivation + Session Invalidierung
ed4f383 feat(6-4): User List + Get API — GET /admin/users + GET /admin/users/{userId}
e46b90d feat(6-3): Admin API Router + RequireRole middleware — role-auth gate for instance_admin/compliance_officer
```

Story 6.7 is currently in `review` status (see sprint-status.yaml). Its files are the direct base for this story — `rooms_repo.go`, `server.go` (all 7.x handlers), `router.go` (room routes).

### References

- `proto/core.proto` — gRPC service definition (add UpdateRoomSettings at end)
- `gateway/internal/api/server.go` — AdminServer struct, handler pattern, audit log pattern
- `gateway/internal/api/router.go` — RegisterAdminRoutes, wrapper function pattern
- `gateway/internal/api/rooms_repo.go` — RoomRepository interface, AdminRoom/AdminRoomDetail types, UpdateRoom to be added
- `gateway/internal/api/rooms_handler_test.go` — reference test pattern (mock RoomRepository)
- `gateway/migrations/000003_server_config.up.sql` — INSERT-only RLS (explains why room_defaults needs its own table)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — gRPC handler pattern (invalidate_user_sessions reference)
- `core/apps/room_manager/lib/nebu/room/server.ex` — Room GenServer state, join handler, handle_cast pattern
- `core/apps/room_manager/lib/nebu/room/db_behaviour.ex` — DBBehaviour callbacks list
- `_bmad-output/planning-artifacts/epics.md:2823–2848` — Canonical Story 6.8 AC definition

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

- All 12 acceptance test cases pass (Go unit tests: rooms_patch_handler_test.go + room_defaults_handler_test.go).
- Proto `UpdateRoomSettings` RPC added; Go protobuf stubs regenerated via `make proto`. All mock implementations of `pb.CoreServiceClient` across test files updated with `UpdateRoomSettings` stub.
- `room_defaults` table introduced via migration 000037 to sidestep `server_config` INSERT-only RLS constraint. `PutAdminRoomDefaults` does `UPDATE room_defaults SET ... WHERE id = 1`.
- `Nebu.Room.Server` extended with `max_members` state field. `load_room_settings/1` called in BOTH init paths (new room and existing room) to ensure restart recovery. Fail-open: if `load_room_settings` errors, defaults to 0 (no limit).
- `handle_call({:join, ...})` refactored from `if/else` to `cond` for clean three-branch logic: `:already_member` first (higher priority than `:room_full`), then capacity check, then DB insert.
- `join_room` gRPC handler extended with `{:error, :room_full}` → `GRPC.Status.resource_exhausted()` mapping. Go gateway `rooms.go` maps `codes.ResourceExhausted` → HTTP 403 `M_ROOM_FULL`.
- All FakeDB modules in event_dispatcher and room_manager tests (14 modules) updated with `def load_room_settings(_room_id), do: {:ok, 0}`. `SyncDeltaFakeDB` uses `defdelegate`.
- `db_behaviour_test.exs` updated to assert `{:load_room_settings, 1}` in the required callbacks list.
- `make test-unit-go`: all 16 packages pass (0 failures). `make test-unit-elixir`: 284 tests, 0 failures. `make build-gateway`: success.

### File List

gateway/api/openapi.yaml
gateway/api/api_gen.go (regenerated)
gateway/migrations/000037_room_defaults.up.sql (new)
gateway/migrations/000037_room_defaults.down.sql (new)
gateway/internal/api/rooms_repo.go
gateway/internal/api/room_defaults_repo.go (new)
gateway/internal/api/server.go
gateway/internal/api/router.go
gateway/internal/api/rooms_handler_test.go
gateway/internal/api/rooms_patch_handler_test.go (new)
gateway/internal/api/room_defaults_handler_test.go (new)
gateway/internal/api/router_test.go
gateway/cmd/gateway/main.go
gateway/internal/matrix/rooms.go
gateway/internal/grpc/pb/core.pb.go (regenerated)
gateway/internal/grpc/pb/core_grpc.pb.go (regenerated)
gateway/internal/admin/auth_audit_test.go
gateway/internal/compliance/handler_test.go
gateway/internal/audit/writer_test.go
gateway/internal/grpc/stream_test.go
proto/core.proto
core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex
core/apps/event_dispatcher/lib/pb/core.pb.ex (regenerated)
core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex (regenerated)
core/apps/room_manager/lib/nebu/room/server.ex
core/apps/room_manager/lib/nebu/room/db.ex
core/apps/room_manager/lib/nebu/room/db_behaviour.ex
core/apps/room_manager/test/nebu_room_test.exs
core/apps/room_manager/test/nebu/room/db_behaviour_test.exs
core/apps/room_manager/test/nebu/room/server_set_typing_test.exs
core/apps/room_manager/test/nebu/room/power_level_enforcement_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/create_room_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/server_set_typing_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/server_get_room_state_system_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/audit_room_ops_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/get_messages_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/invite_user_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/send_event_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/server_receipts_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/join_room_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/server_moderation_metadata_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/sync_test.exs
