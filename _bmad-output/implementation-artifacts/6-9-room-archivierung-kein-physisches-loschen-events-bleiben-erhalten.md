---
security_review: required
---

# Story 6.9: Room Archivierung (kein physisches Löschen, Events bleiben erhalten)

Status: review

## Story

As an instance admin,
I want to archive rooms that are no longer needed,
so that their history is preserved for compliance purposes while users can no longer send new messages.

## Acceptance Criteria

1. `POST /api/v1/admin/rooms/{roomId}/archive` — `instance_admin` role required; body: `{"reason": "..."}` (required, min 10 chars):
   - Sets `rooms.status = 'archived'`, `rooms.archived_at = NOW()`, `rooms.archive_reason = reason`
   - Calls `gRPC CoreService.ArchiveRoom(room_id)` → Core stops the Room GenServer via `Horde.DynamicSupervisor.terminate_child/2`; GenServer is NOT restarted (because `init/1` checks `rooms.status` on start and returns `{:stop, :normal}` if status is `'archived'`)
   - After archival: `PUT /_matrix/client/v3/rooms/{roomId}/send/*` returns `403 M_FORBIDDEN` with `errcode: "M_ROOM_ARCHIVED"`; `GET /messages` and `/sync` continue to work (read-only access, no GenServer required)
   - Events in the `events` table are NOT deleted; the append-only constraint is maintained
   - Calls `AuditWriter.log(admin, "room_archived", "room", room_id, %{reason: reason}, "success")`
   - Returns `200 {"room_id": "...", "status": "archived"}`
   - Returns `409` if room is already archived
   - Returns `404` if room does not exist

2. `POST /api/v1/admin/rooms/{roomId}/unarchive` — `instance_admin` role required:
   - Sets `rooms.status = 'active'`, clears `rooms.archived_at` (sets to NULL)
   - Calls `gRPC CoreService.UnarchiveRoom(room_id)` → Core restarts the Room GenServer via `RoomSupervisor.start_room/1`
   - Calls `AuditWriter.log(admin, "room_unarchived", "room", room_id, nil, "success")`
   - Returns `200 {"room_id": "...", "status": "active"}`
   - Returns `409` if room is already active (not archived)
   - Returns `404` if room does not exist

3. `gRPC CoreService` proto adds:
   - `rpc ArchiveRoom(ArchiveRoomRequest) returns (ArchiveRoomResponse)`
   - `rpc UnarchiveRoom(UnarchiveRoomRequest) returns (UnarchiveRoomResponse)`
   - `ArchiveRoomRequest`: `room_id string = 1`
   - `ArchiveRoomResponse`: `ok bool = 1`
   - `UnarchiveRoomRequest`: `room_id string = 1`
   - `UnarchiveRoomResponse`: `ok bool = 1`

4. Gateway's `PutSendEvent` handler must check `rooms.status` before calling Core's `SendEvent` gRPC:
   - If `rooms.status = 'archived'` → return `403 M_FORBIDDEN` with `{"errcode":"M_ROOM_ARCHIVED","error":"Room is archived"}`
   - Check is a DB query: `SELECT status FROM rooms WHERE room_id = $1`
   - `GET /messages` and `/sync` — no change; read-only endpoints do not check room status

5. Unit tests (Go):
   - POST archive → 200, status="archived" in DB
   - POST archive already-archived → 409
   - POST archive unknown room → 404
   - POST unarchive → 200, status="active" in DB
   - POST unarchive not-archived room → 409
   - PUT send-event on archived room → 403 M_ROOM_ARCHIVED
   - GET messages on archived room → 200 (read-only still works)

6. Unit tests (Elixir):
   - `archive_room` gRPC handler: Room GenServer is running → `Horde.DynamicSupervisor.terminate_child` called; returns `ok: true`
   - `archive_room` gRPC handler: Room GenServer not running → returns `ok: true` (no-op, room was already not running)
   - `unarchive_room` gRPC handler: calls `RoomSupervisor.start_room/1`; returns `ok: true`
   - Room.Server `init/1`: when `rooms.status = 'archived'` in DB → returns `{:stop, :normal}` (prevents Horde restart loop)

7. `go build ./...`, `make test-unit-go`, and `make test-unit-elixir` pass with zero failures after this story.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **POST archive → 200, room status archived** — Go unit test (mock `RoomRepository` + mock gRPC)
   - Given: room `!roomA:server` exists with `status="active"`
   - When: `POST /api/v1/admin/rooms/!roomA:server/archive` with body `{"reason": "No longer needed"}`
   - Then: status 200, body `{"room_id":"!roomA:server","status":"archived"}`; mock `ArchiveRoom` DB call invoked; mock `ArchiveRoom` gRPC called

2. **POST archive already-archived → 409** — Go unit test
   - Given: room `!roomA:server` has `status="archived"` in DB
   - When: `POST /api/v1/admin/rooms/!roomA:server/archive` with body `{"reason": "No longer needed"}`
   - Then: status 409, `{"error":{"code":"M_CONFLICT","message":"Room is already archived"}}`

3. **POST archive unknown room → 404** — Go unit test
   - Given: no room with id `!doesnotexist:server`
   - When: `POST /api/v1/admin/rooms/!doesnotexist:server/archive`
   - Then: status 404, `{"error":{"code":"M_NOT_FOUND","message":"Room not found"}}`

4. **POST archive short reason → 400** — Go unit test
   - Given: room exists
   - When: `POST /api/v1/admin/rooms/!roomA:server/archive` with body `{"reason": "short"}`
   - Then: status 400, `{"error":{"code":"M_BAD_JSON","message":"reason must be at least 10 characters"}}`

5. **POST unarchive → 200, room status active** — Go unit test
   - Given: room `!roomA:server` has `status="archived"`
   - When: `POST /api/v1/admin/rooms/!roomA:server/unarchive`
   - Then: status 200, body `{"room_id":"!roomA:server","status":"active"}`; mock `UnarchiveRoom` gRPC called; `UnarchiveRoom` DB update called

6. **POST unarchive not-archived room → 409** — Go unit test
   - Given: room `!roomA:server` has `status="active"`
   - When: `POST /api/v1/admin/rooms/!roomA:server/unarchive`
   - Then: status 409, `{"error":{"code":"M_CONFLICT","message":"Room is not archived"}}`

7. **PUT send-event on archived room → 403 M_ROOM_ARCHIVED** — Go unit test
   - Given: `rooms.status = 'archived'` for `!roomA:server`; `StatusChecker.GetRoomStatus` mock returns `"archived"`
   - When: `PUT /_matrix/client/v3/rooms/!roomA:server/send/m.room.message/txn1`
   - Then: status 403, `{"errcode":"M_ROOM_ARCHIVED","error":"Room is archived"}`

