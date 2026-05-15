Feature: MSC2965 OIDC Discovery Endpoints — auth_issuer + auth_metadata
  As a Matrix client (e.g. Element Web)
  I want to retrieve the OIDC issuer and metadata from Nebu
  So that I can configure OIDC-native login without showing a "misconfigured server" error

  # Story 13-7 — ATDD: tests written FIRST (red phase), before implementation.
  # MSC2965 defines two unauthenticated discovery endpoints:
  #   GET /_matrix/client/unstable/org.matrix.msc2965/auth_issuer
  #   GET /_matrix/client/unstable/org.matrix.msc2965/auth_metadata
  # Matrix spec v1.15+ also stabilised these at:
  #   GET /_matrix/client/v1/auth_issuer
  #   GET /_matrix/client/v1/auth_metadata
  #
  # All scenarios MUST FAIL until:
  #   1. gateway/internal/matrix/oidc_discovery.go is created with AuthIssuerHandler + AuthMetadataHandler
  #   2. Both unstable and stable routes are registered in gateway/cmd/gateway/main.go
  #   3. The existing 404 stub for auth_metadata is replaced

  # AC1, AC4, AC7 — auth_issuer returns the configured OIDC issuer URL, no auth required
  Scenario: OIDCDiscovery_AuthIssuer_Unstable — unstable path returns configured issuer
    Given the docker compose stack is started
    When a client calls GET /_matrix/client/unstable/org.matrix.msc2965/auth_issuer without authentication
    Then the response status is 200
    And the response body contains "issuer"
    And the response body contains "keycloak"

  Scenario: OIDCDiscovery_AuthIssuer_Stable — stable v1 path returns configured issuer
    Given the docker compose stack is started
    When a client calls GET /_matrix/client/v1/auth_issuer without authentication
    Then the response status is 200
    And the response body contains "issuer"
    And the response body contains "keycloak"

  # AC2, AC4, AC7 — auth_metadata proxies the OIDC discovery document, no auth required
  Scenario: OIDCDiscovery_AuthMetadata_Unstable — unstable path returns OIDC discovery document
    Given the docker compose stack is started
    When a client calls GET /_matrix/client/unstable/org.matrix.msc2965/auth_metadata without authentication
    Then the response status is 200
    And the response body contains "issuer"
    And the response body contains "authorization_endpoint"
    And the response body contains "token_endpoint"

  Scenario: OIDCDiscovery_AuthMetadata_Stable — stable v1 path returns OIDC discovery document
    Given the docker compose stack is started
    When a client calls GET /_matrix/client/v1/auth_metadata without authentication
    Then the response status is 200
    And the response body contains "issuer"
    And the response body contains "authorization_endpoint"
    And the response body contains "token_endpoint"

  # AC4 — Both endpoints must work without an Authorization header
  Scenario: OIDCDiscovery_NoAuthRequired — both endpoints return 200 without Authorization header
    Given the docker compose stack is started
    When a client calls GET /_matrix/client/unstable/org.matrix.msc2965/auth_issuer without authentication
    Then the response status is 200
    When a client calls GET /_matrix/client/unstable/org.matrix.msc2965/auth_metadata without authentication
    Then the response status is 200
