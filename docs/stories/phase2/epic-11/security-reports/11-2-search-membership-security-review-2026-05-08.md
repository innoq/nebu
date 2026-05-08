# Security Review — Story 11.2 (Search Membership Enforcement)

**Reviewer:** Kassandra (nebu-agent-kassandra)
**Date:** 2026-05-08
**Story:** docs/stories/phase2/epic-11/11-2-search-membership-enforcement.md
**Diff scope:** staged diff (`git diff --staged`)
**Files in scope:**
- `core/apps/event_dispatcher/lib/nebu/search/db.ex` (NEW — `Nebu.Search.DB` module)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/search_membership_test.exs` (NEW — integration + structural tests)
- `core/apps/event_dispatcher/test/test_helper.exs` (MODIFIED — exclude `:integration` tag)
- `Makefile` (MODIFIED — added `test-integration-elixir` target, extended `test-integration`)
- `docs/stories/phase2/epic-11/11-2-search-membership-enforcement.md` (MODIFIED — story status → review)
- `_bmad/nebu/pipeline-state.yaml`, `_bmad-output/implementation-artifacts/sprint-status.yaml` (pipeline metadata, no security implications)

---

## Threat Model for This Story

The headline security goal of Story 11.2 is **cross-room leakage prevention** — a user must NEVER receive search results from rooms they are not an active member of. The story ships only the DB layer (the SQL contract + Elixir wrapper); the gRPC handler and HTTP route arrive in stories 11.3 and 11.4. This review therefore focuses on the SQL contract itself and on the boundary assumptions the DB module makes about its callers.

Dimensions assessed (per CLAUDE.md scope and `references/security-review.md`):

- SQL injection in the search query — parameter binding, term handling
- Cross-room leakage — membership filter correctness
- Encrypted-room bypass — state-key validation
- IDOR — caller-supplied `user_id` parameter trust boundary
- Auth bypass — at the handoff to Story 11.3
- Timing attacks — none in scope (no secret comparisons here)
- Body-size / rate limits — deferred to Stories 11.4 / 11.5
- Crypto — no crypto introduced
- New SQL migrations — none in this story (000042 already shipped in 11.1)
- New tables / RLS — none added

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | MEDIUM | `core/apps/event_dispatcher/lib/nebu/search/db.ex:30-35` | Encryption-detection `NOT EXISTS` subquery filters on `state_key = ''`, which excludes rows with `NULL` state_key. The `events.state_key` column is nullable (migration 000038 does not enforce `NOT NULL`). If any historical or buggy write path stored an `m.room.encryption` event with `state_key IS NULL`, the room is not flagged as encrypted and plaintext message bodies leak through search. Defense-in-depth weakness, not currently exploitable through the gateway path, but the filter is more restrictive than the safety property requires. | Replace `AND enc.state_key = ''` with `AND (enc.state_key = '' OR enc.state_key IS NULL)`, or remove the `state_key` check entirely (the `event_type = 'm.room.encryption'` filter is sufficient — any presence of an encryption state event should bias search toward exclusion). The latter is the safer choice: "fail closed" on any encryption-typed event in the room. |
| 2 | MEDIUM | `core/apps/event_dispatcher/lib/nebu/search/db.ex:53-67` (docstring + spec of `search_messages/4`) | The `user_id` parameter is documented as "the Matrix user ID of the searcher" but the docstring does not warn callers that this value MUST be derived from the authenticated session — never from request body, query string, or any client-controlled field. Story 11.3 wires this to the gRPC handler; if the handler were to read `user_id` from the request payload (a plausible mistake given the proto pattern in this project), the membership check is bypassed and any authenticated user can search any other user's rooms. IDOR risk at the handoff point. | Add an explicit security note to the moduledoc and to `search_messages/4`'s `@doc`: "The `user_id` MUST be the authenticated user's ID, taken from the validated session/JWT — never from a client-controllable field. Treating this parameter as a trust boundary is the entire point of the SQL membership scoping." This makes the contract explicit for the Story 11.3 dev agent and for future maintainers. |
| 3 | LOW | `core/apps/event_dispatcher/lib/nebu/search/db.ex:70-78` | `{:error, reason}` is returned verbatim from `Ecto.Adapters.SQL.query/3`. Postgres errors can include schema details, parameter values, and (rarely) row contents in constraint violation messages. If Story 11.3's gRPC handler propagates this error verbatim to the client, that's an information-disclosure leak. | Story 11.3 must NOT pass `reason` directly to clients. The DB-layer behavior here is acceptable (raw error preserved for logging), but record this expectation now: the gRPC handler in 11.3 must log the full reason server-side and return only a generic `M_UNKNOWN` / `M_FORBIDDEN` shape with a sanitized message. Adding a comment in `search_messages/4` would also help. |
| 4 | LOW | `core/apps/event_dispatcher/lib/nebu/search/db.ex:13-39` | `LIMIT $3` and `OFFSET $4` are typed `pos_integer()` and `non_neg_integer()` in the spec but Elixir specs are documentation only — they are not runtime-enforced. A negative or absurdly large `limit`/`offset` will pass through. Postgres handles negative OFFSET by erroring; very large LIMIT runs a slow scan. Not a SQL injection (parameters are bound), but a DOS vector if the gRPC handler does not clamp inputs. | Story 11.3's caller MUST clamp `limit` to a sane upper bound (Matrix spec suggests 50; many servers cap at 100) and reject negative values. Defensive option here: add `limit = min(max(limit, 1), 100)` and `offset = max(offset, 0)` inside `search_messages/4` so the DB layer is robust regardless of caller hygiene. |

### Detail

**Finding #1 — Encryption-bypass via NULL state_key (MEDIUM)**

```elixir
AND NOT EXISTS (
  SELECT 1 FROM events enc
  WHERE enc.room_id = e.room_id
    AND enc.event_type = 'm.room.encryption'
    AND enc.state_key = ''        -- ← excludes NULL state_keys
)
```

In SQL three-valued logic, `NULL = ''` is `NULL` (treated as false). The `events.state_key` column was added by migration `000038_events_state_key.up.sql` as a nullable `TEXT`. If any code path — historical, buggy, or future — has stored or stores an `m.room.encryption` row with `state_key IS NULL`, the encryption check evaluates "no encryption event found" and search will return that room's plaintext message bodies. The Matrix spec says the canonical encryption state event has `state_key = ""`, but defense-in-depth requires either (a) accepting both `''` and `NULL` as the canonical empty key, or (b) removing the `state_key` discriminator entirely — the event_type alone is uniquely identifying for encryption. Recommendation: remove the `state_key = ''` clause. The query intent is "is there any encryption event in this room?" and the type filter captures that.

**Finding #2 — IDOR risk at the user_id trust boundary (MEDIUM)**

The DB module is a quiet contract: it trusts that `user_id` is the authenticated caller. But the docstring does not say this. Story 11.3 will wire this to a gRPC handler, and the dev agent there must derive `user_id` from the authenticated session — not from request fields. If the gRPC `SearchMessagesRequest` proto (yet to be added) carries a `user_id` field that the handler forwards uncritically, a malicious authenticated user could search any other user's rooms simply by setting that field. This has happened before (cf. recurring "device-id threading gaps" pattern in MEMORY.md). The remediation is cheap (one paragraph in the docstring) and the cost of getting it wrong is the entire purpose of this epic.

**Finding #3 — Raw DB errors propagate to caller (LOW)**

Postgres error messages can disclose: column types, constraint names, query plan details, and occasionally parameter values. Returning `{:error, reason}` to the gRPC layer is fine; what must not happen is returning that reason verbatim to the HTTP client. This is a hand-off note for Story 11.3.

**Finding #4 — No runtime clamping of limit/offset (LOW)**

Elixir specs are documentation. If Story 11.3 forgets to clamp, an authenticated user can issue `LIMIT 1000000000 OFFSET -1` and either DOS the database or trigger errors that leak (see Finding #3). Defensive clamping in this DB module is a one-line guard that survives every future caller.

---

## Areas Reviewed and Found CLEAN

- **SQL injection:** All four parameters (`user_id`, `term`, `limit`, `offset`) are passed through PostgreSQL prepared-statement bind parameters via `Ecto.Adapters.SQL.query/3`. Zero string interpolation of user input into the SQL. The SQL constant is a static module attribute. ✓
- **Search-term parsing:** `websearch_to_tsquery('pg_catalog.simple', $2)` is the right primitive. It is a forgiving parser that does not raise on unbalanced quotes, special characters, or unknown operators — protecting against malformed-input DOS. ADR-010 alignment confirmed. ✓
- **Membership filter correctness (AC1, AC3):** `WHERE user_id = $1 AND left_at IS NULL` is the canonical active-membership predicate used elsewhere in the codebase (`Nebu.Room.DB.@sql_get_rooms_for_user`, `@sql_load_members`). Kicked users (`left_at IS NOT NULL`) are excluded at query time, not by post-filter. ✓
- **SQL-layer enforcement (AC2):** Membership is enforced as a SQL subquery, not by fetching all events and filtering in Elixir. Confirmed by inspection of the SQL constant and by the structural test `Nebu.Search.DBStructuralTest`. ✓
- **State-event search content:** The trigger `events_search_vector_update` (migration 000042) only populates `search_vector` from `content->>'body'`. Non-message events have empty tsvectors and would not match `@@`. The redundant `event_type = 'm.room.message'` filter is layered defense, not the only protection. ✓
- **Timing attacks on secret comparison:** N/A — no secret comparison in this code path.
- **Crypto primitives:** N/A — no crypto introduced.
- **Hardcoded secrets:** None.
- **New SQL migrations:** None in this story.
- **New tables / RLS:** None.
- **Open redirects, XSS, CSRF, security headers:** N/A — no HTTP surface in this story.
- **Body-size limits, rate limits:** Out of scope; deferred to Stories 11.4 / 11.5 per epic plan.
- **Test code:** Test fixtures use parameterized SQL throughout (`$1`, `$2`, ...). The use of `ON CONFLICT DO NOTHING` in fixture inserts is a test-hygiene pattern, not a production security concern. Cleanup via `on_exit` is best-effort but acceptable for integration tests against an ephemeral CI database. ✓
- **Makefile changes:** `test-integration-elixir` and the extended `test-integration` target wire Elixir integration tests into CI. The connection string `postgresql://nebu_app:nebu_app_dev_pw@postgres:5432/nebu` is a development credential already present in `docker-compose.yml` and committed elsewhere; this is not a new secret leak. ✓
- **Test-helper change:** `ExUnit.configure(exclude: [:integration])` correctly gates DB-bound tests behind the integration profile. ✓

