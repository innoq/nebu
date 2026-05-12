Feature: Room Version Upgrade via Element Web UI
  As a room owner in Element Web
  I want to upgrade my room to the recommended chat version
  So that I am moved to the new room without seeing an error dialog

  # Story 9-27 AC6 — Element Web shows Room Upgrade successfully
  #
  # Fixes in upgrade_room/2 (story 9-27):
  #   1. Bare `:ok =` pattern matches replaced with case/GRPC.RPCError error handling
  #   2. m.room.create emit return value now checked
  #   3. archive_room_atomic/1 called after tombstone (Matrix spec §11.35.1)
  #
  # Test uses real OIDC login via Dex (Authorization Code + PKCE) — no ROPC shortcuts.
  # Alex is the room owner (Power Level 100) in this scenario — this matches the default
  # credentials fixture (NEBU_USERS.alex) so no fixture override is needed.

  Background:
    Given the Nebu stack is running
    And alex is logged in via Element Web

  @ac6-room-upgrade
  Scenario: RoomOwner_UpgradesRoom_NoErrorDialog
    Given alex has created a room named "e2e-upgrade-template"
    When alex opens Room Settings for "e2e-upgrade-template"
    And alex upgrades the room to the recommended version
    Then alex sees the new room without an error
    And alex does not see an error dialog
