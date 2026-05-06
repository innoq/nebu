---
status: ready-for-dev
epic: 9
story: 22
security_review: not-needed
---

# Story 9.22: GAP-SINCE-IGNORED — Per-Device Sync Token Storage

Status: ready-for-dev

## Story

As a Matrix client connecting from multiple devices simultaneously,
I want the server to correctly honour my device-specific `?since` token on every sync request,
So that each device receives its own independent delta and does not receive stale or duplicate events caused by token overwriting from parallel sessions.

**Size:** L (weeks — design + migration + Elixir + Go)

---

## Background

### Symptom

The `?since` query parameter sent by the client is silently ignored. In
`core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` the handler reads:

```elixir
_since_token = request.since_token   # _ = deliberately unused
case pg_store_module().get_since_token(user_id) do ...
```

The server always loads the token from its own `sync_tokens` table keyed by `user_id`
and returns delta data relative to that server-side checkpoint — not relative to the
client-supplied `since`.

### Design Decision (MVP)

This is by design for the MVP: the token is treated as opaque and authoritative
server-side state so the client cannot forge or replay arbitrary positions. The client
is expected to echo back the `next_batch` it received, and the server validates by
comparing its stored token against what the client sent.

**The current implementation skips even that validation**, which means:

1. The server never detects a mismatch (e.g., client replaying an old token).
2. With parallel sessions from multiple devices, whichever device calls
   `persist_since_token` last wins and overwrites the shared per-user token.
   Device A (since=S1) and Device B (since=S2) race; the loser's next sync
   returns a delta anchored at the winner's token — wrong events, or events
   already seen, or gaps.

### Matrix Spec Requirement

Per the Client-Server API spec the server MUST validate the `since` token it issues:
an unrecognised token MUST result in a full sync (`M_UNKNOWN` or fallback). A token
from a different device session is effectively "unrecognised" from that device's
perspective.

### Fix Scope

The architectural fix is to change the `sync_tokens` primary key from `user_id`
to `(user_id, device_id)`. Every sync checkpoint is then scoped to a specific device
session.

This requires:

1. **New migration** — add `device_id` column to `sync_tokens` table (or replace with
   `device_sync_tokens` table).
2. **Proto change** — `GetSyncDeltaRequest` must carry `device_id` from the gateway.
3. **Gateway change** — `handleIncrementalSync` must extract `device_id` from the JWT
   session and pass it in the gRPC request.
4. **Elixir change** — `get_sync_delta/2` must use `(user_id, device_id)` as the lookup
   key; `persist_since_token` must be extended to accept `device_id`.
5. **Token validation** — after lookup, compare the stored `since_token` against
   `request.since_token`; on mismatch return `fallback_to_initial: true`.
6. **Graceful cleanup** — orphaned device tokens for logged-out sessions should be
   removed when the session is invalidated.

### Severity

SHOULD (not MUST). The bug causes incorrect delta delivery only when a user has two or
more concurrent device sessions. Single-device users are unaffected. Deferred from
9-19 (sync gap fixes).

---

## Acceptance Criteria

**AC1 — Per-device token stored separately:**
After implementing the migration, two concurrent sessions from the same user with
different `device_id` values each maintain an independent row in `sync_tokens`
(or equivalent table). Writes from Device A do not overwrite the token for Device B.

**AC2 — Client `since` token is validated:**
When the client sends a `?since` token that matches the stored token for that
`(user_id, device_id)`, the server returns an incremental delta (current behaviour).
When the token does not match the stored token for that device, the server returns
a full sync (`fallback_to_initial: true`).

**AC3 — Unknown `device_id` triggers full sync:**
If `GET /sync?since=<token>` is called with a `device_id` for which no stored token
exists (e.g., first request after re-login), the server returns a full initial sync,
identical to omitting `?since`. No crash, no 500 error.

**AC4 — `invalidate_session` cleans up device token:**
When `POST /logout` is called (or a session is administratively invalidated), the
`sync_tokens` row for that `(user_id, device_id)` is deleted in the same transaction
as the session invalidation.