8. **Router test: POST /admin/rooms/{roomId}/archive registered → 501 when Rooms=nil** — Go unit test
   - Given: `AdminServer{}` with no repositories
   - When: `POST /api/v1/admin/rooms/someRoom/archive` with valid body
   - Then: status 501

9. **Router test: POST /admin/rooms/{roomId}/unarchive registered → 501 when Rooms=nil** — Go unit test
   - Given: `AdminServer{}` with no repositories
   - When: `POST /api/v1/admin/rooms/someRoom/unarchive`
   - Then: status 501

10. **Elixir: archive_room gRPC — GenServer running → terminate_child called** — ExUnit
    - Given: Room GenServer running for `!roomA:server`
    - When: `ArchiveRoom` gRPC called with `room_id: "!roomA:server"`
    - Then: `Horde.DynamicSupervisor.terminate_child` called; returns `%Core.ArchiveRoomResponse{ok: true}`

11. **Elixir: archive_room gRPC — GenServer not running → ok: true (no-op)** — ExUnit
    - Given: No running Room GenServer for `!roomA:server`
    - When: `ArchiveRoom` gRPC called
    - Then: returns `%Core.ArchiveRoomResponse{ok: true}` without error

12. **Elixir: unarchive_room gRPC → start_room called** — ExUnit
    - Given: any room_id
    - When: `UnarchiveRoom` gRPC called with `room_id: "!roomA:server"`
    - Then: `RoomSupervisor.start_room/1` called; returns `%Core.UnarchiveRoomResponse{ok: true}`

13. **Elixir: Room.Server init on archived room → {:stop, :normal}** — ExUnit (crash/restart test)
    - Given: FakeDB where `get_room_status/1` returns `{:ok, "archived"}`
    - When: `Nebu.Room.Server.start_link("!archived:server")` called (simulating Horde restart)
    - Then: GenServer stops immediately with `:normal` reason (no crash loop)

## Tasks / Subtasks

