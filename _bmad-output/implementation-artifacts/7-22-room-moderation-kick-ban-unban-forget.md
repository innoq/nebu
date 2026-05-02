---
id: 7-22
type: feature
security_review: optional
created: 2026-04-30
---

# Story 7.22: Room Moderation — kick / ban / unban / forget

Status: review

## Story

As a room moderator or admin,
I want to kick, ban, unban, and forget rooms via the Matrix API,
so that I can manage room membership and keep my room list clean.

## Context / Background

Matrix moderation model:

- **kick** — Sends a `m.room.member` state event with `membership: leave` for the target user.
  Requires caller power level ≥ `kick` threshold in `m.room.power_levels`.
- **ban** — Sends a `m.room.member` state event with `membership: ban`. Requires power level ≥ `ban`.
- **unban** — Sets `membership: leave` for a banned user (un-bans, does not re-join). Requires
  power level ≥ `ban`.
- **forget** — Marks the room as excluded from the caller's future `/sync` responses. The caller
  must have membership `leave` or `ban` first (cannot forget a room they are still joined to).
  This is a per-user flag in the Session Manager, not a room-level change.

The Room GenServer in `core/apps/room_manager` already handles `SendEvent` for state events and
`LeaveRoom`. The power-level check is enforced inside the GenServer (as it is for other
state-changing operations). Four new gRPC RPCs are needed:

```protobuf
rpc KickUser(KickUserRequest)     returns (KickUserResponse);
rpc BanUser(BanUserRequest)       returns (BanUserResponse);
rpc UnbanUser(UnbanUserRequest)   returns (UnbanUserResponse);
rpc ForgetRoom(ForgetRoomRequest) returns (ForgetRoomResponse);
```

All four endpoint handlers go in `gateway/internal/matrix/rooms.go`. Route registration in
`gateway/cmd/gateway/main.go` uses `jwtMiddleware` + `bodyLimit1MiB` (POST endpoints).

## Acceptance Criteria

1. `POST /_matrix/client/v3/rooms/{roomId}/kick` with `{"user_id":"@target:server","reason":"..."}`:
   - Creates a `m.room.member` state event with `membership: leave` for `@target:server`.
   - Requires caller power level ≥ `kick` value from `m.room.power_levels`; returns
     `403 M_FORBIDDEN` if insufficient.
   - Returns `200 {}` on success.

2. `POST /_matrix/client/v3/rooms/{roomId}/ban` with `{"user_id":"@target:server","reason":"..."}`:
   - Creates a `m.room.member` state event with `membership: ban` for `@target:server`.
   - Requires caller power level ≥ `ban` value; returns `403 M_FORBIDDEN` if insufficient.
   - Returns `200 {}` on success.

3. `POST /_matrix/client/v3/rooms/{roomId}/unban` with `{"user_id":"@target:server"}`:
   - Sets `membership: leave` for a currently banned user.
   - Requires caller power level ≥ `ban` value; returns `403 M_FORBIDDEN` if insufficient.
   - Returns `200 {}` on success.

4. `POST /_matrix/client/v3/rooms/{roomId}/forget` with `{}`:
   - Marks the room as excluded from future `/sync` for the calling user.
   - Returns `403 M_FORBIDDEN` if the caller's current membership is `join` (must leave first).
   - Returns `200 {}` on success.

5. All four endpoints return `400 M_BAD_JSON` when the request body is malformed or `user_id` is
   missing where required (kick/ban/unban).

6. All four endpoints return `404 M_NOT_FOUND` when the room does not exist.

7. All four endpoints return `403 M_FORBIDDEN` when the requesting user is not a room member
   (kick/ban/unban require membership; forget requires prior leave/ban).

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [Kick_Success_ModeratorPowerLevel] — Godog
   - Given: `@moderator:server` has power level 50 (≥ kick threshold 50) in `!test:server`;
     `@target:server` is a joined member
   - When: `POST /_matrix/client/v3/rooms/!test:server/kick` body `{"user_id":"@target:server"}`
   - Then: HTTP 200 `{}`; subsequent `/members` does not include `@target:server` with `join`

2. [Kick_Forbidden_InsufficientPowerLevel] — Godog
   - Given: `@user:server` has power level 0 (below kick threshold 50) in `!test:server`
   - When: `POST /_matrix/client/v3/rooms/!test:server/kick` body `{"user_id":"@other:server"}`
   - Then: HTTP 403 `{"errcode":"M_FORBIDDEN",...}`

3. [Ban_Success] — Godog
   - Given: `@admin:server` has power level 100 (≥ ban threshold 50) in `!test:server`
   - When: `POST /_matrix/client/v3/rooms/!test:server/ban` body `{"user_id":"@target:server"}`
   - Then: HTTP 200 `{}`

