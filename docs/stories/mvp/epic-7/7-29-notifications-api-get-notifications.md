---
id: 7-29
type: feature
security_review: not-needed
created: 2026-04-30
---

# Story 7.29: Notifications API — GET /notifications

Status: ready-for-dev

## Story

As an end-user,
I want to retrieve my notification history via `GET /_matrix/client/v3/notifications`,
so that my Matrix client can display a badge count, a notification inbox, and highlight events
that mention me — even after reconnecting.

## Context / Background

Matrix clients (FluffyChat, Element) poll `/notifications` on startup to hydrate the notification
inbox and determine unread badge counts. Without this endpoint the client silently fails to show
any historic notifications.

The endpoint is pagination-based: the `from` query parameter is an opaque cursor wrapping a
notification row ID. Results are ordered newest-first. The `only=highlight` filter restricts the
list to events where the push rule actions include `"highlight"`.

**New PostgreSQL table** (migration 000031):

```sql
CREATE TABLE notifications (
  id          BIGSERIAL PRIMARY KEY,
  user_id     TEXT        NOT NULL,
  room_id     TEXT        NOT NULL,
  event_id    TEXT        NOT NULL,
  event_json  JSONB       NOT NULL,
  actions     JSONB       NOT NULL DEFAULT '["notify"]',
  read        BOOLEAN     NOT NULL DEFAULT FALSE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX notifications_user_created
  ON notifications (user_id, created_at DESC);
```

RLS policy: `nebu_app` may only SELECT/INSERT/UPDATE rows where `user_id = current_setting('app.user_id')`.

**Event Dispatcher writes notifications:** For MVP every delivered event generates one notification
row per recipient with `actions = ["notify"]`. Highlight detection (mention matching) is a Phase 2
concern — the schema already accommodates it via the `actions` column.

**New gRPC call** in `proto/core.proto`:

```protobuf
message GetNotificationsRequest {
  string user_id        = 1;
  int64  from_cursor    = 2;  // 0 = start from newest
  int32  limit          = 3;  // 0 = use default (50)
  bool   only_highlight = 4;
}
message GetNotificationsResponse {
  repeated NotificationItem notifications = 1;
  string next_cursor                      = 2;  // "" = no more pages
}
message NotificationItem {
  string actions_json = 1;
  string event_json   = 2;
  string profile_tag  = 3;
  bool   read         = 4;
  string room_id      = 5;
  int64  ts           = 6;
}
```

**New handler file:** `gateway/internal/matrix/notifications.go`

**Route registration** in `gateway/cmd/gateway/main.go` under `jwtMiddleware`.

## Acceptance Criteria

1. `GET /_matrix/client/v3/notifications` returns HTTP 200 with `{"next_token":"...","notifications":[...]}` for an authenticated user.

2. The `from` query parameter is treated as an opaque cursor (wraps the numeric notification id); omitting it returns the newest page.

3. `only=highlight` filters results to notifications whose `actions` array contains `"highlight"`; without the parameter all notifications are returned.

4. `limit` defaults to 50 and is clamped to a maximum of 200; values above 200 return a 400 `M_INVALID_PARAM`.

5. Migration 000031 creates the `notifications` table with the schema above plus an index on `(user_id, created_at DESC)` and RLS so `nebu_app` can only access rows matching its own `user_id`.

6. The Event Dispatcher (Elixir) inserts one notification row per recipient for every event it delivers (MVP: `actions = ["notify"]`).

7. JWT required — requests without a valid token are rejected by `jwtMiddleware` before the handler is reached.

8. `next_token` is absent (or empty string) when no further pages exist.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [GetNotifications_ReturnsPagedList] — Godog
   - Given: authenticated user `@alice:server` with 3 notifications in the database
   - When: `GET /_matrix/client/v3/notifications?limit=2`
   - Then: HTTP 200; `notifications` array has exactly 2 items; `next_token` is non-empty; each item has keys `actions`, `event`, `read`, `room_id`, `ts`

2. [GetNotifications_FromCursor_SecondPage] — Godog
   - Given: same user with 3 notifications; first page returned `next_token = "TOKEN"`
   - When: `GET /_matrix/client/v3/notifications?from=TOKEN&limit=2`
   - Then: HTTP 200; `notifications` contains the remaining 1 item; `next_token` is absent or empty

3. [GetNotifications_OnlyHighlight_FiltersCorrectly] — Godog
   - Given: user has 2 notifications — one with `actions=["notify"]`, one with `actions=["notify","highlight"]`
   - When: `GET /_matrix/client/v3/notifications?only=highlight`
   - Then: HTTP 200; `notifications` contains exactly the 1 highlight notification

4. [GetNotifications_LimitExceedsMax_Returns400] — Godog
   - Given: authenticated user
   - When: `GET /_matrix/client/v3/notifications?limit=999`
   - Then: HTTP 400 `{"errcode":"M_INVALID_PARAM","error":"limit must not exceed 200"}`

5. [GetNotifications_EmptyResult] — Godog
   - Given: authenticated user with no notifications
   - When: `GET /_matrix/client/v3/notifications`
   - Then: HTTP 200; `notifications` is an empty array; `next_token` absent or empty

6. [GetNotifications_Unauthenticated_Rejected] — Godog
   - Given: no Authorization header
   - When: `GET /_matrix/client/v3/notifications`
   - Then: HTTP 401 `{"errcode":"M_MISSING_TOKEN",...}`

## Implementation Notes

**Files to create / modify:**

- `gateway/migrations/000031_notifications.up.sql` + `000031_notifications.down.sql` — table, index, RLS.
- `proto/core.proto` — add `GetNotificationsRequest`, `GetNotificationsResponse`, `NotificationItem` messages; add `GetNotifications` RPC to the `NebuCore` service.
- `gateway/internal/matrix/notifications.go` — `GetNotificationsHandler` struct with a `CoreClient` interface (only `GetNotifications`). Parse query params, call gRPC, map to Matrix JSON response.
- `gateway/internal/matrix/notifications_test.go` — unit tests with `httptest` covering all ACs.
- `gateway/features/notifications.feature` — Godog feature file (written first, red phase).
- `gateway/cmd/gateway/main.go` — register `GET /_matrix/client/v3/notifications` under `jwtMiddleware`.
- `core/apps/event_dispatcher/` — after dispatching an event, insert one notification row per recipient.
- `core/apps/session_manager/` or a new `core/apps/notification_store/` — implement `GetNotifications` gRPC handler querying PostgreSQL with cursor pagination.

**Cursor encoding:** Encode the integer notification `id` as a base64url string for the opaque token. The gateway decodes it; non-decodable tokens yield 400 `M_INVALID_PARAM`.

**Error-mapping pattern** (consistent with existing handlers):
- `codes.InvalidArgument` → 400 `M_INVALID_PARAM`
- `codes.Unauthenticated` → 401 `M_MISSING_TOKEN`
- `codes.NotFound` → 404 `M_NOT_FOUND`
- `codes.Unavailable` → 503 `M_UNAVAILABLE`
- default → 500 `M_UNKNOWN`

**Phase 2 (out of scope for this story):**
- Highlight/mention detection using content push rules.
- `POST /_matrix/client/v3/rooms/{roomId}/receipt/m.read/{eventId}` marking notifications read (separate story).
- HTTP pusher delivery.
