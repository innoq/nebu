# Kassandra Security Review — Story 11.4: Gateway POST /_matrix/client/v3/search Handler

**Date:** 2026-05-11
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Scope:** Staged diff for Story 11.4 — Gateway `POST /_matrix/client/v3/search` handler, gRPC wrapper, and main.go registration
**Story file:** `docs/stories/phase2/epic-11/11-4-search-gateway-handler.md`
**Verdict:** **CLEAN** — APPROVED for commit. Zero CRITICAL, zero HIGH, zero MEDIUM. Two LOW observations (defense-in-depth hardening, non-blocking).

---

## Files Reviewed

| File | Status | Lines |
|---|---|---|
| `gateway/internal/matrix/search.go` | NEW | 244 |
| `gateway/internal/matrix/search_test.go` | NEW | 572 |
| `gateway/internal/grpc/client.go` | MODIFY (added `SearchMessages` wrapper) | +6 |
| `gateway/cmd/gateway/main.go` | MODIFY (handler registration) | +8 |

Cross-referenced Elixir Core implementation (read-only, in scope of Stories 11.2/11.3 already reviewed):
- `core/apps/event_dispatcher/lib/nebu/search/db.ex` — parameterised SQL contract
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2603-2716` — `search_messages/2` gRPC handler

---

## Threat Model for This Story

The Gateway handler is the trust boundary where untrusted client JSON meets a privileged backend query. Threats considered:

| Threat | Attack Vector | Impact |
|---|---|---|
| SQL injection via `search_term` | Crafted Postgres syntax in JSON `search_term` | Read or destroy events table |
| IDOR — search rooms user is not a member of | Forge `user_id` in body, or pass another user's user_id via crafted gRPC | Cross-tenant message exfiltration |
| Auth bypass — invoke handler without JWT | Direct HTTP request bypassing middleware | All of the above without authentication |
| DoS via giant `limit` or unbounded pagination | `limit: 2^31` or `next_batch: <bogus>` | Memory exhaustion / connection saturation |
| DoS via giant JSON body | Multi-MB nested JSON | Memory exhaustion |
| Information disclosure via error messages | Force `codes.Internal` | Leak schema, queries, table names |
| Open redirect / path traversal | N/A — no redirects or filesystem ops in handler | — |
| Timing attack on secret comparison | N/A — no secret comparison in handler | — |

All of these are addressed. Details below.

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|---|---|---|---|
| LOW-1 | LOW | `search.go:122-138` (gRPC error mapping) | The `default` branch maps unexpected gRPC codes to 500 `M_UNKNOWN` but does NOT log the underlying gRPC status code or message server-side. Story Dev Notes example explicitly included `slog.Error("SearchMessages gRPC failed", "code", st.Code(), "msg", st.Message())`. Missing this means an incident responder has no signal in Gateway logs when Core returns `codes.Internal`, `codes.Unavailable`, etc. — only Core's logs would show it. Defence-in-depth observability gap, not an exploit. | Add `slog.Error("SearchMessages gRPC failed", "code", st.Code(), "msg", st.Message(), "user_id", userID)` in the `default` arm. Do NOT include `st.Message()` in the response body (already correct — sanitised to "search failed"). |
| LOW-2 | LOW | `search.go:76-80` (JSON decode without Content-Type check) | The handler skips `requireJSON(w, r)` and decodes the body unconditionally. Story Task 3 explicitly listed `requireJSON` as the first step. Effect: a client sending `Content-Type: text/plain` with a JSON body still parses successfully. This is a spec-conformance gap (Matrix CS API expects 415 for non-JSON content types on JSON-bodied endpoints) rather than a security flaw, since `bodyLimit1MiB` bounds the input regardless. No injection vector. | Insert `if !requireJSON(w, r) { return }` immediately after the userID check, matching the pattern used in `login.go`, `account_data.go`, `tags.go`, etc. |

No CRITICAL, HIGH, or MEDIUM findings.

---

## Security Checklist — Mandatory Items

### 1. SQL Injection — CLEAN

The handler does NOT construct SQL. The flow is:

1. Gateway parses `search_term` from JSON into a `string` field (`searchRequest.SearchCategories.RoomEvents.SearchTerm`).
2. Trims whitespace, rejects empty.
3. Passes as-is to `pb.SearchMessagesRequest.SearchTerm` (protobuf string field).
4. gRPC marshals it as length-prefixed bytes.
5. Core (`Nebu.Search.DB.search_messages/5`) invokes `Ecto.Adapters.SQL.query/3` with parameterised SQL:

```sql
WHERE e.search_vector @@ websearch_to_tsquery('pg_catalog.simple', $2)
  AND e.event_type = 'm.room.message'
  AND e.room_id IN (SELECT room_id FROM room_members WHERE user_id = $1 AND left_at IS NULL)
  ...
  AND e.room_id = ANY($5)
