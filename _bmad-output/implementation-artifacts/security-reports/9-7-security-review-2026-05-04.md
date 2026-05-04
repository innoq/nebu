# Security Review — Story 9-7 (room-state-event-types-full-implementation) — 2026-05-04

**Agent:** Kassandra
**Diff base:** `git diff --staged` (feature/phase-2-epic-9, 23 staged files, +1844/-22)
**Classification:** CRITICAL
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

The 9-7 diff opens a generic state-event write path (`PUT /rooms/{roomId}/state/{eventType}`) for fifteen whitelisted Matrix state types. Authentication, membership and the gateway whitelist are wired correctly, the new SQL is fully parameterised, and the migration is benign. The breaking issue lives one layer deeper: state events are routed through `Room.Server.send_event/6`, which gates writes on the `:send_event` action — `events_default` (default 0). Matrix-spec mandates that state events be governed by `state_default` (default 50). As shipped, any joined member can mutate `m.room.name`, `m.room.topic`, `m.room.join_rules`, `m.room.history_visibility`, `m.room.server_acl`, `m.room.tombstone`, `m.room.encryption`, etc. This is the core invariant the story exists to enforce, and it is the one this implementation breaks.

## Findings

### [CRITICAL] State-event writes use `events_default`, not `state_default` — Matrix Power-Level invariant violation

- **CWE / OWASP:** CWE-285 (Improper Authorization) / A01:2021 — Broken Access Control
- **Datei:** `core/apps/room_manager/lib/nebu/room/server.ex:354`
- **Beschreibung:** `handle_call({:send_event, ..., state_key}, ...)` checks `Nebu.Room.PowerLevels.can?(state.power_levels, user_id, :send_event)` for every event regardless of whether the call is a regular message or a state event. `:send_event` resolves to `events_default` (default `0`). State events — per Matrix spec v1.18 §11 and the Room version 6+ auth rules — must be authorised against `state_default` (default `50`) when no per-event override is set. The new gateway path `PutSetRoomState` (`gateway/internal/matrix/rooms.go:419`) populates `StateKey` and routes through this same `send_event` code path, so the missing distinction reaches production for fifteen state-event types: `m.room.name`, `m.room.topic`, `m.room.join_rules`, `m.room.history_visibility`, `m.room.guest_access`, `m.room.server_acl`, `m.room.encryption`, `m.room.tombstone`, `m.room.canonical_alias`, `m.room.avatar`, `m.room.pinned_events`, `m.room.member`, `m.space.child`, `m.space.parent`, plus the create event. `m.room.power_levels` itself is correctly gated by a separate path (`set_power_levels/3` → `:change_state`) and is not affected.
- **Impact:** Any joined member with the default user level (0) can rename the room, change the topic, flip `join_rules` from `invite` to `public`, set `history_visibility` to `world_readable`, push a `tombstone` redirecting the room to an attacker-controlled successor, or install custom `m.room.encryption` content. None of these require admin or moderator privileges. In a tenant-isolated chat product this is an integrity, confidentiality and availability failure simultaneously — a low-privileged member can hijack any room they are in. This is the kind of finding that ends up in a CVE entry under "Improper Authorisation in chat-server state events".
- **Empfehlung:** Branch on `state_key` in the GenServer. When `state_key` is non-nil (state event), evaluate `Nebu.Room.PowerLevels.can?(state.power_levels, user_id, :change_state)` instead of `:send_event`. The clean two-clause pattern:
  ```elixir
  required_action = if state_key == nil, do: :send_event, else: :change_state
  unless Nebu.Room.PowerLevels.can?(state.power_levels, user_id, required_action) do
    {:reply, {:error, :forbidden}, state}
  ```
  The 5-arg backward-compat clause at `server.ex:344` already forwards `state_key=""` for non-state callers — make it forward `state_key=nil` (or use a separate sentinel) so the branch can distinguish "state event with empty key" (e.g. `m.room.name`) from "regular message". Spec-faithful longer term: also honour the per-event `events.<type>` override in `power_levels`, but the `state_default` baseline closes the breach. Add an ExUnit test that asserts a level-0 member cannot set `m.room.name` and cannot set `m.room.tombstone` in a room with default power levels.
- **Referenz:** Matrix Client-Server API v1.18 §10.12.1 (Power levels), §11 (Room state events); OWASP ASVS V4.2.2; NIST SP 800-53 AC-3.

### [LOW] `state-%d-%s` txn_id has a theoretical multi-pod collision window

