---
id: 7-24
type: feature
security_review: not-needed
created: 2026-04-30
---

# Story 7.24: Account Data pro Raum — GET/PUT /user/{userId}/rooms/{roomId}/account_data/{type}

Status: ready-for-dev

## Story

As an end-user,
I want to store and retrieve arbitrary per-room configuration data (e.g. notification settings, custom metadata) associated with my account for a specific room,
so that my Matrix client can persist room-scoped preferences across devices and sessions.

## Context / Background

The global account data endpoints (`GET/PUT /user/{userId}/account_data/{type}`) exist as stubs. This story implements the room-scoped variant. Room account data is stored as (userId, roomId, eventType, content) tuples and surfaced in the `/sync` response under `rooms.join.{roomId}.account_data.events`.

Affected files:
- New handler: `gateway/internal/matrix/account_data.go`
- New migration: `migrations/000029_room_account_data.up.sql`
- New gRPC calls: `GetRoomAccountData` + `SetRoomAccountData` in `proto/core.proto`
- Elixir gRPC handler: `core/apps/session_manager/`
- Sync integration: `gateway/internal/matrix/sync.go`

## Acceptance Criteria

1. `PUT /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}` stores arbitrary JSON content for the (userId, roomId, type) triple; returns `{}` with HTTP 200.

2. `GET /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}` returns the stored JSON content object; returns M_NOT_FOUND (HTTP 404) if no data exists for that triple.

3. If the `userId` in the path does not match the authenticated user's JWT subject, both endpoints return M_FORBIDDEN (HTTP 403).

4. After a successful PUT, the next `/sync` response for that user includes the account_data event under `rooms.join.{roomId}.account_data.events` with the correct `type` and `content`.

5. Migration `000029_room_account_data.up.sql` creates table `room_account_data (user_id TEXT NOT NULL, room_id TEXT NOT NULL, event_type TEXT NOT NULL, content JSONB NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (user_id, room_id, event_type))` with an RLS policy allowing `nebu_app` to read/write only rows where `user_id = current_setting('app.user_id')`.

6. Concurrent PUTs for the same triple use upsert semantics (INSERT … ON CONFLICT DO UPDATE) — last write wins.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [PUT stores and GET retrieves room account data] — Godog (`gateway/features/account_data.feature`)
   - Given: authenticated user `@alice:nebu.test` is a member of room `!room1:nebu.test`
   - When: PUT `/_matrix/client/v3/user/@alice:nebu.test/rooms/!room1:nebu.test/account_data/m.fully_read` with body `{"event_id":"$abc"}`
   - Then: HTTP 200 `{}`; subsequent GET returns `{"event_id":"$abc"}`

2. [GET returns M_NOT_FOUND when no data exists] — Godog
   - Given: authenticated user `@alice:nebu.test`, room `!room1:nebu.test`, no account data for `m.fully_read`
   - When: GET `/_matrix/client/v3/user/@alice:nebu.test/rooms/!room1:nebu.test/account_data/m.fully_read`
   - Then: HTTP 404, `errcode` = `M_NOT_FOUND`

3. [userId mismatch returns M_FORBIDDEN] — Godog
   - Given: authenticated user `@alice:nebu.test`
   - When: PUT `/_matrix/client/v3/user/@bob:nebu.test/rooms/!room1:nebu.test/account_data/m.tag` with any body
   - Then: HTTP 403, `errcode` = `M_FORBIDDEN`

4. [PUT triggers account_data event in next /sync] — Godog
   - Given: authenticated user `@alice:nebu.test`, PUT `m.fully_read` data to room `!room1:nebu.test`
   - When: GET `/sync` with `since` token after the PUT
   - Then: response body contains `rooms.join.!room1:nebu.test.account_data.events` array with one entry of `type` = `m.fully_read`

5. [Upsert semantics on concurrent PUTs] — Go httptest (`gateway/internal/matrix/account_data_test.go`)
   - Given: existing `m.tag` account data `{"tags":{"m.favourite":{}}}` for a user/room
   - When: PUT same type with new body `{"tags":{}}`
   - Then: GET returns `{"tags":{}}` (last write wins), no error

## Implementation Notes

**New handler file:** `gateway/internal/matrix/account_data.go`

```go
// GetRoomAccountDataHandler and PutRoomAccountDataHandler
// Route pattern: {userId} path param validated against jwtMiddleware subject
// gRPC: core.GetRoomAccountData(user_id, room_id, event_type) → content JSON
// gRPC: core.SetRoomAccountData(user_id, room_id, event_type, content JSON) → ok
```

**Route registration** in `gateway/cmd/gateway/main.go`:
```
GET  /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}  → jwtMiddleware(GetRoomAccountDataHandler)
PUT  /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}  → jwtMiddleware(bodyLimit1MiB(PutRoomAccountDataHandler))
```

**Migration:** `migrations/000029_room_account_data.up.sql` — create table + RLS policy (pattern follows `sessions` table RLS in existing migrations).

**gRPC proto additions** (`proto/core.proto`):
```proto
rpc GetRoomAccountData(GetRoomAccountDataRequest) returns (GetRoomAccountDataResponse);
rpc SetRoomAccountData(SetRoomAccountDataRequest) returns (SetRoomAccountDataResponse);
```

**Elixir:** New functions in `core/apps/session_manager/lib/session_manager/grpc_handler.ex` — Ecto upsert on `room_account_data` with `on_conflict: :replace_all`.

**Sync integration:** In `sync.go`, after fetching room join data, call `GetRoomAccountData` for each joined room and append results to `account_data.events`.

## Tasks

- [ ] Write failing Godog scenarios in `gateway/features/account_data.feature`
- [ ] Write failing Go httptest in `gateway/internal/matrix/account_data_test.go`
- [ ] Add migration `migrations/000029_room_account_data.up.sql`
- [ ] Extend `proto/core.proto` with `GetRoomAccountData` + `SetRoomAccountData`; run `make proto`
- [ ] Implement Elixir gRPC handler in `session_manager`
- [ ] Implement `gateway/internal/matrix/account_data.go`
- [ ] Register routes in `main.go`
- [ ] Integrate room account_data into `/sync` response in `sync.go`
- [ ] Run `make test-unit-go` + `make test-unit-elixir` — all pass
- [ ] Run `make test-integration` — Godog scenarios green
