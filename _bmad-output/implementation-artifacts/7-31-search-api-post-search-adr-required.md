---
id: 7-31
type: feature
security_review: optional
created: 2026-04-30
---

# Story 7.31: Search API — POST /search (ADR-010 required)

Status: backlog

## Story

As an end-user,
I want to search for messages across rooms via `POST /_matrix/client/v3/search`,
so that I can find past conversations without scrolling through the entire history.

## Blocker

**This story is BLOCKED pending approval of ADR-010 (Full-Text Search Strategy).**

Before any implementation work begins, the Architecture Decision Record at
`docs/architecture/adr/010-search-fulltext-strategy.md` must be written and accepted.
The ADR must answer:

- PostgreSQL FTS (`tsvector`/`tsquery` + GIN index) vs. external search component (e.g. Meilisearch, Elasticsearch, Typesense).
- Recommended path for MVP: PostgreSQL FTS — no new infrastructure dependency.
- Trade-offs: query complexity, multi-language tokenisation, ranking quality, operational cost.
- Migration path if a future epic introduces an external engine.

Only after ADR-010 is merged should this story move to `ready-for-dev`.

## Context / Background

`POST /_matrix/client/v3/search` is the most complex Matrix API endpoint. It supports:

- Full-text search across event `content.body` (and optionally other keys).
- Room filter (restrict search to a subset of rooms the user is in).
- Result ordering by `relevance` or `recent`.
- Pagination via `next_batch` token.
- `event_context`: return N events before/after each result for inline display.

**MVP recommendation (subject to ADR-010):** PostgreSQL FTS.

```sql
-- Generated column on events table (or a dedicated fts_index table):
ALTER TABLE events ADD COLUMN fts tsvector
  GENERATED ALWAYS AS (to_tsvector('simple', coalesce(content->>'body', ''))) STORED;

CREATE INDEX events_fts_idx ON events USING GIN (fts);
```

Search query becomes:

```sql
SELECT * FROM events
WHERE fts @@ websearch_to_tsquery('simple', $query)
  AND room_id = ANY($room_ids)   -- membership-filtered room list
ORDER BY ts_rank(fts, ...) DESC  -- or created_at DESC for 'recent'
LIMIT $limit OFFSET $cursor;
```

**Membership enforcement (critical):** The gateway must only search rooms where the requesting
user is currently (or was previously, depending on history visibility) a member. The room list
must be fetched from the Core (gRPC `GetUserRooms`) before issuing the search query.

**Security note:** Full-text index could expose `content.body` from rooms the user has since left
if history_visibility is not checked. The ADR must address this. See `security_review: optional`
above.

**New gRPC call** (after ADR approval):

```protobuf
message SearchEventsRequest {
  string user_id       = 1;
  string search_term   = 2;
  repeated string room_ids = 3;  // empty = all accessible rooms
  string order_by      = 4;      // "recent" | "relevance"
  int32  limit         = 5;
  string next_batch    = 6;      // pagination cursor
  int32  before_limit  = 7;      // event_context
  int32  after_limit   = 8;      // event_context
}
message SearchEventsResponse {
  repeated SearchResult results = 1;
  string next_batch             = 2;
  int32  count                  = 3;  // total estimated matches
}
message SearchResult {
  string event_json             = 1;
  double rank                   = 2;
  repeated string before_events = 3;  // event_json strings
  repeated string after_events  = 4;
}
```

**New handler file:** `gateway/internal/matrix/search.go`

## Acceptance Criteria

The following ACs are conditional on ADR-010 being approved. They may be revised based on the
ADR outcome (e.g. if an external engine is chosen, the migration steps change).

1. ADR-010 is written, reviewed, and merged at `docs/architecture/adr/010-search-fulltext-strategy.md`
   before any implementation code is written.

2. `POST /_matrix/client/v3/search` with `search_categories.room_events.search_term` returns HTTP 200
   with matching events from rooms the user is a member of.

3. Results respect the `filter.rooms` list — if provided, only those rooms (intersected with
   rooms the user is a member of) are searched.

4. `order_by: "recent"` returns results sorted by event timestamp descending.
   `order_by: "relevance"` (or omitted) returns results sorted by FTS rank descending.

5. Pagination via `next_batch` token: passing the returned token as a query parameter
   `?next_batch=TOKEN` returns the next page of results without overlap.

6. `event_context.before_limit` and `event_context.after_limit` return the surrounding events
   in `context.events_before` / `context.events_after` on each result.

7. Only events in rooms where the user is (or was, within their history_visibility) a member are
   returned — no cross-room leakage.

8. Performance: a search against a user's rooms must complete in under 500 ms for a dataset of
   100 000 events (verified in an integration benchmark, not a unit test).