ORDER BY rank DESC, e.origin_server_ts DESC
LIMIT $3 OFFSET $4
```

- `$1` = `user_id` (from gRPC metadata, trusted)
- `$2` = `search_term` (parameterised — `websearch_to_tsquery` further sanitises this as a tsquery, not arbitrary SQL)
- `$3` = `limit` (Elixir-clamped int)
- `$4` = `offset` (Elixir-decoded int)
- `$5` = `room_filter` ([]string passed as Postgres array — `= ANY($5)`)

No string interpolation anywhere on the path. `websearch_to_tsquery` is the safe Postgres tsquery parser (handles operators like `&`, `|`, `!`, quotes literally — does not eval SQL).

**Verdict:** No injection surface in this story.

### 2. IDOR — CLEAN (Core enforces; Gateway does not subvert)

This is the security spine of the entire feature, and Story 11.4 implements it correctly.

**Gateway invariants verified:**

- `search.go:69` reads `userID` from `middleware.ContextKeyUserID` — this is populated by `jwtMiddleware` from the verified OIDC JWT. Never from the request body.
- `search.go:110` builds `grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)` — sets `x-user-id` outgoing gRPC metadata header.
- `search.go:112-119` constructs `pb.SearchMessagesRequest` and explicitly leaves `UserId` unset (the explicit `// SECURITY: UserId intentionally NOT set` comment is preserved).
- `search.go:121` passes `grpcCtx` (the metadata-enriched context) to `SearchMessages`.

**Core invariants verified (unchanged from Story 11.3 review):**

- `server.ex:2615`: `{user_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)` — reads from gRPC metadata, NOT from `request.user_id`.
- `server.ex:2617-2621`: if `user_id` is nil/empty, raises `GRPC.RPCError{status: unauthenticated}`.
- SQL membership subquery `WHERE user_id = $1 AND left_at IS NULL` enforces scope at SQL layer.
- Encrypted rooms excluded via `NOT EXISTS (... event_type = 'm.room.encryption' ...)`.

**Tests verifying the invariant:**

- `search_test.go:465-503` `TestPostSearch_UserIDFromContext_NotFromBody` — asserts `mock.capturedReq.UserId == ""` AND `metadata.FromOutgoingContext` contains `x-user-id = "@alice:test.local"`.

This is the regression test promised in Story 11.2/11.3 Kassandra reviews (MEDIUM-2). It is present, well-targeted, and named clearly.

**Verdict:** The trust boundary the Kassandra MEMORY.md "DB-module user_id trust-boundary docstring" pattern (Epic 11) warns about is preserved correctly. Gateway hands off `user_id` only through the validated metadata channel.

### 3. Auth Bypass — CLEAN

`gateway/cmd/gateway/main.go:732-733`:

```go
mux.Handle("POST /_matrix/client/v3/search",
    bodyLimit1MiB(jwtWithStatusCheck(http.HandlerFunc(searchHandler.PostSearch))))
```

- `bodyLimit1MiB` (outer) — caps request body at 1 MiB.
- `jwtWithStatusCheck` — composition of `jwtMiddleware` (signature + audience + expiry + denylist) and `WithUserStatusCheck` (denies deactivated accounts with 401 `M_UNKNOWN_TOKEN`).
- Handler runs only if both pass.

