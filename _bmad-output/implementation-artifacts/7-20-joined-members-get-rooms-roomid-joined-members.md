---
id: 7-20
type: feature
security_review: not-needed
created: 2026-04-30
---

# Story 7.20: Joined Members — GET /rooms/{roomId}/joined_members

Status: ready-for-dev

## Story

As an end-user,
I want to retrieve a compact map of all currently joined members of a room,
so that my Matrix client can efficiently render participant lists and avatar rows.

## Context / Background

Matrix defines two member-listing endpoints with different shapes:

- `GET /rooms/{roomId}/members` (already implemented in `members.go`) — returns an array of full
  `m.room.member` state events with all membership values (join, leave, invite, ban), paginated.
- `GET /rooms/{roomId}/joined_members` (this story) — returns only users with `membership=join`,
  in a compact map keyed by MXID, with just `display_name` and `avatar_url`. No pagination.

The compact format is preferred by clients for sidebar rendering because it avoids iterating a
potentially large event array.

The existing `GetRoomState` gRPC call returns `members` (a list of joined MXIDs). Profile data
(displayname, avatar_url) is read directly from PostgreSQL via the `ProfileDB` interface that
`profile.go` already defines. This handler can use the same pattern: gRPC for membership, DB for
profile data.

Handler goes into `gateway/internal/matrix/members.go` (existing file). Route registration in
`gateway/cmd/gateway/main.go` with `jwtMiddleware` (authenticated endpoint).

**Response shape:**
```json
{
  "joined": {
    "@alice:server": { "display_name": "Alice", "avatar_url": "mxc://server/abc" },
    "@bob:server":   { "display_name": "Bob",   "avatar_url": null }
  }
}
```

## Acceptance Criteria

1. `GET /_matrix/client/v3/rooms/{roomId}/joined_members` returns HTTP 200 with a `joined` map
   containing only users whose current membership is `join`.

2. Each value in the map has `display_name` (string or null) and `avatar_url` (string or null) from
   the user's profile. Missing profiles return null for both fields; no 404 is raised for individual
   users.

3. The requesting user must be a current room member — returns `403 M_FORBIDDEN` otherwise.

4. Returns `404 M_NOT_FOUND` if the room does not exist.

5. No pagination — all joined members are returned in a single response.

6. JWT required — `jwtMiddleware` enforces auth before the handler runs.

7. `display_name` field is omitted from the per-user object if it is null (Matrix spec allows
   omission); `avatar_url` follows the same rule. (Either convention — omit or explicit null — is
   acceptable as long as it is consistent; document the choice in the handler comment.)

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [GetJoinedMembers_ReturnsCompactMap] — Godog
   - Given: authenticated user `@alice:server` who is a member of room `!test:server`; room also
     has member `@bob:server` with displayname "Bob"
   - When: `GET /_matrix/client/v3/rooms/!test:server/joined_members`
   - Then: HTTP 200; body contains `"joined"` object; `@alice:server` and `@bob:server` are keys;
     `@bob:server` has `"display_name": "Bob"`

2. [GetJoinedMembers_Forbidden_NonMember] — Godog
   - Given: authenticated user `@carol:server` who is NOT a member of `!private:server`
   - When: `GET /_matrix/client/v3/rooms/!private:server/joined_members`
   - Then: HTTP 403 `{"errcode":"M_FORBIDDEN",...}`

3. [GetJoinedMembers_NotFound_UnknownRoom] — Godog
   - Given: authenticated user with valid JWT
   - When: `GET /_matrix/client/v3/rooms/!doesnotexist:server/joined_members`
   - Then: HTTP 404 `{"errcode":"M_NOT_FOUND",...}`

4. [GetJoinedMembers_ProfileNull_WhenNoProfile] — Go unit test (httptest)
   - Given: gRPC mock returns members `["@new:server"]`; ProfileDB returns `ErrProfileNotFound`
   - When: handler processes response
   - Then: `@new:server` appears in `joined` map with `display_name: null` and `avatar_url: null`

## Implementation Notes

**Files to modify:**

- `gateway/internal/matrix/members.go` — Add `GetJoinedMembersHandler` struct with its own minimal
  interface (consumer-defined per Go convention):
  ```go
  type GetJoinedMembersCoreClient interface {
      GetRoomState(ctx context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error)
  }
  ```
  Add `GetJoinedMembers` method. Profile lookup uses `ProfileDB` (same interface as `profile.go`).
  For each MXID in `resp.Members`, call `db.GetProfile(ctx, mxid)` — tolerate `ErrProfileNotFound`
  by setting both fields to null.
- `gateway/cmd/gateway/main.go` (~line 404+) — Register:
  ```
  GET /_matrix/client/v3/rooms/{roomId}/joined_members
  ```
  wrapped in `jwtMiddleware`.
- `gateway/internal/matrix/members_test.go` — Unit tests for all ACs using `httptest`.
- `gateway/features/joined_members.feature` — Godog feature file (written first, red phase).

**Performance note:** Profile lookups for N members result in N sequential DB queries in this MVP.
This is acceptable for Phase 1 (rooms are small). A batch `GetProfiles` DB method is a Phase 2
optimisation — add a TODO comment but do not implement it now.

**Error-mapping pattern** (same as `GetRoomMembers`):
- `codes.PermissionDenied` → 403 `M_FORBIDDEN`
- `codes.NotFound` → 404 `M_NOT_FOUND`
- `codes.Unavailable` → 503 `M_UNAVAILABLE`
- default → 500 `M_UNKNOWN`

No proto changes needed — `GetRoomStateRequest/Response` is already sufficient (members field
contains joined MXIDs). This story requires no `make proto` run.
