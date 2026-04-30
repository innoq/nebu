Feature: Account Data API — GET/PUT /user/{userId}/account_data/{type} and /user/{userId}/rooms/{roomId}/account_data/{type}
  As an end-user
  I want to store and retrieve per-room and global account data
  So that my Matrix client can persist configuration and preferences across devices

  # Story 7-24 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until AccountDataHandler is implemented
  # (existing stubs return 404/empty {} without persistence → GET scenarios fail).

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And kai creates a room named "account-data-test-room"

  # AC1+AC2 — PUT stores data; GET retrieves the same data (per-room)
  Scenario: PutGet_RoomAccountData — PUT stores and GET retrieves room account data
    When kai puts room account data type "m.fully_read" with body {"event_id":"$abc"} for the created room
    Then the response status is 200
    And the response body is "{}"
    When kai gets room account data type "m.fully_read" for the created room
    Then the response status is 200
    And the response body contains "event_id"
    And the response body contains "$abc"

  # AC2 — GET returns M_NOT_FOUND when no data exists
  Scenario: Get_RoomAccountData_NotFound — GET returns 404 when no data exists
    When kai gets room account data type "m.tag" for the created room
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC3 — userId mismatch returns M_FORBIDDEN
  Scenario: Put_RoomAccountData_Forbidden — userId mismatch returns 403
    Given alex is authenticated via OIDC
    When alex puts room account data type "m.tag" with body {"tags":{}} for user "@kai:nebu.test" in the created room
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # AC1+AC2 — PUT stores data; GET retrieves the same data (global)
  Scenario: PutGet_GlobalAccountData — PUT stores and GET retrieves global account data
    When kai puts global account data type "m.push_rules" with body {"global":{}}
    Then the response status is 200
    And the response body is "{}"
    When kai gets global account data type "m.push_rules"
    Then the response status is 200
    And the response body contains "global"

  # AC2 — global GET returns M_NOT_FOUND when no data exists
  Scenario: Get_GlobalAccountData_NotFound — global GET returns 404 when no data exists
    When kai gets global account data type "m.nonexistent_type"
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC6 — Upsert: second PUT with different content overwrites (last write wins)
  Scenario: Upsert_RoomAccountData — concurrent PUTs use upsert semantics
    When kai puts room account data type "m.tag" with body {"tags":{"m.favourite":{}}} for the created room
    Then the response status is 200
    When kai puts room account data type "m.tag" with body {"tags":{}} for the created room
    Then the response status is 200
    When kai gets room account data type "m.tag" for the created room
    Then the response status is 200
    And the response body contains "\"tags\":"
    And the response body does not contain "m.favourite"
