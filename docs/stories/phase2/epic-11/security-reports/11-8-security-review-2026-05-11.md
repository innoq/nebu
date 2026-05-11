# Security Review — Story 11-8 (GET /rooms/{roomId}/event/{eventId} + GetRelations RPC stub)

**Diff scope:** staged diff for Story 11-8 — adds GET /_matrix/client/v3/rooms/{roomId}/event/{eventId} (gateway route + handler + gRPC RPC), fixes 500 on /_matrix/client/v1/rooms/{roomId}/relations/{eventId} by adding `rpc :GetRelations` to the Elixir gRPC service stub.
**Date:** 2026-05-11
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Methodology:** Adversarial review against the security scope in `references/security-review.md`; cross-checked against MEMORY.md recurring patterns (RLS / device-id threading / nullable state_key / user_id trust-boundary).

---

## Files in scope

| File | Change |
|------|--------|
| `gateway/internal/matrix/event.go` | NEW — HTTP handler for GET /rooms/{roomId}/event/{eventId} |
| `gateway/cmd/gateway/main.go` | NEW route registration behind `jwtWithStatusCheck` |
| `gateway/internal/grpc/client.go` | NEW gateway-side `GetEvent` gRPC client wrapper |
| `proto/core.proto` | NEW `rpc GetEvent` + `GetEventRequest` / `GetEventResponse` messages |
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | NEW `get_event/2` gRPC handler |
| `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` | Added `rpc :GetRelations` + `rpc :GetEvent` to service stub |
| `core/apps/event_dispatcher/lib/pb/core.pb.ex` | New `GetEventRequest` / `GetEventResponse` modules |
| `core/apps/room_manager/lib/nebu/room/db.ex` | NEW `fetch_event/2` (parameterized SQL) |
| `core/apps/room_manager/lib/nebu/room/db_behaviour.ex` | NEW `fetch_event` callback |

Test files added but excluded from security analysis (no production code path):
`gateway/internal/matrix/event_test.go`, `gateway/features/get_room_event.feature`, `gateway/features/thread_relations.feature`, `gateway/test/integration/get_room_event_steps_test.go`, `gateway/test/integration/thread_relations_steps_test.go`, `core/apps/event_dispatcher/test/event_dispatcher/get_event_test.exs`.

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | LOW | `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2936-2938` | Internal `fetch_event` failure reason interpolated into gRPC error message via `inspect(reason)` — may leak DB internals via gRPC trailer / Elixir logs | Drop `inspect(reason)` from the user-facing message; log it separately via `Logger.error/1`. The gateway already maps `codes.Unknown` to `M_UNKNOWN "Internal server error"` so HTTP clients are not exposed today, but the gRPC layer between gateway and core still carries the verbose message in cleartext. |
| 2 | LOW | `gateway/cmd/gateway/main.go:746-747` | No per-user rate limit on the new endpoint — only `jwtWithStatusCheck`. An authenticated client can flood `GET /rooms/{roomId}/event/{eventId}` with arbitrary IDs, causing repeated `fetch_event` DB lookups against a parameterized index. | Acceptable for parity with the rest of the read-side Matrix API (sync, messages, context all run only under `jwtWithStatusCheck` — see `references/security-review.md` baseline). If brute-force probing becomes a concern, attach a per-user `NewUserRateLimiter` (as Story 11-5 did for `/search`). Tracked as advisory. |

No CRITICAL, HIGH, or MEDIUM findings.

---

## Detail

### Finding #1 — Verbose internal error reason leaks via gRPC message [LOW]

`server.ex:2935-2938`:
```elixir
{:error, reason} ->
  raise GRPC.RPCError,
    status: GRPC.Status.internal(),
    message: "fetch_event failed: #{inspect(reason)}"
```

`inspect(reason)` on a `%Postgrex.Error{}` (or any unexpected term) emits the full struct including PostgreSQL error code, table name, constraint name, and query text. This message rides on the gRPC trailer back to the gateway. The gateway (`event.go:84-85`) does log it via `slog.Error(..., "msg", st.Message(), ...)` but converts it to `M_UNKNOWN "Internal server error"` before responding to the HTTP client — so the customer-facing surface is fine.

Residual exposure:
- Gateway logs contain the full Postgrex struct (could include schema info).
- Anyone with access to gRPC traffic between gateway and core (e.g. a sidecar log shipper) sees it in plaintext.

Mechanism: standard verbose-error pattern. Same pattern already exists in `fetch_events_by_relation` (`server.ex:2893-2896`) — it predates this story but the new code copies it, so flagging here propagates the fix to both.

Remediation:
```elixir
{:error, reason} ->
  require Logger
  Logger.error("fetch_event failed", reason: reason, room_id: room_id, event_id: event_id)
  raise GRPC.RPCError,
    status: GRPC.Status.internal(),
    message: "fetch_event failed"
```

This keeps the diagnosis in the structured server log (where it belongs) and removes it from the wire.

### Finding #2 — Read endpoint not rate-limited per user [LOW / Advisory]

`gateway/cmd/gateway/main.go:746-747` registers the endpoint behind `jwtWithStatusCheck` only. No `searchRL` / `NewUserRateLimiter`-style bucket.

Consistent with `/_matrix/client/v3/sync`, `/.../messages`, `/.../context/{eventId}`, `/.../relations/...` — none of those have per-user rate limits either. The codebase reserves per-user rate limiting for hot or expensive endpoints (`/search`).

