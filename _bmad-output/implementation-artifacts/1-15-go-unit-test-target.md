# Story 1.15: Go Unit Test Target

Status: done

## Story

As a developer,
I want `make test-unit-go` to run all Go unit tests in a container,
so that CI can verify Go code correctness without requiring a local Go installation.

## Acceptance Criteria

1. **Container execution:** Given `Makefile` with a `test-unit-go` target, when `make test-unit-go` runs, then it executes `go test -race ./...` inside the Go build container defined by `DOCKER_GO`.

2. **Tests pass:** Given at least one unit test file exists (e.g., `gateway/internal/grpc/client_test.go`), when `make test-unit-go` runs, then it exits with code 0 and prints test results.

3. **Failing test detection:** Given a deliberately failing test, when `make test-unit-go` runs, then it exits with a non-zero code and prints the failing test name and package.

4. **Single source of truth:** Given `DOCKER_GO` variable in Makefile, when it is used in `test-unit-go`, then it references the same Go build image used for `make build-gateway` (`golang:1.26-alpine` — single source of truth for Go version).

## Tasks / Subtasks

- [x] Update `Makefile` `test-unit-go` target to use `-race` flag (AC: #1, #2, #3, #4)
  - [x] Change `go test ./...` to `go test -race ./...` in the `test-unit-go` shell command
  - [x] If the race detector fails due to missing CGO toolchain in Alpine, add `apk add --no-cache gcc musl-dev >/dev/null 2>&1 &&` before the `go test` call (see Dev Notes)
  - [x] Verify `DOCKER_GO` variable still points to `golang:1.26-alpine` (unchanged — do NOT modify the variable)
  - [x] Run `make test-unit-go` locally and confirm all 9 packages exit with code 0

## Dev Notes

### Scope: One-line Makefile change

This story is **a single-line change** in `Makefile`. No new Go source files, no test files, no gateway changes.

```makefile
# BEFORE (current state):
test-unit-go:
	$(DOCKER_GO) sh -c "cd gateway && go test ./..."

# AFTER:
test-unit-go:
	$(DOCKER_GO) sh -c "cd gateway && go test -race ./..."
```

**No new test files required.** 9 test packages already exist and pass (all verified as of story 1-14 completion).

### Existing Test Packages (pre-existing, all must continue to pass)

All 9 packages under `gateway/` (run as `go test -race ./...` from the `gateway/` directory):

| Package | Test File | What It Tests |
|---|---|---|
| `github.com/nebu/nebu/migrations` | `migrations/migrations_test.go` | Embedded FS contains expected SQL files |
| `github.com/nebu/nebu/internal/db` | `internal/db/db_test.go` + `serverconfig_test.go` | DB error handling, unreachable DB |
| `github.com/nebu/nebu/internal/middleware` | `internal/middleware/psk_test.go` | PSK auth middleware (valid/invalid tokens) |
| `github.com/nebu/nebu/internal/registry` | `internal/registry/registry_test.go` | Node registry CRUD + HTTP handler |
| `github.com/nebu/nebu/internal/grpc` | `internal/grpc/client_test.go` | gRPC client skeleton (lazy dial, stub returns) |
| `github.com/nebu/nebu/internal/health` | `internal/health/health_test.go` | Health + readiness handlers (fakeCore) |
| `github.com/nebu/nebu/internal/admin` | `internal/admin/metrics_test.go` | Prometheus metrics + HTTP middleware counter |
| `github.com/nebu/nebu/internal/config` | `internal/config/config_test.go` | Env var loading, TLS fields, defaults |

### Race Detector + Alpine: Potential CGO Issue

**Background:** Go's race detector on Linux requires CGO and a C compiler (TSan runtime). `golang:1.26-alpine` does **NOT** include `gcc` by default.

**Step 1 — Try the direct approach first:**
```makefile
test-unit-go:
	$(DOCKER_GO) sh -c "cd gateway && go test -race ./..."
```
Run `make test-unit-go`. If it succeeds → done.

**Step 2 — If you see a CGO-related error** (e.g., `race detection not supported on linux/amd64 with CGO disabled` or `cgo: C compiler not found`), apply this fix:
```makefile
test-unit-go:
	$(DOCKER_GO) sh -c "apk add --no-cache gcc musl-dev >/dev/null 2>&1 && cd gateway && go test -race ./..."
```

This installs the C toolchain inside the ephemeral Alpine container before running tests. Stdout is suppressed (`>/dev/null 2>&1`) so test output remains clean.

**Do NOT** introduce a separate `DOCKER_GO_TEST` variable — AC4 requires `DOCKER_GO` to be the source of truth.

### Current Makefile Context (what you are editing)

File: `Makefile` (project root)

```makefile
DOCKER_GO     = docker run --rm -v $(PWD):/workspace -w /workspace golang:1.26-alpine
DOCKER_ELIXIR = docker run --rm -v $(PWD):/workspace -w /workspace elixir:1.19-alpine
DOCKER_BUF    = docker run --rm -v $(PWD):/workspace -w /workspace bufbuild/buf

.PHONY: build-gateway build-core dev setup test-unit-go test-unit-elixir test-integration proto gen-api

## build-gateway: Build the Go Gateway Docker image (multi-stage)
build-gateway:
	docker build -t nebu-gateway:dev ./gateway

...

## test-unit-go: Run Go unit tests inside container
test-unit-go:
	$(DOCKER_GO) sh -c "cd gateway && go test ./..."   ← CHANGE THIS LINE ONLY
```

Only the shell command string changes. `DOCKER_GO` variable is **unchanged**.

### AC4 Verification: Single Source of Truth

`DOCKER_GO` uses `golang:1.26-alpine`. `build-gateway` calls `docker build ./gateway` which uses `gateway/Dockerfile`:
```dockerfile
FROM golang:1.26-alpine AS builder   ← same version as DOCKER_GO
```
AC4 is **already satisfied** — both reference `golang:1.26-alpine`. No code change needed for AC4.

### Project Structure Notes

- Only file modified: `Makefile` (root)
- `gateway/go.mod`: module `github.com/nebu/nebu`, Go 1.26 — no new dependencies
- `-race` flag is additive — all 9 packages that pass without `-race` should pass with it
- Internal mux (`:8008`), public server (`:8080`/`:8443`), gRPC client — all unchanged

### References

- [Source: epics.md#Story-1.15] Full AC and user story
- [Source: architecture.md#G6] CI/CD: unit tests via `go test` always run; `-race` flag per epics AC
- [Source: architecture.md#Makefile] DOCKER_GO definition and test-unit-go pattern
- [Source: Makefile:test-unit-go] Current implementation (line ~30): `go test ./...` → change to `go test -race ./...`
- [Source: gateway/Dockerfile:1] `FROM golang:1.26-alpine AS builder` — confirms DOCKER_GO version alignment

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

- Direct `go test -race ./...` failed: `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1` — as predicted in Dev Notes.
- Applied CGO fix: prepend `apk add --no-cache gcc musl-dev >/dev/null 2>&1 &&` to the shell command.
- Second run: all 9 packages passed with exit code 0.

### Completion Notes List

- Modified `Makefile` `test-unit-go` target: added `-race` flag and CGO toolchain install via `apk add gcc musl-dev` (stdout suppressed).
- `DOCKER_GO` variable unchanged — still `golang:1.26-alpine` (AC4 satisfied).
- All 9 Go test packages pass with `go test -race ./...`: admin, config, db, grpc, health, middleware, registry, migrations.

### File List

- `Makefile`

## Change Log

- 2026-03-25: Added `-race` flag to `test-unit-go` Makefile target; added CGO toolchain install for Alpine race detector support (Story 1-15).
- 2026-03-25: Code review (claude-opus-4-6): Changed `apk add` output suppression from `>/dev/null 2>&1` to `-q` flag for consistency with `proto` target and to preserve error visibility.
