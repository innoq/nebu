---
security_review: not-needed
---

# Story 8.11: Service Startup Resilience + DinD-free Integration Test (GitLab CI `services:`)

Status: ready-for-dev

## Story

**As a** maintainer running CI on opencode's Kubernetes runners (no DinD, no Podman),
**I want** the gateway to retry its DB connection at startup and the integration test job to use GitLab CI `services:` instead of docker-compose,
**so that** integration tests run on plain K8s runners without a privileged Docker daemon.

**Size:** M

---

## Background

### Why This Story Exists

opencode's GitLab CI runners are plain Kubernetes pods. Both Docker-in-Docker and Podman are blocked (`cannot clone: Operation not permitted` — `CLONE_NEWUSER` syscall denied by seccomp). The current `integration-test` job requires DinD and is therefore `when: manual` as of the change on 2026-05-03.

GitLab CI `services:` starts each service as a sidecar container in the same K8s pod — no daemon, no privilege escalation needed. The blocker: all services start in parallel, without `depends_on` ordering. The gateway's `RunMigrations` calls fail immediately if postgres is not yet ready, causing the gateway container to exit before tests can run.

### Current Architecture (what changes)

- `gateway/internal/db/db.go` — `RunMigrations` opens a connection and calls `m.Up()` with no retry
- `gateway/cmd/gateway/main.go:107` — calls `db.RunMigrations(cfg.DBURL, cfg.DBURLMigrate)` and `os.Exit(1)` on error
- `gateway/internal/grpc/client.go:70` — already resilient: lazy non-blocking dial + background 5s probe, logs a warning if core is not ready. **No change needed here.**
- `docker-compose.yml` — `gateway depends_on: postgres: condition: service_healthy` provides ordering in local dev. This path is preserved.

### What Does NOT Change

- `make test-integration` (local docker-compose flow) remains unchanged
- `integration-test` DinD job stays `when: manual` (for runners that do have DinD)
- The `RunMigrations` function signature is backward-compatible — unit tests in `db_test.go` that test immediate failure on bad URL continue to pass

---

## Acceptance Criteria

1. **Gateway DB retry** — `db.WaitAndRunMigrations(ctx context.Context, dbURL string, migrateURL ...string) error` is added to `gateway/internal/db/db.go`. It retries the DB connection every 2 seconds until the context deadline or success. `RunMigrations` is kept as-is (unit tests must still pass). `main.go` is updated to call `WaitAndRunMigrations` with a `30s` timeout context instead of the direct `RunMigrations` call.

2. **Unit tests for retry** — `db_test.go` adds:
   - `TestWaitAndRunMigrations_ReturnsErrorAfterTimeout` — context with 1ms timeout returns error for unreachable DB
   - `TestWaitAndRunMigrations_ContextCancelledImmediately` — cancelled context returns immediately without retrying

3. **`scripts/wait-for-stack.sh`** — executable shell script. Polls until all four services respond successfully (or fails after 120s):
   - postgres: `pg_isready -h postgres -U nebu -d nebu` (or TCP probe if `pg_isready` unavailable)
   - dex: `GET http://dex:5556/dex/.well-known/openid-configuration` → HTTP 200
   - core: `GET http://core:4000/health` → HTTP 200
   - gateway: `GET http://gateway:8008/health` → HTTP 200
   Logs each service's status per check. Exits 0 on success, 1 on timeout.

4. **`integration-test-k8s` job** in `.gitlab-ci.yml` — stage: `integration`, no `tags:`, no DinD. Uses `services:` for postgres, dex, core, gateway (pre-built registry images for core/gateway, stock images for postgres/dex). `before_script` runs `scripts/wait-for-stack.sh`. `script` runs the same `go test -v -tags integration ./test/integration/...` as the DinD job. Rules: `when: manual`, `allow_failure: true` (same as the DinD job — test run before making automatic).

5. **Existing `integration-test` DinD job unchanged** — it still runs `make test-integration-ci` and is `when: manual` / `allow_failure: true`. Neither job is made automatic in this story.