Additionally, defence-in-depth check in `search.go:69-73`: if `userID == ""` in context (e.g. test harness invokes handler directly), it returns 401 `M_UNKNOWN_TOKEN`. Confirmed by `TestPostSearch_Unauthenticated`.

Wrapping order matches existing handlers (event_context, public_rooms, rooms, account_data, tags). No deviation.

**Verdict:** No auth bypass.

### 4. Timing Attacks — N/A

No secret comparison in this handler. The only string comparisons are:
- `term == ""` (after trim) — public input vs. literal, no timing oracle
- `userID == ""` — context value vs. literal

Neither involves a secret. **N/A.**

### 5. Body Size Limit — CLEAN

`bodyLimit1MiB` (1 MiB cap) applied at registration (main.go:733). Wraps the entire chain. Confirmed identical to the pattern used for every other JSON POST/PUT handler in main.go.

### 6. Rate Limit Mapping — CLEAN

`search.go:124-132`:

```go
case codes.ResourceExhausted:
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusTooManyRequests)
    _ = json.NewEncoder(w).Encode(map[string]interface{}{
        "errcode":        "M_LIMIT_EXCEEDED",
        "error":          "search rate limit exceeded",
        "retry_after_ms": 60000,
    })
```

- HTTP 429 ✓
- `M_LIMIT_EXCEEDED` errcode ✓ (Matrix spec §3.2.1)
- `retry_after_ms` field present, fixed at 60000 (60s). Not leaking rate-limiter internal state — a fixed value is a sensible MVP.
- Test `TestPostSearch_ResourceExhausted_429` validates 429 + `M_LIMIT_EXCEEDED` + positive `retry_after_ms`.

**Note:** Story 11.4 does NOT add Gateway-side rate limiting — it relies on Core. Story 11.5 will add Gateway-side. This is documented in the story Dev Notes and is acceptable scope for this story.

### 7. JWT Validation / User ID Trust Boundary — CLEAN

