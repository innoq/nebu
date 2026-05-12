# Epic 9 SEC Gate 2 — Final Security Review
**Date:** 2026-05-06
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Scope:** Stories 9-19 through 9-25 (`git diff 54f20d4..HEAD`)
**Prior SEC Gate 2:** `epic-9-security-review-2026-05-05.md` (covered 9-1 through 9-18)
**Classification:** MEDIUM (no CRITICAL, no HIGH)

---

## Summary

| Severity | Count |
|----------|-------|
| CRITICAL | 0 |
| HIGH | 0 |
| MEDIUM | 2 |
| LOW | 1 |
| Informational | 2 |

No findings block the commit. Two MEDIUM findings require follow-up stories before epic closure or must be explicitly risk-accepted.

---

## Findings

---

### MEDIUM-1 — forgotten_rooms table missing RLS and explicit GRANT

**Location:** `gateway/migrations/000040_forgotten_rooms.up.sql`
**Story:** 9-19 (GAP-FORGET)

**Mechanism:**
The `forgotten_rooms` table was created without an `ALTER TABLE ENABLE ROW LEVEL SECURITY` statement, without a `FORCE ROW LEVEL SECURITY` declaration, and without an explicit `GRANT SELECT, INSERT, UPDATE, DELETE ON forgotten_rooms TO nebu_app` statement.

The application currently has access to this table via the `ALTER DEFAULT PRIVILEGES` blanket grant set up in `dev/postgres/init/01-roles.sql` (which grants SELECT, INSERT, UPDATE, DELETE on all future tables created by `nebu_migrate` to `nebu_app`). All queries in `queryForgottenRoomIDs`, `buildLeaveRooms`, and `buildInviteRooms` correctly parameterize the `user_id` filter using the authenticated user's ID.

However, the lack of RLS means that if any application-level bug causes `userID` to be absent or incorrectly set, any `nebu_app` connection can read or write any user's forgotten rooms list without a database-level safeguard. This is inconsistent with the defense-in-depth posture of every other user-data table added since migration 000029:

- `room_account_data` (000029): explicit GRANT + RLS policy
- `notifications` (000031): explicit GRANT + RLS policy
- `push_rules`, `pushers` (000032): explicit GRANT + RLS policy
- `forgotten_rooms` (000040): **neither**

**Impact:**
Defense-in-depth gap. No currently exploitable path is visible — the application-level `WHERE user_id = $1` clause prevents IDOR in practice. The risk is latent: a future regression that incorrectly passes a blank `userID` would silently expose or corrupt all users' forgotten-rooms lists with no database-level catch.

**Remediation:**
Add a follow-up migration:
```sql
-- Add missing RLS to forgotten_rooms (defense-in-depth, matching other user tables).
ALTER TABLE forgotten_rooms ENABLE ROW LEVEL SECURITY;
ALTER TABLE forgotten_rooms FORCE ROW LEVEL SECURITY;

CREATE POLICY forgotten_rooms_nebu_app_policy ON forgotten_rooms
    FOR ALL
    TO nebu_app
    USING (user_id = current_setting('app.user_id', true))
    WITH CHECK (user_id = current_setting('app.user_id', true));
```

The `withUserDB` wrapper that is already used for `room_account_data` operations sets `app.user_id` correctly. The `forgotten_rooms` queries run outside `withUserDB` (directly via `h.db.QueryContext`). For the RLS policy to be effective, the `forgotten_rooms` queries must also run inside `withUserDB`. Either: (a) wrap the `queryForgottenRoomIDs` and the subquery uses in `buildLeaveRooms`/`buildInviteRooms` in `withUserDB`, or (b) rely on the `WHERE user_id = $1` clause as the sole guard and add the RLS as an audit backstop without the `USING` clause enforcement — acceptable if (a) is deemed too disruptive.

---

### MEDIUM-2 — querySinceTsMs is device-unaware with per-device schema

**Location:** `gateway/internal/matrix/sync.go:95-106`
**Story:** 9-22 (per-device sync tokens) / 9-19 (GAP-LEAVE-ONCE)

**Mechanism:**
```go
func (h *GetSyncHandler) querySinceTsMs(ctx context.Context, userID string) int64 {
    var updatedAt int64
    if err := h.db.QueryRowContext(ctx,
        `SELECT updated_at FROM sync_tokens WHERE user_id = $1`, userID,
    ).Scan(&updatedAt); err != nil {
        return 0
    }
    return updatedAt
}
```

Migration 000041 changed `sync_tokens` from `PRIMARY KEY (user_id)` to `PRIMARY KEY (user_id, device_id)`. A user with two active devices now has two rows:

```
(user_id=alice, device_id='',      updated_at=T_legacy)
(user_id=alice, device_id='phone', updated_at=T_phone)
```

The `querySinceTsMs` query has no `device_id` filter and no `ORDER BY`. PostgreSQL returns one row in an unspecified order. The `sinceMs` value fed to `buildLeaveRooms` may belong to a different device than the one making the request.

**Impact (data integrity, not privilege escalation):**
- If the returned `sinceMs` is from a more-recently-synced device (`T_phone > T_legacy`), device A (legacy `device_id=''`) will not see leave events that occurred between `T_legacy` and `T_phone`. Those rooms will permanently disappear from `rooms.leave` for that device until the next FallbackToInitial (which uses `sinceMs=0`).
- This cannot be exploited to access another user's data. It is a data-integrity defect affecting only the requesting user's own sync state.

