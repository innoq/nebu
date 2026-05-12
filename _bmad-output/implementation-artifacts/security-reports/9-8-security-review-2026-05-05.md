# Security Review — Story 9-8 Room Version Upgrade (Full Implementation)

- **Reviewer:** Kassandra (BMAD Security Review Agent)
- **Date:** 2026-05-05
- **Scope:** `git diff --staged` for branch `feature/phase-2-epic-9` (Story 9-8)
- **Story:** `_bmad-output/implementation-artifacts/9-8-room-version-upgrade-full-implementation.md`
- **Frameworks applied (as weighted lenses):** OWASP Top 10 2021, OWASP ASVS 4.0 L2, CWE Top 25 (2024), STRIDE, NIST SP 800-53 r5
- **Nebu invariants:** OIDC token validation, audit-log immutability, Matrix power-level enforcement, Ed25519 verify-before-accept, no plaintext secrets, no hardcoded secrets, TLS 1.3 enforcement

## Executive Summary

Story 9-8 replaces the prior 501-stub for `POST /_matrix/client/v3/rooms/{roomId}/upgrade`
with a full atomic upgrade flow: gateway handler, new gRPC RPC `UpgradeRoom`, and a Core
implementation that emits `m.room.tombstone`, creates a new room with a `predecessor`
field, copies select state events, and re-invites all old members. The diff also
introduces `Nebu.Room.DB.get_room_create_event/1` (a parameterised JSONB query) and
extends `build_state_events/2` to read the persisted `m.room.create` content.

The change is **functionally well-tested** (1,125-line ExUnit suite plus Go handler
tests plus a Godog feature). The Gateway handler is correctly chained behind
`bodyLimit1MiB` + `jwtWithStatusCheck`, OIDC validation is enforced before any
gRPC call, the new SQL query is parameterised, and the gRPC PSK interceptor
already protects the Core endpoint from external callers.

There are **no CRITICAL findings**.

The main residual risks are non-CRITICAL but worth fixing:

1. **HIGH-1 — Trust-boundary deviation:** Core's `upgrade_room/2` derives the
   requester identity from `request.requester_id` (a body field) instead of from
   trusted `x-user-id` gRPC metadata. This violates the project's documented
   "Elixir trusts metadata only" architectural rule (`Nebu.Grpc.Metadata`
   `@moduledoc`). The Gateway currently always sets both fields from the same
   JWT-derived `userID`, so the runtime behaviour is correct — but the contract
   relies on a single caller getting it right, and other RPCs (`update_profile`,
   `set_power_levels`) follow the safer metadata-only pattern with explicit
   defense-in-depth.

2. **MEDIUM-1 — Non-atomic upgrade sequence:** The seven-step sequence in
   `upgrade_room/2` is not wrapped in a transaction. A crash, gRPC deadline,
   or DB error after the tombstone is written but before the new room is
   fully populated leaves the old room dead and the replacement room
   half-built. Self-DoS for the room owner.

3. **MEDIUM-2 — Race on concurrent upgrades:** No mutex / lock prevents two
   parallel `upgrade_room` calls on the same `old_room_id`. Two new rooms are
   created, two tombstones are emitted, and `replacement_room` becomes
   ambiguous. Owner-only attack surface.

4. **MEDIUM-3 — Failed upgrades are not audited:** Audit log entry
   (`room_upgraded`, `success`) is only written on the happy path. 403 / 404 /
   500 attempts produce no audit row, blunting incident-response visibility for
   privilege-escalation probing against the upgrade endpoint.

5. **LOW-1 — Existence-disclosure side channel:** Non-existent room → 404,
   existing room without sufficient power → 403. Distinguishing the two lets
   a non-member enumerate room IDs.

6. **LOW-2 — Sender-attribution drift in copied state events:** `copy_state_events/3`
   re-emits every preserved state event with `sender = requester_id`, even though
   the original content was authored by someone else. The audit chain of custody
   for things like `m.room.encryption`, `m.room.history_visibility`, etc. is
   reset to the upgrader. This is a Matrix-spec deviation more than a security
   issue, but it does affect non-repudiation in audit replay.