6. **`make test-integration` still works** — the docker-compose path (`db.RunMigrations` is still called via the gateway container's startup in docker-compose, which has `depends_on: postgres: condition: service_healthy`) is unaffected.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `TestWaitAndRunMigrations_ReturnsErrorAfterTimeout` — Go unit test (httptest/db_test.go)
   - Given: unreachable DB URL, context with 1ms deadline
   - When: `WaitAndRunMigrations(ctx, badURL)` is called
   - Then: returns non-nil error within the deadline; error message mentions "context deadline exceeded" or "timeout"

2. `TestWaitAndRunMigrations_ContextCancelledImmediately` — Go unit test
   - Given: already-cancelled context
   - When: `WaitAndRunMigrations(cancelledCtx, badURL)` is called
   - Then: returns error immediately (< 100ms), no retry loops

3. `scripts/wait-for-stack.sh` integration smoke — manual / `make test-integration-k8s` (no Godog required)
   - Given: `integration-test-k8s` CI job running with `services:` sidecars
   - When: `scripts/wait-for-stack.sh` is called as `before_script`
   - Then: script exits 0 before the 120s timeout, all four health checks pass

---

## Tasks / Subtasks

- [ ] Task 1: Add `WaitAndRunMigrations` to `gateway/internal/db/db.go` (AC: #1)
  - [ ] 1.1 Write failing unit tests `TestWaitAndRunMigrations_*` in `db_test.go` first
  - [ ] 1.2 Implement `WaitAndRunMigrations` with 2s retry loop, context cancellation
  - [ ] 1.3 Verify `RunMigrations` tests still pass (backward-compat check)

- [ ] Task 2: Update `gateway/cmd/gateway/main.go` (AC: #1, #6)
  - [ ] 2.1 Replace `db.RunMigrations(cfg.DBURL, cfg.DBURLMigrate)` call with `db.WaitAndRunMigrations(ctx, cfg.DBURL, cfg.DBURLMigrate)` where `ctx` has 30s deadline
  - [ ] 2.2 Confirm existing `make test-unit-go` passes

- [ ] Task 3: Create `scripts/wait-for-stack.sh` (AC: #3)
  - [ ] 3.1 Implement polling loop (2s sleep between checks, 120s total timeout)
  - [ ] 3.2 Use `wget` or `curl` for HTTP probes (available in alpine/debian CI images)
  - [ ] 3.3 Make script executable (`chmod +x`)

- [ ] Task 4: Add `integration-test-k8s` job to `.gitlab-ci.yml` (AC: #4, #5)
  - [ ] 4.1 Add `services:` block: postgres:16-alpine, `${CI_REGISTRY_IMAGE}/ci-dex:v2.45.1` (alias: dex), core, gateway (registry images)
  - [ ] 4.2 Set environment variables matching `docker-compose.yml` values (DB URLs, OIDC config, secrets)
  - [ ] 4.3 Add `before_script: apk add postgresql-client && scripts/wait-for-stack.sh`
  - [ ] 4.4 Set `when: manual`, `allow_failure: true`, no `tags:`
  - [ ] 4.5 Verify `build-ci-dex` Kaniko job has been run at least once before first test pipeline

---

## Dev Notes

### Files to Touch

| File | Change |
|---|---|
| `gateway/internal/db/db.go` | Add `WaitAndRunMigrations` |
| `gateway/internal/db/db_test.go` | Add 2 unit tests for retry |
| `gateway/cmd/gateway/main.go:107` | Call `WaitAndRunMigrations` with 30s ctx |
| `scripts/wait-for-stack.sh` | New file — stack readiness poller |
| `.gitlab-ci.yml` | Add `integration-test-k8s` job |

### `WaitAndRunMigrations` Implementation Sketch

```go
// WaitAndRunMigrations retries RunMigrations every 2s until ctx is done or success.
// Use this at gateway startup so the container survives a slow postgres boot in
// environments where start ordering is not guaranteed (e.g. GitLab CI services:).
func WaitAndRunMigrations(ctx context.Context, dbURL string, migrateURL ...string) error {
    for {
        err := RunMigrations(dbURL, migrateURL...)
        if err == nil {
            return nil
        }
        select {
        case <-ctx.Done():
            return fmt.Errorf("waiting for database: %w (last migration error: %v)", ctx.Err(), err)
        case <-time.After(2 * time.Second):
        }
    }
}
```

### `wait-for-stack.sh` Environment Requirements

The script runs inside `golang:1.26-alpine`. Alpine has `wget` but not `pg_isready` (that's in `postgresql-client`). Use TCP probe for postgres or install `postgresql-client` in the before_script. Preferred: add `apk add postgresql-client` in the CI job's `before_script` (same line as `apk add make openssl gcc musl-dev`).

### GitLab CI `services:` — Important Notes

- Services are accessible by their `alias:` name as hostname
- Each service needs correct env vars; for postgres use `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`
- The `nebu_migrate` and `nebu_app` roles are created by `dev/postgres/init/` scripts — these only run on empty volumes. In CI (no volumes), the init scripts do NOT run. The `integration-test-k8s` job must therefore set `NEBU_DB_URL` and `NEBU_DB_URL_MIGRATE` to the single superuser role (`POSTGRES_USER`) or handle role creation in `before_script`.
- Alternative (simpler): use `POSTGRES_USER=nebu_app` and `POSTGRES_PASSWORD=nebu_app_dev_pw` — single role, skip the nebu_migrate split. The migration role split is a security hardening for production deployments; in CI it is not required.
- `NEBU_INTERNAL_SECRET_FILE`: the `make setup` target generates `.secrets/internal_secret`. In the `services:` job this file is not available. Inject `NEBU_INTERNAL_SECRET` directly as env var and verify the gateway/core accept the secret via env var (or generate it in `before_script: openssl rand -hex 32 > .secrets/internal_secret`).

### `dex` Configuration in CI

The dex service needs a config YAML. In the DinD job, `docker-compose.yml` mounts `./dev/dex/config.yaml`. In `services:` mode there is no volume mount from the project directory.

**Solution (already done):** `docker/Dockerfile.dex-ci` bakes `dev/dex/config.yaml` into the image — same config docker-compose uses. A Kaniko build job `build-ci-dex` pushes it to `${CI_REGISTRY_IMAGE}/ci-dex:v2.45.1`. The `integration-test-k8s` job uses this image as the dex service entry (alias: `dex`).

The config's `issuer: http://dex:5556/dex` uses hostname `dex`, which matches the `alias: dex` in the `services:` block — no config change needed.

The `build-ci-dex` job rebuilds automatically when `docker/Dockerfile.dex-ci` or `dev/dex/config.yaml` changes. Run it manually once before the first `integration-test-k8s` pipeline run.

### Security Note

`security_review: not-needed` — no auth/crypto/SQL injection surface touched. The retry loop in `WaitAndRunMigrations` uses the same DB URL as `RunMigrations`; no new credential handling.

---

## Project Structure Notes

- `scripts/` — shell script utilities, already used for `scan-secrets.sh`, `verify-*.sh`. `wait-for-stack.sh` fits here.
- `gateway/internal/db/` — `db.go` and `db_test.go` are the only files to touch in the db package.
- `.gitlab-ci.yml` — the new job goes after the existing `integration-test` job in the `integration` stage section.

---

## References

- [Source: gateway/internal/db/db.go#RunMigrations] — current migration runner, no retry
- [Source: gateway/cmd/gateway/main.go:107] — `os.Exit(1)` on migration failure
- [Source: gateway/internal/grpc/client.go:70] — gRPC client already resilient (lazy dial)
- [Source: docker-compose.yml] — service dependency model with healthchecks
- [Source: .gitlab-ci.yml] — existing `integration-test` (DinD, `when: manual`) and `integration-test-podman` (Podman, `when: manual`)
- [Source: _bmad-output/planning-artifacts/epics.md#Story 8.6] — Dual CI story; story 8.6 AC requires `make test-integration` to run green; this story provides the K8s-native path

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List
