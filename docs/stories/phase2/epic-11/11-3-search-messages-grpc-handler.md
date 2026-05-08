---
status: review
epic: 11
story: 3
security_review: required
matrix: true
ui: false
---

# Story 11.3: Elixir Core — SearchMessages gRPC Handler

Status: review

## Story

As a developer,
I want the Elixir Core to handle SearchMessages gRPC calls,
so that the Go gateway can delegate search queries to Core.

**Size:** M

---

## Acceptance Criteria

**AC1 — Proto definition:**
Given `proto/core.proto` is updated,
When it is inspected,
Then `SearchMessages` RPC is defined with:
- Request fields: `user_id` (string), `search_term` (string), `room_filter` (repeated string), `sender_filter` (repeated string), `limit` (int32), `next_batch` (string)
- Response fields: `results` (repeated SearchResult), `next_batch` (string), `total_count` (int32)
- Where SearchResult has: `rank` (float), `event` (bytes — raw JSON), `events_before` (repeated bytes), `events_after` (repeated bytes), `profile_info` (map<string, ProfileInfo>)
- And `make proto` regenerates Go stubs + Elixir stubs without compile errors

**AC2 — user_id from gRPC metadata (SECURITY CRITICAL):**
Given the Core gRPC handler receives a SearchMessages request,
When the query executes,
Then it calls `Nebu.Search.DB.search_messages/4` with `user_id` derived from gRPC metadata (NOT from the request message),
And results are returned in the SearchMessagesResponse format

**AC3 — Special character handling:**
Given a search term with special characters (`'`, `&`, `)`, `:`),
When the query runs,
Then no SQL error occurs and results are returned safely (websearch_to_tsquery handles this)

**AC4 — Unauthenticated request rejected:**
Given an unauthenticated gRPC request (no user_id in metadata),
When the handler processes it,
Then it returns `GRPC.RPCError` with status `UNAUTHENTICATED`

**AC5 — room_filter intersected with membership:**
Given `room_filter` is non-empty in the request,
When the handler processes it,
Then results are further filtered to only the listed rooms (intersected with membership, not replaced)

---

## Security Trust Boundary — CRITICAL

> **From Kassandra Review (Story 11.2, Finding MEDIUM-2):**
> The `user_id` MUST be the authenticated user's ID, taken from gRPC metadata (`x-user-id` header
> set by the Go JWTMiddleware) — NEVER from the `SearchMessagesRequest.user_id` field.
> The `SearchMessagesRequest` proto carries a `user_id` field for documentation/auditing purposes only.
> The handler MUST ignore it and always use `Nebu.Grpc.Metadata.trusted_identity(stream)`.
>
> **If the handler reads `user_id` from the request body**, a malicious authenticated user can
> search any other user's rooms simply by setting that field — bypassing all membership enforcement.
> This is the entire point of the SQL scoping in Story 11.2. Do NOT make this mistake.

Pattern (already established in 13+ handlers in server.ex):
```elixir
{user_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

if is_nil(user_id) or user_id == "" do
  raise GRPC.RPCError,
    status: GRPC.Status.unauthenticated(),
    message: "missing x-user-id metadata"
end
```

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

These are **ExUnit unit tests** for the `search_messages/2` gRPC handler.
They live in a new module in:
`core/apps/event_dispatcher/test/nebu/event_dispatcher/search_messages_grpc_test.exs`

These tests call `Nebu.EventDispatcher.Server.search_messages/2` directly (unary handler,
synchronous). They inject a fake `Nebu.Search.DB` module via
`Application.put_env(:event_dispatcher, :search_db_module, FakeSearchDB)`.

**Test module strategy:**
- `use ExUnit.Case, async: false` — Application.put_env is process-global
- No Horde, no Room GenServer, no PostgreSQL
- Fake gRPC stream: `%{http_request_headers: %{"x-user-id" => user_id}}`
- FakeSearchDB injects deterministic results into the handler test
- Tag: no `:integration` tag — pure unit tests using fake DB injection

**1. `test "AC2 — user_id comes from metadata, not request field"`**
- Given: stream has `x-user-id: "@alice:test.local"` in metadata
- Given: request has `user_id: "@mallory:test.local"` (attacker-controlled field)
- Given: FakeSearchDB records which user_id it was called with
- When: `Server.search_messages(request, stream)` is called
- Then: FakeSearchDB was called with `"@alice:test.local"` (from metadata), NOT `"@mallory:test.local"`
- This test is the primary security regression test for the trust boundary

