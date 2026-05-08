# Security Review — Story 9-29 (Bug Fix: Matrix Relations API)

**Diff scope:** Staged changes for Story 9-29 — adds the missing base /relations/{eventId} route, the three-segment /relations/{eventId}/{relType}/{eventType} variant, and dir/recurse/event_type/from query params. Touches:

- `gateway/cmd/gateway/main.go` (3 new route registrations, all wrapped with `jwtWithStatusCheck`)
- `gateway/internal/matrix/relations.go` (new query-param parsing + Core call extension)
- `gateway/internal/matrix/relations_test.go` (unit tests)
- `gateway/internal/grpc/pb/core.pb.go` and `core/apps/event_dispatcher/lib/pb/core.pb.ex` (regenerated proto stubs: GetRelationsRequest gets `event_type`, `dir`, `recurse`, `from`; response gets `prev_batch`)
- `proto/core.proto` (proto definition update; doc comments only on existing service block)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (Elixir handler: dir sanitisation, opts map, room-scoped `event_in_room?` check unchanged)
- `core/apps/room_manager/lib/nebu/room/db.ex` and `db_behaviour.ex` (new 5-arity `fetch_events_by_relation` with dynamic WHERE builder)
- Test fakes: arity bumps to keep behaviours green (no production logic)
- Gherkin feature + Godog steps (E2E coverage)

**Date:** 2026-05-08
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Story flag:** `security_review: not-needed` (per pipeline-state.yaml). Review run anyway because the diff touches a Go gateway route handler, an Elixir gRPC RPC implementation, and dynamically-constructed SQL — all areas Kassandra inspects regardless of declared flag.

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| — | — | — | No exploitable vulnerabilities found. | — |

### Detail

**No security issues found.**

The full attack surface introduced by this diff was traced end-to-end:

1. **Auth coverage on every new route.** All three route registrations in `gateway/cmd/gateway/main.go:733–743` are wrapped with `jwtWithStatusCheck`, identical to the pattern used by every other authenticated read endpoint. No bypass possible through the new base or three-segment variants. The handler additionally re-checks `userID == ""` (relations.go:86–89) and returns 401 `M_MISSING_TOKEN` — defence-in-depth holds even if the middleware is ever misconfigured.

2. **SQL injection — clean.** The dynamic WHERE builder in `core/apps/room_manager/lib/nebu/room/db.ex:660–715` was the highest-risk surface in this diff and was inspected line-by-line:
   - All user-controlled values (`rel_type`, `event_type`) flow through positional placeholders (`$#{idx}` where `idx` is a server-controlled integer counter) and are bound via `Ecto.Adapters.SQL.query/3`'s params list — never interpolated.
   - The only string-interpolated SQL fragments are: (a) the WHERE clause names — which are constants in the `case kind do` block, not user data; (b) the `ORDER BY` direction, which is sanitised at db.ex:651–655 via an explicit `case "f" -> "ASC"; _ -> "DESC"` — any value other than the literal "f" produces "DESC", so even a malicious `dir` reaching this layer cannot inject; (c) the `LIMIT $#{limit_idx}` placeholder index, which is a server-counted integer.
   - The `dir` value is also validated at the HTTP layer (relations.go:98–102) — anything outside `{"f","b"}` returns 400 `M_BAD_PARAM` before reaching gRPC, giving two layers of validation.

3. **IDOR / authorization — clean.** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2731–2745` enforces room membership (`MapSet.member?(state.members, user_id)` → 403 `permission_denied`) before any data is returned, and at server.ex:2749–2757 enforces a **room-scoped** `event_in_room?(event_id, room_id)` check — preventing room-members from probing event existence in *other* rooms via the relations endpoint. This is the same guard pattern used by Story 9-28 and is not loosened here.

4. **Limit / DoS — clean.** Limit is clamped at the HTTP layer (relations.go:108–111: `n > 100 → n = 100`), at the gRPC handler (server.ex:2727: `request.limit |> max(1) |> min(100)`), and at the DB layer (db.ex:646: `limit = max(1, limit)`). Triple defence; no path for an attacker to extract more than 100 rows per call. `recurse=true` is parsed but currently ignored in the DB layer (per the documented MVP scope) — there is no recursion amplification.

5. **Information disclosure — clean.** `slog.Error` at relations.go:153 logs only `code`, `err` from the gRPC status — no user-supplied params, tokens, or PII. Error responses to the client are generic Matrix errcodes (`M_FORBIDDEN`, `M_NOT_FOUND`, `M_UNKNOWN`) without leaking internal state.

6. **Open redirect / XSS / CSRF — N/A.** GET endpoint, JSON-only response, no HTML templating, no redirects, no state-changing operations.

7. **Body size limits — N/A.** GET request, no body parsed.

8. **Crypto — N/A.** No new cryptographic primitives introduced.

9. **Pagination tokens — observation, not a finding.** The `from` query param is parsed at relations.go:129 and forwarded to Core, but the Elixir handler does not currently consume `request.from` (server.ex `get_relations`). This means pagination is effectively a no-op at MVP — but it is *not* a security issue: an attacker cannot inject anything via `from` because the value is bound as a positional placeholder when (eventually) it reaches the SQL layer, and the current code path simply ignores it. Suggest tracking as a functional follow-up, not a security one.

10. **Test fakes — clean.** The arity bumps in seven test files (`fetch_events_by_relation/4` → `/5`) are mechanical adjustments to keep the `Nebu.Room.DBBehaviour` callbacks satisfied. No production-code paths exercised through tests, no auth shortcuts, no DB seeding.

### Defence-in-depth check (cross-referenced against MEMORY.md recurring patterns)

- **"Missing RLS on new tables"** (Epic 9 pattern) — N/A: no new tables in this diff.
- **"Device-ID threading gaps"** (Epic 9 pattern) — N/A: no per-device columns touched. `fetch_events_by_relation` reads from `events` (room-scoped, never device-scoped).

---

### Summary

CRITICAL: 0 — no commit block
HIGH: 0 — no commit block
MEDIUM: 0
LOW: 0

**Verdict:** APPROVED

**Classification:** CLEAN
