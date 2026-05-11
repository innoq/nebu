# Epic 11 Security Review — SEC Gate 2
**Date:** 2026-05-11
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Scope:** git diff 2ba7db5..HEAD (Stories 11-1 through 11-6)
**Blocking severity threshold:** CRITICAL (per .claude/security-agent.yaml)

## Summary

**Classification: CLEAN**

0 CRITICAL, 0 HIGH, 0 MEDIUM, 2 LOW, 3 INFO

Epic 11 (Full-Text Search) is **APPROVED** for epic-end commit. The implementation enforces membership scoping at the SQL layer, sources `user_id` exclusively from JWT-derived gRPC metadata, parameterises all search-term input, and adds per-user rate limiting. All four per-story SEC Gate 1 reviews were APPROVED, and the cross-cutting epic view confirms no new attack surface beyond what those reviews already validated. The two LOW findings are defensive-hardening suggestions, not vulnerabilities; the three INFO items are observations for follow-up tracking.

## Findings

### FINDING-1: LRU eviction permits per-user rate-limit reset under churn (memory-pressure DOS variant)
**Severity:** LOW
**File:** gateway/internal/middleware/ratelimit.go:221 (cache capacity = `lruCapacity` = 10_000)
**Description:** `NewUserRateLimiter` uses a 10 000-entry LRU cache keyed on `user_id`. Once 10 001 distinct users have called `/_matrix/client/v3/search` within the limiter's lifetime, the least-recently-used token bucket is evicted; the evicted user's next request creates a *fresh* limiter with a full burst of 10. In a deployment with > 10 000 active searching users, a sufficiently patient attacker can churn the LRU (with many distinct attacker-controlled accounts, or by simply waiting through a busy period) to reset their own bucket and effectively bypass the 10 req/min ceiling.
**Attack scenario:** Attacker controls 10 000+ user accounts (or rides on a busy production instance with high user churn). They send 10 search requests rapidly, get burst-limited, wait until enough other users push their entry out of the LRU, then send 10 more — repeating to amplify their effective rate beyond 10 req/min. The amplification factor is bounded by how fast other users can push them out of the LRU; on a 10 000-user deployment with `lruCapacity=10_000` this requires effort, but the bypass is not impossible.
**Fix:** Two options. (1) Keep current behaviour but document the assumption that `lruCapacity` is sized > active-user count; emit a Prometheus gauge `nebu_user_ratelimit_lru_evictions_total` so ops can alarm when the cache becomes the bottleneck. (2) Stronger: persist the limiter state in Postgres for users observed in the last 5 minutes (durable bucket) and use the LRU only as a hot-path cache. **Recommendation:** Option 1 for MVP, escalate to Option 2 only if telemetry shows non-trivial eviction churn. This is identical to the per-IP `NewIPRateLimiter` design (Story 5.21) which has the same property and was already accepted.

### FINDING-2: Pagination offset capped at 10 000 but no compound limit on (offset + limit)
**Severity:** LOW
**File:** core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2636 (`{n, ""} when n >= 0 -> min(n, 10_000)`)
**Description:** The `next_batch` pagination token is base64-encoded and clamped to a maximum offset of 10 000. Combined with `limit ≤ 100`, the maximum effective range is `LIMIT 100 OFFSET 10000` per call. With PostgreSQL, `OFFSET 10000` still scans+discards 10 000 rows for every request, which is wasteful and slightly amplifies CPU load (~10× compared to a small-offset query). The per-user rate limit of 10 req/min caps amplification to ~6 000 wasted-row-scans/sec per user — acceptable for MVP, but worth tracking as the dataset grows.
**Attack scenario:** Authenticated attacker uses their 10 req/min budget exclusively on `offset=10000, limit=100` queries to maximise per-request DB cost. Damage is bounded by the rate limiter and by the existing GIN index efficiency, so this is a low-severity load-amplification concern, not a usable DOS.
**Fix:** Track p95 search-query duration in Prometheus (epic-end observation, not a code change). If queries with high offsets become a hotspot, switch to keyset (`origin_server_ts < $cursor`) pagination — already deferred to a follow-up per Story 11.3 review.

### FINDING-3: search_term not sanitised against ts_query special characters at API boundary (defense-in-depth, not a vulnerability)
**Severity:** INFO
**File:** gateway/internal/matrix/search.go:91, core/apps/event_dispatcher/lib/nebu/search/db.ex:24
**Description:** The search term flows from HTTP body → JSON-decoded into `searchRequest.SearchTerm` → trimmed → gRPC field → `websearch_to_tsquery('pg_catalog.simple', $2)`. The `$2` is bound as a SQL parameter so there is no SQL-injection risk. `websearch_to_tsquery` ALSO swallows malformed input gracefully (it returns an empty tsquery, no exception). Net result: the term is safe.
**Why INFO and not LOW:** there is genuinely no attack here. I am noting it only because a future maintainer could mistakenly read `websearch_to_tsquery($2)` as string interpolation. The current code is correct (positional parameter binding via Ecto). Keep the inline comment in `db.ex:24` so the parameterised-binding intent is visible.
**Fix:** None required.

