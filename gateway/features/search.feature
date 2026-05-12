Feature: Search — POST /_matrix/client/v3/search E2E
  As a developer
  I want to verify the full search flow including auth and membership scoping
  So that CI catches regressions in the search API

  # Story 11.6 — ATDD: tests written FIRST (red phase), before step wiring.
  # The handler is implemented (Story 11.4) and rate limiter is wired (Story 11.5).
  # These scenarios verify the full integration path end-to-end.

  # AC1 — Happy path: search finds a sent message
  Scenario: Happy path search finds a sent message
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And kai creates a room named "search-e2e-room-11-6"
    And kai sends the message "findme-unique-11-6" to the room
    When kai calls POST /search with term "findme-unique-11-6"
    Then the response status is 200
    And the search results contain a non-zero rank
    And the search result content body contains "findme-unique-11-6"

  # AC2 — Auth enforcement: no token returns 401
  Scenario: Unauthenticated search returns 401
    Given the docker compose stack is started
    When an unauthenticated client calls POST /search with term "findme-unique-11-6"
    Then the response status is 401
    And the response body has errcode "M_UNKNOWN_TOKEN"

  # AC3 — Membership scoping: non-member gets zero results (NOT 403)
  # The SQL membership subquery enforces access at DB level — the Gateway returns 200 + empty.
  Scenario: Non-member search returns zero results not 403
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    And kai creates a private search room without inviting alex
    And kai sends the message "member-only-content-11-6" to the private search room
    When alex calls POST /search with term "member-only-content-11-6"
    Then the response status is 200
    And the search result count is 0
    And the response body does not contain "member-only-content-11-6"

  # AC4 — Input validation: empty search_term returns 400
  Scenario: Empty search_term returns 400
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    When kai calls POST /search with an empty search term
    Then the response status is 400
    And the response body has errcode "M_INVALID_PARAM"

  # AC5 — Rate limit: 11th request in one minute returns 429
  # Uses marieAccessToken to avoid cross-scenario bucket contamination from kaiAccessToken.
  # The NewUserRateLimiter (Story 11.5) starts with Burst=10: first 10 pass, 11th is blocked.
  Scenario: Rate limit blocks 11th request in one minute
    Given the docker compose stack is started
    And marie is authenticated via OIDC
    When marie sends 10 consecutive POST /search requests
    And marie sends one more POST /search request
    Then the response status is 429
    And the response body has errcode "M_LIMIT_EXCEEDED"
    And the response body contains "retry_after_ms"
    And the response header "Retry-After" is present
