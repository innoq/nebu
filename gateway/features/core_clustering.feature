Feature: Core node failover preserves room state

  # Story 13-6: Horizontal Scaling Validation — Core Clustering (libcluster + Horde)
  #
  # AC3: Godog scenario passes against the 2-core scale stack.
  # AC2: Message delivery after failover succeeds (HTTP 200 + event_id).
  # AC5: The docker-compose.scale.yml defines core2 connected to core1 via
  #      CLUSTER_NODES env var or libcluster autodiscovery.
  #
  # RED PHASE: This scenario will FAIL until:
  #   1. docker-compose.scale.yml adds core2 service with correct env vars.
  #   2. libcluster is configured in Core runtime.exs to form a 2-node cluster.
  #   3. The step definition in gateway/test/integration/core_clustering_steps_test.go
  #      is implemented with Docker stop + room availability polling.
  #
  # Run against the 2-core scale stack with:
  #   docker compose -f docker-compose.yml -f docker-compose.scale.yml up -d
  #   go test -tags=integration ./gateway/test/integration/... -run TestIntegrationSuite
  #
  # The @scale tag is used to skip this scenario when the 2-core stack is not running.

  @scale
  Scenario: Core node failover preserves room state
    Given the 2-core docker compose stack is running
    And a room exists and a message has been sent
    When core instance 1 is stopped
    Then a new message can be sent to the room within 10 seconds
    And the message is accepted with HTTP 200 and an event_id

  @scale
  Scenario: Two core instances form a cluster on startup
    Given the 2-core docker compose stack is running
    Then core1 and core2 are connected in a Horde cluster
