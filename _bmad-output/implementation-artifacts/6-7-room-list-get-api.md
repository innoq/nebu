---
security_review: required
---

# Story 6.7: Room List + Get API

Status: ready-for-dev

## Story

As an instance admin,
I want to list all rooms on the instance and view individual room details,
so that I have full visibility of all spaces on the server.

## Acceptance Criteria

1. `GET /api/v1/admin/rooms` — `instance_admin` role required; query params: `cursor` (optional), `limit` (1–100, default 20), `search` (optional, ILIKE on `name`), `status` (optional filter: `active` | `archived`):
   - Queries `rooms` table ordered by `(created_at DESC, room_id)` using keyset pagination (same pattern as Story 6.4 users)
   - Response: `{"data": [<room objects>], "meta": {"total": N, "next_cursor": "..."}}`
   - Each room object: `{"room_id", "name", "topic", "canonical_alias", "visibility", "is_public", "member_count", "status", "created_at", "creator_user_id", "admin_note"}`
   - `member_count`: `COUNT(*) FROM room_members WHERE room_id = $1 AND left_at IS NULL`
   - `status`: `"active"` when `archived_at IS NULL`, `"archived"` when `archived_at IS NOT NULL`
   - `created_at`: ISO 8601 string (epoch ms → RFC3339 conversion, reuse `epochMsToISO8601` from `users_repo.go`)
   - `search` applies `ILIKE '%term%'` on `rooms.name`
   - `status=active` filter adds `WHERE archived_at IS NULL`; `status=archived` adds `WHERE archived_at IS NOT NULL`; no `status` param → no filter (show all rooms)
   - Invalid `status` value → `400 M_BAD_JSON "invalid status: must be active or archived"`
   - Invalid `cursor` → `400 M_BAD_JSON "Invalid cursor"`
   - Invalid `limit` (out of range) → `400 M_BAD_JSON "limit must be between 1 and 100"`
   - Returns `501 Not Implemented` if `Rooms` repository is not wired (stub safety)

2. `GET /api/v1/admin/rooms/{roomId}` — `instance_admin` role required:
   - Returns single room object with all list fields plus `max_members` (NULL-safe: null if column does not exist yet) and `message_count` (count of events in `events` table for this room) and `power_levels_json`
   - Returns `404 {"error": {"code": "M_NOT_FOUND", "message": "Room not found"}}` if room does not exist
   - Returns `501 Not Implemented` if `Rooms` repository is not wired

3. Both endpoints call `audit.LogEvent(ctx, s.CoreClient, actorID, "admin_room_viewed", "room", roomID, nil, "success", "")` (never-raise — same pattern as Story 6.4)

4. `gateway/api/openapi.yaml` is updated to replace the placeholder `/admin/rooms` GET with full query param definitions and add `GET /admin/rooms/{roomId}`; schemas `RoomListResponse` and `RoomDetailResponse` are added; `make gen-api` is run to regenerate `api_gen.go`

5. `RoomRepository` interface is defined in `gateway/internal/api/rooms_repo.go` (consumer-defined, same pattern as `UserRepository` in `users_repo.go`):
   - `ListRooms(ctx, afterID, afterCreatedAt string, limit int, search, statusFilter string) ([]AdminRoom, int, string, error)`
   - `GetRoom(ctx, roomID string) (*AdminRoomDetail, error)` — returns `(nil, nil)` when not found

6. Unit tests in `gateway/internal/api/rooms_handler_test.go` (written FIRST, failing):
   - List rooms → 200 with data array and meta.total (mock repo)
   - List with `status=archived` → only archived rooms returned
   - List with `search=alpha` → only rooms matching name returned
   - List with `status=invalid` → 400 M_BAD_JSON
   - Invalid cursor → 400 M_BAD_JSON
   - limit=0 → 400 M_BAD_JSON
   - Get existing room → 200 with room detail including message_count
   - Get unknown room → 404 M_NOT_FOUND
   - Rooms repository nil → 501

7. `go build ./...` and `make test-unit-go` pass with zero failures after this story.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **List rooms — 200 with pagination** — Go unit test (mock `RoomRepository`)
   - Given: mock repo returns 3 rooms and total=3, next_cursor=""
   - When: `GET /api/v1/admin/rooms` with `instance_admin` role
   - Then: status 200, body `{"data":[...3 rooms...], "meta":{"total":3}}`

