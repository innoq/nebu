# Security Review — Story 6-8 — Room Settings Update API — 2026-05-01

**Agent:** Kassandra
**Diff base:** `HEAD` (commit `26b4c3f`) + staged fixes (`git diff --staged`)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=claude-opus-4-7[1m]`

## Executive Summary

Story 6-8 introduces `PATCH /api/v1/admin/rooms/{roomId}`, `PUT /api/v1/admin/config/room-defaults`, a new mutable `room_defaults` table (migration 000037), and a Core gRPC RPC `UpdateRoomSettings` that reaches the `Nebu.Room.Server` GenServer. Auth chain is wired correctly (`jwtMW → RequireRole("instance_admin")`), the gRPC RPC sits behind the existing PSK `AuthInterceptor`, the dynamic SQL `UPDATE` builder uses parameter binding throughout, and validation bounds (`max_members ∈ [2, 100000]`, visibility enum, name 1–255 runes, topic ≤1000 runes) are enforced both in OpenAPI and re-checked in the handler. No CRITICAL or HIGH findings. Two MEDIUM defense-in-depth gaps: missing body-size middleware on the new admin endpoints, and an unbounded `default_max_members` upper bound that lets an admin trip a Postgres `INTEGER` overflow with a JSON number > 2^31. Both admin-only, no exploitation path beyond noisy 500s.

## Findings

### [MEDIUM] Missing body-size limit on new admin PATCH/PUT routes

- **CWE / OWASP:** CWE-400 / A04:2021
- **Datei:** `gateway/internal/api/router.go:67–71`, `gateway/cmd/gateway/main.go:1137`
- **Beschreibung:** The two new routes are registered through `apihandler.RegisterAdminRoutes(...)` and wrapped only with `jwtMW + RequireRole`. No `bodyLimit64KiB` (or any `http.MaxBytesReader`) is in the chain. The dev notes acknowledge this as the existing pattern for `/api/v1/admin/*` (Dev Notes line 803), but each new POST/PATCH/PUT extends the unbounded surface.
- **Impact:** An authenticated `instance_admin` (or a stolen admin JWT) can submit arbitrarily large JSON bodies. The strict-handler decoder reads them fully into memory before validation. Bounded by Go's int decoder for individual fields, but `name`/`topic` are strings without per-field limits at decode time; a multi-megabyte topic forces full allocation before the rune-count check rejects it. Admin DoS — narrow.
- **Empfehlung:** Add `bodyLimit64KiB` wrapping inside `RegisterAdminRoutes` (or wire it in `main.go` after the call) for all state-changing admin endpoints. Either consolidate as a follow-up story for the whole admin surface, or apply per-route now: `jwtMW(RequireRole("instance_admin", checker)(bodyLimit64KiB(patchAdminRoomHandler(sh))))`.
- **Referenz:** OWASP ASVS V13.1.3 (input size), Go `net/http.MaxBytesReader`.

### [MEDIUM] Unbounded `default_max_members` triggers Postgres INTEGER overflow

- **CWE / OWASP:** CWE-20 / A04:2021
- **Datei:** `gateway/internal/api/server.go:807–809` (`PutAdminRoomDefaults`), `gateway/api/openapi.yaml` (`PutRoomDefaultsRequest.default_max_members: minimum: 0`, no `maximum`)
- **Beschreibung:** `PutRoomDefaultsRequest.DefaultMaxMembers` is a Go `int`, validated only `< 0`. The DB column `room_defaults.default_max_members` is `INTEGER` (32-bit signed). A request `{"default_max_members": 9999999999, "default_visibility": "private"}` parses cleanly into Go's int64-on-amd64, then fails at the SQL layer with `pq: integer out of range`, surfacing as HTTP 500 with the wrapped error `UpsertRoomDefaults: ...`. The `PATCH /admin/rooms/{roomId}` path is correctly bounded to `[2, 100000]` — only the room-defaults endpoint is affected.
- **Impact:** Admin-only DoS-by-noise; raises 500s and pollutes logs. No data exposure (the wrapped error does not include connection strings or row data — `database/sql` returns the driver message only). No bypass.
- **Empfehlung:** Add `maximum: 100000` to `PutRoomDefaultsRequest.default_max_members` in `openapi.yaml` (mirroring the PATCH path) and re-check in the handler with the same `< 0 || > 100000` rule. Update `api_gen.go` via `make gen-api`.
- **Referenz:** OWASP ASVS V5.1.4 (boundary checks).

### [INFO] Admin override of room state changes bypasses Matrix Power Levels — by design

- **CWE / OWASP:** —
- **Datei:** `gateway/internal/api/server.go:712–796` (`PatchAdminRoom`)
- **Beschreibung:** `PATCH /api/v1/admin/rooms/{roomId}` modifies `name`, `topic`, `visibility`, `max_members` directly via `UPDATE rooms` without consulting the room's Power Level state event. Per Nebu invariants, "room-scoped state-changing operations must check the actor's power level before mutation." This handler is the deliberate `instance_admin` override path — the role gate (`RequireRole("instance_admin")`) is the explicit substitute for in-room PL. Audit log captures every change with the actor.
- **Impact:** None — the design is intentional and the trust boundary is enforced one layer up (system role rather than per-room PL).
- **Empfehlung:** None. Note: the handler updates the `rooms` row but does not emit corresponding `m.room.name` / `m.room.topic` Matrix state events. Matrix clients reading the timeline will see a divergence between admin-set values and the canonical room state. Out of scope for security review — flag for a future story or AC clarification.
- **Referenz:** Nebu invariants §Authentication & Authorization, Matrix Spec §m.room.name / m.room.topic.

### [INFO] gRPC `UpdateRoomSettings` is correctly authenticated

- **CWE / OWASP:** —
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex:6` (`intercept(Nebu.Grpc.AuthInterceptor)`), `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:253` (`update_room_settings/2`)
- **Beschreibung:** The new RPC is attached to the existing `Nebu.EventDispatcher.Server` and inherits the endpoint-level `AuthInterceptor`, which enforces a constant-time comparison of `x-nebu-node-token` against the PSK file (rejects missing or invalid with `unauthenticated`). The handler does not skip or override this. The handler itself does not authorize the caller's role — by Nebu's two-tier model (gateway authorizes user; PSK authorizes node), this is correct.
- **Impact:** None — verified, attack-surface noted for future.
- **Empfehlung:** None.
- **Referenz:** Story 5.29a Block B.

### [INFO] Dynamic SQL builder in `UpdateRoom` is parameterized correctly

- **CWE / OWASP:** —
- **Datei:** `gateway/internal/api/rooms_repo.go:248–296` (`dbRoomRepo.UpdateRoom`)
- **Beschreibung:** The `UPDATE rooms SET %s WHERE room_id = $1` query is built by joining hard-coded `setClauses` (`"max_members = $2"`, `"visibility = $3"`, etc.) with values added to a separate `args []any` slice. No user-controlled string ever enters the SQL text. Column names are static. The pattern is identical to the established Nebu approach.
- **Impact:** None — verified.
- **Empfehlung:** None.

### [INFO] `max_members` enforcement in `Nebu.Room.Server.handle_call({:join, …})`

- **CWE / OWASP:** —
- **Datei:** `core/apps/room_manager/lib/nebu/room/server.ex:238–267`
- **Beschreibung:** The `cond` ordering is `:already_member` → `:room_full` → DB insert. This correctly avoids a join-bypass by re-joining (an existing member is not blocked by capacity), and capacity is checked before the DB write so a full room never holds the row-lock. The `update_settings` cast updates the in-memory state; if the GenServer restarts before the cast lands, `init/1` reloads from DB via `load_room_settings/1` (fail-open: errors default to 0/no limit). Fail-open is the correct policy here — capacity is a soft limit, not a security control.
- **Impact:** None.
- **Empfehlung:** None.

### [INFO] `room_defaults` table has no RLS — appropriate

- **CWE / OWASP:** —
- **Datei:** `gateway/migrations/000037_room_defaults.up.sql`
- **Beschreibung:** The new table holds a single server-wide configuration row, not user-scoped or compliance-scoped data. Per the Nebu RLS rule ("compliance/user-scoped tables only"), no RLS is required. `CHECK` constraints enforce `default_max_members >= 0` and `default_visibility IN ('public', 'private')`. The down migration is a clean `DROP TABLE`.
- **Impact:** None.
- **Empfehlung:** None.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ (no compliance data touched) |
| `reason` field on compliance access         | ✅ (no compliance access) |
| Audit-log immutability                      | ✅ (no migration touches `audit_log`) |
| `instance_admin` notification (if in-scope) | ✅ (not in scope — admin acts) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (`jwtMW` upstream, verified wiring) |
| Matrix Power Level checks                   | ⚠️ (deliberate admin override — see INFO finding) |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ✅ (no TLS config touched) |
| AES-256-GCM correctness                     | ✅ (no crypto touched) |
| Ed25519 verify-before-accept                | ✅ (no signature path touched) |
| No secrets in logs / error messages         | ✅ (`slog.Warn` calls log `room_id`, `err` only) |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 2 |
| LOW       | 0 |
| INFO      | 5 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

The two MEDIUM findings (missing body limit on new admin routes, unbounded `default_max_members`) are defense-in-depth gaps with admin-only impact. Recommend addressing both as a small follow-up — adding `bodyLimit64KiB` inside `RegisterAdminRoutes` covers the same gap retroactively for the entire admin surface, which is the better long-term fix.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