- **CWE / OWASP:** CWE-330 (Use of Insufficiently Random Values) — defence-in-depth only
- **Datei:** `gateway/internal/matrix/rooms.go:417`
- **Beschreibung:** `stateTxnID := fmt.Sprintf("state-%d-%s", time.Now().UnixNano(), eventType)` is unique within a single Go process (monotonic nanoseconds + per-type suffix). Across multiple gateway pods, two requests with the same `(roomId, userId, eventType)` arriving in the same nanosecond — extremely rare but not impossible at scale — would collide on the ETS dedup key `{room_id, user_id, txn_id}`. The second request's response would silently echo the first event's `event_id`, and the second state mutation would be dropped. There is no security impact (the user is the same in both txns, no privilege escalation), and the practical likelihood is negligible.
- **Impact:** A single user under heavy concurrent load could see one of two near-simultaneous state writes silently dropped with a 200 OK. Not a vulnerability, but a correctness papercut that hides a class of multi-pod race conditions.
- **Empfehlung:** Append a 64-bit `crypto/rand` suffix or a `uuid.NewString()` to the txn_id. Cheap, removes the collision window:
  ```go
  var b [8]byte
  _, _ = rand.Read(b[:])
  stateTxnID := fmt.Sprintf("state-%d-%s-%x", time.Now().UnixNano(), eventType, b)
  ```
- **Referenz:** CWE-330; defence-in-depth.

### [INFO] New attack surface: 15 state event types now writable from any authenticated member

- **CWE / OWASP:** —
- **Datei:** `gateway/internal/matrix/rooms.go:355`, `gateway/internal/matrix/state_event_types.go:12`
- **Beschreibung:** Story 9-6's whitelist (`allowedStateEventTypes`) is enforced before body decoding (rooms.go:355). The set is restricted to Matrix-spec state types and excludes vendor / `com.*` / `io.*` namespaces. Body size is constrained by `bodyLimit1MiB` at the route registration (`cmd/gateway/main.go:822`). URL/header size is bounded by `MaxHeaderBytes: 16 * 1024` (`cmd/gateway/main.go:1197`), so `state_key` cannot grow unbounded. Auth middleware (`jwtWithStatusCheck`) is applied. JSON body content is JSON-encoded back to bytes via `json.Marshal(body)` and forwarded via gRPC — no template / SQL / shell sink. Recording the surface explicitly so future regressions on the wrappers (whitelist, body limit, header limit) are caught early.
- **Impact:** None today, given the wrappers above. Captures the trust boundary.
- **Empfehlung:** Keep the whitelist as the single authoritative list. If a future story adds a new state type, the change must be in `state_event_types.go` only.

### [INFO] Migration 000038: `state_key TEXT NULL` with partial index — clean

- **CWE / OWASP:** —
- **Datei:** `gateway/migrations/000038_events_state_key.up.sql:5,8`
- **Beschreibung:** `ALTER TABLE events ADD COLUMN state_key TEXT;` (nullable) is an additive, non-destructive change. The partial index `events_room_state_idx (room_id, event_type, state_key, origin_server_ts DESC) WHERE state_key IS NOT NULL` correctly serves the `DISTINCT ON (event_type, state_key)` query in `get_generic_state_events/1` and avoids bloating the index with the (vastly larger) regular-event population. The `down` migration drops the index before the column, the only safe order. The events table retains its append-only contract — no UPDATE / DELETE grants are added. Audit-immutability invariant holds.
- **Impact:** None.
- **Empfehlung:** None. Migration is well-formed.

### [INFO] `get_generic_state_events/1` query: parameterised, no injection surface

- **CWE / OWASP:** —
- **Datei:** `core/apps/room_manager/lib/nebu/room/db.ex:524`
- **Beschreibung:** Single bind parameter (`$1` for `room_id`); the excluded `event_type` list is a hard-coded SQL literal; `DISTINCT ON (event_type, state_key) ... ORDER BY event_type, state_key, origin_server_ts DESC` is the canonical "latest per group" pattern and the `ORDER BY` prefix matches the `DISTINCT ON` columns, so the result is deterministic. `content::text` returns a JSONB-validated string; subsequent `Jason.encode!` is unnecessary because the column already holds canonical JSON, but no injection is introduced. The empty-case fallbacks (`state_key || ""`, `content_json || "{}"`, `sender || ""`) are defensive and do not mask integrity errors.
- **Impact:** None.
- **Empfehlung:** None.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ |
| `reason` field on compliance access         | ✅ |
| Audit-log immutability                      | ✅ |
| `instance_admin` notification (if in-scope) | ✅ |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ |
| Matrix Power Level checks                   | ❌ |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ✅ |
| AES-256-GCM correctness                     | ✅ |
| Ed25519 verify-before-accept                | ✅ |
| No secrets in logs / error messages         | ✅ |

The Matrix Power Level invariant is violated by the state-event path (CRITICAL finding above). All other invariants are unchanged by this diff. Compliance / audit / OIDC / crypto surfaces are not touched. The Ed25519 signing path in `Room.Server.send_event` is preserved — every state event is signed with `:persistent_term.get(:nebu_signing_key)` before persistence.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 1 |
| HIGH      | 0 |
| MEDIUM    | 0 |
| LOW       | 1 |
| INFO      | 3 |

## Pipeline Decision

**CRITICAL findings present** — Pipeline stops. User decision required: fix, accept with written justification, or convert to follow-up story.

The `state_default` regression is the entire point of the Matrix power-level model. Accepting it as a known risk would mean shipping a chat product where any room member can rename, redirect or downgrade the encryption of any room they belong to. Recommendation: fix in this story before merge.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