A determined member of one large room could enumerate event IDs (provided they already know valid IDs in that room — random guessing is infeasible against the `$<base64url>{1,64}` ID space). The realistic abuse is "spam the same valid event ID" which causes one indexed PK lookup per call.

No remediation required for this story. Flagged as advisory so the team can decide whether a global "Matrix read" rate-limit bucket should exist before Phase 2 traffic ramps.

---

## What was deliberately verified and found CLEAN

These checks were executed and explicitly cleared — they do not appear in findings only because nothing was found, not because they were skipped:

**SQL injection — clean.** `fetch_event/2` (`db.ex:319-330`) uses `Ecto.Adapters.SQL.query/3` with positional params `$1`/`$2` and a hard-coded SELECT (`@sql_fetch_event` module attribute). No string interpolation of user input into the SQL statement. The `LIMIT 1` is a literal. No dynamic ORDER BY, no concatenation.

**Auth bypass — clean.** The new route in `main.go:746-747` is registered exclusively under `jwtWithStatusCheck` (which wraps `JWTMiddleware` + DB status check). The handler refuses if `userID == ""`. The Elixir gRPC handler explicitly sources identity from `Nebu.Grpc.Metadata.trusted_identity(stream)` (i.e. the validated `x-user-id` header set by the gateway via `WithUserMetadata`), NOT from `request.user_id`. The proto field is even commented `// user_id is documentation/auditing only — the handler MUST use x-user-id from gRPC metadata`. This is the exact pattern that closed the auth-bypass finding in Story 11.3.

**IDOR / cross-room probing — clean.** The handler enforces membership via `MapSet.member?(state.members, user_id)` *before* calling `fetch_event`. `fetch_event` itself scopes by `WHERE event_id = $1 AND room_id = $2` — a member of room A who guesses an event ID from room B gets `not_found` because the room_id condition fails. No cross-room oracle. Order of checks (room exists → user is member → event exists in that room) is correct fail-closed.

**Nil user_id fail-closed — clean.** If the gateway forgets to attach `x-user-id`, `trusted_identity` returns `nil`. `MapSet.member?(MapSet.new([...]), nil)` is `false` → permission_denied. The gateway also refuses to call gRPC when `userID == ""`, so this is defense-in-depth, not the primary check.

**Input validation — clean.** `ValidateMatrixRoomID` / `ValidateMatrixEventID` (`validate.go:21-55`) cap length at 512 bytes and enforce strict regex (`!`-prefixed sigil + base64url body + optional `:serverName`). Invalid IDs are rejected with `400 M_INVALID_PARAM` before reaching the gRPC layer.

**JWT validation — clean.** No new JWT logic; reuses existing `JWTMiddleware` which already enforces `exp`, audience, and HS/RS algorithm pinning per Story 6-* hardening.

**CSRF — N/A.** GET endpoint, idempotent, JWT bearer in Authorization header (not cookie).

**XSS — N/A.** JSON-only response; `encoding/json` does not interpret HTML.

**Timing attacks — N/A.** No secret/HMAC comparisons in the new code.

**Open redirects — N/A.** No redirect logic.

**Body-size limits — N/A.** GET only.

**Path traversal — N/A.** No filesystem operations.

**Plaintext secrets in logs — clean.** `slog.Error` at `event.go:84` logs `code`, `msg`, `room_id`, `event_id`. No tokens, no JWT, no userID. The `msg` field can carry the Finding #1 details — addressed under Finding #1.

**Weak crypto primitives — N/A.** No new crypto.

**Hardcoded secrets — clean.** None.

**Security headers — N/A.** API endpoint, not HTML; pre-existing global middleware covers it.

**MEMORY.md recurring patterns:**
- *Missing RLS on new tables* — N/A, no schema migration.
- *Device-ID threading gaps* — N/A, no per-device columns.
- *Nullable state_key + equality filter* — N/A, `fetch_event` filters on PK columns only (no state_key predicate).
- *DB-module user_id trust-boundary docstring* — `fetch_event/2` does not take a `user_id` parameter at all; authz is enforced one level up in the gRPC handler. This is the right separation — the DB layer cannot accidentally trust a forged user_id because it never sees one.

**GetRelations RPC stub fix — clean.** The change in `core_grpc.pb.ex` only adds two RPC declarations (`:GetRelations` and `:GetEvent`) to a generated-style file. No logic, no auth bypass risk; it merely makes the existing `get_relations/2` handler reachable. The handler itself was security-reviewed in Story 9-28 / 9-29 and was not modified.

---

## Summary

CRITICAL: 0 — block commit
HIGH: 0 — block commit
MEDIUM: 0
LOW: 2 — advisory

**Verdict: APPROVED**

Story 11-8 introduces a textbook-correct read endpoint:
- Parameterized SQL, no concatenation
- Identity from validated gRPC metadata, never from request body
- Membership check before any data return
- Fail-closed nil-user handling
- Room-scoped WHERE prevents cross-room probing
- Existing JWT middleware reused, no novel auth path
- The GetRelations fix is a one-line RPC registration that unblocks already-reviewed code

Two LOW advisory items (verbose-error message; no per-user rate limit) are inherited patterns from existing endpoints and are not blockers for this story. Finding #1 is worth queuing as a small cleanup story covering both `get_event` and `fetch_events_by_relation` to suppress `inspect(reason)` from over-the-wire error messages — the gateway already neutralises HTTP exposure, but the gRPC trailer remains noisy.

No new entries required in MEMORY.md — no new recurring pattern observed.
