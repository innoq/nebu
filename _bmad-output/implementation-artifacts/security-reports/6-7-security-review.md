# Security Review — Story 6-7 (Room List + Get API) — 2026-05-01

**Agent:** Kassandra
**Diff base:** `git diff --staged`
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

Story 6-7 adds two read-only admin endpoints (`GET /api/v1/admin/rooms`, `GET /api/v1/admin/rooms/{roomId}`), a backing repository, and a non-destructive ALTER on the `rooms` table. Access control reuses the established `jwtMW(RequireRole("instance_admin", checker))` chain. SQL is parameterised end-to-end; the only injection-class observation is unescaped LIKE metacharacters in the `search` parameter (already accepted as LOW for Story 6-4). Audit logging is wired on both success paths with the never-raise contract. No CRITICAL or HIGH findings. Pipeline may proceed.

## Findings

### [LOW] Search input is not LIKE-escaped (CWE-138, low impact)

- **CWE / OWASP:** CWE-138 (Improper Neutralization of Special Elements) / A03:2021
- **Datei:** `gateway/internal/api/rooms_repo.go:71`
- **Beschreibung:** The `search` parameter is bound through a parameterised placeholder (`$N`), so SQL injection (CWE-89) is not present. However, LIKE metacharacters (`%`, `_`, `\`) in the admin-supplied search string are passed through literally, so a search of `_oom%` matches more rows than the admin intended. Story 5-26 introduced `matrix.EscapeLIKE` exactly for this case in `gateway/internal/matrix/user_directory.go:66`; the Admin API search side has not adopted it. Same observation as Story 6-4 LOW-1 — recorded here for parity, not as a new defect class.
- **Impact:** Functional/UX only. The route is gated by `instance_admin`; the only actor with access already has unrestricted listing privileges (`?search=` is not required to enumerate). No data-leak or privilege-boundary path.
- **Empfehlung:** Either document wildcard-LIKE semantics as intentional admin search, or adopt `EscapeLIKE` for cross-package consistency. Defer the decision; not merge-blocking.
- **Referenz:** OWASP ASVS V5.3.4

### [INFO] New admin attack surface — both routes correctly gated

- **Datei:** `gateway/internal/api/router.go:41-44`
- **Beschreibung:** Two new routes are introduced. Both are wrapped in `jwtMW(RequireRole("instance_admin", checker)(...))`, mirroring the user-list/get pattern from Story 6-4. JWT runs outermost so `ContextKeySystemRole` is populated before `RequireRole` reads it. `RoleOverrideChecker` is wired (`rolesRepo`) so DB role grants from Story 6-6 also satisfy the gate. No silent un-auth path.
- **Action:** None — recorded for the audit trail as a positive finding.

### [INFO] Cursor opacity is not authenticated (signed)

- **Datei:** `gateway/internal/api/pagination.go:24-31` (existing, unchanged)
- **Beschreibung:** The new room-list endpoint reuses the existing `EncodeCursor` / `DecodeCursor` helpers. Cursors are `Base64URL(JSON({"after_id":..., "after_created_at":...}))` — opaque but not MAC-signed. A caller can decode and forge a cursor with arbitrary keyset values. Within the `instance_admin` role boundary this is not exploitable: the admin can already list all rooms unfiltered, so jumping to an arbitrary keyset position discloses no extra data. `parseISO8601ToEpochMs` rejects malformed timestamps cleanly (returns 500 via the strict-handler error path; `DecodeCursor` itself returns 400 for un-decodable cursors via the test-covered path).
- **Action:** None — same posture as Story 6-4 INFO-2. Reconsider only if Admin pagination ever escapes the `instance_admin` boundary.

### [INFO] `power_levels_json` is returned as a raw JSON string

- **Datei:** `gateway/internal/api/rooms_repo.go:189`, `gateway/internal/api/rooms_repo.go:235`
- **Beschreibung:** `r.power_levels_json` is read as TEXT and surfaced in the response body verbatim under the field `power_levels_json` (string). Since the column is owner-trusted (written only by Nebu's own send-state-event flow), and the response is JSON-serialised by `encoding/json` (which will re-quote the entire string), there is no JSON-injection or response-splitting path. Worth recording because the field name suggests it might be parsed JSON — clients should treat it as opaque text or re-parse it themselves.
- **Action:** None — design choice, no security exposure.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ |
| `reason` field on compliance access         | ✅ |
| Audit-log immutability                      | ✅ |
| `instance_admin` notification (if in-scope) | ✅ |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ |
| Matrix Power Level checks                   | ✅ |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ✅ |
| AES-256-GCM correctness                     | ✅ |
| Ed25519 verify-before-accept                | ✅ |
| No secrets in logs / error messages         | ✅ |

Notes:

- **Compliance RSP coverage** — `rooms` is global metadata, not a compliance-scoped table. Migration `000036_rooms_admin_columns.up.sql` adds five columns (`topic`, `creator_user_id`, `max_members`, `status`, `archive_reason`) but does not introduce user-scoped data, does not enable/disable RLS, and does not touch any compliance table.
- **Audit-log immutability** — migration is rooms-only; no `ALTER TABLE audit_log` or grant change. Audit insertion is wired through `audit.LogEvent` (gRPC, never-raise) on both success paths (`server.go:102, 135`).
- **OIDC token validation** — the routes use the same `jwtWithStatusCheck → jwtMiddleware → RequireRole` chain that previous stories validated. No new auth surface, no skipped middleware.
- **Power Level checks** — both endpoints are read-only (GET); no state mutation, so power-level enforcement does not apply. The new `power_levels_json` field is _read_ from the DB, not authored by the API.
- **Secrets in logs** — `slog.Warn("ListAdminRooms audit skipped — CoreClient is nil")` and `slog.Warn("GetAdminRoom audit skipped — CoreClient is nil", "room_id", request.RoomId)`. Room IDs are not secrets; the JWT, session cookie, and PSK are not logged anywhere on this path.

## Migration Review — `000036_rooms_admin_columns`

- Up: adds five columns with defaults, a CHECK constraint on `status`, and two indexes (`rooms_status_idx`, `rooms_created_at_id_idx`). No grant change, no RLS toggle, no audit-table touch.
- Down: drops the constraint, both indexes, and all five columns. Schema-symmetric and reversible. No destructive operation outside the schema introduced by this story.
- `CREATE INDEX rooms_created_at_id_idx ON rooms (created_at DESC, room_id)` — supports the keyset pagination ORDER BY `(created_at DESC, room_id DESC)` at the storage layer; query plan will use the index. No DoS via expensive scan on large tables.
- `GetRoom` joins `events` via `LEFT JOIN events e ON e.room_id = r.room_id` and aggregates `COUNT(e.event_id)`. The `events_room_id_ts_idx` index (migration 000010) covers the `room_id` lookup. For very large rooms this is still an index scan rather than a sequential scan — acceptable for an admin-only endpoint that is not on a hot path.

## Areas Reviewed — Clear

### A. SQL Injection (CWE-89)

`rooms_repo.go` builds the query with `fmt.Sprintf` _only_ to compute `$N` placeholder positions; user input is appended to `args ...any` and bound through pgx parameter binding. Verified for: `search` (line 71), `status` (line 77), cursor `(afterCreatedAtMs, afterID)` (line 92), `limit` (line 121), and `roomID` in `GetRoom` (line 211). No interpolation of attacker-controlled strings into SQL.

### B. IDOR / Authorisation Bypass (CWE-285, CWE-639)

Both routes wrapped in `jwtMW(RequireRole("instance_admin", checker)(...))` at `router.go:41,44`. Confirmed by existing `router_test.go` regression tests + new `TestListAdminRooms_NilRepository_Returns501` / `TestGetAdminRoom_NilRepository_Returns501` (verifying handler is reached only via the role gate). No row-level scoping is needed: an `instance_admin` is by design authorised to list every room.

### C. Information Disclosure (CWE-200, CWE-209)

Errors returned as Matrix-style `{"error":{"code":...,"message":...}}` envelopes:

- 400: `"limit must be between 1 and 100"`, `"Invalid cursor"`, `"status must be 'active' or 'archived'"` — no PII / stack / DB error exposed.
- 404: `"Room not found"` — generic.
- 500: handler returns the wrapped Go `error` to the strict-handler chain; no direct `fmt.Sprintf("%v", err)` to the client.

`creator_user_id` and `power_levels_json` are room metadata (already visible to all room members via `m.room.create` / `m.room.power_levels` state events). No new disclosure surface.

### D. Audit Logging (Nebu invariant)

Both endpoints emit `action="admin_room_viewed", target_type="room"`:

- List: `target_id=""` (set-level access, no single-room target)
- Get: `target_id=request.RoomId`

Failure path is `_ = audit.LogEvent(...)` — never-raise per `audit/writer.go:64`. `CoreClient` nil-guarded so tests/dev still produce 200 responses. Actor ID extracted via `middleware.ContextKeyUserID` set by the JWT middleware — no client-controlled actor.

### E. Body-size, Rate-limit, Timeouts

Both endpoints are GET (no body). The Admin API is currently not behind `adminRL` / `complianceRL`; this is consistent with the Story 6-3 design decision and was accepted in earlier reviews. Out of scope for 6-7.

### F. Goroutine / Resource Hygiene

No new goroutines. No `io.ReadAll` on attacker-controlled streams. `db.QueryContext` / `QueryRowContext` use the request `ctx` end-to-end so client cancellation tears down the query.

### G. Crypto Primitives

No new crypto introduced. None needed.

### H. Test Coverage

`gateway/internal/api/rooms_handler_test.go` (757 lines) covers every AC:

| AC | Tests |
|---|---|
| AC#1 status filter | `TestListAdminRooms_StatusArchivedFilter`, `TestListAdminRooms_InvalidStatus_Returns400` |
| AC#1 search filter | `TestListAdminRooms_SearchFiltersByName` |
| AC#1 pagination | `TestListAdminRooms_PaginationMetaReturned`, `TestListAdminRooms_DefaultLimit_NoError` |
| AC#1 limit bounds | `TestListAdminRooms_LimitZero_Returns400`, `TestListAdminRooms_LimitAbove100_Returns400` |
| AC#1 invalid cursor | `TestListAdminRooms_InvalidCursor_Returns400` |
| AC#1 schema | `TestListAdminRooms_RoomObjectFields` |
| AC#2 unknown room → 404 | `TestGetAdminRoom_UnknownRoom_Returns404` |
| AC#2 member_count active-only | `TestGetAdminRoom_MemberCount_ActiveOnly` |
| AC#2 message_count | `TestGetAdminRoom_MessageCount` |
| AC#2 detail fields | `TestGetAdminRoom_DetailFields` |
| AC#2 route registered | `TestGetAdminRoom_RouteRegistered`, `TestGetAdminRoom_NilRepository_Returns501` |
| AC#3 audit list | `TestListAdminRooms_AuditLogEmitted` |
| AC#3 audit get | `TestGetAdminRoom_AuditLogEmitted` |

Every AC has at least one test. No security property is asserted only by code-comment.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 0 |
| LOW       | 1 |
| INFO      | 3 |

## Pipeline Decision

**CLEAN** — no CRITICAL or HIGH findings. Pipeline may proceed.

Non-blocking follow-ups:
1. (LOW-1) Product decision on whether `?search=` should LIKE-escape metacharacters for parity with `matrix.EscapeLIKE`.
2. (INFO) Reconsider signed pagination cursors only if Admin API ever exposes paginated reads outside the `instance_admin` role boundary.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