9. Unauthenticated requests are rejected with 401 `M_MISSING_TOKEN` before the handler is reached.

## Acceptance Tests

### Tests written FIRST (before implementation code):

Note: all tests below are to be authored after ADR-010 is accepted. They are written here as
**intent specifications** to guide the ADR and ensure testability of the chosen approach.

1. [Search_ReturnsMatchingEvents] — Godog
   - Given: authenticated user `@alice:server`; room `!test:server` contains a message with body "hello world"
   - When: `POST /_matrix/client/v3/search` body
     `{"search_categories":{"room_events":{"search_term":"hello"}}}`
   - Then: HTTP 200; `results.room_events.results` contains an entry whose `result.content.body`
     includes "hello world"

2. [Search_FilterByRoom_ExcludesOtherRooms] — Godog
   - Given: user is member of two rooms; "hello" appears in both; filter specifies only room A
   - When: `POST /search` with `filter.rooms: ["!roomA:server"]`
   - Then: HTTP 200; all returned results are from `!roomA:server`

3. [Search_Pagination_NoOverlap] — Godog
   - Given: user has 10 matching events; search with `limit=5` returns first page with `next_batch`
   - When: second `POST /search?next_batch=TOKEN` with same body
   - Then: HTTP 200; 5 new unique results returned; no event_id duplicated across pages

4. [Search_EventContext_SurroundingEvents] — Godog
   - Given: room with events e1, e2 (matching), e3
   - When: `POST /search` with `event_context: {"before_limit":1,"after_limit":1}`
   - Then: result for e2 includes `context.events_before: [e1]` and `context.events_after: [e3]`

5. [Search_MembershipEnforcement_NonMemberRoomExcluded] — Godog
   - Given: user is NOT a member of `!private:server`; that room has matching events
   - When: `POST /search` body `{"search_categories":{"room_events":{"search_term":"secret"}}}`
   - Then: HTTP 200; no results from `!private:server`

6. [Search_Unauthenticated_Rejected] — Godog
   - Given: no Authorization header
   - When: `POST /_matrix/client/v3/search` with valid body
   - Then: HTTP 401 `{"errcode":"M_MISSING_TOKEN",...}`

7. [Search_EmptyTerm_Returns400] — Godog
   - Given: authenticated user
   - When: `POST /search` with empty `search_term: ""`
   - Then: HTTP 400 `{"errcode":"M_INVALID_PARAM","error":"search_term must not be empty"}`

## Implementation Notes

**ADR-010 must be created at:** `docs/architecture/adr/010-search-fulltext-strategy.md`

The ADR template should follow the existing ADR style in the project. Minimum sections: Context,
Decision, Consequences, Alternatives considered.

**Files to create / modify (after ADR-010 approval):**

- `docs/architecture/adr/010-search-fulltext-strategy.md` — the ADR (prerequisite, not implementation).
- `gateway/migrations/000031_events_fts.up.sql` + `000031_events_fts.down.sql` — add `fts tsvector`
  generated column and GIN index to the `events` table (or equivalent per ADR decision).
- `proto/core.proto` — add `SearchEvents` RPC with `SearchEventsRequest` / `SearchEventsResponse`.
- `gateway/internal/matrix/search.go` — `SearchHandler` struct + handler method.
- `gateway/internal/matrix/search_test.go` — unit tests with `httptest`.
- `gateway/features/search.feature` — Godog feature file (written first, red phase).
- `gateway/cmd/gateway/main.go` — register `POST /_matrix/client/v3/search` under `jwtMiddleware`.
- `core/apps/room_manager/` or a dedicated search module — implement `SearchEvents` gRPC handler.

**Membership enforcement implementation pattern:**
1. Gateway calls `GetUserRooms(user_id)` → list of room_ids the user is/was a member of.
2. Intersect with `filter.rooms` if provided.
3. Pass the filtered room_id list to `SearchEvents` RPC.
4. Core enforces the list at query level — never returns events from rooms not in the list.

**Error-mapping pattern:**
- `codes.InvalidArgument` → 400 `M_INVALID_PARAM`
- `codes.Unauthenticated` → 401 `M_MISSING_TOKEN`
- `codes.Unavailable` → 503 `M_UNAVAILABLE`
- default → 500 `M_UNKNOWN`

**Performance gate:** Before marking this story done, run `make test-integration` with the
benchmark scenario (AC8). 500 ms p95 threshold on 100 000 events is a hard gate.

**Out of scope for this story:**
- Searching `content.name` / `content.topic` (keys other than `content.body`).
- Encrypted event search (requires client-side key export — Phase 3+).
- Spell correction or fuzzy matching.
- Highlights / snippet extraction in results (nice-to-have, Phase 2).
