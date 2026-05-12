# Security Review — Story 9-30 (Bug Fix: GET /relations 500 — Postgrex.JSONB)

**Diff scope:** Staged changes for Story 9-30 — narrow bug fix in
`event_map_to_proto/1` plus regression tests. Touches:

- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (+10 / -7)
  - Adds a single shape-based guard branch in the `cond` of
    `event_map_to_proto/1` to detect `%Postgrex.JSONB{decoded: map}` structs and
    extract `.decoded` before `Jason.encode!/1`. Comment refresh only otherwise.
- `core/apps/event_dispatcher/test/event_dispatcher/thread_relations_test.exs`
  (+103 / -4) — adds `FakePostgrexJSONB` stand-in struct, `FakeRelationsDBWithJSONB`
  injection module, and a regression test covering the JSONB struct path.
  Test-only file, not loaded in production.
- `e2e/features/element/messages/thread_messages.feature` (+71) — Gherkin
  scenarios covering thread indicator, panel open, and `/relations 200`.
- `e2e/step-definitions/element/thread_messages.steps.ts` (+371) — Playwright
  step definitions under `e2e/`. Not part of the production bundle.

**Date:** 2026-05-08
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Story flag:** `security_review: not-needed` (per story context). Review run
anyway because the diff modifies an Elixir gRPC RPC implementation
(`event_map_to_proto/1`) — a path Kassandra inspects regardless of declared
flag whenever Core event-shaping logic changes.

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| — | — | — | No exploitable vulnerabilities found. | — |

### Detail

**No security issues found.**

The full attack surface introduced by this diff was traced end-to-end:

1. **Trust boundary of the new guard branch.** The added branch in
   `server.ex:478` matches `is_struct(raw) and Map.has_key?(raw, :decoded) and
   is_map(raw.decoded) -> raw.decoded`. The `raw` value originates from
   `Map.get(event, "content", %{})`, where `event` is a row map produced by
   `Nebu.Room.DB.fetch_events_by_relation/5` (and sibling helpers) via
   `Postgrex`. There is no user-controlled path that can plant an arbitrary
   struct with a `:decoded` key into `event["content"]` — the value is either:
   (a) a plain map (Postgrex JSONB → object), (b) a binary (Postgrex JSONB →
   string), or (c) a `%Postgrex.JSONB{decoded: …}` struct (Postgrex's wrapper).
   No user-supplied JSON can deserialise into an Elixir struct here because
   Postgrex returns native Elixir terms, not arbitrary `__struct__` keys, and
   the `events.content` JSONB column round-trips through pure JSON (no
   ETF/erlang term decoding). Shape-matching is therefore safe on this trusted
   data path.

2. **No injection vectors introduced.** The change is a purely defensive
   normalisation step. `Jason.encode!/1` is invoked on the same data as before
   — the only difference is that the `%Postgrex.JSONB{...}` wrapper is now
   unwrapped to its `.decoded` map first. No new SQL, no new HTML rendering,
   no new shell calls, no new file paths, no new redirect targets.

3. **No auth or authorization changes.** The /relations endpoint's auth chain
   (Story 9-29 — `jwtWithStatusCheck` middleware → membership check at
   `server.ex:2731` → room-scoped `event_in_room?`) is untouched. The fix sits
   *after* all auth and ownership checks; it cannot be reached by an
   unauthenticated or unauthorized caller.

4. **No new endpoints, routes, or migrations.** Confirmed via diff: no edits
   under `gateway/cmd/gateway/main.go`, no new files under `gateway/migrations/`,
   no new `Ecto.Migration` modules, no new gRPC handler functions, no new
   route registrations.

5. **No crypto, no secrets, no logging changes.** The diff introduces no
   `:crypto` calls, no key material, no comparison operators, and no
   `Logger`/`slog` statements. `Jason.encode!/1` and `Jason.decode/1` were
   already present on this code path before the fix.

6. **DoS / resource exhaustion — clean.** The guard is O(1) per event:
   `is_struct` and `Map.has_key?` are both constant time, and `raw.decoded` is
   a single map dereference. No new loops, no new recursion, no unbounded
   collection traversal. The pre-existing `min(100)` clamp on `request.limit`
   in the gRPC handler still bounds how many events flow through
   `event_map_to_proto/1` per call.

7. **Information disclosure — clean.** `event_map_to_proto/1` returns the same
   shape as before (`%Core.Event{...}` with a JSON-encoded `content` string).
   Nothing new is exposed; previously the call would crash with
   `Protocol.UndefinedError` on JSONB-struct rows, which `GRPC.Server`
   translated to a generic `INTERNAL` status (no leak) — so the *fix* does
   not regress information disclosure either.

8. **Error-path safety.** The `Jason.encode!/1` call still raises on malformed
   maps containing non-JSON-serialisable terms (e.g. a `Reference`, `Pid`, or
   another struct without a Jason encoder). Since Postgrex never returns such
   terms from a JSONB column, this is not exploitable in the data path —
   but Kassandra notes it as a defence-in-depth observation: should a future
   change ever inject non-JSON terms into an event map upstream, the call
   would crash here and bubble up as gRPC `INTERNAL` (HTTP 500). That is the
   correct fail-safe behaviour and was already the case before this fix.

9. **Test code (`thread_relations_test.exs`, `e2e/`) — clean.**
   - The `FakePostgrexJSONB` stand-in struct lives only in the test module
     (`Nebu.EventDispatcher.ThreadRelationsTest.FakePostgrexJSONB`) and is
     never compiled into the production release.
   - The `Application.put_env(:event_dispatcher, :messages_db_module, …)`
     injection pattern is the project's established test-double mechanism
     (already used by Story 9-27, 9-28, 9-29) and is gated to test runs
     by `messages_db_module()` resolving the env at call time only when
     `Mix.env() == :test` paths exercise it.
   - The Playwright step definitions live under `e2e/step-definitions/` —
     no production code is added there. The capture of the `/relations`
     response uses `page.waitForResponse` against the real running stack;
     no cookie forging, no DB seeding, no auth shortcuts.

### Defence-in-depth check (cross-referenced against MEMORY.md recurring patterns)

- **"Missing RLS on new tables"** (Epic 9 pattern) — N/A: no new tables in
  this diff.
- **"Device-ID threading gaps"** (Epic 9 pattern) — N/A: no per-device
  columns touched. The fix sits below the gRPC layer in the proto-shaping
  helper; it does not select rows.

---

### Summary

CRITICAL: 0 — no commit block
HIGH: 0 — no commit block
MEDIUM: 0
LOW: 0

**Verdict:** APPROVED

**Classification:** CLEAN
