---
id: 7-27
type: feature
security_review: not-needed
created: 2026-04-30
---

# Story 7.27: Public Room Directory — GET/POST /publicRooms

Status: ready-for-dev

## Story

As an end-user,
I want to browse a list of public rooms on this Nebu instance and search by name,
so that I can discover and join rooms without needing a direct invite.

## Context / Background

The Matrix spec defines `GET /_matrix/client/v3/publicRooms` (unauthenticated, rate-limited) and `POST /_matrix/client/v3/publicRooms` (authenticated, supports filter body). Both return the same paginated response shape.

Only rooms where `join_rule = public` are included. Pagination uses an opaque cursor token (`next_batch` / `since` query param) backed by the last seen `room_id` in lexicographic order, which is stable and simple to implement without offset arithmetic.

## Acceptance Criteria

1. `GET /_matrix/client/v3/publicRooms?limit=N&since=<cursor>` returns a paginated list of public rooms. `limit` defaults to 20, capped at 100. Response shape: `{"chunk":[...],"next_batch":"...","total_room_count_estimate":N}`. `next_batch` is omitted when there are no more pages.

2. `POST /_matrix/client/v3/publicRooms` accepts `{"limit":N,"since":"...","filter":{"generic_search_term":"foo"}}`. `generic_search_term` filters rooms by name or topic using a case-insensitive substring match (ILIKE). Returns same shape as GET.

3. Each room entry in `chunk` contains at minimum: `room_id`, `name`, `topic` (if set), `num_joined_members`, `world_readable` (false for all Nebu rooms), `guest_can_join` (false).

4. `num_joined_members` reflects the live member count from the Room GenServer (via gRPC), not a stale DB value.

5. Only rooms with `join_rule = public` appear. Private, invite-only, or archived rooms are excluded.

6. `GET /publicRooms` is accessible without a JWT (rate-limited via `looseRL` middleware). `POST /publicRooms` requires a valid JWT.

7. Cursor-based pagination is stable: requesting page 2 with the `next_batch` from page 1 always returns the correct next page, even if new rooms are created between requests.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [GET returns public rooms paginated] — Godog (`gateway/features/public_rooms.feature`)
   - Given: 3 public rooms (`!pub1`, `!pub2`, `!pub3`) and 1 private room (`!priv1`) exist
   - When: GET `/_matrix/client/v3/publicRooms?limit=2` (unauthenticated)
   - Then: HTTP 200, `chunk` has 2 entries, both are public rooms, `next_batch` is present

2. [GET page 2 via next_batch cursor] — Godog
   - Given: 3 public rooms; page 1 returned 2 rooms with `next_batch`
   - When: GET `/_matrix/client/v3/publicRooms?limit=2&since=<next_batch>`
   - Then: `chunk` has 1 remaining room, `next_batch` is absent

3. [POST with filter narrows results] — Godog
   - Given: public rooms named "General", "Engineering", "Random"
   - When: POST `/_matrix/client/v3/publicRooms` with `{"filter":{"generic_search_term":"engi"}}` and valid JWT
   - Then: `chunk` contains "Engineering" only

4. [Private room excluded from directory] — Godog
   - Given: private room `!priv1` exists
   - When: GET `/_matrix/client/v3/publicRooms` (unauthenticated)
   - Then: `!priv1` is not present in any `chunk` entry across all pages

5. [num_joined_members is accurate] — Go httptest (`gateway/internal/matrix/public_rooms_test.go`)
   - Given: public room with 5 members tracked in Room GenServer
   - When: GET `/publicRooms`
   - Then: the matching chunk entry has `num_joined_members` = 5

6. [POST requires JWT] — Go httptest
   - Given: no Authorization header
   - When: POST `/_matrix/client/v3/publicRooms` with valid body
   - Then: HTTP 401, `errcode` = `M_MISSING_TOKEN`

## Implementation Notes

**New handler file:** `gateway/internal/matrix/public_rooms.go`
- `GetPublicRoomsHandler` (unauthenticated) and `PostPublicRoomsHandler` (JWT required) — share a common `listPublicRooms(ctx, limit, since, filterTerm string)` function.
- Cursor: `since` is the `room_id` of the last item on the previous page. SQL: `WHERE join_rule = 'public' AND room_id > $since ORDER BY room_id LIMIT $limit+1` (fetch one extra to detect next page).
- `total_room_count_estimate`: a fast `SELECT COUNT(*)` from the rooms table where `join_rule = 'public'` (approximate, ignores filter).

**gRPC proto additions** (`proto/core.proto`):
```proto
rpc ListPublicRooms(ListPublicRoomsRequest) returns (ListPublicRoomsResponse);
// Request: limit, since (cursor), filter_term
// Response: rooms (list of RoomSummary), next_cursor, total_estimate
```

The gRPC handler in Elixir queries the DB for the room list and calls the appropriate Room GenServer(s) for live member counts. For rooms whose GenServer is not running, fall back to the DB member count.

**Route registration** in `gateway/cmd/gateway/main.go`:
```
GET  /_matrix/client/v3/publicRooms  → looseRL(GetPublicRoomsHandler)
POST /_matrix/client/v3/publicRooms  → jwtMiddleware(bodyLimit1MiB(PostPublicRoomsHandler))
```

**Filter implementation** — `generic_search_term` is passed through to the Elixir gRPC handler which executes `ILIKE '%term%'` on `room_name` and `room_topic` columns.

## Tasks

- [ ] Write failing Godog scenarios in `gateway/features/public_rooms.feature`
- [ ] Write failing Go httptest in `gateway/internal/matrix/public_rooms_test.go`
- [ ] Extend `proto/core.proto` with `ListPublicRooms`; run `make proto`
- [ ] Implement Elixir gRPC handler (DB query + live member count resolution)
- [ ] Implement `gateway/internal/matrix/public_rooms.go`
- [ ] Register routes in `main.go`
- [ ] Run `make test-unit-go` + `make test-unit-elixir` — all pass
- [ ] Run `make test-integration` — Godog scenarios green
