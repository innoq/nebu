Feature: State Event Type Whitelist — PUT /rooms/{roomId}/state/{eventType}
  As a system operator
  I want the gateway to validate state event types against a whitelist before forwarding to Core
  So that unknown or malformed event types cannot be injected into the system

  # Story 9-6 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until PutSetRoomState enforces the whitelist.

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And kai creates a room named "whitelist-test-room"

  # AC1 — m.room.name is whitelisted → request is forwarded (not rejected with 400)
  Scenario: WhitelistedType_mRoomName_NotRejected — whitelisted type passes gateway check
    When kai sends PUT /rooms/{roomId}/state/m.room.name with body {"name":"Renamed Room"}
    Then the response status is not 400
    And the response body does not contain "M_BAD_JSON"

  # AC2 — m.room.encryption is whitelisted → pass-through required by Matrix spec
  Scenario: WhitelistedType_mRoomEncryption_NotRejected — encryption type is not rejected at gateway
    When kai sends PUT /rooms/{roomId}/state/m.room.encryption with body {"algorithm":"m.megolm.v1.aes-sha2"}
    Then the response status is not 400
    And the response body does not contain "M_BAD_JSON"

  # AC3 — evil.custom.inject is NOT whitelisted → gateway returns 400 M_BAD_JSON
  Scenario: UnknownType_Rejected_400_M_BAD_JSON — unknown event type is rejected before Core
    When kai sends PUT /rooms/{roomId}/state/evil.custom.inject with body {"payload":"injected"}
    Then the response status is 400
    And the response body contains "M_BAD_JSON"
