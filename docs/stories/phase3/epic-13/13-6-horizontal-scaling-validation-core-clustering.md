---
status: done
epic: 13
story: 6
security_review: not-needed
matrix: false
ui: false
---

# Story 13.6: Horizontal Scaling Validation — Core Clustering (libcluster + Horde)

Status: ready-for-dev

## Story

As a system operator,
I want to validate that 2 Core nodes in a Horde cluster correctly hand off Room GenServers when one node is terminated,
So that horizontal Core scaling is proven safe before production use.

**Size:** M

---

## Acceptance Criteria

**AC1 — Room GenServer migrates on node termination:**
Given a 2-core Docker Compose configuration (`docker-compose.scale.yml`),
When both cores are running and a Room GenServer is active on Core 1,
Then `docker stop nebu-core-1` causes the Room GenServer to migrate to Core 2 within 10 seconds

**AC2 — Message delivery after failover:**
Given the Room GenServer is migrated to Core 2,
When a new message is sent to that room via the Gateway,
Then the message is accepted and delivered correctly (no data loss — event appears in room history)

**AC3 — Godog scenario passes:**
Given `gateway/features/core_clustering.feature`,
When `make test-integration` runs against the 2-core stack,
Then the scenario "Core node failover preserves room state" passes

**AC4 — libcluster forms cluster on K8s:**
Given `deploy/helm/nebu/values.yaml` with `core.replicaCount: 2`,
When `helm template` renders the core Deployment,
Then the template shows libcluster Kubernetes DNS strategy configuration (env vars: `RELEASE_DISTRIBUTION=name`, `RELEASE_NODE`, `CLUSTER_STRATEGY=kubernetes`)

**AC5 — 2-core Docker Compose configuration:**
Given `docker-compose.scale.yml` (created in story 13.5),
When it defines a second Core service (`core2`),
Then `core2` connects to `core1` via `CLUSTER_NODES` env var or libcluster autodiscovery via Docker DNS

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Godog scenario: "Core node failover preserves room state" — FAILING before implementation**
- Given: `gateway/features/core_clustering.feature` does NOT exist
- When: `make test-integration` runs
- Then: scenario is missing → fail (or exit non-zero)
- [After implementation: scenario passes]

**2. Room GenServer crash/restart test — ExUnit**
- Given: a room is active on Core 1 in a 2-node Horde cluster (test setup via ExUnit)
- When: `Process.exit(room_pid, :kill)` is called
- Then: the room is restarted on the surviving node within 5 seconds and `Horde.Registry.lookup/2` returns a new pid

**3. Message delivery after failover**
- Given: the Godog scenario step "When a message is sent after Core 1 is stopped"
- When: Gateway routes the message
- Then: HTTP 200 response with an event_id (no 5xx error)

Crash/restart test is mandatory (GenServer state story with Horde supervision).
