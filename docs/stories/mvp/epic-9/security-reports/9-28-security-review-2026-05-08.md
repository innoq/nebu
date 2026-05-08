# Security Review — 9-28: Thread Relations

**Reviewer:** Kassandra
**Date:** 2026-05-08
**Story:** 9-28 — Bug Fix: first thread reply not appearing in thread panel
**Diff scope:** Thread Relations endpoint (`GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/{relType}`),
bundled aggregations in `/sync` (`unsigned.m.relations.m.thread`), new DB queries
(`fetch_events_by_relation`, `count_thread_children`, `event_in_room?`), SQL migration 000042.

## Files Reviewed

- `gateway/internal/matrix/relations.go` — HTTP handler (new)
- `gateway/cmd/gateway/main.go` — route registration (+9 lines)
- `gateway/internal/grpc/client.go` — gRPC shim (+8 lines)
- `gateway/internal/grpc/pb/core.pb.go` — generated proto (+372/−130)
- `gateway/internal/grpc/pb/core_grpc.pb.go` — generated proto (+46)
- `gateway/internal/matrix/sync.go` — bundled aggregations in `/sync` (+16/−2)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — `get_relations/2`, `attach_thread_aggregations/3` (+133/−7)
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — proto message defs (+28)
- `core/apps/room_manager/lib/nebu/room/db.ex` — `fetch_events_by_relation/4`, `count_thread_children/2`, `event_in_room?/2` (+83)
- `core/apps/room_manager/lib/nebu/room/db_behaviour.ex` — callback specs (+39)
- `gateway/migrations/000042_thread_relations_index.up.sql` — expression index on JSONB (new)
- `gateway/migrations/000042_thread_relations_index.down.sql` — rollback (new)
- `proto/core.proto` — `GetRelationsRequest`, `GetRelationsResponse`, `unsigned_relations` field (+31/−8)

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | MEDIUM | `server.ex:2759` | Internal DB error details in gRPC message via `inspect(reason)` | Log details server-side; send generic string in RPCError.message |
| 2 | MEDIUM | `server.ex:2717`, `relations.go:68,83` | `rel_type` accepted without allow-list or length cap; arbitrary strings stored in DB query context | Add allow-list in Core before query (`@allowed_rel_types ~w(m.thread m.reference m.annotation m.replace)`) |
| 3 | LOW | `main.go:732-733` | No rate limiting on `/relations` endpoint (consistent with comparable GET room endpoints, but documented) | Apply `mediumRL` (or equivalent) post-MVP; document as accepted operational gap |
| 4 | LOW | `sync.go:763` | `unsigned_relations` bytes from Core passed as `json.RawMessage` without `json.Valid` check | Add `json.Valid(unsignedRel)` guard before assignment |

---

## Detail

### Finding #1 — Internal error details in gRPC message [MEDIUM]

**Location:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2757–2760`

```elixir
{:error, reason} ->
  raise GRPC.RPCError,
    status: GRPC.Status.internal(),
    message: "fetch_events_by_relation failed: #{inspect(reason)}"
```

`inspect(reason)` on a DB error may produce strings such as
`{:error, %DBConnection.ConnectionError{message: "connection not available", reason: :queue_timeout, ...}}`,
which includes internal connection pool details. The Go gateway correctly collapses all `codes.Internal`
errors to `"Internal error"` at the HTTP boundary (`relations.go:95`), so Matrix clients never see these
details. However, the verbose message travels over gRPC to the gateway and is logged there with `slog.Error`
(`relations.go:94`), exposing DB internals to anyone with gateway log access.

This is the same pattern as MEDIUM-2 in story 9-27. It is a recurring issue in newly-added gRPC handlers
(see Recurring Patterns in MEMORY.md) — the pattern `inspect(reason)` in RPCError message persists.

**Impact:** Defense-in-depth gap. No direct exploit path; HTTP layer sanitizes the message. If gateway
logs are accessible to a malicious insider or through log aggregation misconfiguration, DB internals leak.

**Fix:** Log with `Logger.error/1` on the Core side; keep the RPCError message generic:
```elixir
{:error, reason} ->
  Logger.error("fetch_events_by_relation failed", room_id: room_id, reason: inspect(reason))
  raise GRPC.RPCError,
    status: GRPC.Status.internal(),
    message: "fetch_events_by_relation failed"
