---
id: 7-21
type: feature
security_review: not-needed
created: 2026-04-30
---

# Story 7.21: Profile Einzelfelder — GET /profile/{userId}/displayname + /avatar_url

Status: ready-for-dev

## Story

As an end-user,
I want to retrieve just the displayname or just the avatar_url of any user's profile in a single
lightweight request,
so that my Matrix client can populate user chips and avatars without fetching the full profile object.

## Context / Background

`GET /_matrix/client/v3/profile/{userId}` (already implemented in `gateway/internal/matrix/profile.go`)
returns both `displayname` and `avatar_url`. Matrix spec also defines two sub-resource endpoints
that return only one field each. Many clients (particularly mobile) use these endpoints to fetch
individual fields on demand.

Both endpoints are **unauthenticated** per the Matrix spec — anyone can look up a user's public
profile. They are already partially covered by the existing `ProfileDB` interface and profile lookup
logic in `profile.go`. This story adds thin wrapper methods to the existing `ProfileHandler`.

No gRPC calls are needed — profile reads go directly to PostgreSQL via `ProfileDB.GetProfile`.
No proto changes.

**Response shapes:**
```json
GET /profile/{userId}/displayname
→ { "displayname": "Alice" }

GET /profile/{userId}/avatar_url
→ { "avatar_url": "mxc://server/mediaId" }
  or { "avatar_url": null }  (when no avatar set)
```

## Acceptance Criteria

1. `GET /_matrix/client/v3/profile/{userId}/displayname` returns HTTP 200 with body
   `{"displayname":"<value>"}` when the user exists.

2. `GET /_matrix/client/v3/profile/{userId}/avatar_url` returns HTTP 200 with body
   `{"avatar_url":"<value>"}` when the user exists and has an avatar; `{"avatar_url":null}` when
   the user exists but has no avatar set.

3. Both endpoints return `404 M_NOT_FOUND` when the `userId` does not exist in the database.

4. Both endpoints are registered without `jwtMiddleware` — unauthenticated access is allowed.

5. Both endpoints are wrapped with `looseRL` (consistent with other unauthenticated Matrix
   endpoints) to prevent enumeration abuse.

6. An empty `displayname` in the DB is returned as `""` (empty string), not null. An unset
   `avatar_url` is returned as `null`.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [GetDisplayname_ReturnsValue] — Godog
   - Given: user `@alice:server` exists with displayname "Alice Wonderland"
   - When: unauthenticated `GET /_matrix/client/v3/profile/@alice:server/displayname`
   - Then: HTTP 200; body `{"displayname":"Alice Wonderland"}`

2. [GetAvatarURL_ReturnsValue] — Godog
   - Given: user `@alice:server` exists with avatar_url "mxc://server/abc123"
   - When: unauthenticated `GET /_matrix/client/v3/profile/@alice:server/avatar_url`
   - Then: HTTP 200; body `{"avatar_url":"mxc://server/abc123"}`

3. [GetAvatarURL_ReturnsNull_WhenNotSet] — Go unit test (httptest)
   - Given: ProfileDB returns `ProfileData{DisplayName:"Alice", AvatarURL:""}` (no avatar)
   - When: `GET /profile/@alice:server/avatar_url`
   - Then: HTTP 200; body `{"avatar_url":null}`

4. [GetDisplayname_NotFound] — Godog
   - Given: no user `@ghost:server` in the database
   - When: unauthenticated `GET /_matrix/client/v3/profile/@ghost:server/displayname`
   - Then: HTTP 404 `{"errcode":"M_NOT_FOUND","error":"User not found"}`

5. [GetAvatarURL_NotFound] — Go unit test (httptest)
   - Given: ProfileDB returns `ErrProfileNotFound`
   - When: `GET /profile/@ghost:server/avatar_url`
   - Then: HTTP 404 `{"errcode":"M_NOT_FOUND",...}`

## Implementation Notes

**Files to modify:**

- `gateway/internal/matrix/profile.go` — Add two methods to the existing `ProfileHandler`:
  ```go
  func (h *ProfileHandler) GetDisplayname(w http.ResponseWriter, r *http.Request)
  func (h *ProfileHandler) GetAvatarURL(w http.ResponseWriter, r *http.Request)
  ```
  Both methods:
  1. Extract `userId` from `r.PathValue("userId")`.
  2. Call `h.db.GetProfile(ctx, userId)`.
  3. On `ErrProfileNotFound` → 404 `M_NOT_FOUND`.
  4. On success → encode the single-field JSON response.

  Reuse the existing `writeMatrixError` helper (already used in `profile.go`).

- `gateway/cmd/gateway/main.go` (~line 404+) — Register two routes **without** `jwtMiddleware`:
  ```
  GET /_matrix/client/v3/profile/{userId}/displayname  → looseRL(profileHandler.GetDisplayname)
  GET /_matrix/client/v3/profile/{userId}/avatar_url   → looseRL(profileHandler.GetAvatarURL)
  ```
  Place them **before** the existing `GET /profile/{userId}` route to avoid path conflicts with
  Go's `net/http` ServeMux (more specific paths must be registered first).

- `gateway/internal/matrix/profile_test.go` — Add unit tests for the two new methods using
  `httptest` and a mock `ProfileDB`.

- `gateway/features/profile_subfields.feature` — Godog feature file (written first, red phase).

**No proto changes.** No gRPC calls. No new DB methods — `ProfileDB.GetProfile` returns both
fields; the handler just projects the relevant field into the response.