The device_id is available in the request context at the point `querySinceTsMs` is called (it's extracted from `middleware.ContextKeyDeviceID`). It is retrieved one line later (line 582) but not passed to `querySinceTsMs`.

**Remediation:**
Pass `deviceID` to `querySinceTsMs` and include it in the query:

```go
func (h *GetSyncHandler) querySinceTsMs(ctx context.Context, userID, deviceID string) int64 {
    if h.db == nil {
        return 0
    }
    var updatedAt int64
    if err := h.db.QueryRowContext(ctx,
        `SELECT updated_at FROM sync_tokens WHERE user_id = $1 AND device_id = $2`,
        userID, deviceID,
    ).Scan(&updatedAt); err != nil {
        return 0
    }
    return updatedAt
}
```

Extract `deviceID` before `querySinceTsMs` is called (move the extraction from line 582 to just before line 573) and pass it as the second argument.

---

### LOW-1 — Log injection via room_id / user_id string interpolation in Elixir Logger calls

**Location:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (multiple `Logger.debug` / `require Logger` additions)
**Stories:** 9-19, 9-22

**Mechanism:**
New debug/info log lines interpolate `room_id`, `recv_room_id`, and `user_id` directly into log strings using Elixir string interpolation:

```elixir
Logger.debug("[leave_room] broadcasting {:new_leave, #{room_id}} to #{length(members)} sync tasks for #{user_id}")
Logger.debug("[do_incremental_sync] {:new_leave} received for room #{recv_room_id}, waking sync task for #{user_id}")
```

Matrix user IDs (`@localpart:server`) and room IDs (`!opaque:server`) follow strict format constraints that exclude newlines. These values originate from the server-side database or from gRPC requests authenticated by the Go gateway's JWT middleware.

**Impact:**
The format constraints on Matrix IDs make meaningful log injection (injecting fake log lines) effectively impossible in the current implementation. Classified LOW rather than Informational only because a future code path that accepts less constrained identifiers (e.g., display names or alias strings) could pass those into similar log patterns.

**Remediation:**
No immediate action required. Consider using structured logging (`Logger.debug/2` with keyword metadata) as a long-term habit for values from external sources, even when the current threat is low.

---

## Informational

### INFO-1 — since_token comparison in Elixir is not constant-time

**Location:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1100-1115` (AC2 token comparison)

The `client_since_token != stored_token` comparison is a structural equality check, not constant-time. This is acceptable because `since_token` is a session checkpoint (an event hash), not a password or HMAC. An attacker cannot use timing measurements to forge a valid token incrementally — they would need another user's JWT to submit requests on that user's behalf, at which point they already have full access. Not a security finding; recorded for completeness.

---

### INFO-2 — buf_ synthetic tokens accepted by Elixir as FallbackToInitial trigger

**Location:** `gateway/internal/matrix/sync.go:673-711` (Story 9-25)

`syntheticNextBatch()` generates tokens of the form `buf_<ms>_<seq>`. An authenticated user can send `GET /sync?since=buf_1_1` to force a full resync. This is intentional design: Elixir's `get_since_token` lookup fails → `{:error, :not_found}` → `FallbackToInitial`. However, any authenticated user can also achieve the same effect by sending `GET /sync` without a `since` parameter. The `buf_` format does not expand the attack surface. Not a security finding; recorded for completeness.

---

## Scope Coverage

| Attack Surface | Checked | Verdict |
|---|---|---|
| SQL injection — new queries (9-22, 9-23, 9-24) | Yes | Clean — all use `$1/$2` parameterized queries |
| RLS bypass — withUserDB pattern (9-22, 9-24) | Yes | Clean — `set_config('app.user_id', $1, true)` + RLS on `room_account_data` |
| Missing RLS — forgotten_rooms (9-19) | Yes | **MEDIUM-1** |
| Auth bypass — per-device logout (9-22) | Yes | Clean — JWT denylist invalidation is first operation in PostLogout |
| IDOR — user A reading user B's account_data (9-24) | Yes | Clean — RLS + `WHERE user_id = $1` double protection |
| Cross-device token use — device A token on device B (9-22) | Yes | Clean — JWT `did` claim binds device; FallbackToInitial if mismatch |
| Information disclosure — invite_state SQL (9-23) | Yes | Clean — queries scoped to rooms where user is invitee; values from DB, not user input |
| Timing attack — since_token comparison (9-22) | Yes | Informational only — not a password/HMAC |
| buf_ token injection — forced FallbackToInitial (9-25) | Yes | Informational only — equivalent to omitting since parameter |
| querySinceTsMs device-unaware (9-22 + 9-19 interaction) | Yes | **MEDIUM-2** |
| Log injection — Elixir Logger interpolation (9-19, 9-22) | Yes | LOW-1 |
| :pg broadcast spoofing — new_join, new_leave (9-19, 9-22) | Yes | Clean — process addressing is internal, no external surface |

---

## Verdict

**No CRITICAL or HIGH findings. Commit is not blocked.**

Two MEDIUM findings require resolution before epic is marked `done` or must be formally risk-accepted:

1. **MEDIUM-1** (`forgotten_rooms` missing RLS): create a follow-up story (or add to epic wrap-up) to add RLS policy.
2. **MEDIUM-2** (`querySinceTsMs` device-unaware): fix `sync.go` to pass `deviceID` to the query.

Both are straightforward low-effort fixes. Neither is exploitable for cross-user data exposure in the current implementation.

---

*Report generated by Kassandra (nebu-agent-kassandra) — SEC Gate 2 mandatory epic-end review.*
*Diff range: `54f20d4..HEAD` | Stories: 9-19, 9-20, 9-21, 9-22, 9-23, 9-24, 9-25*
