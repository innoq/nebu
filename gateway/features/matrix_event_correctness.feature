# FAILING STUBS — story 9-10b implements step definitions and fixes
#
# Story 9-10a: Matrix Event Correctness — Spike (DM-Loop Root Cause)
# These scenarios document spec contracts identified by the audit in
# docs/matrix-event-audit-2026-05-05.md. They are written FIRST (ATDD gate)
# and are expected to fail at runtime until story 9-10b makes them green.
#
# Spec references:
#   §11.12.1 — Key Distribution (keys/query)
#   §11.10   — Room Encryption (m.room.encryption)
#   §8.4.3   — Unsigned Data in timeline events (unsigned.age)
#   §8.4     — Sync response (device_lists, device_one_time_keys_count)
#
# Step vocabulary:
#   - "the docker compose stack is started"         → steps_test.go
#   - "kai is authenticated via OIDC"               → room_flow_steps_test.go
#   - "kai creates a room named ..."                → room_flow_steps_test.go
#   - "the response status is N"                    → steps_test.go
#   - "the response body contains ..."              → steps_test.go
#   - "the response body does not contain ..."      → profile_subfields_steps_test.go
#   - kai/marie PUT /state/{eventType}              → set_room_state_full_steps_test.go
#
# NEW steps needed in story 9-10b (matrix_event_correctness_steps_test.go):
#   - "kai sends POST /_matrix/client/v3/keys/query with body:"   — keys/query request
#   - "the response JSON has key {key} with child key {childKey}" — nested JSON presence check
#   - "the response JSON {key} does not contain key {childKey}"   — nested JSON absence check
#   - "the response JSON {key} is a non-null object"              — type assertion
#   - "the response JSON {key} is a non-null array"               — type assertion
#   - "the response JSON {parent}.{child} is a non-null array"    — nested type assertion
#   - "the response JSON timeline events have an unsigned.age field" — timeline event audit
#   - "kai sends GET /_matrix/client/v3/sync"                     — simple sync fetch
#   - "kai creates a DM room with {userId} and captures the room ID" — DM room creation
#   - "kai sends keys/query for {userId}"                          — targeted keys/query

