---
id: 7-23
type: feature
security_review: not-needed
created: 2026-04-30
---

# Story 7.23: Room Aliases — GET /rooms/{roomId}/aliases

Status: review

## Story

As an end-user,
I want to retrieve the list of aliases associated with a room,
so that my Matrix client can display canonical room addresses and allow sharing via human-readable names.

## Context / Background

Matrix rooms can have one or more human-readable aliases (e.g. `#general:server`). The alias
directory endpoints (`PUT/DELETE/GET /_matrix/client/v3/directory/room/{roomAlias}`) are currently
stubs in the codebase. Alias storage is deferred to a future epic.

This story implements `GET /rooms/{roomId}/aliases` as a **spec-compliant stub** that:
- Validates auth (JWT required, user must be a room member or the room must be world-readable).
- Validates the room exists.
- Returns `{"aliases":[]}` in the MVP (no alias storage yet).

The handler is deliberately written to be extensible: when alias storage is added, a gRPC call to
a future `GetRoomAliases` RPC can be dropped in without changing the HTTP layer.

Auth visibility rule per Matrix spec: users who have ever been in the room (or the room has
`history_visibility: world_readable`) can see aliases. For the MVP, enforce JWT + current
membership only (simplest correct implementation).

Handler goes in `gateway/internal/matrix/rooms.go`. Route registration with `jwtMiddleware` in
`gateway/cmd/gateway/main.go`.

## Acceptance Criteria

1. `GET /_matrix/client/v3/rooms/{roomId}/aliases` returns HTTP 200 with body `{"aliases":[]}` for
   an authenticated user who is a current member of the room (MVP: no aliases stored yet).

2. Returns `403 M_FORBIDDEN` when the requesting user is not a current room member and the room
   does not have `history_visibility: world_readable`.

3. Returns `404 M_NOT_FOUND` when the room does not exist.

4. JWT is required — requests without a valid token are rejected by `jwtMiddleware` before the
   handler is reached.

5. The response always contains the `aliases` key, even when the array is empty (never omit the
   field, never return null).

6. When alias storage is added in a future story, the handler can populate `aliases` from a gRPC
   call without any changes to the route registration or middleware chain.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [GetRoomAliases_EmptyArray_ForMember] — Godog
   - Given: authenticated user `@alice:server` who is a member of `!test:server`
   - When: `GET /_matrix/client/v3/rooms/!test:server/aliases`
   - Then: HTTP 200; body `{"aliases":[]}`

2. [GetRoomAliases_Forbidden_NonMember] — Godog
   - Given: authenticated user `@carol:server` who is NOT a member of `!private:server`
   - When: `GET /_matrix/client/v3/rooms/!private:server/aliases`
   - Then: HTTP 403 `{"errcode":"M_FORBIDDEN",...}`

3. [GetRoomAliases_NotFound_UnknownRoom] — Godog
   - Given: authenticated user with valid JWT
   - When: `GET /_matrix/client/v3/rooms/!doesnotexist:server/aliases`
   - Then: HTTP 404 `{"errcode":"M_NOT_FOUND",...}`

4. [GetRoomAliases_Unauthenticated] — Go unit test (httptest)
   - Given: request with no `Authorization` header (jwtMiddleware not bypassed)
   - When: `GET /rooms/!test:server/aliases`
   - Then: HTTP 401 (handled by jwtMiddleware, handler never reached)

## Implementation Notes

**Files to modify:**

- `gateway/internal/matrix/rooms.go` — Add `GetRoomAliasesHandler` struct:
  ```go
  type GetRoomAliasesCoreClient interface {
      GetRoomState(ctx context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error)
  }

  type GetRoomAliasesHandler struct {
      coreClient GetRoomAliasesCoreClient
      serverName string
  }
  ```
  `GetRoomAliases` method:
  1. Extract `roomId` from `r.PathValue("roomId")`.
  2. Call `GetRoomState` with no event_type filter — solely to verify membership and room
     existence (reuse the gRPC call already used by `/members`).
  3. On success: return `{"aliases":[]}`.
  4. On gRPC error: map to Matrix error (same pattern as other room handlers).

  Add a TODO comment above the aliases slice:
  ```go
  // TODO(7-23): When alias storage is implemented (future story), replace this
  // empty slice with a GetRoomAliases gRPC call to Elixir Core.
  aliases := []string{}
  ```

- `gateway/cmd/gateway/main.go` (~line 404+) — Register:
  ```
  GET /_matrix/client/v3/rooms/{roomId}/aliases
  ```
  wrapped in `jwtMiddleware`.

- `gateway/internal/matrix/rooms_test.go` — Unit tests for `GetRoomAliases` using `httptest` and
  a mock `GetRoomAliasesCoreClient`.

- `gateway/features/room_aliases.feature` — Godog feature file (written first, red phase).

**No proto changes needed.** The existing `GetRoomStateRequest/Response` is sufficient for
membership verification. No new gRPC RPCs required for the MVP.

**Design decision:** Using `GetRoomState` for membership verification (rather than a dedicated
`IsMember` RPC) avoids a new proto message. This is consistent with how `GetRoomMembers` works.
If a dedicated membership-check RPC is added in a future story, this handler can be migrated then.

**Error-mapping** (consistent with all other room handlers):
- `codes.PermissionDenied` → 403 `M_FORBIDDEN`
- `codes.NotFound` → 404 `M_NOT_FOUND`
- `codes.Unavailable` → 503 `M_UNAVAILABLE`
- default → 500 `M_UNKNOWN`