- `user_id` is ONLY read from `middleware.ContextKeyUserID` — populated by `jwtMiddleware` after signature/audience/expiry verification.
- The Matrix search request body (per spec §11.14) does NOT contain a `user_id` field. Gateway parser deliberately defines no such field. Even if a malicious client adds one, the JSON decoder ignores unknown fields (Go's default `json.Decode` behaviour) — and Gateway never reads it.
- `SearchMessagesRequest.UserId` left empty in the proto request (line 113 comment + AC9 regression test).
- gRPC outgoing metadata `x-user-id` carries the validated identity.

**Verdict:** No path by which user-controlled input reaches Core's `user_id` parameter.

### 8. Open Redirect — N/A

No `Location` header, no `http.Redirect`, no URL parsing of user input. **N/A.**

### 9. Path Traversal — N/A

No filesystem operations. No `os.Open`, no `filepath.Join` with user input. **N/A.**

### 10. Security Headers — N/A (middleware responsibility)

Confirmed by inspection: security headers (X-Frame-Options, CSP, etc.) are applied by the global middleware chain in main.go's `Server` setup, not by individual handlers. Out of this story's scope.

### 11. SearchMessagesRequest.UserId NOT populated from client input — CLEAN

Triple-verified:
1. `search.go:113` — explicit `// SECURITY: UserId intentionally NOT set` comment.
2. The struct literal `&pb.SearchMessagesRequest{...}` omits `UserId` (Go's zero-value for string = `""`).
3. `TestPostSearch_UserIDFromContext_NotFromBody` asserts `mock.capturedReq.UserId == ""` and would FAIL if a future regression set it.

---

## Additional Vectors Examined (Beyond the Mandatory Checklist)

### A. DoS via Pagination Token (`next_batch`)

Gateway forwards `next_batch` from URL query parameter (`search.go:108`) to Core as a string. Core (`server.ex:2632-2640`) decodes base64 with safe fallback to offset 0, then clamps the parsed integer to `min(n, 10_000)`. Invalid tokens → offset 0, not error. No way for client to bypass the 10,000 offset ceiling. **CLEAN.**

### B. DoS via Oversized `limit`

- JSON `limit` deserialised as `int32`. Negative or zero → Gateway defaults to 10 (`search.go:97-100`).
- Positive values forwarded as-is.
- Core (`server.ex:2627-2628`) clamps via `min(max(raw_limit, 1), 100)`.

Worst case: client sends `limit: 2147483647`. Gateway passes int32 max. Core clamps to 100. Defense-in-depth is in place at Core (which is the right layer — the SQL is where the resource is consumed). **CLEAN.**

**Minor hardening suggestion (non-blocking):** Gateway could also clamp to `[1, 100]` to fail closer to the source. Not required since Core enforces and the gRPC int32 transit cost is trivial.

### C. JSON Decode DoS

`json.NewDecoder(r.Body).Decode(&req)` with `bodyLimit1MiB` upstream. Go's stdlib JSON parser is iterative for arrays/objects (no stack overflow on flat 1 MiB input). Pathological nesting depth is bounded by the 1 MiB cap (each opening `{` or `[` consumes one byte; max nesting ~500K levels would require >1 MiB). **CLEAN.**

### D. Information Disclosure via Error Messages

- All client-facing errors use `writeMatrixError` with sanitised messages ("search failed", "not a member of any searched room", "search_categories.room_events.search_term must not be empty"). No DB error details, no gRPC status messages leaked.
- Server-side logging: missing in the gRPC error fallback (see LOW-1).

### E. Sender Filter Abuse (User Enumeration)

`sender_filter` is forwarded as-is to Core. Core uses it as `e.sender = ANY($6)` (in `@sql_search_messages_with_sender_filter`, not shown above but verified in db.ex). However, the membership filter (`e.room_id IN (... room_members WHERE user_id = $1 ...)`) is applied FIRST in WHERE conjunction — so a user can only filter by senders within rooms they're already a member of. Cannot use sender_filter to probe whether `@victim:server` exists or is active. **CLEAN.**

### F. Event Content Leakage in Highlights

`extractHighlights(term)` returns words from the user's own search term lowercased. This is the user echoing their own input back to themselves. No cross-user leakage. **CLEAN.**

### G. Event Content Leakage in Results

`buildResults` unmarshals `r.Event` JSON from Core. Core only returns events from rooms where the user is an active member (SQL enforced) AND the room is not encrypted. Gateway passes events through verbatim. **CLEAN.**

### H. JSON Field Confusion / Type Confusion

`searchRequest` struct uses strongly typed fields. JSON `null` for the embedded structs is handled by `if req.SearchCategories == nil` / `RoomEvents == nil` checks. Type-mismatched JSON (e.g. `"limit": "not a number"`) yields a decode error → caught by the `err != nil` branch, returns `M_NOT_JSON`. **CLEAN.**

### I. Highlights Field Leaking PII

`extractHighlights` lowercases. If user searches for `"Bob@example.com"`, highlights include `["bob@example.com"]`. This is the user's own input echoed back — no leak. **CLEAN.**

### J. Inconsistent Empty Filter Handling

`if f := req.SearchCategories.RoomEvents.Filter; f != nil` — when `filter` is absent, both `roomFilter` and `senderFilter` remain nil. Core treats `nil`/empty list as "no filter" (full member-scoped search). When `filter: {}` (empty object), still nil slices. Consistent. **CLEAN.**

---

## Areas Reviewed and Found CLEAN

| Area | Verdict |
|---|---|
| SQL injection via search_term, room_filter, sender_filter, limit, next_batch | CLEAN (Core parameterised) |
| IDOR / cross-room data access | CLEAN (metadata-only user_id; SQL membership filter) |
| Auth bypass | CLEAN (jwtWithStatusCheck + defence-in-depth handler check) |
| Body size DoS | CLEAN (bodyLimit1MiB) |
| Pagination DoS | CLEAN (Core offset clamp at 10_000) |
| Limit DoS | CLEAN (Core clamp 1..100) |
| Rate limit mapping (ResourceExhausted → 429) | CLEAN |
| Permission denied mapping (PermissionDenied → 403) | CLEAN |
| Generic error mapping (default → 500 + sanitised message) | CLEAN |
| JSON decode robustness | CLEAN |
| Trust boundary: SearchMessagesRequest.UserId unset | CLEAN (verified by test) |
| Open redirect, path traversal, timing leaks, weak crypto | N/A (no such surface in handler) |
| Sender filter user enumeration | CLEAN (membership filter applies first) |
| Highlights field PII leakage | CLEAN (user's own input) |
| Encrypted room content leakage | CLEAN (Core excludes via `NOT EXISTS m.room.encryption`) |
| Pagination next_batch tampering | CLEAN (base64-decoded int, clamped) |
| Test coverage of security invariants (AC9 trust-boundary) | CLEAN (`TestPostSearch_UserIDFromContext_NotFromBody`) |

---

## Summary

| Severity | Count | Action |
|---|---|---|
| CRITICAL | 0 | — |
| HIGH | 0 | — |
| MEDIUM | 0 | — |
| LOW | 2 | LOW-1 logging gap, LOW-2 Content-Type validation — non-blocking; recommended as a follow-up minor commit or absorbed into a hardening PR |

**Verdict:** **APPROVED for commit.** No CRITICAL/HIGH findings → does not block.

This story executes the security contract laid down by the Story 11.2 (DB-layer) and Story 11.3 (gRPC-layer) Kassandra reviews exactly as specified:

1. `user_id` flows only through validated gRPC metadata (`x-user-id`).
2. `SearchMessagesRequest.UserId` is never set by Gateway.
3. The trust boundary is documented in code (`// SECURITY: ...` comment).
4. The trust boundary is regression-tested (`TestPostSearch_UserIDFromContext_NotFromBody`).

The Kassandra MEMORY.md pattern "DB-module user_id trust-boundary docstring" (Epic 11) which warned about losing the invariant at gRPC hand-off → Gateway-handler boundary is preserved. The team learned the lesson.

---

## Recommendations Carried Forward

1. **LOW-1**: Add server-side `slog.Error` in the default gRPC error arm to aid incident response. Two lines. Suggest folding into Story 11.5 (rate-limiting) which will touch the same error path.

2. **LOW-2**: Add `requireJSON(w, r)` check to enforce HTTP 415 on non-JSON Content-Type, matching the pattern of `login.go`, `account_data.go`, `tags.go`. Spec-conformance, not security. Suggest folding into Story 11.6 (Godog scenarios) which will exercise Content-Type handling.

3. **Future hardening (non-blocking):** Consider Gateway-side `limit` clamp to `[1, 100]` for defence-in-depth. Today only Core clamps. Not exploitable because Core enforces, but a Gateway-side ceiling would prevent any future Core regression from being client-controllable.

---

## Memory Updates

The Kassandra MEMORY.md pattern catalogued during Epic 11 work:

> **DB-module user_id trust-boundary docstring** (Pattern, Epic 11): New DB modules taking `user_id` for authorization scoping must document loudly that it MUST come from the validated session, not from request payload. The hand-off to gRPC handler stories is the natural spot to lose that invariant — see Story 11.3.

Story 11.4 demonstrates the pattern was honoured correctly across the Gateway → gRPC → Core boundary. No update to MEMORY.md required — this is a confirmation, not a new finding.

---

## Methodology Notes

- Read every line of `search.go`, `search_test.go`, the relevant slice of `client.go` (lines 354-359), and the relevant slice of `main.go` (lines 720-734).
- Cross-referenced Elixir Core handler `server.ex:2603-2716` and SQL contract `db.ex:1-100` to verify the trust boundary holds end-to-end, not just at the Gateway layer.
- Verified middleware composition (`bodyLimit1MiB`, `jwtWithStatusCheck`) matches the pattern of other JSON POST endpoints (event_context, account_data, tags, devices, public_rooms).
- Verified `WithUserMetadata` helper sets `x-user-id` outgoing metadata and that the regression test `TestPostSearch_UserIDFromContext_NotFromBody` exercises this path with `metadata.FromOutgoingContext`.
- Did not re-review Stories 11.2/11.3 — those have their own security review reports (2026-05-08) and the SQL contract has not changed.

Kassandra signs off. The team built this story carefully — recognising it was the natural place for the trust boundary to slip, and not letting it. That is the work.