**2. `test "AC4 — unauthenticated request returns GRPC.RPCError UNAUTHENTICATED"`**
- Given: stream has NO `x-user-id` in metadata (empty map `%{}`)
- When: `Server.search_messages(request, stream)` is called
- Then: raises `%GRPC.RPCError{status: GRPC.Status.unauthenticated()}`

**3. `test "AC2/happy-path — returns SearchMessagesResponse with results"`**
- Given: stream has `x-user-id: "@alice:test.local"` in metadata
- Given: FakeSearchDB returns `{:ok, [%{"event_id" => "ev1", "room_id" => "!r:s", "sender" => "@alice:test.local", "content" => %{"msgtype" => "m.text", "body" => "hello world"}, "origin_server_ts" => 1000, "rank" => 0.9}]}`
- When: `Server.search_messages(%Core.SearchMessagesRequest{search_term: "hello", limit: 10}, stream)` is called
- Then: returns `%Core.SearchMessagesResponse{results: [%Core.SearchResult{rank: 0.9, ...}], next_batch: ...}`

**4. `test "AC5 — room_filter in request is intersected with membership"`**
- Given: stream has `x-user-id: "@bob:test.local"` in metadata
- Given: request has `room_filter: ["!roomA:s", "!roomB:s"]`
- Given: FakeSearchDB records what room_ids argument it received
- When: `Server.search_messages(request, stream)` is called
- Then: FakeSearchDB was called with the room_filter list passed through correctly
- (The intersection with actual membership happens at SQL layer in `search_messages/4` — see AC5 implementation note)

**5. `test "AC3 — special chars in search term do not raise"`**
- Given: request has `search_term: "hello' & ): world:"`
- Given: FakeSearchDB accepts any term and returns empty results
- When: `Server.search_messages(request, stream)` is called
- Then: returns `%Core.SearchMessagesResponse{}` without raising (no SQL error from websearch_to_tsquery)

**6. `test "DB error is sanitized — raw error not propagated to client"`**
- Given: FakeSearchDB returns `{:error, %Postgrex.Error{message: "schema detail leak"}}`
- When: `Server.search_messages(request, stream)` is called
- Then: raises `GRPC.RPCError` with status `GRPC.Status.internal()`
- And: the GRPC error message does NOT contain "schema detail leak" (sanitized)

**Persistence-Strategy note:** `search_messages/2` is stateless (no GenServer state). No crash/restart test required.

---

## Tasks / Subtasks

- [x] Task 1: Update `proto/core.proto` with SearchMessages RPC + message types (AC1)
  - [x] Add `SearchMessages` to the `CoreService` service
  - [x] Add `SearchMessagesRequest` message
  - [x] Add `SearchMessagesResponse` message
  - [x] Add `SearchResult` message
  - [x] Add `ProfileInfo` message
  - [x] Run `make proto` and verify no compile errors in Go + Elixir

- [x] Task 2: Write failing ATDD tests FIRST (AC2, AC3, AC4, AC5)
  - [x] Create `core/apps/event_dispatcher/test/nebu/event_dispatcher/search_messages_grpc_test.exs`
  - [x] Implement FakeSearchDB module for injection
  - [x] All 6 acceptance tests as described above
  - [x] Verify tests fail (red phase)

- [x] Task 3: Add `search_db_module` injection point to `server.ex` (AC2)
  - [x] Add `defp search_db_module do Application.get_env(:event_dispatcher, :search_db_module, Nebu.Search.DB) end`
  - [x] Follow the exact pattern used for `messages_db_module`, `profile_db_module`, etc.

