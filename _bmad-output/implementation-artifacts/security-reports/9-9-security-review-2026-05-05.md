# Security Review — Story 9-9 (Archive TOCTOU Fix) — 2026-05-05

**Agent:** Kassandra
**Diff base:** `git diff --staged` (working tree against HEAD)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4.7`

## Executive Summary

Story 9-9 closes the TOCTOU window between `archive_room_atomic/1` and `send_event` by adding a `SELECT ... FOR UPDATE` check on the `rooms.status` row inside `Nebu.Repo.transaction/1`. The lock is on the same row that `archive_room_atomic/1` already locks, so the two transactions serialise correctly — the race window described in epic-6 SEC Gate 2 HIGH-2 is closed. Authorisation surface is unchanged (membership and power-level checks fire before the new code path). Fail-open semantics on DB error are consistent with established Nebu patterns (`init/1`, gateway-level guard) and do not create a security bypass: the gateway-level guard plus the in-memory `init/1` archive check still hold even when the new transaction errors out. No CRITICAL or HIGH findings. One MEDIUM (gateway error-code mapping is structurally too coarse), two INFO observations.

## Findings

### [MEDIUM] Gateway maps every `codes.FailedPrecondition` from `Core.SendEvent` to `403 M_ROOM_ARCHIVED`

- **CWE / OWASP:** CWE-209 (information exposure through error messages) — defence-in-depth
- **Datei:** `gateway/internal/matrix/rooms.go:564-565`
- **Beschreibung:** The new `case codes.FailedPrecondition:` branch in `PutSendEvent`'s `switch st.Code()` translates *any* `FAILED_PRECONDITION` reply from `Core.SendEvent` into the fixed body `{"errcode":"M_ROOM_ARCHIVED","error":"Room is archived"}`. Today the only Core SendEvent path that emits `FAILED_PRECONDITION` is the new `:room_archived` branch (line 123–126 of `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`), so the mapping is presently correct. However, the gateway is not inspecting the gRPC `message` field — a future Core change that introduces another `FailedPrecondition` reason on the same RPC (e.g. room-version mismatch, sender not joined, encryption-required) would silently inherit the M_ROOM_ARCHIVED label. The unit test `TestPutSendEvent_CoreFailedPrecondition_WithNonArchiveMessage_Returns403` deliberately enshrines this behaviour ("the errcode is derived from the gRPC code alone"), so the fragility is intentional but not documented as a contract.
- **Impact:** Today: none. Future: a misleading Matrix error code will surface to clients and audit logs whenever a different precondition failure is added to `Core.SendEvent`. The wrong errcode obstructs incident triage and can mislead retry/UI logic. Not a confidentiality / integrity / availability issue.
- **Empfehlung:** Either (a) tighten Core to embed a stable token in the gRPC message (e.g. `M_ROOM_ARCHIVED:` prefix already exists — switch on prefix in the gateway), or (b) record an in-code contract comment in both `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (above the SendEvent error mapping) and `gateway/internal/matrix/rooms.go:564` documenting that `FailedPrecondition` is reserved exclusively for archived-room rejection on this RPC. Option (b) is the cheapest and matches the test commitment.
- **Referenz:** OWASP ASVS V7.4.1 (consistent error responses), NIST SI-11 (error handling)

### [INFO] SELECT FOR UPDATE locks the same row as `archive_room_atomic/1` — TOCTOU window closed

- **CWE / OWASP:** CWE-367 (TOCTOU) — *resolved* by this story
- **Datei:** `core/apps/room_manager/lib/nebu/room/db.ex:483-503`, `core/apps/event_dispatcher/lib/nebu/admin/db.ex:252-287`
- **Beschreibung:** Both transactions execute `SELECT ... FROM rooms WHERE room_id = $1 FOR UPDATE` on the same primary-key row, then either read the status (Room.Server) or `UPDATE rooms SET status = 'archived'` (admin/db). PostgreSQL row-level locks acquired by `FOR UPDATE` block conflicting `FOR UPDATE`, `FOR NO KEY UPDATE`, and `UPDATE` operations on the same row. The serialisation guarantee holds for both orderings:
  - Archive-first: `archive_room_atomic/1` commits `status='archived'` → subsequent `check_room_status_for_update/1` reads `archived` → send rejected with `:room_archived`.
  - Send-first: `check_room_status_for_update/1` holds the row lock → `archive_room_atomic/1`'s `SELECT FOR UPDATE` blocks until the send transaction commits → archive proceeds afterwards. The send transaction reads `'active'`, the event is inserted; the archive then succeeds.
