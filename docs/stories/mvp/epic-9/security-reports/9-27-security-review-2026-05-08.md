# Security Review: 9-27 Room Upgrade 500 Fix
Date: 2026-05-08
Reviewer: Kassandra
Story: 9-27-room-upgrade-500-fix

## Classification: CLEAN

No CRITICAL or HIGH findings. Two MEDIUM and one LOW finding documented for defense-in-depth follow-up; none block ship.

## Files Reviewed

- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — `upgrade_room/2` (lines ~2400–2595), `emit_state_event/5`, `copy_state_events/3`, `generate_room_id/0`
- `core/apps/event_dispatcher/lib/nebu/admin/db.ex` — `archive_room_atomic/1` (lines 244–287)
- `gateway/internal/matrix/rooms_upgrade.go` — `PostUpgradeRoom` HTTP handler
- `gateway/internal/matrix/validate.go` — `ValidateMatrixRoomID`
- `gateway/cmd/gateway/main.go` — route mounting (line 805–806)
- `gateway/test/integration/upgrade_room_steps_test.go` — Godog steps
- `e2e/step-definitions/element/room.steps.ts` — Playwright/Cucumber steps

## Findings

### MEDIUM-1 — `new_version` is not validated (server-side)

**Where:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2417-2419`

```elixir
new_version  = if request.new_version == "" or is_nil(request.new_version),
                 do: "10",
                 else: request.new_version
```

The gateway only checks that `new_version` is non-empty (`rooms_upgrade.go:86`). Core then accepts any string and:

1. Embeds it verbatim into the immutable `m.room.create` event content under `room_version` (line 2477) — written to PostgreSQL events table forever.
2. Logs it in the audit log JSON (line 2561, 2577).

There is no allow-list (Matrix spec defines versions "1"–"11"), no length cap (other than the 1 MiB body limit), and no character-set check.

**Impact:**

- Storage of arbitrary attacker-controlled strings (up to ~1 MiB) into the append-only event log.
- No SQL injection (parameterized via Ecto), no XSS in current admin UI render path, no JSON injection (Jason encodes safely). So this is *not* directly exploitable.
- However, the spec contract is broken: the gateway has a `M_UNSUPPORTED_ROOM_VERSION` mapping (`rooms_upgrade.go:107-108`) for `codes.InvalidArgument`, but Core never returns InvalidArgument — meaning a client passing `new_version: "999"` or `new_version: "<garbage>"` succeeds with HTTP 200, polluting the event store.

**Fix:** Add an allow-list check in Core before any mutation:

```elixir
@allowed_versions ~w(1 2 3 4 5 6 7 8 9 10 11)
if new_version not in @allowed_versions do
  raise GRPC.RPCError,
    status: GRPC.Status.invalid_argument(),
    message: "unsupported room version"
end
```

Combine with a length cap (e.g., 16 bytes) and a charset check (`^[0-9a-zA-Z._-]{1,16}$`).

---

### MEDIUM-2 — Internal error details leak to gRPC error message

**Where:** Multiple sites in `upgrade_room/2`:

- line 2470 — `"Failed to emit tombstone event: #{inspect(reason)}"`
- line 2488 — `"Failed to emit m.room.create in new room: #{inspect(reason)}"`
- line 2497 — `"Failed to join requester to new room: #{inspect(reason)}"`
- line 2508 — `"Failed to set power levels on new room: #{inspect(reason)}"`
- line 2539 — `"Failed to archive old room after upgrade: #{inspect(reason)}"`
- line 2592 — `"Failed to start new room: #{inspect(reason)}"`

The `inspect(reason)` may include DB-driver structs, connection details, or internal atoms (`{:error, %DBConnection.ConnectionError{...}}` etc.).

**Impact:**

- The HTTP boundary is safe — `gateway/internal/matrix/rooms_upgrade.go:110` collapses all `codes.Internal` errors to `"Internal server error"`. The detailed message does NOT leak to Matrix clients.
- However, the verbose message travels over gRPC to the gateway and lands in gateway logs. If an attacker ever gains read access to gateway logs, internal DB errors are exposed. Defense-in-depth requires sanitizing at the source.

**Fix:** Log `inspect(reason)` server-side via `Logger.error/1` and keep the gRPC `RPCError.message` generic (e.g., `"failed to emit tombstone event"`). This matches the pattern already used in the rescue branch (line 2548–2550, 2581–2585) which writes details to `Logger` separately.

---

### LOW-1 — No per-route rate limit on `/upgrade`

**Where:** `gateway/cmd/gateway/main.go:805-806`

```go
mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/upgrade",
    bodyLimit1MiB(jwtWithStatusCheck(http.HandlerFunc(upgradeRoomHandler.PostUpgradeRoom))))
```

The endpoint is JWT-protected and body-size-limited (1 MiB), but no rate-limit middleware is applied. Compared to other state-changing Matrix endpoints in the same file, this is consistent — most authenticated routes share this pattern. However, `upgrade_room` is uniquely expensive: it creates a new room, copies all state events, sends invites to every old member, terminates a GenServer, and writes an audit row.

**Impact:**

- An authenticated room owner can repeatedly upgrade their room → exhaust DB writes, churn GenServers, balloon the events table. Authenticated DoS by a malicious insider is possible.
- Power level check (line 2442) prevents unauthenticated abuse — the attacker must already own the room.

**Fix (optional, post-MVP):** Wrap with `mediumRL` or a dedicated `expensiveRL` tier (e.g., 5 req/min/user). Consistent with the project's MVP rate-limit posture, this is a documented follow-up rather than a blocker.

---

### INFO-1 — Audit log ordering & rescue robustness (positive observation)

The implementation correctly:

1. Performs power-level check (line 2442) BEFORE any mutation.
2. Wraps the audit-writer call inside a nested `try/rescue` (line 2571–2585) so an audit-writer outage cannot mask the original error.
3. Uses `reraise e, __STACKTRACE__` (line 2586) to preserve the stack trace.
4. Calls `archive_room_atomic` AFTER the tombstone is committed (correct ordering — failure of archive does not leave the old room un-tombstoned).
5. Treats `archive_room_atomic` returning `{:error, :not_found}` as idempotent (line 2533–2535) — correct.
6. `archive_room_atomic` itself uses `SELECT FOR UPDATE` inside an Ecto transaction (`admin/db.ex:259`) — concurrency-safe.

These are good security/robustness patterns and should be preserved.

## Summary

The 9-27 fix replaces unsafe `:ok = ...` matches with proper `case` clauses and adds Matrix-spec-mandated room archival after the tombstone — both improving correctness and security posture. SQL injection, IDOR, auth bypass, path traversal, weak crypto, and timing-attack vectors were all reviewed and found CLEAN. Two defense-in-depth gaps (input validation on `new_version`, error-message sanitization at the gRPC boundary) and one operational concern (no rate limit on an expensive endpoint) are documented as MEDIUM/LOW follow-ups; none block shipping the 500-fix.

## Severity Counts

- CRITICAL: 0
- HIGH: 0
- MEDIUM: 2
- LOW: 1
- INFO: 1