**AC5 — Single-device users are unaffected:**
Existing Matrix client integration tests (join, leave, message send/receive, Element
Web smoke flows) all continue to pass after the migration. No regression.

**AC6 — `device_id` passed from gateway JWT to gRPC:**
The `device_id` is extracted from the `sessions` table (looked up by the JWT
`access_token` or `session_id`) in the gateway and forwarded in the
`GetSyncDeltaRequest` proto message.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **`TestSyncTokens_PerDevice_NoOverwrite`** — Go (sync_test.go or new device_sync_test.go)
   - Given: two device sessions for user `@alice`, device_id `D1` and `D2`, each with distinct stored since_tokens `S1` and `S2`
   - When: `GET /sync?since=S1` is called with device `D1` context, then `GET /sync?since=S2` with device `D2`
   - Then: each response uses the correct per-device checkpoint; S1 write does not affect S2 lookup

2. **`TestSyncTokens_TokenMismatch_FallsBackToInitial`** — Go (sync_test.go)
   - Given: device `D1` has stored token `S1`, device `D2` has stored token `S2`
   - When: `GET /sync?since=S1` is issued but the JWT resolves to device `D2`
   - Then: server returns full sync (`fallback_to_initial: true`), not a delta anchored to S1

3. **`TestSyncTokens_UnknownDevice_FallsBackToInitial`** — Go (sync_test.go)
   - Given: no sync_tokens row exists for `(user_id, device_id)`
   - When: `GET /sync?since=<any_token>` is called
   - Then: server returns full initial sync (not a 500 error)

4. **ExUnit — `test "persist_since_token/4 stores per-device row"`** — Elixir (pg_store test)
   - Given: `persist_since_token(user_id, device_id, since_token, last_event_id)` is called for two different `device_id` values
   - When: `get_since_token(user_id, device_id)` is called for each
   - Then: each returns its own `since_token`; the other device's token is unaffected

5. **ExUnit — `test "get_since_token/2 returns not_found for unknown device"`** — Elixir (pg_store test)
   - Given: no row exists for `(user_id, device_id)`
   - When: `get_since_token(user_id, device_id)` is called
   - Then: `{:error, :not_found}` is returned

6. **ExUnit — `test "invalidate_session/2 deletes device sync_token row"`** — Elixir (pg_store test)
   - Given: a `sync_tokens` row exists for `(user_id, device_id)`
   - When: `invalidate_session(user_id, device_id)` is called
   - Then: the row is deleted; `get_since_token(user_id, device_id)` returns `{:error, :not_found}`

7. **Regression — Godog scenario `@sync_incremental` remains green** — Godog
   - Given: the full integration stack with a single-device Matrix client session
   - When: initial sync + incremental sync with `?since` token
   - Then: both return 200 with correct room data; no regressions to existing flows

---

## Technical Implementation Plan

### Files to create

| File | Change |
|---|---|
| `gateway/migrations/000041_sync_tokens_per_device.up.sql` | Add `device_id` column + new composite PK to `sync_tokens` |
| `gateway/migrations/000041_sync_tokens_per_device.down.sql` | Revert: drop `device_id` column, restore `user_id` PK |

### Files to modify

| File | Change |
|---|---|
| `proto/core.proto` | Add `device_id` field to `GetSyncDeltaRequest` |
| `core/apps/session_manager/lib/nebu/session/pg_store/postgres.ex` | Extend `persist_since_token/3→4`, `get_since_token/1→2`, `invalidate_session/1→2` to include `device_id` |
| `core/apps/session_manager/lib/nebu/session/pg_store.ex` | Update `@callback` signatures |
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | `get_sync_delta/2`: read `device_id` from request, pass to `pg_store_module()`, validate token match |
| `gateway/internal/matrix/sync.go` | `handleIncrementalSync`: resolve `device_id` from session, pass in `GetSyncDeltaRequest` |
| `gateway/internal/matrix/sync_test.go` | Add Go unit tests (AC1–AC3) |
| `core/apps/session_manager/test/nebu/session/pg_store/postgres_test.exs` | Add ExUnit tests (AC4–AC6) |