- [x] Write FAILING tests first (RED phase) — `gateway/internal/api/rooms_archive_handler_test.go` (AC: #1–#9)
  - [x] All 9 Go test cases (archive, archive-already-archived, archive-404, archive-bad-reason, unarchive, unarchive-409, send-event-archived, router-501 archive, router-501 unarchive)
  - [x] Define mock `RoomRepository` extension with `ArchiveRoom` + `UnarchiveRoom` + `GetRoomStatus`
  - [x] Mock `ArchiveRoomCoreClient` and `UnarchiveRoomCoreClient` interfaces (or extend existing mock)

- [x] Write FAILING Elixir tests — `core/apps/event_dispatcher/test/nebu/event_dispatcher/archive_room_test.exs` + Room.Server init test (AC: #10–#13)
  - [x] 4 ExUnit tests for archive/unarchive gRPC handlers and Room.Server archived-init guard

- [x] Extend `gateway/api/openapi.yaml` with new routes (AC: #1, #2)
  - [x] Add `ArchiveRoomRequest` schema (reason: string, minLength: 10)
  - [x] Add `ArchiveRoomResponse` schema (room_id: string, status: string)
  - [x] Add `UnarchiveRoomResponse` schema (room_id: string, status: string)
  - [x] Add `POST /admin/rooms/{roomId}/archive` path (operationId: `ArchiveAdminRoom`)
  - [x] Add `POST /admin/rooms/{roomId}/unarchive` path (operationId: `UnarchiveAdminRoom`)
  - [x] Run `make gen-api` to regenerate `api_gen.go`

- [x] Extend `proto/core.proto` with `ArchiveRoom` and `UnarchiveRoom` RPCs (AC: #3)
  - [x] Add `rpc ArchiveRoom(ArchiveRoomRequest) returns (ArchiveRoomResponse)` to `CoreService`
  - [x] Add `rpc UnarchiveRoom(UnarchiveRoomRequest) returns (UnarchiveRoomResponse)` to `CoreService`
  - [x] Add message types: `ArchiveRoomRequest {string room_id = 1;}`, `ArchiveRoomResponse {bool ok = 1;}`, `UnarchiveRoomRequest {string room_id = 1;}`, `UnarchiveRoomResponse {bool ok = 1;}`
  - [x] Run `make proto` to regenerate Go stubs + Elixir stubs

- [x] Add `archived_at` column to `rooms` table via migration `000038` (AC: #1, #2)
  - [x] **NOTE:** `archived_at` already exists in the `rooms` table from migration 000009 (as a BIGINT nullable column — legacy column). No new migration is needed for this column.
  - [x] Confirmed: `status TEXT NOT NULL DEFAULT 'active'`, `archived_at BIGINT`, `archive_reason TEXT` all already exist from migrations 000009 + 000036. No new migration needed.

- [x] Extend `RoomRepository` interface + `dbRoomRepo` in `gateway/internal/api/rooms_repo.go` (AC: #1, #2)
  - [x] Add `ArchiveRoom(ctx, roomID, reason string) (*ArchiveResult, error)` — conditional UPDATE WHERE status='active' RETURNING; 0 rows → SELECT EXISTS to distinguish 404 vs 409
  - [x] Add `UnarchiveRoom(ctx, roomID string) (*UnarchiveResult, error)` — conditional UPDATE WHERE status='archived' RETURNING; same 404/409 logic
  - [x] Sentinel errors: `ErrRoomNotFound` and `ErrRoomWrongStatus`

- [x] Add `GetRoomStatus` to `RoomRepository` for use by `SendEventHandler` (AC: #4, #7)
  - [x] `GetRoomStatus(ctx, roomID string) (string, error)` — `SELECT COALESCE(status, 'active') FROM rooms WHERE room_id = $1`; sql.ErrNoRows → return ""

- [x] Implement `ArchiveAdminRoom` and `UnarchiveAdminRoom` handlers in `gateway/internal/api/server.go` (AC: #1, #2)
  - [x] Both handlers: nil-guard → 501; body validation; DB update; gRPC call (best-effort); audit log; return response
  - [x] Response types: `archiveRoom200Resp`, `archiveRoom400Resp`, `archiveRoom404Resp`, `archiveRoom409Resp`, `unarchiveRoom200Resp`, `unarchiveRoom404Resp`, `unarchiveRoom409Resp`

- [x] Register new routes in `gateway/internal/api/router.go` (AC: #1, #2)
  - [x] `POST /api/v1/admin/rooms/{roomId}/archive` → `archiveAdminRoomHandler(sh, adminServer)` — nil-Rooms guard first, then body pre-check
  - [x] `POST /api/v1/admin/rooms/{roomId}/unarchive` → `unarchiveAdminRoomHandler(sh)`
  - [x] Both wrapped in `jwtMW(RequireRole("instance_admin", checker)(...))`

- [x] Add archive check to `SendEventHandler` in `gateway/internal/matrix/rooms.go` (AC: #4)
  - [x] `RoomStatusChecker` interface with `GetRoomStatus(ctx, roomID string) (string, error)`
  - [x] Before calling gRPC SendEvent: check status; "archived" → 403 M_ROOM_ARCHIVED (fail-open on DB error)
  - [x] `StatusChecker RoomStatusChecker` added to `SendEventConfig` and `SendEventHandler`

- [x] Wire repositories and handlers in `gateway/cmd/gateway/main.go`
  - [x] Created separate `sendEventRoomsRepo := apihandler.NewRoomRepo(bootstrapDB)` (forward reference fix)
  - [x] Passed `StatusChecker: sendEventRoomsRepo` in `SendEventConfig`

- [x] Implement Elixir `archive_room` gRPC handler in `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (AC: #3, #6)
  - [x] `def archive_room/2`: lookup PID via `RoomSupervisor.lookup_room`; if running → `Horde.DynamicSupervisor.terminate_child`; always return `ok: true`
    - If `{:error, :not_found}`: no-op (room GenServer already stopped)
    - Return `%Core.ArchiveRoomResponse{ok: true}` in both cases

- [x] Implement Elixir `unarchive_room` gRPC handler in `server.ex` (AC: #3, #6)
  - [x] `def unarchive_room/2`: calls `RoomSupervisor.start_room`; returns `ok: true` (best-effort — DB is authoritative)

- [x] Extend `Nebu.Room.Server.init/1` with archived-room guard (AC: #6, test #13)
  - [x] In `init/1` BEFORE loading members: call `db_module().get_room_status(room_id)`; if `{:ok, "archived"}` → `{:stop, :normal}`
  - [x] Refactored to `do_init/1` private function; archived guard fires first in `init/1`
  - [x] `child_spec` already uses `restart: :transient` — `{:stop, :normal}` is not restarted by Horde

- [x] Add `get_room_status/1` to `Nebu.Room.DBBehaviour`, `Nebu.Room.DB`, and ALL FakeDB modules in tests (AC: #6)
  - [x] Added `@callback get_room_status/1` to `db_behaviour.ex`
  - [x] Implemented `get_room_status/1` in `db.ex` (SQL: `SELECT COALESCE(status, 'active') FROM rooms WHERE room_id = $1`)
  - [x] Added stub to all FakeDB modules in room_manager/ and event_dispatcher/ test files

- [x] Update `router_test.go` — 501 tests for archive and unarchive routes (AC: #8, #9) — pre-existing tests passed

- [x] GREEN phase: `make test-unit-go` → 0 failures; `make test-unit-elixir` → 0 failures; `make build-gateway` → success

## Dev Notes

### Critical: DB Schema — `archived_at` Column Already Exists

The `rooms` table has had `archived_at BIGINT` (nullable) since migration 000009:
```sql
CREATE TABLE rooms (
    room_id      TEXT    PRIMARY KEY,
    name         TEXT,
    visibility   TEXT    NOT NULL DEFAULT 'private',
    created_at   BIGINT  NOT NULL,
    archived_at  BIGINT   -- ← already exists! (nullable BIGINT epoch ms)
);
```

Story 6.7 (migration 000036) added `status TEXT NOT NULL DEFAULT 'active'` with CHECK constraint `status IN ('active', 'archived')` and `archive_reason TEXT`. So the full set of archive columns already exists:
- `archived_at BIGINT` — from migration 000009
- `status TEXT NOT NULL DEFAULT 'active'` — from migration 000036
- `archive_reason TEXT` — from migration 000036

**No new migration is needed for Story 6.9.** Verify before starting: if `archived_at` is somehow missing, create migration `000038`. Otherwise skip.

Next migration number if needed: **000038** (last is 000037_room_defaults).

### Critical: DB Update Strategy — Conditional UPDATE (Prevents Race Conditions)

Use a single `UPDATE ... WHERE room_id = $1 AND status = 'active'` for archiving, and `WHERE room_id = $1 AND status = 'archived'` for unarchiving. This atomically prevents double-archive/unarchive race conditions:

```sql
-- ArchiveRoom:
UPDATE rooms
SET status = 'archived',
    archived_at = $2,        -- current Unix ms (time.Now().UnixMilli())
    archive_reason = $3
WHERE room_id = $1 AND status = 'active'
RETURNING room_id, status
```

```sql
-- UnarchiveRoom:
UPDATE rooms
SET status = 'active',
    archived_at = NULL
WHERE room_id = $1 AND status = 'archived'
RETURNING room_id, status
```

When 0 rows affected: execute a separate `SELECT EXISTS(SELECT 1 FROM rooms WHERE room_id = $1)` to distinguish 404 (not found) from 409 (wrong status). Return `ErrRoomNotFound` sentinel or `ErrRoomWrongStatus` sentinel accordingly.

### Critical: ArchiveResult / UnarchiveResult Types

```go
// ArchiveResult is returned by RoomRepository.ArchiveRoom on success.
type ArchiveResult struct {
    RoomID string `json:"room_id"`
    Status string `json:"status"`  // always "archived"
}

// UnarchiveResult is returned by RoomRepository.UnarchiveRoom on success.
type UnarchiveResult struct {
    RoomID string `json:"room_id"`
    Status string `json:"status"`  // always "active"
}

// Sentinel errors for status conflicts:
var ErrRoomNotFound    = errors.New("room not found")
var ErrRoomWrongStatus = errors.New("room has wrong status for this operation")
```

In the handler:
- `(nil, ErrRoomNotFound)` → 404
- `(nil, ErrRoomWrongStatus)` → 409
- `(result, nil)` → 200

### Critical: OpenAPI Schema — New Paths (POST with no conflicting operationId)

The existing `/admin/rooms/{roomId}` path has `GET` and `PATCH` operations. The new archive/unarchive paths are **separate** sub-paths:

```yaml
/admin/rooms/{roomId}/archive:
  post:
    operationId: ArchiveAdminRoom
    summary: Archive a room (instance_admin required)
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
            $ref: "#/components/schemas/ArchiveRoomRequest"
    responses:
      "200":
        description: Room archived
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/ArchiveRoomResponse"
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
      "409":
        description: Room already archived
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
      "501":
        description: Not implemented

/admin/rooms/{roomId}/unarchive:
  post:
    operationId: UnarchiveAdminRoom
    summary: Unarchive a room (instance_admin required)
    parameters:
      - name: roomId
        in: path
        required: true
        schema:
          type: string
    responses:
      "200":
        description: Room unarchived
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/UnarchiveRoomResponse"
      "404":
        description: Room not found
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
      "409":
        description: Room not archived
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
      "501":
        description: Not implemented
```

```yaml
# Add to components.schemas:
ArchiveRoomRequest:
  type: object
  required:
    - reason
  properties:
    reason:
      type: string
      minLength: 10

ArchiveRoomResponse:
  type: object
  required:
    - room_id
    - status
  properties:
    room_id:
      type: string
    status:
      type: string

UnarchiveRoomResponse:
  type: object
  required:
    - room_id
    - status
  properties:
    room_id:
      type: string
    status:
      type: string
```

After editing `openapi.yaml`, run `make gen-api`. The generated `api_gen.go` will add `ArchiveAdminRoom` and `UnarchiveAdminRoom` to both `ServerInterface` and `StrictServerInterface`. **The build will fail until both handlers are implemented** — add 501 stubs first.

### Critical: StrictServerInterface Compile Check

After `make gen-api`, `api_gen.go` adds `ArchiveAdminRoom` and `UnarchiveAdminRoom` to `StrictServerInterface`. The compile-time check in `server.go`:
```go
var _ StrictServerInterface = (*AdminServer)(nil)
```
will fail until both methods are added to `AdminServer`. **Add 501 stubs first**, then implement fully.

### Critical: proto/core.proto — Message Field Numbers

Add the new RPCs after the existing `UpdateRoomSettings` RPC (last in the `CoreService` block):

```protobuf
// ArchiveRoom — Story 6.9: Admin API terminates the Room GenServer for an archived room.
// The Go gateway has already updated rooms.status='archived' in DB before calling this.
// Core stops the GenServer via Horde.DynamicSupervisor.terminate_child/2.
// Returns ok=true in all cases (even if GenServer was already stopped).
rpc ArchiveRoom(ArchiveRoomRequest) returns (ArchiveRoomResponse);

// UnarchiveRoom — Story 6.9: Admin API restarts the Room GenServer for an unarchived room.
// The Go gateway has already updated rooms.status='active' in DB before calling this.
// Core restarts the GenServer via RoomSupervisor.start_room/1.
rpc UnarchiveRoom(UnarchiveRoomRequest) returns (UnarchiveRoomResponse);
```

```protobuf
// ArchiveRoom — Story 6.9
message ArchiveRoomRequest {
  string room_id = 1;
}
message ArchiveRoomResponse {
  bool ok = 1;
}

// UnarchiveRoom — Story 6.9
message UnarchiveRoomRequest {
  string room_id = 1;
}
message UnarchiveRoomResponse {
  bool ok = 1;
}
```

After editing `proto/core.proto`, run `make proto`. This regenerates:
- `gateway/internal/grpc/pb/core.pb.go` + `core_grpc.pb.go` (Go stubs)
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` + `core_grpc.pb.ex` (Elixir stubs)

**Critical:** All existing mock implementations of `pb.CoreServiceClient` across the Go test suite must get `ArchiveRoom` and `UnarchiveRoom` stub methods. Search: `grep -r "CoreServiceClient" gateway/internal/ --include="*_test.go"`.

### Critical: ArchiveAdminRoom Handler Logic

```go
func (s *AdminServer) ArchiveAdminRoom(ctx context.Context, req ArchiveAdminRoomRequestObject) (ArchiveAdminRoomResponseObject, error) {
    if s.Rooms == nil {
        return ArchiveAdminRoom501Response{}, nil
    }

    roomID := req.RoomId
    body := req.Body
    if body == nil {
        return &archiveRoom400Resp{msg: "request body is required"}, nil
    }

    // 1. Validate reason (required, min 10 chars)
    reason := strings.TrimSpace(body.Reason)
    if len(reason) < 10 {
        return &archiveRoom400Resp{msg: "reason must be at least 10 characters"}, nil
    }

    // 2. DB update (atomic conditional UPDATE)
    result, err := s.Rooms.ArchiveRoom(ctx, roomID, reason)
    if errors.Is(err, ErrRoomNotFound) {
        return &archiveRoom404Resp{}, nil
    }
    if errors.Is(err, ErrRoomWrongStatus) {
        return &archiveRoom409Resp{msg: "Room is already archived"}, nil
    }
    if err != nil {
        return nil, err
    }
    if result == nil {
        return &archiveRoom404Resp{}, nil
    }

    // 3. gRPC ArchiveRoom (best-effort — DB is authoritative)
    if s.CoreClient != nil {
        _, grpcErr := s.CoreClient.ArchiveRoom(ctx, &pb.ArchiveRoomRequest{RoomId: roomID})
        if grpcErr != nil {
            slog.Warn("ArchiveRoom gRPC failed — GenServer may still be running",
                "room_id", roomID, "err", grpcErr)
            // Best-effort: continue. Room.Server init/1 will stop on next restart
            // because it checks rooms.status from DB (archived → {:stop, :normal}).
        }
    }

    // 4. Audit log (never-raise)
    if s.CoreClient != nil {
        actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
        _ = audit.LogEvent(ctx, s.CoreClient, actorID, "room_archived", "room", roomID,
            map[string]any{"reason": reason}, "success", "")
    } else {
        slog.Warn("ArchiveAdminRoom audit skipped — CoreClient is nil", "room_id", roomID)
    }

    return &archiveRoom200Resp{roomID: result.RoomID, status: result.Status}, nil
}
```

### Critical: UnarchiveAdminRoom Handler Logic

```go
func (s *AdminServer) UnarchiveAdminRoom(ctx context.Context, req UnarchiveAdminRoomRequestObject) (UnarchiveAdminRoomResponseObject, error) {
    if s.Rooms == nil {
        return UnarchiveAdminRoom501Response{}, nil
    }

    roomID := req.RoomId

    // 1. DB update (atomic conditional UPDATE)
    result, err := s.Rooms.UnarchiveRoom(ctx, roomID)
    if errors.Is(err, ErrRoomNotFound) {
        return &unarchiveRoom404Resp{}, nil
    }
    if errors.Is(err, ErrRoomWrongStatus) {
        return &unarchiveRoom409Resp{msg: "Room is not archived"}, nil
    }
    if err != nil {
        return nil, err
    }
    if result == nil {
        return &unarchiveRoom404Resp{}, nil
    }

    // 2. gRPC UnarchiveRoom (restarts GenServer)
    if s.CoreClient != nil {
        _, grpcErr := s.CoreClient.UnarchiveRoom(ctx, &pb.UnarchiveRoomRequest{RoomId: roomID})
        if grpcErr != nil {
            slog.Warn("UnarchiveRoom gRPC failed — GenServer not restarted",
                "room_id", roomID, "err", grpcErr)
            // Best-effort: continue. GenServer will start on next Matrix event for this room.
        }
    }

    // 3. Audit log
    if s.CoreClient != nil {
        actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
        _ = audit.LogEvent(ctx, s.CoreClient, actorID, "room_unarchived", "room", roomID,
            nil, "success", "")
    } else {
        slog.Warn("UnarchiveAdminRoom audit skipped — CoreClient is nil", "room_id", roomID)
    }

    return &unarchiveRoom200Resp{roomID: result.RoomID, status: result.Status}, nil
}
```

### Critical: SendEventHandler — Archive Status Check

The `SendEventHandler` in `gateway/internal/matrix/rooms.go` must check room status before calling Core. The cleanest approach is to inject a `RoomStatusChecker` interface:

```go
// RoomStatusChecker allows SendEventHandler to check room archive status.
// Implemented by dbRoomRepo (via GetRoomStatus method) — no new struct needed.
type RoomStatusChecker interface {
    GetRoomStatus(ctx context.Context, roomID string) (string, error)
}

// SendEventConfig holds dependencies for NewSendEventHandler.
type SendEventConfig struct {
    CoreClient    SendEventCoreClient
    ServerName    string
    StatusChecker RoomStatusChecker  // NEW — inject roomsRepo
}

// SendEventHandler handles PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}.
type SendEventHandler struct {
    coreClient    SendEventCoreClient
    serverName    string
    statusChecker RoomStatusChecker  // NEW
}
```

In `PutSendEvent`, add the check BEFORE the gRPC call:

```go
// Check if room is archived before sending
if h.statusChecker != nil {
    status, err := h.statusChecker.GetRoomStatus(r.Context(), roomID)
    if err != nil {
        // Log but don't block — fail-open for status check errors
        slog.Warn("PutSendEvent: GetRoomStatus failed", "room_id", roomID, "err", err)
    } else if status == "archived" {
        writeMatrixError(w, http.StatusForbidden, "M_ROOM_ARCHIVED", "Room is archived")
        return
    }
    // status == "" means room not found — let SendEvent gRPC return NOT_FOUND
}
```

**In `main.go`:** Pass `roomsRepo` as `StatusChecker` in `SendEventConfig`:
```go
sendEventHandler := matrix.NewSendEventHandler(matrix.SendEventConfig{
    CoreClient:    coreClient.CoreServiceClient(),
    ServerName:    cfg.ServerName,
    StatusChecker: roomsRepo,  // roomsRepo already wired from Story 6.7
})
```

**Note:** `roomsRepo` already has type `RoomRepository` (interface). After adding `GetRoomStatus` to `RoomRepository` interface, `dbRoomRepo` implements it, and it also satisfies `RoomStatusChecker`. No new type needed.

### Critical: Elixir archive_room Handler Pattern

The `archive_room` handler follows the same pattern as `update_room_settings` and `invalidate_user_sessions`:

```elixir
# ─── ArchiveRoom — Story 6.9: Admin API terminates Room GenServer ─────────
#
# Called AFTER Go gateway has already set rooms.status='archived' in DB.
# Uses Horde.DynamicSupervisor.terminate_child/2 to stop the GenServer
# gracefully. The `:transient` restart strategy ensures Horde does NOT
# restart a process that exits normally.
#
# Even if the GenServer is not running, returns ok=true (idempotent).
def archive_room(%Core.ArchiveRoomRequest{} = req, _stream) do
  case Nebu.Room.RoomSupervisor.lookup_room(req.room_id) do
    {:ok, pid} ->
      # terminate_child sends a normal shutdown signal — with :transient restart
      # strategy, Horde will NOT restart the process after a normal exit.
      case Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid) do
        :ok ->
          Logger.info("ArchiveRoom: GenServer terminated", room_id: req.room_id)
        {:error, reason} ->
          Logger.warning("ArchiveRoom: terminate_child failed — process may have already stopped",
            room_id: req.room_id, reason: inspect(reason))
      end

    {:error, :not_found} ->
      Logger.info("ArchiveRoom: GenServer not running (already stopped)", room_id: req.room_id)
  end

  %Core.ArchiveRoomResponse{ok: true}
end
```

### Critical: Elixir unarchive_room Handler Pattern

```elixir
# ─── UnarchiveRoom — Story 6.9: Admin API restarts Room GenServer ─────────
#
# Called AFTER Go gateway has already set rooms.status='active' in DB.
# Uses RoomSupervisor.start_room/1 which calls Horde.DynamicSupervisor.start_child.
# Room.Server.init/1 will now load status='active' from DB and proceed normally.
def unarchive_room(%Core.UnarchiveRoomRequest{} = req, _stream) do
  case Nebu.Room.RoomSupervisor.start_room(req.room_id) do
    {:ok, _pid} ->
      Logger.info("UnarchiveRoom: GenServer started", room_id: req.room_id)
      %Core.UnarchiveRoomResponse{ok: true}

    {:error, reason} ->
      raise GRPC.RPCError,
        status: GRPC.Status.internal(),
        message: "unarchive_room failed to start GenServer: #{inspect(reason)}"
  end
end
```

### Critical: Room.Server init/1 — Archived Guard

The Room GenServer uses `restart: :transient` in `child_spec`. A `:transient` process is NOT restarted if it exits with a normal reason. The archive mechanism works as follows:

1. `terminate_child` sends a shutdown to the running GenServer → it exits normally
2. Horde sees normal exit → does NOT restart (`:transient` behaviour)
3. If Horde tries to restart (e.g. node restart) → `init/1` checks DB status → returns `{:stop, :normal}` → Horde does NOT restart again

Add the archive guard at the TOP of `init/1`, BEFORE loading members (because `load_members` may return `:not_found` for a room that doesn't exist yet, but an archived room should always stop before any state is loaded):

```elixir
@impl GenServer
def init(room_id) do
  # Story 6.9: Archived guard — stop immediately if room is archived in DB.
  # This prevents Horde from restarting an archived room after node restart.
  # :transient restart strategy means {:stop, :normal} will NOT trigger a restart.
  case db_module().get_room_status(room_id) do
    {:ok, "archived"} ->
      Logger.info("Room.Server: init stopped — room is archived", room_id: room_id)
      {:stop, :normal}

    _ ->
      # Proceed with normal initialization (status = 'active' or DB error → fail-open)
      do_init(room_id)
  end
end

defp do_init(room_id) do
  # ... existing init logic moved here ...
end
```

**Note:** The existing `child_spec` already has `restart: :transient`:
```elixir
def child_spec(room_id) do
  %{
    id: {__MODULE__, room_id},
    start: {__MODULE__, :start_link, [room_id]},
    restart: :transient,   # ← this is the key
    shutdown: 5_000,
    type: :worker
  }
end
```

This is exactly right. Normal exit (`{:stop, :normal}`) → Horde does NOT restart. Abnormal exit (`{:stop, reason}` with non-normal reason) → Horde WOULD restart. So `{:stop, :normal}` is the correct return from init for archived rooms.

### Critical: Elixir DBBehaviour — get_room_status/1

Add to `Nebu.Room.DBBehaviour`:
```elixir
@callback get_room_status(room_id :: String.t()) ::
            {:ok, String.t()} | {:error, term()}
```

Add to `Nebu.Room.DB`:
```elixir
@sql_get_room_status "SELECT COALESCE(status, 'active') FROM rooms WHERE room_id = $1"

def get_room_status(room_id) do
  case Nebu.Repo.query(@sql_get_room_status, [room_id]) do
    {:ok, %{rows: [[status]]}} -> {:ok, status}
    {:ok, %{rows: []}} -> {:ok, "active"}  # room not yet in DB (new room path) — fail-open
    {:error, reason} -> {:error, reason}
  end
end
```

**Add to ALL FakeDB modules** (same pattern as `load_room_settings/1` was added in Story 6.8):
```elixir
def get_room_status(_room_id), do: {:ok, "active"}
```

Search for all FakeDB modules:
```bash
grep -r "@behaviour Nebu.Room.DBBehaviour" core/apps/ --include="*.exs" -l
```

There were 14 FakeDB modules in Story 6.8 — expect the same count. Every one needs `get_room_status/1`.

### Critical: Response Types for ArchiveAdminRoom / UnarchiveAdminRoom

```go
type archiveRoom200Resp struct{ roomID, status string }

func (r *archiveRoom200Resp) VisitArchiveAdminRoomResponse(w http.ResponseWriter) error {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    return json.NewEncoder(w).Encode(map[string]string{
        "room_id": r.roomID,
        "status":  r.status,
    })
}

type archiveRoom400Resp struct{ msg string }

func (r *archiveRoom400Resp) VisitArchiveAdminRoomResponse(w http.ResponseWriter) error {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusBadRequest)
    return json.NewEncoder(w).Encode(map[string]any{
        "error": map[string]string{"code": "M_BAD_JSON", "message": r.msg},
    })
}

type archiveRoom404Resp struct{}

func (r *archiveRoom404Resp) VisitArchiveAdminRoomResponse(w http.ResponseWriter) error {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusNotFound)
    return json.NewEncoder(w).Encode(map[string]any{
        "error": map[string]string{"code": "M_NOT_FOUND", "message": "Room not found"},
    })
}

type archiveRoom409Resp struct{ msg string }

func (r *archiveRoom409Resp) VisitArchiveAdminRoomResponse(w http.ResponseWriter) error {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusConflict)
    return json.NewEncoder(w).Encode(map[string]any{
        "error": map[string]string{"code": "M_CONFLICT", "message": r.msg},
    })
}

// Mirror pattern for unarchive:
type unarchiveRoom200Resp struct{ roomID, status string }
type unarchiveRoom404Resp struct{}
type unarchiveRoom409Resp struct{ msg string }
// (VisitUnarchiveAdminRoomResponse — same structure as archive counterparts)
```

### Critical: Router Wrapper Functions

```go
// archiveAdminRoomHandler extracts {roomId}, pre-validates body, delegates to sh.ArchiveAdminRoom.
// Story 6.9: POST /api/v1/admin/rooms/{roomId}/archive
func archiveAdminRoomHandler(sh ServerInterface) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        roomID := r.PathValue("roomId")
        if r.Body == nil || r.ContentLength == 0 {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusBadRequest)
            _, _ = w.Write([]byte(`{"error":{"code":"M_BAD_JSON","message":"request body is required"}}`))
            return
        }
        sh.ArchiveAdminRoom(w, r, roomID)
    })
}

// unarchiveAdminRoomHandler extracts {roomId}, delegates to sh.UnarchiveAdminRoom.
// Story 6.9: POST /api/v1/admin/rooms/{roomId}/unarchive
// No body required for unarchive.
func unarchiveAdminRoomHandler(sh ServerInterface) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        roomID := r.PathValue("roomId")
        sh.UnarchiveAdminRoom(w, r, roomID)
    })
}
```

Register in `RegisterAdminRoutes`:
```go
// Story 6.9: Archive + Unarchive room — instance_admin required.
mux.Handle("POST /api/v1/admin/rooms/{roomId}/archive",
    jwtMW(RequireRole("instance_admin", checker)(archiveAdminRoomHandler(sh))))
mux.Handle("POST /api/v1/admin/rooms/{roomId}/unarchive",
    jwtMW(RequireRole("instance_admin", checker)(unarchiveAdminRoomHandler(sh))))
```

### Critical: Mock Pattern for Tests

Follow the exact pattern from `rooms_handler_test.go` and `rooms_patch_handler_test.go`. Extend the existing `mockRoomRepository` (or create a new one for this test file) with the new methods:

```go
type mockRoomRepository68 struct {  // use a new mock name to avoid conflicts
    // archive
    archiveResult *ArchiveResult
    archiveErr    error
    // unarchive
    unarchiveResult *UnarchiveResult
    unarchiveErr    error
    // status (for SendEvent test)
    roomStatus string
    statusErr  error
    // Existing methods: ListRooms, GetRoom, UpdateRoom (must still be implemented)
}

func (m *mockRoomRepository68) ArchiveRoom(_ context.Context, _ string, _ string) (*ArchiveResult, error) {
    return m.archiveResult, m.archiveErr
}
func (m *mockRoomRepository68) UnarchiveRoom(_ context.Context, _ string) (*UnarchiveResult, error) {
    return m.unarchiveResult, m.unarchiveErr
}
func (m *mockRoomRepository68) GetRoomStatus(_ context.Context, _ string) (string, error) {
    return m.roomStatus, m.statusErr
}
// ListRooms, GetRoom, UpdateRoom: delegate to existing mock or return zero values
```

For the gRPC mock, add `ArchiveRoom` and `UnarchiveRoom` stubs to the existing `pb.CoreServiceClient` mock pattern used across test files.

### Critical: All Go CoreServiceClient Mocks Need New Methods

After `make proto`, `pb.CoreServiceClient` interface gains `ArchiveRoom` and `UnarchiveRoom`. Every mock in the test suite that implements this interface needs stubs. Search:
```bash
grep -r "CoreServiceClient\|pb\.CoreServiceClient\|MockCoreClient\|fakeCoreClient" gateway/internal/ --include="*_test.go" -l
```

Each one needs:
```go
func (m *mockCoreClient) ArchiveRoom(ctx context.Context, req *pb.ArchiveRoomRequest, opts ...grpc.CallOption) (*pb.ArchiveRoomResponse, error) {
    return &pb.ArchiveRoomResponse{Ok: true}, nil
}
func (m *mockCoreClient) UnarchiveRoom(ctx context.Context, req *pb.UnarchiveRoomRequest, opts ...grpc.CallOption) (*pb.UnarchiveRoomResponse, error) {
    return &pb.UnarchiveRoomResponse{Ok: true}, nil
}
```

### Critical: File Inventory

**Files to CREATE (new):**
- `gateway/internal/api/rooms_archive_handler_test.go` — ATDD tests for archive/unarchive endpoints + send-event-on-archived (written first, RED phase)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/archive_room_test.exs` — Elixir tests for archive/unarchive gRPC handlers + Room.Server init guard
- `gateway/migrations/000038_rooms_archived_at.up.sql` (ONLY IF `archived_at` column is missing from the DB — verify first)
- `gateway/migrations/000038_rooms_archived_at.down.sql` (ONLY IF above is created)

**Files to UPDATE:**
- `proto/core.proto` — add `rpc ArchiveRoom` + `rpc UnarchiveRoom` + 4 message types
- `gateway/api/openapi.yaml` — add `ArchiveRoomRequest`, `ArchiveRoomResponse`, `UnarchiveRoomResponse` schemas + 2 new POST paths
- `gateway/internal/api/api_gen.go` — regenerated via `make gen-api` (do not edit manually)
- `gateway/internal/api/rooms_repo.go` — add `ArchiveRoom`, `UnarchiveRoom`, `GetRoomStatus` to `RoomRepository` interface; add `ArchiveResult`, `UnarchiveResult` types; add `ErrRoomNotFound`, `ErrRoomWrongStatus` sentinels; implement all three on `dbRoomRepo`
- `gateway/internal/api/server.go` — add `ArchiveAdminRoom` + `UnarchiveAdminRoom` handlers; add all 7 response types; add `ErrRoomNotFound`/`ErrRoomWrongStatus` (if not defined in rooms_repo.go)
- `gateway/internal/api/router.go` — add archive + unarchive routes + wrapper functions
- `gateway/internal/api/router_test.go` — add 501 tests for the 2 new routes
- `gateway/internal/matrix/rooms.go` — extend `SendEventConfig` and `SendEventHandler` with `RoomStatusChecker`; add archive check in `PutSendEvent`
- `gateway/cmd/gateway/main.go` — pass `roomsRepo` as `StatusChecker` in `SendEventConfig`
- `gateway/internal/grpc/pb/core.pb.go` — regenerated via `make proto`
- `gateway/internal/grpc/pb/core_grpc.pb.go` — regenerated via `make proto`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — add `def archive_room/2` + `def unarchive_room/2` handlers
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — regenerated via `make proto`
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — regenerated via `make proto`
- `core/apps/room_manager/lib/nebu/room/server.ex` — add archived guard to `init/1`; refactor existing init body to `do_init/1`; add `def archive/1` public function (optional — only if needed by handler)
- `core/apps/room_manager/lib/nebu/room/db.ex` — add `get_room_status/1`
- `core/apps/room_manager/lib/nebu/room/db_behaviour.ex` — add `@callback get_room_status/1`
- `core/apps/room_manager/test/nebu/room/db_behaviour_test.exs` — assert `{:get_room_status, 1}` in required callbacks list
- All FakeDB modules (14+) in `core/apps/` tests that implement `Nebu.Room.DBBehaviour` — add `def get_room_status(_), do: {:ok, "active"}`
- All Go test files that implement `pb.CoreServiceClient` mock — add `ArchiveRoom` + `UnarchiveRoom` stubs

### Critical: Read-Only Endpoints — No Changes Needed

`GET /messages` and `GET /sync` do NOT need to check room status. They query the `events` table directly (via gRPC `GetMessages` / `GetSyncDelta`) which remains fully accessible for archived rooms. The append-only guarantee is maintained. **Do not add status checks to any read path.**

### Critical: Audit Pattern

Same call site pattern as Stories 6.7 and 6.8:
```go
actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
_ = audit.LogEvent(ctx, s.CoreClient, actorID,
    "room_archived",   // action — or "room_unarchived"
    "room",            // targetType
    roomID,            // targetID
    map[string]any{"reason": reason},  // metadata (nil for unarchive)
    "success",         // outcome
    "",                // errorDetail
)
```

The `audit.LogEvent` signature (from `gateway/internal/audit/writer.go`): `LogEvent(ctx, coreClient, actorID, action, targetType, targetID, metadata, outcome, errorDetail)`. The 9-argument version is correct.

### Previous Story Learnings (from 6.7 and 6.8)

1. **501 guard is mandatory**: check `if s.Rooms == nil { return XxxAdminRoom501Response{} }` first.

2. **slog.Warn on nil CoreClient**: always log when audit is skipped (MINOR-1 pattern).

3. **make gen-api FIRST**: add 501 stubs to `server.go`, then implement. Build fails until both `ArchiveAdminRoom` and `UnarchiveAdminRoom` are in `AdminServer`.

4. **FakeDB modules — every single one**: Story 6.8 updated 14 modules. Missing even one causes Elixir compile error. Run `grep -r "@behaviour Nebu.Room.DBBehaviour" core/apps/ -l` before committing.

5. **make proto updates ALL mocks**: After `make proto`, all Go test mocks that embed `pb.CoreServiceClient` must add the two new methods. Missing one causes compile error.

6. **Conditional UPDATE pattern**: Use `WHERE ... AND status = 'active'` to atomically handle the 409 conflict case. Separate room-exists check only when 0 rows affected.

7. **Body pre-check in router wrapper**: For archive (has a body), check `r.Body == nil || r.ContentLength == 0`. For unarchive (no body), skip the check.

8. **audit.LogEvent: 9 arguments**: see Story 6.7/6.8 call sites. The 9th argument is `errorDetail string` (empty string for success).

9. **archived_at type**: It's `BIGINT` (epoch ms) in the DB, same as `created_at`. Use `time.Now().UnixMilli()` in Go. Not ISO 8601.

10. **Test file naming**: Follow `rooms_archive_handler_test.go` — consistent with existing `rooms_handler_test.go` and `rooms_patch_handler_test.go` naming.

### Git Context

```
8eeaee1 refactor: replace `git` commands with `rtk` for improved context management
6520117 feat(6-6): role_overrides migration, POST /roles grant/revoke, RequireRole DB-override
012bce0 feat(6-5): User Deactivation + Reactivation + Session Invalidierung
ed4f383 feat(6-4): User List + Get API — GET /admin/users + GET /admin/users/{userId}
e46b90d feat(6-3): Admin API Router + RequireRole middleware — role-auth gate for instance_admin/compliance_officer
```

Stories 6.7 and 6.8 are in `review` status — their files are the direct base. `rooms_repo.go`, `server.go`, `router.go` all need extensions.

### References

- `_bmad-output/planning-artifacts/epics.md:2851–2876` — Canonical Story 6.9 AC definition
- `gateway/internal/api/rooms_repo.go` — `RoomRepository` interface to extend; `AdminRoom`, `AdminRoomDetail` types
- `gateway/internal/api/server.go` — `AdminServer` struct, handler pattern, audit log pattern, response type pattern
- `gateway/internal/api/router.go` — `RegisterAdminRoutes`, wrapper function pattern (line 228 onwards)
- `gateway/internal/matrix/rooms.go:391–483` — `SendEventHandler`, `SendEventConfig`, `PutSendEvent` (archive check goes here)
- `gateway/migrations/000009_rooms.up.sql` — confirms `archived_at BIGINT` column already exists
- `gateway/migrations/000036_rooms_admin_columns.up.sql` — confirms `status`, `archive_reason` already added
- `proto/core.proto:89` — `UpdateRoomSettings` (last RPC; add ArchiveRoom/UnarchiveRoom after it)
- `core/apps/room_manager/lib/nebu/room/server.ex` — `init/1` (add archived guard), `child_spec` (`:transient` restart confirmed)
- `core/apps/room_manager/lib/nebu/room/room_supervisor.ex` — `start_room/1` (used by unarchive), `lookup_room/1` (used by archive)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:253` — `update_room_settings` pattern (archive_room follows same structure)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:699` — `invalidate_user_sessions` pattern (unarchive_room follows same structure)
- `core/apps/room_manager/lib/nebu/room/db_behaviour.ex` — add `get_room_status/1` callback
- `_bmad-output/implementation-artifacts/6-7-room-list-get-api.md` — complete room API patterns
- `_bmad-output/implementation-artifacts/6-8-room-settings-update-api-max-members-visibility-serverweite-defaults.md` — FakeDB update pattern (load_room_settings → get_room_status), gRPC proto extension pattern

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

- Router wrapper `archiveAdminRoomHandler` received nil-body test expecting 501 but got 400 (body pre-check fires before handler nil-Rooms guard). Fixed by adding `*AdminServer` parameter to `archiveAdminRoomHandler` so the nil-Rooms 501 check runs before the body check.
- Forward reference in `main.go`: `roomsRepo` defined at line ~1125 but used at line ~779 for `SendEventConfig.StatusChecker`. Fixed by creating separate `sendEventRoomsRepo := apihandler.NewRoomRepo(bootstrapDB)` before `sendEventHandler`.
- Multiple FakeDB modules in room_manager and event_dispatcher tests missing `get_room_status/1` after adding `@callback` to `DBBehaviour`. Fixed by adding the stub to all 13 affected test files.

### Completion Notes List

- All 6 ACs satisfied: archive/unarchive HTTP endpoints, gRPC proto, SendEvent 403 guard, Go unit tests (13 tests), Elixir unit tests (7 tests), build clean.
- No new DB migration needed: `archived_at BIGINT`, `status TEXT NOT NULL DEFAULT 'active'`, `archive_reason TEXT` all exist from migrations 000009 + 000036.
- Fail-open pattern: `GetRoomStatus` DB error → log + proceed (don't block SendEvent).
- Best-effort gRPC: `ArchiveRoom`/`UnarchiveRoom` gRPC failure → log + return 200 (DB is authoritative).
- `Room.Server.init/1` refactored to `do_init/1` private helper; archived guard fires first via `get_room_status/1` check.
- Horde `:transient` restart strategy: `{:stop, :normal}` from `init/1` prevents restart loop for archived rooms.
- `make test-unit-go`: 0 failures; `make test-unit-elixir`: 0 failures; `make build-gateway`: success.

### File List

**Modified:**
- `proto/core.proto` — added ArchiveRoom + UnarchiveRoom RPCs + 4 message types
- `gateway/api/openapi.yaml` — added archive/unarchive paths + 3 schema components
- `gateway/internal/api/api_gen.go` — regenerated (make gen-api)
- `gateway/internal/api/rooms_repo.go` — added ArchiveRoom, UnarchiveRoom, GetRoomStatus methods + sentinel errors + result types
- `gateway/internal/api/server.go` — added ArchiveAdminRoom, UnarchiveAdminRoom handlers + 7 response types
- `gateway/internal/api/router.go` — added archive/unarchive routes + archiveAdminRoomHandler(sh, adminServer) + unarchiveAdminRoomHandler
- `gateway/internal/matrix/rooms.go` — added RoomStatusChecker interface + StatusChecker field + archive check in PutSendEvent
- `gateway/cmd/gateway/main.go` — added sendEventRoomsRepo + StatusChecker wiring in SendEventConfig
- `gateway/internal/api/rooms_handler_test.go` — added 3 stubs to mockRoomRepository
- `gateway/internal/api/rooms_patch_handler_test.go` — added 3 stubs to mockRoomRepositoryWithUpdate
- `gateway/internal/admin/auth_audit_test.go` — added ArchiveRoom + UnarchiveRoom stubs to mockCoreClient
- `gateway/internal/compliance/handler_test.go` — added ArchiveRoom + UnarchiveRoom stubs to mockCoreClient
- `gateway/internal/grpc/stream_test.go` — added ArchiveRoom + UnarchiveRoom stubs to mockCoreClient
- `gateway/internal/audit/writer_test.go` — added ArchiveRoom + UnarchiveRoom stubs to mockCoreClient
- `core/apps/room_manager/lib/nebu/room/db_behaviour.ex` — added get_room_status/1 @callback
- `core/apps/room_manager/lib/nebu/room/db.ex` — added get_room_status/1 implementation
- `core/apps/room_manager/lib/nebu/room/server.ex` — added archived guard in init/1; refactored to do_init/1
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — added archive_room/2 + unarchive_room/2
- `core/apps/room_manager/test/nebu/room/db_behaviour_test.exs` — added {get_room_status, 1} to required callbacks
- `core/apps/room_manager/test/nebu_room_test.exs` — added get_room_status/1 to FakeDBWithMaxMembers + stubs
- `core/apps/room_manager/test/nebu/room/server_set_typing_test.exs` — added get_room_status/1 to FakeDB
- `core/apps/room_manager/test/nebu/room/power_level_enforcement_test.exs` — added get_room_status/1 to FakeDB + FailingWriteDB
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/audit_room_ops_test.exs` — added get_room_status/1 stub
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/create_room_test.exs` — added get_room_status/1 stub
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/get_messages_test.exs` — added get_room_status/1 stub
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/invite_user_test.exs` — added get_room_status/1 stub
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/join_room_test.exs` — added get_room_status/1 stub
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/send_event_test.exs` — added get_room_status/1 stub
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_get_room_state_system_test.exs` — added get_room_status/1 stub
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_moderation_metadata_test.exs` — added get_room_status/1 stub
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_receipts_test.exs` — added get_room_status/1 stub
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_set_typing_test.exs` — added get_room_status/1 stub
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/sync_test.exs` — added get_room_status/1 to SyncTestFakeDB + delegate
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/update_room_settings_test.exs` — added get_room_status/1 stub

**Pre-existing (written in RED phase, now green):**
- `gateway/internal/api/rooms_archive_handler_test.go` — 13 Go acceptance tests
- `gateway/internal/grpc/pb/core.pb.go` + `core_grpc.pb.go` — regenerated (make proto)
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` + `core_grpc.pb.ex` — regenerated (make proto)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/archive_room_test.exs` — 7 Elixir acceptance tests