2. **List with status=archived filter** — Go unit test
   - Given: mock repo called with `statusFilter="archived"`
   - When: `GET /api/v1/admin/rooms?status=archived`
   - Then: status 200; mock verifies `ListRooms` was called with `statusFilter="archived"`

3. **List with search filter** — Go unit test
   - Given: mock repo called with `search="alpha"`
   - When: `GET /api/v1/admin/rooms?search=alpha`
   - Then: status 200; mock verifies `ListRooms` called with `search="alpha"`

4. **Invalid status param → 400** — Go unit test
   - When: `GET /api/v1/admin/rooms?status=deleted`
   - Then: status 400, body `{"error":{"code":"M_BAD_JSON","message":"invalid status: must be active or archived"}}`

5. **Invalid cursor → 400** — Go unit test
   - When: `GET /api/v1/admin/rooms?cursor=not-valid-base64!`
   - Then: status 400, body `{"error":{"code":"M_BAD_JSON","message":"Invalid cursor"}}`

6. **limit=0 → 400** — Go unit test
   - When: `GET /api/v1/admin/rooms?limit=0`
   - Then: status 400, body `{"error":{"code":"M_BAD_JSON","message":"limit must be between 1 and 100"}}`

7. **Get existing room → 200 with detail** — Go unit test
   - Given: mock repo returns `AdminRoomDetail` with `message_count=42`
   - When: `GET /api/v1/admin/rooms/!abc123:example.com`
   - Then: status 200, body `{"data":{"room_id":"!abc123:example.com","message_count":42,...}}`

8. **Get unknown room → 404** — Go unit test
   - Given: mock repo returns `(nil, nil)`
   - When: `GET /api/v1/admin/rooms/!nonexistent:example.com`
   - Then: status 404, body `{"error":{"code":"M_NOT_FOUND","message":"Room not found"}}`

9. **Rooms repo nil → 501** — Go unit test
   - Given: `AdminServer{}` with no `Rooms` field set
   - When: `GET /api/v1/admin/rooms` with `instance_admin` role
   - Then: status 501

## Tasks / Subtasks

