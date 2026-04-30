---
id: 7-25
type: feature
security_review: not-needed
created: 2026-04-30
---

# Story 7.25: Tags API — GET/PUT/DELETE /user/{userId}/rooms/{roomId}/tags

Status: ready-for-dev

## Story

As an end-user,
I want to tag rooms (e.g. as favourite or low priority) and have those tags persist across sessions,
so that my Matrix client can organise rooms into custom categories.

## Context / Background

Matrix tags are stored as room account data with type `m.tag`. The content is `{"tags":{"m.favourite":{"order":0.5},"u.work":{}}}`. PUT and DELETE update this single `m.tag` entry atomically and emit an account_data event in the next `/sync` response.

This story depends on the `room_account_data` table and gRPC calls introduced in Story 7-24. If 7-24 is not yet complete, the Tags API can stub the underlying storage until 7-24 lands.

Standard Matrix tag names: `m.favourite`, `m.lowpriority`. Custom tags must use the `u.*` prefix per the Matrix spec.

## Acceptance Criteria

1. `GET /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags` returns `{"tags":{...}}` where the tags object contains all tags set for this user/room pair. Returns `{"tags":{}}` if no tags have been set (never 404).

2. `PUT /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}` accepts an optional body `{"order":0.5}` and sets or replaces that tag in the `m.tag` account data entry. Returns `{}` with HTTP 200.

3. `DELETE /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}` removes the tag from the `m.tag` entry. Returns `{}` with HTTP 200 even if the tag did not exist (idempotent).

4. `{tag}` is validated: must not be empty, must not exceed 100 characters. Invalid tag → HTTP 400 with M_INVALID_PARAM.

5. PUT and DELETE each trigger an `m.tag` account_data event in the next `/sync` response under `rooms.join.{roomId}.account_data.events`.

6. If the `userId` in the path does not match the authenticated user's JWT subject, all three endpoints return M_FORBIDDEN (HTTP 403).

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [GET returns empty tags for new room] — Godog (`gateway/features/tags.feature`)
   - Given: authenticated user `@alice:nebu.test`, no tags set for room `!room1:nebu.test`
   - When: GET `/_matrix/client/v3/user/@alice:nebu.test/rooms/!room1:nebu.test/tags`
   - Then: HTTP 200, body = `{"tags":{}}`

2. [PUT sets a tag, GET reflects it] — Godog
   - Given: authenticated user `@alice:nebu.test`, room `!room1:nebu.test`
   - When: PUT `/_matrix/client/v3/user/@alice:nebu.test/rooms/!room1:nebu.test/tags/m.favourite` with body `{"order":0.5}`
   - Then: HTTP 200 `{}`; GET tags returns `{"tags":{"m.favourite":{"order":0.5}}}`

3. [DELETE removes a tag idempotently] — Godog
   - Given: `m.favourite` tag set for `@alice:nebu.test` / `!room1:nebu.test`
   - When: DELETE `/_matrix/client/v3/user/@alice:nebu.test/rooms/!room1:nebu.test/tags/m.favourite`; then DELETE again
   - Then: both requests return HTTP 200 `{}`; GET tags returns `{"tags":{}}`

4. [PUT/DELETE triggers m.tag sync event] — Godog
   - Given: `@alice:nebu.test` adds `u.work` tag to `!room1:nebu.test`
   - When: GET `/sync` with `since` token after the PUT
   - Then: `rooms.join.!room1:nebu.test.account_data.events` contains an entry with `type` = `m.tag`

5. [userId mismatch returns M_FORBIDDEN] — Godog
   - Given: authenticated `@alice:nebu.test`
   - When: PUT `/_matrix/client/v3/user/@bob:nebu.test/rooms/!room1:nebu.test/tags/m.favourite`
   - Then: HTTP 403, `errcode` = `M_FORBIDDEN`

6. [Invalid tag name returns 400] — Go httptest (`gateway/internal/matrix/tags_test.go`)
   - Given: authenticated user, valid room
   - When: PUT with empty tag `""` or tag exceeding 100 characters
   - Then: HTTP 400, `errcode` = `M_INVALID_PARAM`

## Implementation Notes

**New handler file:** `gateway/internal/matrix/tags.go`

Implementation pattern:
1. `GetTagsHandler` — calls `GetRoomAccountData(userId, roomId, "m.tag")`; if not found returns `{"tags":{}}`.
2. `PutTagHandler` — reads existing `m.tag` content (or `{"tags":{}}` on not-found), merges the new tag entry, calls `SetRoomAccountData`.
3. `DeleteTagHandler` — reads existing content, removes the tag key, calls `SetRoomAccountData`. Noop if key absent.

All three read/write via the gRPC calls introduced in Story 7-24 (`GetRoomAccountData` / `SetRoomAccountData`). Tag merge/delete is a pure Go map operation on the decoded JSON.

**Route registration** in `gateway/cmd/gateway/main.go`:
```
GET    /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags         → jwtMiddleware(GetTagsHandler)
PUT    /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}   → jwtMiddleware(bodyLimit1MiB(PutTagHandler))
DELETE /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}   → jwtMiddleware(DeleteTagHandler)
```

**Tag validation** — extracted into a shared `validateTag(tag string) error` helper in `tags.go`. Max length 100. Empty string disallowed.

**Sync integration** — no additional sync work needed beyond Story 7-24: the `m.tag` account data entry is fetched alongside all other account data types for the room.

## Tasks

- [ ] Write failing Godog scenarios in `gateway/features/tags.feature`
- [ ] Write failing Go httptest in `gateway/internal/matrix/tags_test.go`
- [ ] Implement `gateway/internal/matrix/tags.go` (GetTagsHandler, PutTagHandler, DeleteTagHandler, validateTag)
- [ ] Register routes in `main.go`
- [ ] Run `make test-unit-go` — all pass
- [ ] Run `make test-integration` — Godog scenarios green
