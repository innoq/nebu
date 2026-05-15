# Step 6 — Code Review: Story 13-6

## Reviewer: Code Review Agent
## Date: 2026-05-13
## Story: 13-6 — Core Clustering Validation (libcluster + Horde)

---

## Test Architecture Review (per Gate 3 mandate)

| AC | Test | Coverage |
|---|---|---|
| AC1: Room GenServer migrates to surviving node after peer termination | AT-CLUSTER-1 (`room_server_clustering_test.exs`) | ✓ |
| AC2: Message delivery after failover with no data loss | AT-CLUSTER-2 (`room_server_clustering_test.exs`) | ✓ |
| AC3: libcluster dependency wired and compiles | Verified: `mix.exs` + `mix.lock` (libcluster 3.5.0) | ✓ |
| AC4: Helm chart injects clustering env vars when `core.replicaCount > 1` | `core-deployment.yaml` conditional block | ✓ |
| AC5: Docker Compose scale config for local 2-core stack | `docker-compose.scale.yml` `core2` service | ✓ |

All ACs covered. No gaps.

---

## Code Quality Findings

### MINOR findings (auto-fixed during implementation)

1. **`await_new_pid/3` unused default argument** — `--warnings-as-errors` blocked test run. Fixed by removing `\\ 5_000` default (function was private, default never used from outside).

2. **Monitor-after-kill `:noproc` race in tests** — Monitor set after `Process.exit(:kill)`. Fixed by establishing monitor before kill in early test design, then superseded by `terminate_child` approach.

3. **Horde CRDT async restart interference** — `Process.exit(:kill)` without prior `terminate_child` caused Horde to queue async restarts; after `on_exit` removed fake DB module, restarts failed and flooded Horde's message queue, causing `power_level_enforcement_test` to timeout. Fixed by calling `Horde.DynamicSupervisor.terminate_child/2` BEFORE `Process.exit/2` in all clustering tests.

4. **`MY_POD_IP` env var ordering in Helm template** — `RELEASE_NODE: "nebu@$(MY_POD_IP)"` referenced `MY_POD_IP` before it was defined in the env list. Fixed by placing `MY_POD_IP` (fieldRef) first.

5. **`ClusterFakeDB` missing DBBehaviour callbacks** — ~12 callbacks absent. Fixed by adding all missing stubs.

6. **`System.monotonic_time/1` in guard clause** — Not valid in guards. Fixed by moving time check into function body.

### No MAJOR findings.

---

## Architecture Assessment

**libcluster integration:** Correct. `Cluster.Supervisor` added to `event_dispatcher` supervision tree. Topologies configured via `runtime.exs` based on `CLUSTER_STRATEGY` env var. Single-node deployments (empty/unset) unaffected.

**Horde supervision:** `:transient` restart strategy for Room GenServers is appropriate. Horde's CRDT-based registry ensures rooms are globally registered across nodes.

**Kubernetes deployment:** Headless Service (`core-headless-service.yaml`) correctly enables libcluster DNS discovery. Conditional rendering on `core.replicaCount > 1` prevents unnecessary resources in single-replica deployments.

**Health endpoint:** `cluster_nodes` field added to health response — required by Godog integration scenario `core1AndCore2AreConnectedInAHordeCluster`.

**Test design:** `@scale` Gherkin tag correctly gates integration tests behind `NEBU_SCALE_TEST=true`. Normal CI (unit tests only) unaffected.

---

## Verdict

**APPROVED — no MAJOR findings. All MINOR issues resolved during implementation.**

85 ExUnit tests, 0 failures. All Go packages pass.