### Step 1 — Migration: add device_id to sync_tokens

```sql
-- gateway/migrations/000041_sync_tokens_per_device.up.sql
-- Replaces per-user sync_tokens PK with per-device composite PK.
-- Existing rows are assigned device_id = '' (unknown/legacy) to preserve
-- data during migration; a follow-up cleanup removes legacy rows.

ALTER TABLE sync_tokens
  ADD COLUMN device_id TEXT NOT NULL DEFAULT '';

-- Drop existing user_id-only PK and replace with composite PK.
ALTER TABLE sync_tokens DROP CONSTRAINT sync_tokens_pkey;
ALTER TABLE sync_tokens ADD PRIMARY KEY (user_id, device_id);
```

```sql
-- gateway/migrations/000041_sync_tokens_per_device.down.sql
-- Revert: back to per-user PK. Legacy rows with device_id != '' are dropped.
DELETE FROM sync_tokens WHERE device_id != '';
ALTER TABLE sync_tokens DROP CONSTRAINT sync_tokens_pkey;
ALTER TABLE sync_tokens ALTER COLUMN device_id DROP NOT NULL;
ALTER TABLE sync_tokens DROP COLUMN device_id;
ALTER TABLE sync_tokens ADD PRIMARY KEY (user_id);
```

### Step 2 — Proto: add device_id to GetSyncDeltaRequest

```protobuf
// proto/core.proto
message GetSyncDeltaRequest {
  string user_id     = 1;
  string since_token = 2;  // opaque token from previous next_batch
  int64  timeout_ms  = 3;  // long-poll wait time; 0 = return immediately; max 30000
  string device_id   = 4;  // device-scoped since-token lookup key (Story 9-22)
}
```

Run `make proto` to regenerate Go + Elixir stubs.

### Step 3 — Elixir PgStore: per-device functions

Extend `Nebu.Session.PgStore.Postgres` (and its `@behaviour`):

```elixir
# New SQL
@upsert_since_token_sql """
INSERT INTO sync_tokens (user_id, device_id, since_token, last_event_id, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, device_id) DO UPDATE
  SET since_token   = EXCLUDED.since_token,
      last_event_id = EXCLUDED.last_event_id,
      updated_at    = EXCLUDED.updated_at
"""

@get_since_token_sql """
SELECT since_token, last_event_id FROM sync_tokens
WHERE user_id = $1 AND device_id = $2
"""

@delete_sync_token_sql """
DELETE FROM sync_tokens WHERE user_id = $1 AND device_id = $2
"""

# New arity-4 functions (keep arity-1/3 for backwards compat during transition)
def persist_since_token(user_id, device_id, since_token, last_event_id) do ...
def get_since_token(user_id, device_id) do ...
def invalidate_session(user_id, device_id) do ...
```

Update `@behaviour Nebu.Session.PgStore` callbacks to match.

### Step 4 — Elixir server.ex: per-device lookup + validation

In `get_sync_delta/2`:

```elixir
def get_sync_delta(request, _stream) do
  user_id    = request.user_id
  device_id  = Map.get(request, :device_id, "")
  client_since_token = request.since_token
  timeout_ms = request.timeout_ms |> max(0) |> min(30_000)

  case pg_store_module().get_since_token(user_id, device_id) do
    {:error, :not_found} ->
      # No stored token for this device — return full initial sync
      initial_req  = %Core.GetInitialSyncRequest{user_id: user_id}
      initial_resp = get_initial_sync(initial_req, %{http_request_headers: %{}})
      %Core.GetSyncDeltaResponse{
        since_token:        initial_resp.since_token,
        rooms:              initial_resp.rooms,
        fallback_to_initial: true
      }

    {:ok, %{since_token: stored_token, last_event_id: last_event_id}} ->
      # Validate: client must echo back the token we issued (spec requirement)
      if client_since_token != stored_token do
        # Mismatch — return full sync rather than delta
        initial_req  = %Core.GetInitialSyncRequest{user_id: user_id}
        initial_resp = get_initial_sync(initial_req, %{http_request_headers: %{}})
        %Core.GetSyncDeltaResponse{
          since_token:        initial_resp.since_token,
          rooms:              initial_resp.rooms,
          fallback_to_initial: true
        }
      else
        task_timeout = timeout_ms + 10_000
        task = Task.async(fn ->
          do_incremental_sync(user_id, last_event_id, timeout_ms)
        end)
        Task.await(task, task_timeout)
      end
  end
end
```