- [ ] Write FAILING tests first — `gateway/internal/api/rooms_handler_test.go` (AC: #6, Acceptance Tests #1–#9)
  - [ ] All test cases with mock `RoomRepository` and `mockRoomRepo` struct

- [ ] Define `AdminRoom`, `AdminRoomDetail`, `RoomRepository` interface in `gateway/internal/api/rooms_repo.go` (AC: #5)
  - [ ] `AdminRoom` struct with all list fields
  - [ ] `AdminRoomDetail` struct embedding `AdminRoom` + `MessageCount`, `MaxMembers`, `PowerLevelsJSON`
  - [ ] `RoomRepository` interface: `ListRooms` + `GetRoom`
  - [ ] `dbRoomRepo` implementing `RoomRepository`
  - [ ] `NewRoomRepo(db *sql.DB) RoomRepository` constructor

- [ ] Implement `dbRoomRepo.ListRooms` SQL query (AC: #1)
  - [ ] COUNT query with search + status filter (no cursor)
  - [ ] List query with search + status + cursor + LIMIT clauses
  - [ ] `member_count` sub-select: `(SELECT COUNT(*) FROM room_members WHERE room_id = r.room_id AND left_at IS NULL)`
  - [ ] Cursor keyset: `(r.created_at, r.room_id) < ($N, $M)` — same pattern as users
  - [ ] `EncodeCursor(lastRoomID, epochMsToISO8601(lastCreatedAt))` for next_cursor

- [ ] Implement `dbRoomRepo.GetRoom` SQL query (AC: #2)
  - [ ] Joins events table for `message_count`: `COUNT(e.event_id)` with `LEFT JOIN events e ON e.room_id = r.room_id`
  - [ ] Returns `(nil, nil)` on `sql.ErrNoRows`

- [ ] Update `gateway/api/openapi.yaml` (AC: #4)
  - [ ] Replace placeholder `GET /admin/rooms` with full spec (cursor, limit, search, status params)
  - [ ] Add `GET /admin/rooms/{roomId}` path
  - [ ] Add schemas `RoomListResponse` and `RoomDetailResponse`
  - [ ] Run `make gen-api` to regenerate `api_gen.go`

- [ ] Implement `ListAdminRooms` handler in `gateway/internal/api/server.go` (AC: #1, #3)
  - [ ] Replace current stub `return ListAdminRooms501Response{}, nil`
  - [ ] Parse limit, cursor, search, status params
  - [ ] Call `s.Rooms.ListRooms(...)` → build response
  - [ ] Audit log (never-raise, same as ListAdminUsers)

- [ ] Implement `GetAdminRoom` handler in `gateway/internal/api/server.go` (AC: #2, #3)
  - [ ] Extract `roomId` from request
  - [ ] Call `s.Rooms.GetRoom(ctx, roomID)` → 404 on nil result
  - [ ] Audit log (never-raise)

- [ ] Add `Rooms RoomRepository` field to `AdminServer` struct in `server.go` (AC: #5)

- [ ] Register `GET /api/v1/admin/rooms/{roomId}` route in `gateway/internal/api/router.go` (AC: #4)
  - [ ] Add `listAdminRoomsHandler(sh)` wrapper (parses cursor/limit/search/status query params) — mirrors `listAdminUsersHandler`
  - [ ] Add `getAdminRoomHandler(sh)` wrapper (extracts `roomId` path value) — mirrors `getAdminUserHandler`
  - [ ] Register both routes with `jwtMW(RequireRole("instance_admin", checker)(...))`

- [ ] Wire `Rooms` field in `gateway/cmd/gateway/main.go`
  - [ ] `roomsRepo := apihandler.NewRoomRepo(bootstrapDB)`
  - [ ] Add `Rooms: roomsRepo` to `adminSrv`

- [ ] Run `make test-unit-go` — zero failures (AC: #7)

## Dev Notes

### Critical: Current rooms Table Schema

The `rooms` table after all migrations (last relevant: `000013_room_power_levels.up.sql`):

```sql
CREATE TABLE rooms (
    room_id          TEXT    PRIMARY KEY,
    name             TEXT,
    visibility       TEXT    NOT NULL DEFAULT 'private',
    created_at       BIGINT  NOT NULL,          -- epoch milliseconds (same as users table)
    archived_at      BIGINT,                    -- epoch ms; NULL = active
    power_levels_json TEXT   NOT NULL DEFAULT '{}'  -- added by migration 000013
);
CONSTRAINT rooms_visibility_check CHECK (visibility IN ('public', 'private'))
```

**MISSING columns that the epics spec mentions but do NOT exist in the current schema:**
- `topic` — NOT in DB; omit from SQL, return empty string `""`
- `canonical_alias` — NOT in DB; omit from SQL, return empty string `""`
- `creator_user_id` — NOT in DB; omit from SQL, return empty string `""`
- `max_members` — NOT in DB (Story 6.8 adds it); return `null` / `0`
- `admin_note` — NOT in DB; return empty string `""`
- `is_public` — derive from `visibility = 'public'` (boolean)
- `status` — derive from `archived_at`: NULL → `"active"`, NOT NULL → `"archived"`

**Action:** Implement the `AdminRoom` struct with all fields (including the missing ones) and populate with defaults for fields not yet in DB. Add a `TODO(story-6.8)` comment for `max_members`.

### Critical: created_at is Epoch Milliseconds (BIGINT)

The `rooms.created_at` column stores UNIX epoch **milliseconds** as `BIGINT` — identical to `users.created_at`. Use the existing `epochMsToISO8601` helper from `users_repo.go`. This is NOT a `TIMESTAMPTZ`. Do not use `time.Time` scanning directly.

### Critical: Keyset Cursor — rooms Use Same Pattern as Users

The cursor for rooms pagination encodes `(created_at, room_id)` — exactly like users use `(created_at, user_id)`. The existing `EncodeCursor` and `DecodeCursor` functions work unchanged. The keyset comparison uses the epoch ms value for `created_at`:

```go
// Parse cursor's afterCreatedAt (ISO 8601) back to epoch ms:
afterCreatedAtMs, _ := parseISO8601ToEpochMs(afterCreatedAt)
cursorClause = fmt.Sprintf(` AND (r.created_at, r.room_id) < ($%d, $%d)`, n, n+1)
args = append(args, afterCreatedAtMs, afterID) // afterID is the room_id
```

The `parseISO8601ToEpochMs` function already exists in `users_repo.go` — do NOT redeclare it. Both `users_repo.go` and `rooms_repo.go` are in the same package `api`, so the function is directly accessible.

### Critical: member_count via Sub-Select

The `left_at` column in `room_members` is a `BIGINT` (epoch ms), not a boolean. `left_at IS NULL` means the member has NOT left (still a member). Use a correlated sub-select to avoid N+1:

```sql
SELECT r.room_id,
       COALESCE(r.name, ''),
       r.visibility,
       r.created_at,
       r.archived_at,
       r.power_levels_json,
       (SELECT COUNT(*) FROM room_members rm WHERE rm.room_id = r.room_id AND rm.left_at IS NULL) AS member_count
FROM rooms r
WHERE 1=1
...
```

### Critical: OpenAPI Changes — Replace Placeholder

The current `GET /admin/rooms` in `openapi.yaml` (line ~293) is a placeholder with no query params and `EmptyResponse`. Story 6.7 REPLACES it entirely. After editing and running `make gen-api`, the generated `api_gen.go` will add:
- `ListAdminRoomsParams` struct with Cursor, Limit, Search, Status fields
- `GetAdminRoom` operation (new)
- `ListAdminRooms200JSONResponse` updated to use `RoomListResponse`
- `GetAdminRoom200JSONResponse`, `GetAdminRoom404JSONResponse`, `GetAdminRoom501Response`
- `StrictServerInterface.GetAdminRoom(ctx, GetAdminRoomRequestObject) (GetAdminRoomResponseObject, error)` method (NEW — `AdminServer` must implement it or the compile check fails)

**YAML to add for `/admin/rooms/{roomId}`:**
```yaml
/admin/rooms/{roomId}:
  get:
    operationId: GetAdminRoom
    summary: Get single room (instance_admin required)
    parameters:
      - name: roomId
        in: path
        required: true
        schema:
          type: string
    responses:
      "200":
        description: Room details
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/RoomDetailResponse"
      "404":
        description: Room not found
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
      "501":
        description: Not implemented
```

**Updated `/admin/rooms` GET spec (replace placeholder):**
```yaml
/admin/rooms:
  get:
    operationId: ListAdminRooms
    summary: List rooms (instance_admin required)
    parameters:
      - name: cursor
        in: query
        required: false
        schema:
          type: string
      - name: limit
        in: query
        required: false
        schema:
          type: integer
      - name: search
        in: query
        required: false
        schema:
          type: string
      - name: status
        in: query
        required: false
        schema:
          type: string
    responses:
      "200":
        description: Room list
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/RoomListResponse"
      "400":
        description: Bad request (invalid cursor, limit, or status)
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
      "501":
        description: Not implemented
```

**Schemas to add:**
```yaml
RoomListResponse:
  type: object
  required:
    - data
    - meta
  properties:
    data:
      type: array
      items:
        type: object
    meta:
      type: object
      required:
        - total
      properties:
        total:
          type: integer
        next_cursor:
          type: string
RoomDetailResponse:
  type: object
  required:
    - data
  properties:
    data:
      type: object
```

### Critical: AdminServer Struct — Add Rooms Field

Add `Rooms RoomRepository` to `AdminServer` in `server.go`, analogous to `Users UserRepository`:

```go
type AdminServer struct {
    DB           *sql.DB
    CoreClient   pb.CoreServiceClient
    Users        UserRepository
    Deactivation DeactivationRepository
    Roles        RoleOverrideRepository
    Rooms        RoomRepository  // Story 6.7
}
```

The `var _ StrictServerInterface = (*AdminServer)(nil)` compile check will fail after `make gen-api` adds `GetAdminRoom` to `StrictServerInterface`. Implement `GetAdminRoom` in `server.go` immediately.

### Critical: StrictServerInterface Will Gain GetAdminRoom After gen-api

After `make gen-api`, the generated `StrictServerInterface` will include:
```go
GetAdminRoom(ctx context.Context, request GetAdminRoomRequestObject) (GetAdminRoomResponseObject, error)
```

The current `ListAdminRooms` in `server.go` returns `ListAdminRooms501Response{}` — this stub must be replaced with the real implementation. The `GetAdminRoom` method must be added as a new function.

### Critical: Router Changes — Two New Route Wrappers

Pattern from `router.go` (Story 6.4): query-param routes need wrappers because `ServerInterface` methods take typed params, not raw `*http.Request`. Add:

```go
// listAdminRoomsHandler wraps sh.ListAdminRooms — parses cursor/limit/search/status
func listAdminRoomsHandler(sh ServerInterface) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var params ListAdminRoomsParams

        if v := r.URL.Query().Get("cursor"); v != "" {
            params.Cursor = &v
        }
        if v := r.URL.Query().Get("limit"); v != "" {
            n, err := strconv.Atoi(v)
            if err != nil {
                zero := 0
                params.Limit = &zero
            } else {
                params.Limit = &n
            }
        }
        if v := r.URL.Query().Get("search"); v != "" {
            params.Search = &v
        }
        if v := r.URL.Query().Get("status"); v != "" {
            params.Status = &v
        }

        sh.ListAdminRooms(w, r, params)
    })
}

// getAdminRoomHandler wraps sh.GetAdminRoom — extracts {roomId}
func getAdminRoomHandler(sh ServerInterface) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        roomID := r.PathValue("roomId")
        sh.GetAdminRoom(w, r, roomID)
    })
}
```

**Also update the existing direct registration** in `RegisterAdminRoutes` — replace:
```go
mux.Handle("GET /api/v1/admin/rooms", jwtMW(RequireRole("instance_admin", checker)(http.HandlerFunc(sh.ListAdminRooms))))
```
with:
```go
mux.Handle("GET /api/v1/admin/rooms", jwtMW(RequireRole("instance_admin", checker)(listAdminRoomsHandler(sh))))
mux.Handle("GET /api/v1/admin/rooms/{roomId}", jwtMW(RequireRole("instance_admin", checker)(getAdminRoomHandler(sh))))
```

### Critical: router_test.go — Existing Test for /api/v1/admin/rooms

`router_test.go` line 83 already includes `/api/v1/admin/rooms` in the no-auth test. After this story, the route handler signature changes (now takes params). The existing test hits the GET route and expects 401 — this still works because the auth check runs before param parsing. No change to `router_test.go` is required for the existing test.

However, after `make gen-api` changes the `ServerInterface.ListAdminRooms` signature from `ListAdminRooms(w, r)` to `ListAdminRooms(w, r, params ListAdminRoomsParams)`, the `sh.ListAdminRooms` bare function reference used in the current `RegisterAdminRoutes` will no longer compile as an `http.HandlerFunc`. The wrapper function above fixes this.

### Critical: AdminRoom and AdminRoomDetail Struct Design

```go
// AdminRoom is the JSON-serialisable representation of a room for the Admin API (Story 6.7).
type AdminRoom struct {
    RoomID        string  `json:"room_id"`
    Name          string  `json:"name"`
    Topic         string  `json:"topic"`          // "" — not in DB yet (Story 6.8)
    CanonicalAlias string `json:"canonical_alias"` // "" — not in DB yet
    Visibility    string  `json:"visibility"`      // "public" | "private"
    IsPublic      bool    `json:"is_public"`       // derived: visibility == "public"
    MemberCount   int     `json:"member_count"`
    Status        string  `json:"status"`          // "active" | "archived"
    CreatedAt     string  `json:"created_at"`      // ISO 8601 (reuse epochMsToISO8601)
    CreatorUserID string  `json:"creator_user_id"` // "" — not in DB yet
    AdminNote     string  `json:"admin_note"`      // "" — not in DB yet
}

// AdminRoomDetail extends AdminRoom with detail-only fields.
// TODO(story-6.8): MaxMembers will be populated after rooms.max_members column is added.
type AdminRoomDetail struct {
    AdminRoom
    MaxMembers      int    `json:"max_members"`       // 0 until Story 6.8 adds the column
    MessageCount    int    `json:"message_count"`
    PowerLevelsJSON string `json:"power_levels_json"` // raw JSON string from DB
}
```

### Critical: ListAdminRooms Handler — Status Validation Order

Validate `status` BEFORE calling `DecodeCursor`. Invalid status param should short-circuit with 400 immediately:

```go
// 1. Validate status param first
statusFilter := ""
if request.Params.Status != nil && *request.Params.Status != "" {
    s := *request.Params.Status
    if s != "active" && s != "archived" {
        return &listRooms400Resp{msg: "invalid status: must be active or archived"}, nil
    }
    statusFilter = s
}
// 2. Parse limit
// 3. Parse cursor (DecodeCursor)
// 4. Parse search
// 5. Call repo
```

### Critical: Audit Log for List vs Get

- `ListAdminRooms`: `audit.LogEvent(..., "admin_room_viewed", "room", "", nil, ...)` — `targetID=""` (no single room, matches ListAdminUsers pattern)
- `GetAdminRoom`: `audit.LogEvent(..., "admin_room_viewed", "room", roomID, nil, ...)` — `targetID=roomID`

### Critical: Response Type Pattern (Mirrors Users)

Create private response types implementing the generated response object interfaces. Names must not clash with existing types:

```go
// listAdminRoomsOKResponse implements ListAdminRoomsResponseObject
type listAdminRoomsOKResponse struct {
    rooms      []AdminRoom
    total      int
    nextCursor string
}

func (resp *listAdminRoomsOKResponse) VisitListAdminRoomsResponse(w http.ResponseWriter) error { ... }

// listRooms400Resp implements ListAdminRoomsResponseObject
type listRooms400Resp struct{ msg string }
func (r *listRooms400Resp) VisitListAdminRoomsResponse(w http.ResponseWriter) error { ... }

// getAdminRoomOKResponse implements GetAdminRoomResponseObject
type getAdminRoomOKResponse struct{ detail *AdminRoomDetail }
func (resp *getAdminRoomOKResponse) VisitGetAdminRoomResponse(w http.ResponseWriter) error { ... }

// getAdminRoom404Response implements GetAdminRoomResponseObject
type getAdminRoom404Response struct{}
func (r *getAdminRoom404Response) VisitGetAdminRoomResponse(w http.ResponseWriter) error { ... }
```

### Critical: main.go Wiring

The `adminSrv` in `main.go` must gain `Rooms`:

```go
roomsRepo := apihandler.NewRoomRepo(bootstrapDB)
adminSrv := &apihandler.AdminServer{
    DB:           bootstrapDB,
    CoreClient:   coreClient.CoreServiceClient(),
    Users:        apihandler.NewUserRepoWithRoles(bootstrapDB, rolesRepo),
    Deactivation: apihandler.NewDeactivationRepo(bootstrapDB),
    Roles:        rolesRepo,
    Rooms:        roomsRepo, // Story 6.7
}
```

### Critical: Next Migration Number is 000036

The last migration is `000035_role_overrides`. This story does NOT need a new migration — all required columns already exist. The `rooms` table has `archived_at` (NULL = active, NOT NULL = archived). No schema change is needed for Story 6.7.

### Critical: Fail-Open on DB Errors (List)

Mirror the users pattern: if the repository call fails, return the error to the framework (which returns 500). Do NOT silently return empty lists.

### Previous Story Learnings (6-6)

1. **MINOR-6 fix pattern:** Body-pre-check wrappers in `router.go` for POST routes with required bodies. Not needed for Story 6.7 (both routes are GET — no request body).

2. **MINOR-1 pattern:** Always `slog.Warn(...)` when `CoreClient` is nil and audit is skipped. Apply same pattern to `ListAdminRooms` and `GetAdminRoom`.

3. **StrictServerInterface compile check:** `var _ StrictServerInterface = (*AdminServer)(nil)` in `server.go` will fail immediately after `make gen-api` adds `GetAdminRoom`. Fix by implementing the handler.

4. **Empty slice vs nil:** In `VisitListAdminRoomsResponse`, always initialise `data` to `[]AdminRoom{}` (not nil) when the list is empty to avoid `null` in JSON (mirrors `listAdminUsersOKResponse.VisitListAdminUsersResponse`).

5. **router_test.go already covers `/api/v1/admin/rooms`** in the no-auth test — do NOT add a duplicate registration pattern that would cause a ServeMux duplicate-pattern panic.

6. **Partial-page next_cursor:** Only encode next_cursor when `len(rooms) == limit`. A partial page (len < limit) means we've reached the end — no next cursor. Identical logic to users.

### File Inventory

**Files to CREATE (new):**
- `gateway/internal/api/rooms_repo.go` — `AdminRoom`, `AdminRoomDetail`, `RoomRepository` interface + `dbRoomRepo` implementation
- `gateway/internal/api/rooms_handler_test.go` — ATDD unit tests (written FIRST, failing)

**Files to UPDATE:**
- `gateway/api/openapi.yaml` — replace placeholder GET /admin/rooms + add GET /admin/rooms/{roomId} + RoomListResponse/RoomDetailResponse schemas
- `gateway/api/api_gen.go` — regenerated via `make gen-api` (do not edit manually)
- `gateway/internal/api/server.go` — add `Rooms RoomRepository` field; implement `ListAdminRooms` (replace stub) + add `GetAdminRoom` handler + response types
- `gateway/internal/api/router.go` — replace direct `sh.ListAdminRooms` registration with `listAdminRoomsHandler(sh)` wrapper; add `GET /api/v1/admin/rooms/{roomId}` route + `getAdminRoomHandler`; add two new wrapper functions
- `gateway/cmd/gateway/main.go` — instantiate `roomsRepo` and add `Rooms: roomsRepo` to `adminSrv`

**Files NOT to touch:**
- `gateway/internal/api/users_repo.go` — no changes needed (reuse helpers from same package)
- `gateway/internal/api/router_test.go` — existing `/api/v1/admin/rooms` test still passes (auth check runs before param parsing)
- Any migration file — no schema change needed for Story 6.7

### Existing Code Context: Package-Level Helpers Available (No Redeclaration Needed)

From `users_repo.go` (same package `api`) — use directly, do NOT copy:
- `epochMsToISO8601(epochMs int64) string`
- `parseISO8601ToEpochMs(iso string) (int64, error)`
- `mergeRoles(base, extras []string) []string` — not needed for rooms, but available

From `pagination.go` (same package):
- `EncodeCursor(afterID, afterCreatedAt string) string`
- `DecodeCursor(cursor string) (afterID, afterCreatedAt string, err error)`
- `ErrInvalidCursor`

From `response.go` (same package):
- `APIResponse[T]`, `Meta`, `APIError` — may use for type alignment if needed

### Project Structure

- All new Go files: `gateway/internal/api/` package `api`, build constraint `//go:build go1.22`
- Test files: same package `api` or `api_test` — follow existing test file pattern (prefer `api_test` external test package for blackbox tests, `api` package for whitebox mock tests — check existing rooms_handler_test.go pattern from users)
- `rooms_handler_test.go` should use `package api_test` (external) with mock that implements `RoomRepository` interface, same as `users_handler_test.go`

### References

- Rooms table schema: `gateway/migrations/000009_rooms.up.sql`, `000013_room_power_levels.up.sql`
- Events table schema: `gateway/migrations/000010_events.up.sql`
- room_members table: `gateway/migrations/000009_rooms.up.sql`
- Users repo pattern: `gateway/internal/api/users_repo.go`
- List users handler: `gateway/internal/api/server.go` (ListAdminUsers, GetAdminUser)
- Router wrappers: `gateway/internal/api/router.go` (listAdminUsersHandler, getAdminUserHandler)
- Cursor pagination: `gateway/internal/api/pagination.go`
- OpenAPI placeholder: `gateway/api/openapi.yaml` lines ~293–305
- api_gen.go stub: `gateway/internal/api/api_gen.go` lines 626–648
- Epic 6 Story 6.7 spec: `_bmad-output/planning-artifacts/epics.md` line 2799

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

| File | Action | Notes |
|------|--------|-------|
| `gateway/internal/api/rooms_repo.go` | CREATE | RoomRepository interface + dbRoomRepo; AdminRoom + AdminRoomDetail structs |
| `gateway/internal/api/rooms_handler_test.go` | CREATE | ATDD unit tests (written first, RED phase) |
| `gateway/api/openapi.yaml` | UPDATE | Replace placeholder GET /admin/rooms; add GET /admin/rooms/{roomId}; add RoomListResponse/RoomDetailResponse schemas |
| `gateway/api/api_gen.go` | REGEN | Regenerated via make gen-api (do not edit manually) |
| `gateway/internal/api/server.go` | UPDATE | Add Rooms field; implement ListAdminRooms (replace stub) + GetAdminRoom + response types |
| `gateway/internal/api/router.go` | UPDATE | Replace direct sh.ListAdminRooms with wrapper; add GET /admin/rooms/{roomId} route + two new wrapper functions |
| `gateway/cmd/gateway/main.go` | UPDATE | Instantiate roomsRepo; add Rooms field to adminSrv |

## Change Log

| Date | Author | Change |
|------|--------|--------|
| 2026-05-01 | Story context engine | Story created from epics.md AC analysis + full codebase read (rooms table schema, users_repo.go patterns, router.go wrappers, api_gen.go stub analysis) |
