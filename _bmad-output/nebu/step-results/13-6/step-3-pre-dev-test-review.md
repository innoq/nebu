# Step 3 — Pre-Dev Test Review: Story 13-6

## Tests Reviewed

### AT-CLUSTER-1 (ExUnit: room_server_clustering_test.exs)
- **Quality:** PASS — well-structured, deterministic poll loop
- **Issue found:** Monitor set AFTER kill (`:noproc` race condition) → FIXED in final test
- **Issue found:** Missing DBBehaviour callbacks in ClusterFakeDB → FIXED
- **Issue found:** Horde CRDT noise from async restart after `Process.exit(:kill)` → FIXED via `terminate_child` before kill

### AT-CLUSTER-2 (ExUnit: room_server_clustering_test.exs)
- **Quality:** PASS
- **Issue found:** Same monitor race + DBBehaviour callbacks as AT-CLUSTER-1 → FIXED
- **Verifies:** `send_event/5` returns `{:ok, event_id}` after crash/restart — covers AC2 fully

### Godog scenario (core_clustering.feature + core_clustering_steps_test.go)
- **Quality:** PASS — all steps implemented, `@scale` tag correctly skips in normal CI
- **Issue found:** `core1AndCore2AreConnectedInAHordeCluster` requires health endpoint to return `cluster_nodes` field → implemented in health.ex
- **Integration behavior:** Requires `NEBU_SCALE_TEST=true` + 2-core Docker Compose stack to run

## Verdict
No MAJOR gaps. All AC are covered by at least one test. Proceed to implementation.