### FINDING-4: User-controlled `room_filter` and `sender_filter` allow size-unbounded array input
**Severity:** INFO
**File:** gateway/internal/matrix/search.go:54 (`Rooms []string`, `Senders []string`)
**Description:** The Matrix `filter.rooms` and `filter.senders` arrays have no length cap at the Go boundary. `bodyLimit1MiB` (1 MiB total body) implicitly bounds this — a 1 MiB JSON body can carry roughly 30 000 short room IDs. The `room_filter` is then injected into a Postgres SQL `= ANY($5)` which executes server-side; PostgreSQL handles arrays of this size efficiently, so no DOS. `sender_filter` is currently not enforced at the DB layer (per Story 11.3 review), so it has no security impact.
**Why INFO:** The 1 MiB body limit is a hard ceiling; the gRPC server has a default max-message size (4 MiB); Postgres copes. No actionable vulnerability. Add an explicit `len(roomFilter) <= 100` check if this matters for product correctness, but not for security.
**Fix:** None required for security. Optional: cap `len(roomFilter)` and `len(senderFilter)` at the Go boundary so the gRPC payload stays small — purely a UX/cost concern.

### FINDING-5: Encrypted-room exclusion uses NOT EXISTS subquery on every search query
**Severity:** INFO
**File:** core/apps/event_dispatcher/lib/nebu/search/db.ex:30-35
**Description:** The SQL `NOT EXISTS (SELECT 1 FROM events enc WHERE enc.room_id = e.room_id AND enc.event_type = 'm.room.encryption' AND (enc.state_key = '' OR enc.state_key IS NULL))` scans `events` for every result candidate. The intent is correct: never search content from rooms that have ever been encrypted. The query plan likely benefits from the existing `(room_id, event_type)` indexes; if no such index exists, every search runs an extra sequential scan over a portion of `events`. This is a performance observation only — the security property (exclude encrypted rooms) is correctly enforced at SQL layer.
**Why INFO:** No security bug. Mentioned because a missing index would degrade search latency under load, which could indirectly amplify a DOS surface; this is monitoring/perf hygiene, not vulnerability.
**Fix:** Confirm `EXPLAIN ANALYZE` of the search query in CI uses an index lookup for the encryption subquery; add a partial index `(room_id) WHERE event_type = 'm.room.encryption'` if not.

## Previously reviewed (per-story SEC Gate 1)