**Classification: HIGH** (one HIGH finding, no CRITICAL).

Per `blocking_severity: CRITICAL` (default), the pipeline may proceed with a
warning. None of the findings represent an exploitable production breach today,
but HIGH-1 is a contract erosion that will eventually be exercised by a future
caller and should be normalised to the rest of the Core API surface.

## Severity Counts

| Severity  | Count |
|-----------|-------|
| CRITICAL  | 0     |
| HIGH      | 1     |
| MEDIUM    | 3     |
| LOW       | 2     |
| INFO      | 2     |

---

## Findings

### HIGH-1 — `upgrade_room/2` trusts body-supplied `requester_id` instead of `x-user-id` gRPC metadata

- **File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2220-2244`
- **Frameworks:** OWASP A01 (Broken Access Control), CWE-639 (Authorization Bypass Through User-Controlled Key), STRIDE-S (Spoofing), ASVS V4.2.1
- **Description:**
  The new `upgrade_room/2` reads the actor identity from the gRPC request body:
  ```elixir
  def upgrade_room(request, _stream) do
    old_room_id  = request.old_room_id
    requester_id = request.requester_id
    ...
    requester_level = get_in(old_state.power_levels, ["users", requester_id]) || 0
    if requester_level < 100 do
      raise GRPC.RPCError, status: GRPC.Status.permission_denied(), ...
    end
  ```
  The project's documented architectural rule is the opposite. `Nebu.Grpc.Metadata`
  declares: "Elixir trusts these values fully — the Go gateway has already validated
  the OIDC token. No re-validation in Elixir (Architecture Rule: auth token never
  forwarded to Elixir)." Sibling RPCs follow this pattern strictly:
  `set_power_levels` and `update_profile` derive the user from
  `Nebu.Grpc.Metadata.trusted_identity(stream)`, and `update_profile` even includes
  defense-in-depth when the body field disagrees with metadata
  (`server.ex:549-553`).

  In `upgrade_room/2`, the second argument (`_stream`) is explicitly discarded,
  so the metadata trust path is unused. The handler authorises whatever
  Matrix user ID a gRPC caller puts in the body.
- **Impact:**
  Today, the only gRPC caller is the Gateway, and `gateway/internal/matrix/rooms_upgrade.go:97`
  always sets `RequesterId: userID` from the JWT-validated context — so the field
  is correctly populated and the runtime behaviour is safe. The risk is
  contractual:
  - Any future Gateway code path or new internal caller that forwards an
    attacker-controlled user identifier into `RequesterId` (e.g. an admin tool
    that "acts on behalf of" a user) will silently bypass authorisation.
  - The gRPC channel is PSK-protected (`Nebu.Grpc.AuthInterceptor`), so an
    external attacker cannot exploit this directly — but a compromise of any
    gateway process or a misconfiguration that allows a second gRPC client
    onto the internal network turns this into an immediate full-room takeover.
  - The architectural-rule violation also signals to future maintainers
    that body trust is acceptable, propagating the pattern.
- **Recommendation:**
  Match the convention used by `update_profile` / `set_power_levels`:
  ```elixir
  def upgrade_room(request, stream) do
    {trusted_user_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)
    if is_nil(trusted_user_id) or trusted_user_id == "" do
      raise GRPC.RPCError, status: GRPC.Status.unauthenticated(),
        message: "missing x-user-id metadata"
    end
    if request.requester_id != "" and request.requester_id != trusted_user_id do
      raise GRPC.RPCError, status: GRPC.Status.permission_denied(),
        message: "requester_id mismatch"
    end
    requester_id = trusted_user_id
    ...
  ```
  Optionally, drop `requester_id` from `UpgradeRoomRequest` in `proto/core.proto`
  entirely — there is no scenario where the Gateway should be telling Core "act
  as user X". Note: `create_room/2` has the same pattern with `creator_id`; out
  of scope for this story but should be normalised epic-end.

---

### MEDIUM-1 — Non-atomic upgrade leaves room in inconsistent state on partial failure

- **File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2220-2320`
- **Frameworks:** STRIDE-T (Tampering with state), STRIDE-D (Denial of Service), CWE-755 (Improper Handling of Exceptional Conditions), NIST AU-9
- **Description:**
  The seven-step upgrade sequence (verify → tombstone → start new room → emit
  create → join + power levels → copy state → invite members → audit) is not
  wrapped in `Repo.transaction/1`. Each `messages_db_module().insert_event/1`
  call is its own DB write, and `Nebu.Room.RoomSupervisor.start_room/1` and
  `Nebu.Room.Server.set_power_levels/3` mutate distributed Horde state.
  If any step after the tombstone fails (DB connection drop, gRPC deadline
  exceeded, Horde partition, OOM during state copy of a large room),
  the result is:
    - the old room has an `m.room.tombstone` and is functionally dead,
    - the new room is partially populated (no power levels, half the state, no
      invitations sent),
    - the audit log row is missing because step 7 (`audit_writer_module().log/6`)
      is the last step.
  Also, all `Logger.warning` paths inside `Enum.each` for invitations swallow
  per-member failures silently — the upgrade still reports `success`.