- **Impact:** Positive — the SEC Gate 2 HIGH-2 finding from epic-6 is mitigated.
- **Empfehlung:** No action. Logged for audit trail.
- **Referenz:** ADR-001 (PostgreSQL as source of truth), epic-6 SEC Gate 2 (2026-05-02)

### [INFO] Fail-open on DB error does not introduce a security bypass

- **CWE / OWASP:** N/A — design observation
- **Datei:** `core/apps/room_manager/lib/nebu/room/server.ex:386-391`
- **Beschreibung:** When `check_room_status_for_update/1` returns `{:error, _}` (DB transaction failure, connection drop, `:not_found`), the handler logs a warning and proceeds to `do_send_event/7`. Surface analysis:
  1. The gateway-level `RoomStatusChecker` (rooms.go:523–533) has already verified `status != "archived"` before the gRPC call. An attacker therefore cannot reach this path with a freshly archived-and-still-readable room state.
  2. `Room.Server.init/1` (line 198–208) checks status on GenServer start and stops with `:normal` for archived rooms, so the only way the GenServer is alive is that the room was active at start.
  3. A real DB outage that causes `check_room_status_for_update/1` to error would also break `archive_room_atomic/1` (same DB), so an attacker cannot use the failure mode to bypass an in-flight archive — the archive transaction itself would have to succeed atomically.
  4. The `:not_found` arm is benign: a non-existent room means no `events` row is anchored to a real room; downstream `insert_event` in production would fail FK validation. (Worth noting that the FakeDB-only path may behave differently in tests — that's a test integrity concern, not runtime.)
  Conclusion: fail-open is a deliberate availability decision and matches `init/1` plus the gateway-level guard. It does **not** open a hole an attacker can drive.
- **Impact:** None.
- **Empfehlung:** No action. Logged for audit trail.

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

- **Compliance RSP / `reason` / `instance_admin`:** Diff does not touch compliance tables or compliance read paths. `rooms` is a non-compliance table (per `_bmad-output/planning-artifacts/architecture.md`).
- **Audit-log immutability:** No migration in this diff. The diff does not relax permissions on `audit_log` or any other table.
- **OIDC validation:** Diff does not touch `gateway/internal/auth/`. The `PutSendEvent` handler continues to consume `userID` from `middleware.ContextKeyUserID` populated by the upstream JWT middleware (rooms.go:516).
- **Matrix Power Level checks:** The new Step 1.5 archived-status check is placed *after* Step 0 (power-level check) and Step 1 (idempotency lookup). The Step 0 power-level check (server.ex:364–367) is unchanged and continues to fire first. Story 9-7's `state_key`-aware `:change_state` vs `:send_event` selection is preserved.
- **Logging:** The new `Logger.warning/1` in server.ex:390 logs `room_id` and `inspect(reason)`. `room_id` is not a secret and `reason` is a tuple from `Nebu.Repo.transaction/1` (DB error type / Postgrex error). No tokens, passwords, or signing material flow through this path. Acceptable.
- **Ed25519 / AES-256-GCM / TLS:** No crypto changes. The signing call in `do_send_event/7` (server.ex:539–541) is the same `:crypto.sign(:eddsa, :none, event_json, [priv, :ed25519])` previously in the inline branch — moved verbatim into the helper.

Audit-log observation: archived-room write *rejections* are not currently logged to `compliance_audit_log`. This is consistent with the existing Nebu posture — `send_event` (the high-volume Matrix path) is not audit-logged on success or failure. Audit logging fires for admin actions (room_archived, room_created, etc.). The diff does not regress this. Not a finding.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 1 |
| LOW       | 0 |
| INFO      | 2 |

## Pipeline Decision

- **CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

The single MEDIUM (`FailedPrecondition` mapping coarseness) is a defence-in-depth recommendation for a future-fragility comment in code; it does not affect the runtime correctness of Story 9-9. No follow-up story is required, but the recommended in-code contract comment should be added when the file is next touched.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