| Story | Per-story verdict | Findings carried forward |
|---|---|---|
| 11-2 (Nebu.Search.DB membership-scoped SQL) | APPROVED — 0 CRITICAL/HIGH; 1 MEDIUM hardening accepted | SQL parameterisation verified at SQL-string layer; membership subquery is unbypassable from caller side. No follow-ups outstanding. |
| 11-3 (SearchMessages gRPC handler) | APPROVED — 0 CRITICAL/HIGH; 1 MEDIUM hardening accepted | `user_id` from metadata only (Finding #1 from per-story review fixed); limit clamping 1–100 (Finding #4 fixed); offset clamping ≤10 000 (Finding #2 mitigated); DB error sanitisation (Finding LOW-3 fixed: see server.ex:2710). All implemented. |
| 11-4 (Go Gateway POST /search) | CLEAN | Trust boundary verified: `req.UserId` is explicitly NOT set on the gRPC request (search.go:113 comment); `WithUserMetadata` populates `x-user-id` from JWT context. No bypass path. |
| 11-5 (NewUserRateLimiter) | CLEAN | Fail-closed key derivation: `key, _ := ctx.Value(ContextKeyUserID).(string); if key == "" { key = extractClientIP(r, false) }` — defence-in-depth IP fallback when JWT context is unexpectedly absent. Middleware-order comment makes the JWT-first chain explicit. |

No previously raised CRITICAL/HIGH findings remain open. All MEDIUMs were resolved in-story; LOWs were accepted as risk or fixed.

## Epic-level cross-cutting analysis

I looked at the full picture of the search code path and considered seven cross-cutting attack vectors that per-story reviews can miss. None resulted in a CRITICAL/HIGH finding.

**1. End-to-end user_id provenance (cross-process trust boundary).**
JWT middleware (`middleware/auth.go:243`) → `context.WithValue(ctx, ContextKeyUserID, userID)` → handler reads it (`search.go:69`) → `coregrpc.WithUserMetadata(ctx, userID, systemRole)` builds outgoing gRPC metadata (`grpc/metadata.go:20`) → Elixir reads it via `stream.http_request_headers` (`Nebu.Grpc.Metadata.user_id/1`) → handler refuses if absent (`server.ex:2617`) → passed to `Nebu.Search.DB.search_messages/5` → bound as SQL `$1`. At no point does the request body's `user_id` field reach the SQL parameters. The proto comment (`core.proto:687-688`) and inline `SECURITY:` comments (`search.go:113`, `server.ex:2614`) make this auditable for future maintainers. **CLEAN.**

**2. Membership scoping under encryption.**
`Nebu.Search.DB` enforces BOTH `room_id IN (SELECT … WHERE user_id = $1 AND left_at IS NULL)` AND `NOT EXISTS (… m.room.encryption …)`. The `room_filter` (when set) AND-intersects with membership — it never replaces it. Cross-room IDOR via `room_filter` is impossible at the SQL level. **CLEAN.**

**3. Rate-limit + auth ordering.**
Chain (`main.go:737`): `bodyLimit1MiB → jwtWithStatusCheck → searchRL → PostSearch`. Body-size limit runs first (rejects 1 MiB+ payloads before parsing), then JWT (rejects anon traffic, populates user_id), then rate-limit (keyed on user_id, with IP fallback if JWT context is unexpectedly empty). Order is correct: an unauthenticated attacker cannot exhaust the per-user buckets (no user_id available to key against), and a misconfigured JWT layer would degrade to IP-keyed limiting rather than failing open. **CLEAN.**

**4. Pagination token integrity.**
`next_batch` is base64-encoded plain integer offset, parsed with `Base.decode64/1 + Integer.parse/1`, clamped to `[0, 10_000]`. The token is not signed/HMACed — anyone can forge any offset. **This is acceptable** because (a) the offset is just a SQL `OFFSET` value, (b) membership scoping is enforced regardless of offset, (c) the offset is clamped, so forging cannot escalate. A forged token cannot leak data from rooms the user is not in. **CLEAN.**

**5. Information disclosure via error paths.**
DB errors → `Logger.error("search_messages failed", user_id: user_id, error: inspect(reason))` server-side; client sees only `"search failed"` (`server.ex:2710-2714`). gRPC `Internal` → Go maps to generic `500 M_UNKNOWN` (`search.go:136`). No schema names, no internal table names, no query fragments cross the trust boundary. **CLEAN.**

**6. Logging of sensitive content.**
The Go handler does not log `search_term` (only error paths via `writeMatrixError` with generic strings). The Elixir handler logs `user_id` on DB error but NOT the `search_term` itself (`server.ex:2710`). Since search terms can contain user-typed sensitive content (e.g., a colleague's name, project codename, contract number), this is exactly the right call. **CLEAN.**

**7. CSRF on `POST /_matrix/client/v3/search`.**
The endpoint is bearer-JWT authenticated via `Authorization: Bearer …`, not cookies. CORS preflight will block a cross-origin browser from setting an `Authorization` header without an explicit allow, and the existing CORS middleware does not permit credentials cross-origin for Matrix routes. CSRF middleware (admin-UI only, double-submit cookie pattern) is intentionally not applied to Matrix routes — correct. **CLEAN.**

**8. Pickup of stored content via FTS index over historical events.**
The migration backfills `search_vector` for ALL existing events (`000042_search_vector.up.sql:42-44`). This means: messages sent BEFORE the migration are now full-text searchable. If your threat model includes "users assumed historical messages were not indexed", this is a behaviour change. The membership-scope SQL still blocks cross-user disclosure, so it is not a vulnerability — but it is worth a one-line entry in the release notes so the operator is aware. **CLEAN, with documentation suggestion.**

**9. trigger function search_path.**
The `events_search_vector_update()` PL/pgSQL function sets `SET search_path = pg_catalog, public` (`000042_search_vector.up.sql:16`). This prevents a search_path-injection attack where a malicious schema with the same name as a pg_catalog function could shadow `to_tsvector`. The `pg_catalog.simple` qualification adds a second layer. Trigger function is locked-down correctly. **CLEAN.**

## Verdict

**CLEAN — Epic 11 is APPROVED for epic-end commit. No CRITICAL or HIGH findings. Two LOW and three INFO items are non-blocking.**

The four per-story SEC Gate 1 reviews were APPROVED individually, and the epic-level cross-cutting view confirms no surface area was missed at the boundaries between stories. The end-to-end `user_id` trust chain is tight (JWT → ctx → metadata → SQL parameter), membership scoping is unbypassable at the SQL layer, encrypted rooms are excluded, rate limiting is correctly placed in the middleware chain, body size is bounded, and DB errors are sanitised.

**Recommended follow-up tracking (not blocking):**
1. Add Prometheus gauge `nebu_user_ratelimit_lru_evictions_total` to monitor LRU pressure on `NewUserRateLimiter` (Finding-1, LOW).
2. Add release note that historical messages are now full-text searchable (cross-cutting observation #8, INFO).
3. Plan keyset pagination migration once dataset grows past ~100K message events (Finding-2, LOW + Story 11.3 deferred item).

Epic-end retrospective may proceed.