- **Impact:** Room owner is permanently locked out of their own room. The new
  room cannot be repaired without DB-level surgery (Matrix has no "rebuild
  state" API). This is self-DoS — the attacker must already have power_level=100
  on the old room — but it is a one-shot bricking primitive that survives
  retries (the second call sees an already-tombstoned room and adds another
  tombstone).
- **Recommendation:**
  Two-pronged:
  1. Reorder so the destructive step (`m.room.tombstone` in old room) happens
     **last**, after the new room has been fully built and verified. If anything
     fails before the tombstone is written, the old room is untouched and the
     client can simply retry.
  2. Wrap the writes that need to land together (`m.room.create` + power levels
     for the new room) in `Repo.transaction/1`, or — at minimum — explicitly
     handle `{:error, _}` from each `emit_state_event` and roll back the
     half-built new room (`Nebu.Room.RoomSupervisor.stop_room/1`) before
     returning the gRPC error.

---

### MEDIUM-2 — No serialisation on concurrent upgrade calls for the same room

- **File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2220-2320`
- **Frameworks:** OWASP A04 (Insecure Design), CWE-362 (Concurrent Execution using Shared Resource with Improper Synchronization — TOCTOU), STRIDE-T
- **Description:**
  The handler calls `lookup_room → get_state → power-level check → start_room → emit_state_event …`
  with no GenServer call/lock, no `:global` registration on `old_room_id`, and no DB
  unique constraint preventing two parallel tombstones in the same room. Two
  concurrent upgrade requests (the same owner clicking twice in Element Web, two
  scripted clients, or one client + one retry) each pass the power-level check,
  each generate a distinct `new_room_id` via `:crypto.strong_rand_bytes(8)`,
  each emit a tombstone in the old room, each create a new room, each invite
  the old members.
- **Impact:**
  - Old room ends up with two `m.room.tombstone` events. Matrix clients use
    "the latest tombstone" but the choice of `replacement_room` is now
    non-deterministic per-client.
  - Old members receive two simultaneous invites to two new (mutually unaware)
    rooms — confusion, support burden.
  - Cannot be triggered by non-owners (power-level check still applies),
    so this is owner-only / accidental, not adversarial. Still a STRIDE-T
    Tampering finding because state can be mutated into an inconsistent
    shape.
- **Recommendation:**
  Serialise on the Room GenServer for the old room. The simplest fix is to add
  a `handle_call({:begin_upgrade, …}, …)` in `Nebu.Room.Server` that atomically
  marks the room as "upgrade in progress" in its state, refuses concurrent
  begin requests, and is cleared after the tombstone is written (or on crash).
  Alternatively, add a partial unique index on `events (room_id, type) WHERE
  type='m.room.tombstone'` so the second tombstone insert fails fast.

---

### MEDIUM-3 — Failed upgrade attempts are not written to the audit log

- **File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2230-2244, 2302-2310`
- **Frameworks:** OWASP A09 (Security Logging and Monitoring Failures), NIST AU-2 / AU-3, ASVS V7.1
- **Description:**
  `audit_writer_module().log(requester_id, "room_upgraded", "room", old_room_id, …, "success")`
  only fires inside the success branch (line 2303). The two `raise GRPC.RPCError`
  paths (room not found, insufficient power) and the inner failure branches
  (`start_room` returning `{:error, _}`, every per-member invite failing) emit
  no audit log entry at all. `Compliance.AuditWriter.log/6` already supports
  `outcome: "failure"` and the `room_upgraded` action is in the
  `@known_actions` allowlist (Story 9-8 added it).
- **Impact:**
  An attacker probing `POST /rooms/{roomId}/upgrade` to enumerate rooms or test
  for power-level misconfiguration leaves no audit trail. Compliance / Compliance-RSP
  reviewers cannot reconstruct attempted privilege escalations against this
  endpoint. Existing endpoints (`admin_login_failed`, `compliance_access_rejected`)
  already follow the failure-audit pattern; this one breaks the convention.
- **Recommendation:**
  Add a failure-audit call in each error branch:
  ```elixir
  audit_writer_module().log(requester_id, "room_upgraded", "room", old_room_id,
    %{"new_version" => new_version}, "failure", "insufficient_power_level")
  ```
  Mirror the convention used by Epic 5 admin handlers.

---

### LOW-1 — Existence-disclosure side channel: 404 vs 403

- **File:**
  - `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2228-2244`
  - `gateway/internal/matrix/rooms_upgrade.go:103-108`
- **Frameworks:** OWASP A01, CWE-204 (Observable Response Discrepancy), STRIDE-I (Information Disclosure)
- **Description:** A non-member (`power_level = 0`) receives 403 `M_FORBIDDEN`
  for a room they don't belong to, and 404 `M_NOT_FOUND` for a non-existent
  room. The two responses let an unauthenticated probe enumerate valid room
  IDs, despite the `!opaque:server` ID space being 64 bits of randomness.
- **Impact:** Modest — Matrix room IDs are not secret in most deployments
  (they leak via aliases, DM creation, sync responses), and 64 bits of
  entropy makes brute enumeration impractical. Mainly a hygiene finding.
  Does, however, give insider attackers (deactivated users, ex-employees with
  retained tokens) a way to enumerate currently-active room IDs.
- **Recommendation:** Return 403 for both cases ("you do not have permission
  to upgrade this room") so that existence is not observable to non-members.
  Matches the project's existing reasoning in archive / leave handlers.

---

### LOW-2 — Copied state events are re-attributed to the upgrader

- **File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2374-2425`
- **Frameworks:** STRIDE-R (Repudiation), CWE-778 (Insufficient Logging), Matrix-CSAPI Section 11.35.1
- **Description:** `copy_state_events/3` re-emits every preserved state event
  with `sender_id = requester_id`. The original `sender` (returned by
  `get_generic_state_events/1`) is discarded:
  ```elixir
  emit_state_event(new_room_id, requester_id, e.type, e.state_key || "", content)
  ```
  Effects:
  - `m.room.encryption` content authored by an old admin appears in the new
    room signed by Nebu with `sender = upgrader`.
  - The audit replay against `events.sender` for the new room shows a
    single user authoring all state, even when the content was set by other
    operators.
- **Impact:** Non-repudiation: a future admin reading event history cannot
  distinguish "this room was always like that" from "the upgrader set it
  during the upgrade". For most state types this is harmless, but for
  policy-sensitive events (`m.room.history_visibility`,
  `m.room.guest_access`, `m.room.join_rules`) it breaks audit attribution.
- **Recommendation:** Either preserve the original `sender` in the copied
  event (`get_generic_state_events/1` already returns it, just plumb it
  through `copy_state_events/3`), or include the original sender in the
  event content under a Nebu-specific key (`org.nebu.upgrade.original_sender`)
  so audits can reconstruct attribution.

---

### INFO-1 — `state_key` from `get_generic_state_events/1` is replayed without validation

- **File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2413, 2423` and `core/apps/room_manager/lib/nebu/room/db.ex:565-597`
- **Description:** `copy_state_events/3` passes `e.state_key || ""` straight
  into `emit_state_event/5` without re-validating that the value is a
  Matrix-safe state-key (e.g. for `m.room.power_levels` the key must be `""`).
  All current event types already had their state keys validated on the
  inbound write path (in `Room.Server`), so the data is implicitly trusted —
  but this is a defense-in-depth gap: any future event type added to the
  generic copy list inherits this assumption.
- **Recommendation:** Add a state-key validator in `emit_state_event/5` (or
  in `copy_state_events/3` before the copy) that asserts the type/state-key
  pair is well-formed.

---

### INFO-2 — `bodyLimit1MiB` is the only DoS guard on the upgrade endpoint

- **File:** `gateway/cmd/gateway/main.go:794-795`
- **Description:** No request-rate limiter is attached to
  `POST /_matrix/client/v3/rooms/{roomId}/upgrade`. The endpoint is
  expensive (creates a Room GenServer, copies state, fans out invitations).
  An authenticated attacker with power_level 100 in any room can issue
  upgrade calls in a tight loop. Same convention is used today by
  `createRoom` and `invite`, so this is not new — flagging for project-wide
  consideration when rate limiting is normalised across `/rooms/*` writes.
- **Recommendation:** Out of scope for Story 9-8. Track as an epic-level
  follow-up alongside `createRoom` / `invite` rate-limiting.

---

## Nebu Invariants Check

| Invariant | Status | Notes |
|---|---|---|
| OIDC token validation (`iss`, `aud`, `exp`, alg whitelist) | ✅ | Enforced upstream by `JWTMiddleware` (`auth.go:182-204`); upgrade handler is chained behind `jwtWithStatusCheck`. |
| Compliance RSP coverage | ⚠️ | `room_upgraded` audit row is written only on success (see MEDIUM-3). |
| `reason` field on compliance access | n/a | This story does not touch compliance access. |
| Audit-log immutability | ✅ | `Compliance.AuditWriter.log/6` runs in its own `Repo.transaction/1`; `room_upgraded` added to `@known_actions` allowlist. |
| `instance_admin` notification | n/a | Out of scope. |
| Matrix Power Level checks before room mutation | ⚠️ | Power-level >= 100 enforced in Core (`server.ex:2240`), but identity is taken from body field — see HIGH-1. |
| No hardcoded secrets | ✅ | Reviewed. |
| TLS 1.3 enforcement | n/a | No TLS termination changes. |
| AES-256-GCM correctness | n/a | No symmetric crypto changes. |
| Ed25519 verify-before-accept | ✅ | Events are signed via `:crypto.sign(:eddsa, …, :ed25519)` with the persistent term key — same pattern as `create_room/2`. No new key handling. |
| No secrets in logs / error messages | ✅ | `Logger.warning` calls only include room IDs, member IDs, `inspect(reason)` — no token material. |

## Stack-Specific Spot Checks

### Go Gateway (`gateway/internal/matrix/rooms_upgrade.go`)

- ✅ `requireJSON` enforces `Content-Type: application/json` (415 fast-fail).
- ✅ `ValidateMatrixRoomID(roomID)` rejects malformed IDs (CWE-20).
- ✅ `dec.DisallowUnknownFields()` prevents downstream parameter smuggling.
- ✅ gRPC error mapping covers `PermissionDenied`, `NotFound`, `InvalidArgument`,
  default → 500 `M_UNKNOWN`. No internal error message leaks to the client.
- ✅ `userID` and `systemRole` come from `r.Context()` (set by `JWTMiddleware`),
  not from request body.
- ✅ Body size capped at 1 MiB via `bodyLimit1MiB` middleware.
- INFO: response writer uses `_ = json.NewEncoder(w).Encode(…)` — error path
  silently swallowed. Fine for this surface; matches sibling handlers.

### Elixir Core (`core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`)

- HIGH-1: trust boundary mismatch (see Findings).
- ✅ `:crypto.sign(:eddsa, :none, …, [priv, :ed25519])` — correct primitive,
  uses canonical JSON form, Ed25519 with no caller-supplied algorithm.
- ✅ `:persistent_term.get(:nebu_signing_key)` — same key handling as
  `create_room/2`, no new key surface.
- ✅ Event ID generated via `Nebu.EventId.generate/1` over the canonical event
  before signature is added — content-hash event IDs (ADR-003) preserved.
- MEDIUM-1, MEDIUM-2, MEDIUM-3, LOW-1, LOW-2, INFO-1: see Findings.

### PostgreSQL (`core/apps/room_manager/lib/nebu/room/db.ex:493-531`)

- ✅ `get_room_create_event/1` uses parameterised SQL with `$1` bind, no string
  interpolation of `room_id` (CWE-89 — clean). The `CASE jsonb_typeof(...)`
  expression operates on the column, not on user input.
- ✅ Uses `ORDER BY origin_server_ts ASC LIMIT 1` for determinism.
- ✅ `Jason.decode/1` defended with `is_map/1` guard, no atom keys created
  from external input (CWE-400 atom-table exhaustion safe — `Jason.decode/1`
  defaults to string keys).
- INFO: query returns the *first* `m.room.create` event. If a misbehaving
  caller managed to write a second create event, this query would still
  return the older one — i.e. the original creator wins. Matches Matrix
  semantics; flagged only because the scenario was not covered by a test.

### Proto (`proto/core.proto:626-635`)

- HIGH-1: `string requester_id = 2;` is the body-trust field. Recommend
  removing once HIGH-1 is fixed.
- ✅ `string new_version = 3;` — string-typed; Core handles `""` and `nil`.
- ✅ Field numbering 1/2/3 — no reuse of deprecated numbers.

---

## Verdict

**Classification: HIGH** (1 HIGH, 3 MEDIUM, 2 LOW, 2 INFO; 0 CRITICAL).

The `blocking_severity: CRITICAL` default does **not** block this story.
The pipeline may proceed with a documented warning. Recommend creating
follow-up stories (or accepting with written justification) for HIGH-1
and the three MEDIUMs before the Epic 9 final security review.

## Recommended Follow-Up Stories

1. **9-8a — Normalise actor identity in Core RPCs:** `upgrade_room/2`
   and `create_room/2` should both source their actor from
   `Nebu.Grpc.Metadata.trusted_identity/1`, with body-field defense-in-depth.
   Drop `requester_id` / `creator_id` from the proto. (Addresses HIGH-1.)
2. **9-8b — Atomic upgrade with rollback on failure:** Reorder the upgrade
   to write the tombstone last; wrap state-event copy in
   `Repo.transaction/1`; on partial failure, stop the partially-built
   new Room GenServer. Add Mutex / DB unique-index protection against
   parallel upgrades. (Addresses MEDIUM-1 & MEDIUM-2.)
3. **9-8c — Failure auditing for room_upgraded:** Emit `room_upgraded`
   audit rows with `outcome: "failure"` on every error branch. (Addresses
   MEDIUM-3.)
4. *(Optional)* **9-8d — Existence-disclosure hardening on
   `/rooms/{roomId}/upgrade`:** Collapse 404 into 403 for callers without
   power. (Addresses LOW-1.)