4. [Unban_Success] — Godog
   - Given: `@target:server` has `membership: ban`; `@admin:server` power level ≥ ban threshold
   - When: `POST /_matrix/client/v3/rooms/!test:server/unban` body `{"user_id":"@target:server"}`
   - Then: HTTP 200 `{}`; `@target:server` membership is now `leave` (not banned)

5. [Forget_Success_AfterLeave] — Godog
   - Given: `@alice:server` has previously left `!old:server` (membership: leave)
   - When: `POST /_matrix/client/v3/rooms/!old:server/forget` body `{}`
   - Then: HTTP 200 `{}`; `!old:server` no longer appears in `@alice:server`'s `/sync` response

6. [Forget_Forbidden_StillJoined] — Godog
   - Given: `@alice:server` is currently joined to `!active:server`
   - When: `POST /_matrix/client/v3/rooms/!active:server/forget` body `{}`
   - Then: HTTP 403 `{"errcode":"M_FORBIDDEN","error":"User must leave the room before forgetting"}`

7. [Kick_BadJSON_MissingUserId] — Go unit test (httptest)
   - Given: gRPC mock not called
   - When: `POST /rooms/!test:server/kick` body `{}`
   - Then: HTTP 400 `{"errcode":"M_BAD_JSON",...}`

## Implementation Notes

**Proto additions** (`proto/core.proto`):

```protobuf
// KickUser — room moderator action; power check enforced by GenServer
message KickUserRequest {
  string room_id   = 1;
  string caller_id = 2;  // user performing the kick
  string target_id = 3;  // user to kick
  string reason    = 4;  // optional
}
message KickUserResponse {}

message BanUserRequest {
  string room_id   = 1;
  string caller_id = 2;
  string target_id = 3;
  string reason    = 4;
}
message BanUserResponse {}

message UnbanUserRequest {
  string room_id   = 1;
  string caller_id = 2;
  string target_id = 3;
}
message UnbanUserResponse {}

message ForgetRoomRequest {
  string room_id = 1;
  string user_id = 2;
}
message ForgetRoomResponse {}
```

Run `make proto` after adding these.

**Files to modify:**

- `proto/core.proto` — Add 4 RPC entries to `CoreService` + 8 message definitions above.
- `gateway/internal/matrix/rooms.go` — Add `ModerationCoreClient` interface + 4 handler methods:
  `PostKickUser`, `PostBanUser`, `PostUnbanUser`, `PostForgetRoom`. Share request-body struct:
  ```go
  type membershipActionRequest struct {
      UserID string `json:"user_id"`
      Reason string `json:"reason,omitempty"`
  }
  ```
- `gateway/cmd/gateway/main.go` (~line 404+) — Register 4 routes with `jwtMiddleware` +
  `bodyLimit1MiB` + `mediumRL`:
  ```
  POST /_matrix/client/v3/rooms/{roomId}/kick
  POST /_matrix/client/v3/rooms/{roomId}/ban
  POST /_matrix/client/v3/rooms/{roomId}/unban
  POST /_matrix/client/v3/rooms/{roomId}/forget
  ```
- `core/apps/room_manager/` — Implement gRPC handlers for `KickUser`, `BanUser`, `UnbanUser`.
  Each constructs a state event and calls the GenServer — reuse the existing `send_event` GenServer
  call with `type: "m.room.member"`. Power-level check happens inside the GenServer (same path as
  `SetPowerLevels`).
- `core/apps/session_manager/` — Implement `ForgetRoom` gRPC handler. Adds a `forgotten_rooms`
  entry to the session record; `/sync` (GetSyncDelta) skips forgotten rooms when building the
  response. Add a `forgotten_rooms` column or JSONB field if not already present.
- `gateway/internal/matrix/rooms_test.go` — Unit tests for all 4 handlers.
- `gateway/features/moderation.feature` — Godog feature file (written first, red phase).

**Error-mapping:**
- `codes.PermissionDenied` → 403 `M_FORBIDDEN`
- `codes.NotFound` → 404 `M_NOT_FOUND`
- `codes.InvalidArgument` → 400 `M_BAD_JSON`
- `codes.FailedPrecondition` → 403 `M_FORBIDDEN` (e.g. forget while joined)
- `codes.Unavailable` → 503 `M_UNAVAILABLE`
- default → 500 `M_UNKNOWN`

**Security note:** Power-level enforcement must live in the Elixir GenServer, not in the Go handler,
to prevent race conditions. The handler only validates the request shape and forwards to Core.