- [x] Task 4: Implement `search_messages/2` gRPC handler in `server.ex` (AC2, AC3, AC4, AC5)
  - [x] Extract `user_id` from gRPC metadata — NOT from request
  - [x] Reject if `user_id` nil or empty with `GRPC.Status.unauthenticated()`
  - [x] Clamp `limit` to `min(max(limit, 1), 100)` (Kassandra Finding #4 hardening)
  - [x] Compute `offset` from `next_batch` (base64-decode to integer; 0 if empty/invalid)
  - [x] Call `search_db_module().search_messages(user_id, search_term, effective_limit, offset)`
  - [x] Handle `room_filter` (if non-empty, pass to DB; the SQL intersection happens at DB layer — see AC5 implementation note)
  - [x] Map each row to `%Core.SearchResult{}`
  - [x] Fetch context events (5 before + 5 after) via `messages_db_module()` — MVP: empty lists (proto allows it)
  - [x] Fetch profile info for each unique sender — MVP: empty map (get_profile/1 not in scope)
  - [x] Build `next_batch` token (base64-encode new offset)
  - [x] Sanitize DB errors — log full reason, return generic `GRPC.Status.internal()` to client

- [x] Task 5: Extend `Nebu.Search.DB.search_messages/4` for room_filter (AC5)
  - [x] If `room_ids` filter is provided, extend the SQL WHERE to AND with the filter list
  - [x] Keep the membership subquery — the filter is an intersection, not a replacement
  - [x] Consider adding `search_messages/5` overload or using keyword opts

- [x] Task 6: Run tests and verify green (all 6 acceptance tests pass)
  - [x] `make test-unit-elixir` passes
  - [x] `make proto` passes (Go + Elixir stubs regenerate)

---

## Dev Notes

### Proto changes required (AC1)

Add to `proto/core.proto` in the `CoreService` service block:

```protobuf
// SearchMessages — Story 11.3: Full-text search over rooms the user is a member of.
// user_id in the request is for documentation/auditing only — the handler MUST use
// x-user-id from gRPC metadata, never from the request body.
rpc SearchMessages(SearchMessagesRequest) returns (SearchMessagesResponse);
```

Add new message types after `ListAdminRoomMembersResponse`:

```protobuf
// ProfileInfo — one entry in SearchResult.profile_info map (keyed by user_id).
message ProfileInfo {
  string displayname = 1;
  string avatar_url  = 2;
}

// SearchResult — one hit in a SearchMessagesResponse.
message SearchResult {
  float             rank           = 1;  // ts_rank_cd score; higher = more relevant
  bytes             event          = 2;  // raw event JSON (same encoding as other bytes fields)
  repeated bytes    events_before  = 3;  // up to 5 events before result event in same room
  repeated bytes    events_after   = 4;  // up to 5 events after result event in same room
  map<string, ProfileInfo> profile_info = 5;  // keyed by user_id (sender + context senders)
}

// SearchMessagesRequest — Story 11.3: SearchMessages gRPC handler.
// SECURITY: user_id field is IGNORED by the handler — user_id is extracted from
// x-user-id gRPC metadata (set by Go JWTMiddleware). This field is present only
// for client auditing/documentation; treating it as authoritative would bypass
// all membership enforcement.
message SearchMessagesRequest {
  string          user_id        = 1;  // IGNORED by handler — use x-user-id metadata
  string          search_term    = 2;
  repeated string room_filter    = 3;  // client-requested room filter; intersected with membership
  repeated string sender_filter  = 4;  // optional sender restriction (not yet enforced at DB layer)
  int32           limit          = 5;  // 0 = default 10; server clamps to 1–100
  string          next_batch     = 6;  // opaque pagination token (base64-encoded offset int)
}

// SearchMessagesResponse — Story 11.3.
message SearchMessagesResponse {
  repeated SearchResult results     = 1;
  string                next_batch  = 2;  // empty when no more results
  int32                 total_count = 3;  // approximate total matching events (for UI display)
}
```

### server.ex injection point

Following the exact established pattern (see lines 1–75 of server.ex for all other modules):

```elixir
# ─── Configurable search DB module for testability ──────────────────────────
# Override via Application.put_env(:event_dispatcher, :search_db_module, FakeSearchDB) in tests.
defp search_db_module do
  Application.get_env(:event_dispatcher, :search_db_module, Nebu.Search.DB)
end
```

### Handler skeleton (for reference)

```elixir
def search_messages(request, stream) do
  # SECURITY: user_id MUST come from gRPC metadata, never from request.user_id
  {user_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

  if is_nil(user_id) or user_id == "" do
    raise GRPC.RPCError,
      status: GRPC.Status.unauthenticated(),
      message: "missing x-user-id metadata"
  end

  search_term = request.search_term
  # Kassandra Finding #4: clamp limit to prevent DOS / over-large queries
  limit = min(max(if(request.limit == 0, do: 10, else: request.limit), 1), 100)

  # Decode next_batch pagination token (base64-encoded integer offset)
  offset =
    case Base.decode64(request.next_batch || "") do
      {:ok, bin} ->
        case Integer.parse(bin) do
          {n, ""} when n >= 0 -> n
          _ -> 0
        end
      :error -> 0
    end

  room_filter = request.room_filter  # empty list = no filter; non-empty = intersect with membership

  # Delegate to Nebu.Search.DB — user_id enforces membership scoping at SQL layer
  case search_db_module().search_messages(user_id, search_term, limit, offset, room_filter) do
    {:ok, rows} ->
      results =
        Enum.map(rows, fn row ->
          # Fetch context events (5 before, 5 after) for each result
          room_id  = Map.get(row, "room_id", "")
          event_id = Map.get(row, "event_id", "")
          {events_before, events_after} = fetch_context_events(room_id, event_id)

          # Fetch profile info for all senders involved
          sender = Map.get(row, "sender", "")
          context_senders = Enum.map(events_before ++ events_after, &Map.get(&1, "sender", ""))
          all_senders = Enum.uniq([sender | context_senders])
          profile_info = fetch_profile_info(all_senders)

          event_json = Jason.encode!(%{
            "event_id"         => event_id,
            "room_id"          => room_id,
            "sender"           => sender,
            "type"             => Map.get(row, "event_type", "m.room.message"),
            "content"          => Map.get(row, "content", %{}),
            "origin_server_ts" => Map.get(row, "origin_server_ts", 0)
          })

          %Core.SearchResult{
            rank:          Map.get(row, "rank", 0.0) |> to_float(),
            event:         event_json,
            events_before: Enum.map(events_before, &Jason.encode!/1),
            events_after:  Enum.map(events_after, &Jason.encode!/1),
            profile_info:  profile_info
          }
        end)

      # Build next_batch token: if we got `limit` results, there may be more
      next_batch =
        if length(results) == limit do
          Base.encode64(Integer.to_string(offset + limit))
        else
          ""
        end

      %Core.SearchMessagesResponse{
        results:     results,
        next_batch:  next_batch,
        total_count: length(results)  # approximate — exact count is expensive
      }

    {:error, reason} ->
      # Kassandra Finding #3: log full reason server-side, return sanitized error to client
      Logger.error("search_messages failed", user_id: user_id, error: inspect(reason))

      raise GRPC.RPCError,
        status: GRPC.Status.internal(),
        message: "search failed"
  end
end
```

### AC5 — room_filter implementation

The `room_filter` in the request must be **intersected** with the user's membership rooms, not trusted as-is. The SQL layer already enforces membership via `room_id IN (SELECT room_id FROM room_members WHERE user_id = $1 AND left_at IS NULL)`. An additional room_filter is an extra restriction:

Option A: Add a 5th parameter to `Nebu.Search.DB.search_messages/5`:
```elixir
def search_messages(user_id, term, limit, offset, room_filter \\ [])
```
When `room_filter` is non-empty, append to the SQL:
```sql
AND e.room_id = ANY($5)
```
This is safe (parameterized) and correct (the membership subquery still runs — it's an AND, not a replacement).

Option B: Pass room_filter as part of the existing signature. Use Option A — it's cleaner.

> **CRITICAL**: Do NOT replace the `room_id IN (SELECT ... FROM room_members ...)` subquery with the room_filter. The membership check must always run. Room_filter is an additional restriction on top.

### Context events (events_before / events_after)

Fetch 5 events before and 5 after each result event in the same room using:
```elixir
messages_db_module().fetch_events(room_id, "b", 5, event_id)  # 5 before
messages_db_module().fetch_events(room_id, "f", 5, event_id)  # 5 after
```
If `fetch_events/4` doesn't support event_id as a cursor yet (it uses `from_token`), use a private helper that queries by `origin_server_ts` relative to the target event. Check the existing `fetch_events/4` signature in `Nebu.Room.DB` before implementing — do not reinvent pagination.

A simple fallback if fetch_events doesn't support event-based cursors:
```elixir
# events_before and events_after can be empty lists for MVP — the proto allows it.
# Story 11.6 Godog scenarios do not currently test context events.
# Fill with [] initially; add proper context lookup if time permits.
```

### Profile info fetch

Fetch profiles for all senders in a result (result event + context events):
```elixir
defp fetch_profile_info(user_ids) do
  user_ids
  |> Enum.reduce(%{}, fn user_id, acc ->
    # Use existing profile_db_module() — upsert_profile only writes, need a get
    # Check if Nebu.Profile.DB has a get_profile/1 function
    # If not: return empty ProfileInfo for MVP (profile display is best-effort)
    acc
  end)
end
```

**Important:** `Nebu.Profile.DB` currently only has `upsert_profile/3` — it does NOT have `get_profile/1`. For MVP, return empty `ProfileInfo` structs (`%Core.ProfileInfo{displayname: "", avatar_url: ""}`). Do NOT add `get_profile/1` to Profile.DB in this story unless it already exists — that would be scope creep. The profile fetch is best-effort and empty is correct behavior for MVP.

### Pagination token format

`next_batch` is a base64-encoded integer offset string:
- Encode: `Base.encode64(Integer.to_string(offset + limit))`
- Decode: `{:ok, bin} = Base.decode64(token); {n, ""} = Integer.parse(bin)`
- If decoding fails or token is empty: use offset = 0

### FakeSearchDB for tests

```elixir
defmodule FakeSearchDB do
  def search_messages(user_id, _term, _limit, _offset, _room_filter \\ []) do
    # Record which user_id was used
    :ets.insert(:search_db_test, {:last_user_id, user_id})
    # Return seeded results or empty list
    case :ets.lookup(:search_db_test, :results) do
      [{:results, rows}] -> {:ok, rows}
      [] -> {:ok, []}
    end
  end
end
```

---

## Files to Create / Modify

| File | Action | Notes |
|---|---|---|
| `proto/core.proto` | MODIFY | Add `SearchMessages` RPC + 4 new message types (SearchMessagesRequest, SearchMessagesResponse, SearchResult, ProfileInfo) |
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | MODIFY | Add `search_db_module/0` defp + `search_messages/2` handler (at end of file, before final `end`) |
| `core/apps/event_dispatcher/lib/nebu/search/db.ex` | MODIFY | Add optional 5th param `room_filter` to `search_messages/5` for AC5 |
| `core/apps/event_dispatcher/test/nebu/event_dispatcher/search_messages_grpc_test.exs` | NEW | 6 ExUnit acceptance tests |

No new migrations. No Go gateway changes (that is Story 11.4). No Playwright tests (ui: false).

---

## Project Structure Notes

- `Nebu.Search.DB` is in `core/apps/event_dispatcher/lib/nebu/search/db.ex` (created in Story 11.2) — do not move or duplicate
- Handler goes in `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — all gRPC handlers live here (2701 lines as of Story 11.2)
- Test file follows naming convention: `core/apps/event_dispatcher/test/nebu/event_dispatcher/<feature>_test.exs`
- `async: false` is mandatory — Horde + ETS + Application.put_env are all process-global
- `search_db_module/0` injection follows the exact pattern of the 9 other injectable modules in server.ex (lines 1–75)

### What NOT to do

- Do NOT read `user_id` from `request.user_id` — only from `Nebu.Grpc.Metadata.trusted_identity(stream)`
- Do NOT replace the membership subquery in `Nebu.Search.DB` with the room_filter — it is an intersection
- Do NOT add `get_profile/1` to `Nebu.Profile.DB` in this story (out of scope)
- Do NOT add a Go gateway handler in this story (that is Story 11.4)
- Do NOT add rate limiting in this story (that is Story 11.5)
- Do NOT add Godog scenarios in this story (that is Story 11.6)
- Do NOT pass `request.user_id` to `search_messages/4` even if the field name is tempting

---

## Previous Story Intelligence (11.2)

From Story 11.2 completion notes (relevant to this story):

- `Nebu.Search.DB` module is at `core/apps/event_dispatcher/lib/nebu/search/db.ex`
- `search_messages/4` signature: `(user_id, term, limit, offset)` → `{:ok, [map()]} | {:error, term()}`
- SQL uses `left_at IS NULL` (NOT `membership = 'join'` — there is NO membership column in room_members)
- The `NOT EXISTS` encryption filter uses `AND (enc.state_key = '' OR enc.state_key IS NULL)` (Kassandra M-1 fix already applied)
- `sql_search_messages/0` public function exists for structural testing
- Integration tests tagged `@moduletag :integration` — excluded from `make test-unit-elixir`
- Elixir unit tests: 207 passing (0 failures, 6 excluded as integration)
- `ExUnit.configure(exclude: [:integration])` is in `event_dispatcher/test/test_helper.exs`

**Security contract documented in `Nebu.Search.DB`:**
```
SECURITY: `user_id` MUST come from the validated session (gRPC metadata or JWT claim),
never from the request payload. Passing a caller-supplied user_id bypasses all
membership enforcement and enables cross-room IDOR.
```

**Kassandra findings to address in this story:**
- MEDIUM-2: Handler MUST use metadata user_id — enforced by AC2 + Test 1
- LOW-3: DB errors sanitized — enforced by Test 6
- LOW-4: Clamp limit/offset — `min(max(limit, 1), 100)` in handler

---

## References

- [Source: core/apps/event_dispatcher/lib/nebu/search/db.ex] — canonical SQL contract + `search_messages/4` signature
- [Source: core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1-75] — injection pattern for all injectable modules
- [Source: core/apps/event_dispatcher/lib/nebu/grpc/metadata.ex] — `trusted_identity/1` extracts `{user_id, system_role}` from `stream.http_request_headers`
- [Source: proto/core.proto:1-120] — existing RPC and message patterns; `ListAdminRoomMembersResponse` is the last message before this story's additions
- [Source: docs/architecture/adr/ADR-010-fts-strategy.md] — PostgreSQL tsvector/GIN, websearch_to_tsquery, ts_rank_cd, ADR Accepted 2026-05-08
- [Source: docs/stories/phase2/epic-11/security-reports/11-2-search-membership-security-review-2026-05-08.md] — Kassandra M-2 (user_id trust boundary), LOW-3 (error sanitization), LOW-4 (clamp limit)
- [Source: core/apps/event_dispatcher/test/nebu/event_dispatcher/upgrade_room_test.exs:243-244] — `build_stream/1` pattern for fake gRPC stream
- [Source: core/apps/event_dispatcher/test/nebu/event_dispatcher/server_moderation_metadata_test.exs:156-163] — `build_stream/2` with system_role

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

(none — clean implementation, all 6 tests green on first run)

### Completion Notes List

- Added `SearchMessages` RPC to `proto/core.proto` with all required message types (ProfileInfo, SearchResult, SearchMessagesRequest, SearchMessagesResponse). `make proto` regenerated Go and Elixir stubs without errors.
- Added `SearchMessages` (and backfilled `UpgradeRoom`, `ListAdminRoomMembers`) to `core_grpc.pb.ex` service stub — protoc-gen-elixir does not auto-update this file when new RPCs are added; manual update is the established pattern.
- Added `search_db_module/0` injection point to `server.ex` following the exact pattern of the 9 other injectable modules (lines 1–80).
- Implemented `search_messages/2` handler in `server.ex` with all AC requirements: metadata-only user_id (AC2/SECURITY), limit clamping (Kassandra #4), next_batch base64 pagination, room_filter passthrough (AC5), DB error sanitization (Kassandra LOW-3, Test 6).
- context events and profile_info are empty for MVP as documented — proto allows empty lists/maps, Story 11.6 Godog scenarios do not test context events.
- Extended `Nebu.Search.DB` with `search_messages/5` overload (room_filter parameter) and a second SQL constant `@sql_search_messages_with_room_filter` that adds `AND e.room_id = ANY($5)`. The 4-arity function delegates to 5-arity with `[]`. The membership subquery is preserved in both SQL variants (intersection, not replacement).
- All 213 event_dispatcher unit tests pass (0 failures, 6 excluded integration, 2 skipped). The `search_messages failed` log line in output is expected — it comes from Test 6 which seeds FakeSearchDB with an error to verify sanitization behavior.

### File List

- `proto/core.proto` — Added `SearchMessages` RPC to service; added `ProfileInfo`, `SearchResult`, `SearchMessagesRequest`, `SearchMessagesResponse` message types
- `gateway/internal/grpc/pb/core.pb.go` — Regenerated by `make proto`
- `gateway/internal/grpc/pb/core_grpc.pb.go` — Regenerated by `make proto`
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — Regenerated by `make proto`
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — Added `UpgradeRoom`, `ListAdminRoomMembers`, `SearchMessages` RPC entries
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — Added `search_db_module/0` injection + `search_messages/2` handler
- `core/apps/event_dispatcher/lib/nebu/search/db.ex` — Added `@sql_search_messages_with_room_filter` SQL constant + `search_messages/5` overload
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/search_messages_grpc_test.exs` — 6 ATDD acceptance tests (pre-existing from ATDD phase, now green)

## Change Log

| Date | Change |
|---|---|
| 2026-05-08 | Story created: ready-for-dev |
| 2026-05-08 | Implementation complete: proto updated, handler implemented, DB extended, all 213 unit tests green |