Persist calls must also use `device_id`:

```elixir
ok = pg_store_module().persist_since_token(user_id, device_id, new_since_token, newest_event_id)
```

### Step 5 — Gateway: resolve device_id from session

In `handleIncrementalSync`, resolve `device_id` from the session:

```go
// The JWT sub is user_id. device_id is stored in the sessions table.
// Look up from sessions table using the session_id embedded in the JWT (or
// derive from the access_token claim, whichever is available in the context).
deviceID, _ := r.Context().Value(middleware.ContextKeyDeviceID).(string)

resp, err := h.coreClient.GetSyncDelta(grpcCtx, &pb.GetSyncDeltaRequest{
    UserId:    userID,
    SinceToken: sinceToken,
    TimeoutMs: timeoutMs,
    DeviceId:  deviceID,
})
```

If `ContextKeyDeviceID` is not yet set by the JWT middleware, extend the middleware to
look up `device_id` from `sessions WHERE session_id = <jwt_claim>` and store it in
context alongside `ContextKeyUserID`.

---

## Dev Notes

### ADR consideration

This change affects the Matrix sync protocol boundary and the gRPC contract between
gateway and core. No new ADR is required (it refines the existing ADR-005 gRPC
contract), but the change should be noted in the arc42 docs maintenance pass.

### Backwards compatibility

- The down-migration preserves data by keeping only `device_id = ''` rows.
- During the transition period, the gateway must pass `device_id = ""` for any session
  where it cannot resolve the device, so old behaviour is preserved.
- The `persist_since_token/3` arity-3 signature can be kept as an alias calling
  `persist_since_token(user_id, "", since_token, last_event_id)` during transition.

### Token validation rationale

The Matrix spec (§5.4) requires the server to treat an unknown `since` token as a
trigger for a full sync. The current server ignores the token entirely. This story
adds: (a) per-device storage so each device's checkpoint is independent, and (b)
token echoing validation so a stale or mismatched token triggers a full sync rather
than returning a wrong delta.

### device_id source in JWT middleware

The `sessions` table already has `device_id TEXT NOT NULL` (migration 000005). The
JWT middleware sets `ContextKeyUserID` from the JWT `sub` claim. It should additionally
set `ContextKeyDeviceID` by querying `sessions WHERE session_id = <jwt_session_claim>`
or by including `device_id` as a JWT claim at login time. The simpler approach is to
add `device_id` as a custom claim in the login response JWT — check Story 2-18
(`PostLogin`) for the JWT construction site.

### Test data setup

For Go unit tests, use `sqlmock` (already in use in `sync_test.go`) to mock:
- `sync_tokens` queries with `(user_id, device_id)` composite key
- `sessions` queries for `device_id` resolution

For Elixir ExUnit tests, use the existing test DB sandbox pattern from
`core/apps/session_manager/test/`.

### Crash/restart resilience

This story changes a PostgreSQL-backed store — no GenServer restart concern.
Tokens survive Core restarts because they are persisted in PostgreSQL before the
gRPC response is returned to the gateway.

### Persistence Strategy

**Option B: PostgreSQL** — per-device tokens are written to `sync_tokens` before
each response. On Core restart, the tokens are read from PostgreSQL on the first
incremental sync request per device. No ETS migration needed.

---

## Status

Status: ready-for-dev
