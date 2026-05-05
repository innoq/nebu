Feature: Room Version Upgrade — POST /rooms/{roomId}/upgrade
  As a Matrix client user
  I want to upgrade a room to a new version
  So that Element Web's "Upgrade to recommended chat version" works correctly

  # Story 9-8: Room Version Upgrade — Full Implementation
  # ATDD RED PHASE: all scenarios expect 200/predecessor content which is not yet implemented.
  # The handler currently returns 501 M_UNRECOGNIZED — all scenarios that check the 200
  # response and replacement_room will fail until Core.UpgradeRoom is implemented.

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And kai creates a room named "upgrade-test-room"

  # AC1 — POST /rooms/{roomId}/upgrade by room owner returns 200 with replacement_room
  #
  # RED PHASE: currently returns 501 M_UNRECOGNIZED.
  # Will pass once handler calls Core.UpgradeRoom and returns 200.
  Scenario: RoomOwner_Upgrade_Returns200WithReplacementRoom
    When kai posts upgrade for room "upgrade-test-room" with new_version "10"
    Then the response status is 200
    And the response body contains "replacement_room"

  # AC2 — New room's m.room.create event contains predecessor with old room ID
  #
  # RED PHASE: currently fails because upgrade returns 501 and no new room is created.
  # Will pass once Core.UpgradeRoom creates the new room with a predecessor state event.
  Scenario: NewRoom_HasPredecessorInCreateEvent
    When kai posts upgrade for room "upgrade-test-room" with new_version "10"
    Then the response status is 200
    And kai calls GET /rooms/{newRoomId}/state/m.room.create
    Then the response status is 200
    And the response body contains "predecessor"

  # AC5 — Non-member attempting upgrade receives 403 M_FORBIDDEN
  #
  # RED PHASE: currently returns 501 (no gRPC call made, no power check).
  # Will pass once the handler calls Core.UpgradeRoom which enforces power levels.
  Scenario: NonMember_Upgrade_Returns403
    Given marie is authenticated via OIDC
    When marie posts upgrade for room "upgrade-test-room" with new_version "10"
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # AC4 — Old room members are invited to the new room after upgrade
  #
  # RED PHASE: currently fails because upgrade returns 501 and no invites are sent.
  # Will pass once Core.UpgradeRoom sends invitations to all old members.
  Scenario: OldMembers_InvitedToNewRoom_AfterUpgrade
    Given alex is authenticated via OIDC
    And alex joins the room
    When kai posts upgrade for room "upgrade-test-room" with new_version "10"
    Then the response status is 200
    And alex calls GET /sync and sees the new room in rooms.invite

  # AC6 — GET /capabilities returns room version "10" as default
  #
  # RED PHASE: currently returns {"default":"6","available":{"6":"stable"}}.
  # Will pass once main.go capabilities JSON is updated to include version "10".
  Scenario: Capabilities_IncludesRoomVersion10AsDefault
    When I call GET /_matrix/client/v3/capabilities
    Then the response status is 200
    And the response body contains "\"default\":\"10\""
    And the response body contains "\"10\":"

  # AC-9.8-3 (GAP-9-001) — State event copy order: m.room.join_rules is always last
  #
  # Story 9.16: This scenario was surfaced by the traceability matrix as a test gap.
  # It verifies the spec-mandated copy order (Matrix spec § 11.35.1):
  #   1. m.room.create + m.room.member (already present from room creation / join)
  #   2. Other state events incl. m.room.power_levels
  #   3. m.room.join_rules — ALWAYS LAST among copied events
  #
  # RED PHASE: Will fail if Core.copy_state_events/3 does NOT emit join_rules last,
  # or if GET /rooms/{newRoomId}/state does not return the events in emission order.
  # No production code must be modified — this test exercises existing behaviour.
  Scenario: StateEventCopyOrder_JoinRulesIsLast
    When kai sends PUT /rooms/{roomId}/state/m.room.join_rules with body {"join_rule":"invite"}
    And the response status is 200
    And kai posts upgrade for room "upgrade-test-room" with new_version "10"
    Then the response status is 200
    And kai calls GET /rooms/{newRoomId}/state
    And the response status is 200
    And the new room state contains "m.room.power_levels" before "m.room.join_rules"
    And the last copied state event type is "m.room.join_rules"