---

## Summary

| Severity | Count | Action |
|---|---|---|
| CRITICAL | 0 | — |
| HIGH | 0 | — |
| MEDIUM | 2 | Address before merge (encryption-bypass widening; user_id docstring warning) |
| LOW | 2 | Advisory (sanitize errors in 11.3; clamp limit/offset) |

**Verdict:** APPROVED — no CRITICAL or HIGH findings; commit not blocked.

The two MEDIUM findings are defense-in-depth improvements that should be addressed in this story or carried as explicit follow-ups in Story 11.3. None of the MEDIUMs are currently exploitable through any in-tree code path: Finding #1 requires a write path that stores `m.room.encryption` with a NULL state_key (does not currently exist); Finding #2 is a future-trust-boundary concern that will become exploitable only if the Story 11.3 dev agent wires `user_id` incorrectly. The two LOW findings are hardening recommendations.

The core security property of this story — **SQL-layer membership enforcement against the cross-room-leakage threat** — is correctly implemented. AC1 and AC3 are upheld by the SQL contract. AC2 (no application-layer post-filter) is upheld and verified by a structural test.

---

## Recommendations Carried Forward

1. **Story 11.3 acceptance criterion add-on:** "The gRPC handler MUST derive `user_id` from the validated session, never from the request payload. A unit test must assert this." — addresses Finding #2 at the layer where it actually becomes exploitable.
2. **Story 11.3 acceptance criterion add-on:** "DB errors are logged server-side with full reason; client receives only a sanitized error code." — addresses Finding #3.
3. **Story 11.5 (rate limiting) acceptance criterion add-on:** "Server clamps `limit` to ≤ 100 and `offset` to ≥ 0 before invoking `Nebu.Search.DB.search_messages/4`." — addresses Finding #4.
4. **In this story, optional but recommended:** widen the encryption-detection clause per Finding #1 — the change is one line and removes a class of future bugs.

---

## Memory Updates

This review surfaces two patterns worth recording in `_bmad/memory/nebu-agent-kassandra/MEMORY.md`:

- **Nullable state_key + equality filters:** Any `WHERE state_key = '...'` filter must account for `state_key IS NULL`. The column is nullable and the filter `=` does not match NULL. Pattern to scan for in future state-event queries.
- **DB-module trust-boundary docstrings:** New DB modules that take a `user_id` parameter without enforcing it via session must document the trust expectation prominently. The handoff to handler stories is the natural spot to lose that invariant.