Feature: Matrix Event Correctness — spec compliance for DM creation flow
  As a Matrix client (Element Web)
  I want the Nebu server to respond with spec-compliant payloads for keys/query, sync, and state events
  So that DM room creation completes without entering an infinite request loop

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC

  # ─── AC4 — keys/query response format (spec §11.12.1) ──────────────────────
  #
  # CRITICAL finding: keys/query must include a device_keys entry for known users
  # (even if empty inner map) and must NOT list them in failures.
  # Current behavior (Story 5-29e): returns {"device_keys":{"@user:server":{}},"failures":{}}
  # for known users — this part should PASS. Scenario verifies the contract holds end-to-end
  # against the real running stack (not just unit tests).
  #
  # Expected failure mode in 9-10a: step definitions for keys/query don't exist yet,
  # so Godog cannot match the steps — scenario is PENDING (counts as failing).

  Scenario: KeysQuery_KnownUser_DeviceKeysEntryPresent — keys/query includes entry for known user
    # AC4 — §11.12.1: device_keys must contain an entry for known users
    When kai sends POST /_matrix/client/v3/keys/query with body:
      """
      {"device_keys":{"@kai:localhost":[]}}
      """
    Then the response status is 200
    And the response JSON has key "device_keys" with child key "@kai:localhost"
    And the response JSON "failures" does not contain key "@kai:localhost"

  Scenario: KeysQuery_UnknownUser_NotInFailures — unknown user is omitted from device_keys, not in failures
    # AC4 — §11.12.1: non-existent users must be silently omitted, NOT listed in failures
    # (non-federated server: "user not found" is not a failure, it is just unknown)
    When kai sends POST /_matrix/client/v3/keys/query with body:
      """
      {"device_keys":{"@nonexistent:test.local":[]}}
      """
    Then the response status is 200
    And the response JSON "failures" does not contain key "@nonexistent:test.local"

  # ─── AC1/AC2 — /sync device fields non-null (spec §8.4) ────────────────────
  #
  # MEDIUM — regression guard from Story 5-29e.
  # device_one_time_keys_count must be {} (empty object, NOT null).
  # device_lists.changed and device_lists.left must be [] (empty array, NOT null).
  # device_unused_fallback_key_types must be [] (empty array, NOT null).
  # Missing/null values cause Element Web to enter an OTK-upload + keys/query polling loop.
  #
  # Expected: should PASS (Story 5-29e fixed this), but Godog scenario is the regression guard.
  # Expected failure mode in 9-10a: step definitions don't exist yet → PENDING.

  Scenario: Sync_DeviceFields_NonNull — sync response always includes non-null device fields
    # AC1 — §8.4: device fields must be present and non-null in every sync response
    When kai sends GET /_matrix/client/v3/sync
    Then the response status is 200
    And the response JSON "device_one_time_keys_count" is a non-null object
    And the response JSON "device_lists" is a non-null object
    And the response JSON "device_lists.changed" is a non-null array
    And the response JSON "device_lists.left" is a non-null array
    And the response JSON "device_unused_fallback_key_types" is a non-null array

  # ─── AC1/AC3 — unsigned.age in timeline events (spec §8.4.3) ───────────────
  #
  # HIGH DEVIATION (confirmed by audit 2026-05-05, Finding 3):
  # unsigned.age is MISSING from all timeline events — syncTimelineEvent struct
  # in sync.go has no Unsigned field. This violates §8.4.3 SHOULD-level guidance.
  # matrix-js-sdk uses unsigned.age for event deduplication and lag detection.
  # Missing unsigned.age causes sporadic re-polling of already-seen events during
  # DM creation (m.room.member invite events), keeping the creation spinner alive.
  #
  # Fix target: story 9-10b — add Unsigned{Age int64} to syncTimelineEvent
  # and populate as time.Now().UnixMilli() - event.OriginTS.
  #
  # This scenario is EXPECTED TO FAIL against the current implementation.
  # DO NOT remove — it is the regression gate for the 9-10b fix.

  Scenario: Sync_TimelineEvents_HaveUnsignedAge — timeline events in sync include unsigned.age
    # AC3 — §8.4.3: every timeline event must carry unsigned.age (milliseconds since event origin)
    Given kai creates a room named "unsigned-age-test-room"
    When kai sends GET /_matrix/client/v3/sync
    Then the response status is 200
    # Traversal contract for the step below (9-10b implementer):
    #   The step iterates ALL events in rooms.join.*.timeline.events[] across all joined rooms.
    #   Assertion passes only if EVERY event has an unsigned.age field with a numeric value > 0.
    #   Edge case: if every joined room has an empty timeline (no events to check), the scenario
    #   passes vacuously — there is nothing to assert against. The 9-10b step definition MUST
    #   guard against this and mark the scenario as inconclusive (or seed at least one event
    #   via the preceding Given) to avoid false-green runs.
    And the response JSON timeline events have an unsigned.age field

  # ─── AC1 — m.room.encryption state event accepted (spec §11.10) ────────────
  #
  # CRITICAL if whitelist check fails — Element Web sends m.room.encryption during
  # DM setup. If rejected (403 M_FORBIDDEN or 400 M_BAD_JSON), DM creation loops.
  # Story 9-6 added m.room.encryption to the whitelist; Story 9-7 wired Core.SendEvent.
  # This scenario verifies the full end-to-end path against the real running stack.
  #
  # Expected: should PASS (Stories 9-6 + 9-7 implemented this).
  # Expected failure mode in 9-10a: step definitions don't exist yet → PENDING.

  Scenario: StateEvent_mRoomEncryption_Accepted — m.room.encryption state event returns 200 with event_id
    # AC1 — §11.10: m.room.encryption must be accepted and stored
    Given kai creates a room named "encryption-state-test-room"
    # roomId capture contract for the step below (9-10b implementer):
    #   The literal "{roomId}" placeholder in the URL is NOT a Gherkin parameter — it is resolved
    #   at step-execution time from the test scenario's context state. The preceding
    #   `Given kai creates a room named ...` step (defined in room_flow_steps_test.go) MUST
    #   capture the created room's ID into the shared scenario context (e.g. `ctx.lastRoomID`),
    #   and the step definition for `kai sends PUT /rooms/{roomId}/state/...` MUST substitute
    #   that captured value before issuing the HTTP request. Do NOT treat "{roomId}" as a
    #   user-supplied string literal.
    When kai sends PUT /rooms/{roomId}/state/m.room.encryption with body {"algorithm":"m.megolm.v1.aes-sha2"}
    Then the response status is 200
    And the response body contains "event_id"

  # ─── AC2 — DM room creation end-to-end (integration scenario) ──────────────
  #
  # HIGH — integration scenario that verifies the full DM creation flow:
  # 1. Create DM room with is_direct:true and invite
  # 2. Query keys for the invited user
  # Both steps must complete with 200. If either loops or returns an error,
  # the DM creation spinner never resolves.
  #
  # Expected failure mode in 9-10a: step definitions don't exist yet → PENDING.

  Scenario: DMCreation_KeysQuery_Completes — DM room creation and keys/query both complete without looping
    # AC2 — DM loop root cause: verify both DM creation and keys/query return 200 cleanly
    Given marie is authenticated via OIDC
    When kai creates a DM room with "@marie:localhost" and captures the room ID
    Then the response status is 200
    When kai sends keys/query for "@marie:localhost"
    Then the response status is 200
    And the response JSON has key "device_keys" with child key "@marie:localhost"