```

---

### Finding #2 — `rel_type` accepted without allow-list or length cap [MEDIUM]

**Location:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2717`, `gateway/internal/matrix/relations.go:68,83`

```go
relType := r.PathValue("relType")
...
RelType: relType,
```

```elixir
rel_type = request.rel_type
...
fetch_events_by_relation(room_id, event_id, rel_type, limit)
```

The `rel_type` path parameter is extracted from the URL path and forwarded directly to Core, which uses it
as `$2` in a parameterized SQL query:

```sql
AND content->'m.relates_to'->>'rel_type' = $2
```

SQL injection is **not possible** here — Ecto uses parameterized queries exclusively. However, there are
two secondary concerns:

1. **Unbounded length:** An attacker authenticated as a room member can supply a `rel_type` value of
   arbitrary length (bounded only by the Go server's URL path length limit, typically 8 KiB). The value is
   used as a SQL parameter, so it passes through to PostgreSQL string comparison. Extremely long values add
   negligible overhead (VARCHAR comparison is length-bounded at the `=` operator), but they are unnecessary
   and deviate from the Matrix spec, which defines a closed set of relation types.

2. **Arbitrary relation type queries:** An authenticated room member can query for relation types not defined
   by the Matrix spec (e.g., `rel_type=m.reference`, `rel_type=m.replace`, or custom event types). This is
   not inherently exploitable, but it allows enumeration of non-thread relations via this endpoint, which may
   not be the intended behavior. The 9-27 review found the same pattern with `new_version` (MEDIUM-1) where
   unvalidated string input was embedded into event content. Here the data is not persisted, but the scope
   is wider than designed.

**Impact:** Borderline MEDIUM. No direct exploit. The broader concern is the same pattern as 9-27 MEDIUM-1:
user-supplied strings accepted without validation create an implicit contract that is wider than intended.
An allow-list closes this cleanly.

**Fix (Core):** Add allow-list validation before the DB query:
```elixir
@allowed_rel_types ~w(m.thread m.reference m.annotation m.replace)

if rel_type not in @allowed_rel_types do
  raise GRPC.RPCError,
    status: GRPC.Status.invalid_argument(),
    message: "unsupported rel_type: #{rel_type}"
end
```

The Go gateway should map `codes.InvalidArgument` to `400 M_INVALID_PARAM`. If expanding to other
relation types later, add them to the allow-list explicitly.

---

### Finding #3 — No rate limiting on `GET /relations` [LOW]

**Location:** `gateway/cmd/gateway/main.go:732-733`

```go
mux.Handle("GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/{relType}",
    jwtWithStatusCheck(http.HandlerFunc(relationsHandler.GetRelations)))
```

The `/relations` endpoint is wrapped with `jwtWithStatusCheck` (JWT validation + user-active status check)
but has no rate-limiting middleware. This is the same posture as comparable read-only room endpoints in
the same file: `GET /context/{eventId}` (line 723–724), `GET /messages` (line 835), and `GET /state`
(lines 900–907) — none apply rate-limiting middleware.

The endpoint triggers two DB queries per call (`fetch_events_by_relation` + up to N+1 aggregation queries
via `attach_thread_aggregations` in `/sync`, separate from the `/relations` path). A legitimate client
calling this repeatedly is the expected use case; a malicious authenticated user could use it to drive
DB load proportional to thread depth.

**Impact:** Low. The pattern is consistent with existing endpoints (no new regression introduced). The risk
is authenticated DoS, not data exposure. Acceptable for MVP with the same posture as existing endpoints.

**Fix (post-MVP):** Apply `mediumRL` to this route when rate limiting is applied to the broader read-only
endpoint tier. Document as a known operational gap consistent with the 9-27 LOW-1 finding.

---

### Finding #4 — `unsigned_relations` passed as `json.RawMessage` without validity check [LOW]

**Location:** `gateway/internal/matrix/sync.go:761-764`

```go
var mRelations json.RawMessage
if unsignedRel := te.GetUnsignedRelations(); len(unsignedRel) > 0 {
    mRelations = json.RawMessage(unsignedRel)
}
```

The `unsigned_relations` bytes produced by `Jason.encode!(agg)` in Core are trusted as valid JSON. In the
current code path this is correct — `Jason.encode!` will panic (crash the calling process) rather than
return malformed JSON, and Core is an internal trusted service. However, the bytes traverse the gRPC
boundary and are cast directly to `json.RawMessage` without a `json.Valid` check. If Core behavior
changes (e.g., the field is populated from an alternative code path that reads bytes directly from the DB),
malformed JSON bytes in `unsigned_relations` would be forwarded as-is into the Matrix `/sync` response
JSON stream, producing a response that clients cannot parse.

**Impact:** Low. The current code path is safe because `Jason.encode!` guarantees valid JSON or crashes.
No attacker can influence `unsigned_relations` from outside the Core. This is a robustness/future-proofing
concern, not a current exploit path.

**Fix:** Add a validity guard in Go before accepting the raw bytes:
```go
if unsignedRel := te.GetUnsignedRelations(); len(unsignedRel) > 0 && json.Valid(unsignedRel) {
    mRelations = json.RawMessage(unsignedRel)
}
```

This is a one-line defensive addition with zero performance impact for the happy path.

---

## Positive Security Observations

1. **SQL injection: CLEAN.** All three new DB functions (`fetch_events_by_relation`, `count_thread_children`,
   `event_in_room?`) use parameterized queries exclusively (`$1`, `$2`, etc. via `Ecto.Adapters.SQL.query`).
   No string interpolation into SQL. JSONB path operators (`->`, `->>`) are used as SQL literals in the
   query template, not derived from user input.

2. **IDOR prevention: CLEAN.** `get_relations/2` enforces membership check BEFORE `event_in_room?` check.
   The `event_in_room?` check scopes event existence to `room_id`, preventing cross-room event existence
   probing by authenticated room members. Order of checks is correct: authenticate → authorize (membership)
   → scope (room-scoped event existence) → fetch.

3. **Auth bypass: CLEAN.** The `/relations` route is registered with `jwtWithStatusCheck` which validates
   the JWT, checks token active status, and sets `ContextKeyUserID`. The handler additionally asserts
   `userID != ""` before proceeding (defense-in-depth). The Core also enforces membership via `MapSet.member?`
   — auth is enforced at both the gateway and Core layers.

4. **Information leakage between rooms: CLEAN.** When `event_in_room?` returns `false`, the handler raises
   `NOT_FOUND` (not `PERMISSION_DENIED`). This prevents a non-member who has somehow passed the membership
   check from distinguishing "event does not exist" from "event exists in another room." Because membership
   is checked first, a non-member gets `PERMISSION_DENIED` before reaching `event_in_room?`. Correct.

5. **XSS/CSRF: CLEAN.** The endpoint is a JSON API (no HTML rendering). No CSRF risk on GET endpoints.
   Content is serialized via `json.NewEncoder(w)` and `Jason.encode!` — no template rendering.

6. **Hardcoded secrets: CLEAN.** No API keys, passwords, or internal secrets in the diff.

7. **Weak crypto: CLEAN.** No new cryptographic operations introduced.

8. **SQL migration safety: CLEAN.** Migration 000042 creates an expression index using `CREATE INDEX
   CONCURRENTLY IF NOT EXISTS` — non-blocking, idempotent, no new tables, no new data columns. No
   sensitive data stored plaintext, no injection vectors in the DDL.

9. **Body-size limit on GET: N/A.** GET endpoints carry no body. Not applicable.

---

## Summary

```
CRITICAL: 0
HIGH:     0
MEDIUM:   2 — advisory; address before epic-end
LOW:      2 — advisory
INFO:     0
```

**Verdict: APPROVED — no CRITICAL or HIGH findings. Commit is not blocked.**

The two MEDIUM findings are defense-in-depth gaps consistent with patterns seen in 9-27 (error
message sanitization, input allow-list). Neither creates a direct exploit path. They should be
addressed as a follow-up story or bundled with the MEDIUM backlog from 9-27.

Recommended follow-up (can be a single small story):
- `inspect(reason)` in gRPC error messages (Finding #1 — recurring pattern)
- `rel_type` allow-list in `get_relations/2` (Finding #2 — parallel to `new_version` validation in 9-27)
- `json.Valid` guard on `unsigned_relations` passthrough (Finding #4 — one-liner)

## Severity Counts

- CRITICAL: 0
- HIGH: 0
- MEDIUM: 2
- LOW: 2
