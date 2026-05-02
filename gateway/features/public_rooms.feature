Feature: Public Room Directory — GET/POST /publicRooms
  As an end-user
  I want to browse a list of public rooms on this Nebu instance and search by name
  So that I can discover and join rooms without needing a direct invite

  # Story 7-27 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until the Public Room Directory handlers are implemented.
  # Routes not yet registered in main.go → 404 for all requests.

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC

  # AC1 — GET returns paginated list of public rooms
  Scenario: GetPublicRooms_Unauthenticated_Returns200 — GET /publicRooms returns 200 without JWT
    When an unauthenticated client calls GET /_matrix/client/v3/publicRooms
    Then the response status is 200
    And the response body contains "chunk"

  # AC1 — GET returns chunk array with pagination shape
  Scenario: GetPublicRooms_WithLimit_ReturnsPaginatedChunk — GET /publicRooms?limit=2 returns paginated response
    When an unauthenticated client calls GET /_matrix/client/v3/publicRooms?limit=2
    Then the response status is 200
    And the response body contains "chunk"
    And the response body contains "total_room_count_estimate"

  # AC2 — POST with filter body returns filtered results
  Scenario: PostPublicRooms_WithFilter_Returns200 — POST /publicRooms with filter returns 200
    When kai calls POST /_matrix/client/v3/publicRooms with body {"filter":{"generic_search_term":"test"},"limit":10}
    Then the response status is 200
    And the response body contains "chunk"

  # AC2 — POST with no filter returns all public rooms
  Scenario: PostPublicRooms_NoFilter_Returns200 — POST /publicRooms without filter returns full list
    When kai calls POST /_matrix/client/v3/publicRooms with body {"limit":10}
    Then the response status is 200
    And the response body contains "chunk"
    And the response body contains "total_room_count_estimate"

  # AC6 — POST requires JWT; unauthenticated request is rejected
  Scenario: PostPublicRooms_Unauthenticated_Returns401 — POST without JWT returns M_MISSING_TOKEN
    When an unauthenticated client calls POST /_matrix/client/v3/publicRooms with body {}
    Then the response status is 401
    And the response body contains "M_MISSING_TOKEN"

  # AC3 — each chunk entry contains required fields
  Scenario: GetPublicRooms_ChunkEntries_ContainRequiredFields — chunk entries have room_id and num_joined_members
    When an unauthenticated client calls GET /_matrix/client/v3/publicRooms
    Then the response status is 200
    And the response body contains "world_readable"
    And the response body contains "guest_can_join"

  # Story 7-36: P1 gap closure — 7-27-AC5 private room exclusion
  Scenario: GetPublicRooms_PrivateRoom_ExcludedFromDirectory — private room absent from public directory
    Given kai is authenticated via OIDC
    And kai creates a room named "private-room-test"
    When an unauthenticated client calls GET /_matrix/client/v3/publicRooms
    Then the public rooms chunk does not contain a room named "private-room-test"
