# Story 1.16: Elixir Unit Test Target

Status: done

## Story

As a developer,
I want `make test-unit-elixir` to run all Elixir unit tests in a container,
so that CI can verify Elixir code correctness without requiring a local Elixir installation.

## Acceptance Criteria

1. **Container execution:** Given `Makefile` with a `test-unit-elixir` target, when `make test-unit-elixir` runs, then it executes `mix test --warnings-as-errors` inside the Elixir build container defined by `DOCKER_ELIXIR`, from the `core/` directory.

2. **Tests pass:** Given all 6 umbrella apps have test files, when `make test-unit-elixir` runs, then it exits with code 0 and prints test results (e.g., `"X tests, 0 failures"`).

3. **Failing test detection:** Given a deliberately failing Elixir test, when `make test-unit-elixir` runs, then it exits with a non-zero code and prints the failing test module and line number.

4. **Single source of truth:** Given `DOCKER_ELIXIR` variable in Makefile, when it is used in `test-unit-elixir`, then it references the same Elixir build image used for `make build-core` (`elixir:1.19-alpine` — single source of truth for Elixir version).

## Tasks / Subtasks

- [x] Update `Makefile` `test-unit-elixir` target to add `mix deps.get` and `--warnings-as-errors` (AC: #1, #2, #3, #4)
  - [x] Add `mix deps.get` before `mix test` (required so the ephemeral container can resolve dependencies)
  - [x] Add `--warnings-as-errors` flag to `mix test`
  - [x] Verify `DOCKER_ELIXIR` variable still points to `elixir:1.19-alpine` (unchanged — do NOT modify)
  - [x] Run `make test-unit-elixir` locally and confirm all tests exit with code 0
  - [x] If any compiler warnings surface with `--warnings-as-errors`, fix the warnings in the relevant module (not in test files — in production code)

## Dev Notes

### Scope: Makefile change + potential warning fixes

The core change is a **one-line Makefile update**. A secondary concern is whether `--warnings-as-errors` exposes any pre-existing compiler warnings that need fixing.

```makefile
# BEFORE (current state):
test-unit-elixir:
	$(DOCKER_ELIXIR) sh -c "cd core && mix local.hex --force && mix test"

# AFTER:
test-unit-elixir:
	$(DOCKER_ELIXIR) sh -c "cd core && mix local.hex --force && mix deps.get && mix test --warnings-as-errors"
```

**Why `mix deps.get`?** The `DOCKER_ELIXIR` container starts fresh with no deps cache. Without `mix deps.get`, `mix test` fails trying to compile modules that reference external libraries (e.g., `:plug`, `:cowboy` used by `event_dispatcher`). Compare with `build-core` which already includes `mix deps.get`.

### Existing Test Files (pre-existing, all must continue to pass)

8 test files across the 6 umbrella apps in `core/apps/`:

| App | Test File | What It Tests |
|---|---|---|
| `event_dispatcher` | `test/nebu_event_test.exs` | Placeholder: app starts |
| `event_dispatcher` | `test/nebu/health_test.exs` | `Nebu.Health.check/0` — 16 tests (UP/DEGRADED/DOWN status logic) |
| `event_dispatcher` | `test/nebu/node_registration_test.exs` | `Nebu.NodeRegistration.register_with_gateway/1` — 3 tests (PSK errors, unreachable gateway) |
| `permissions` | `test/nebu_permissions_test.exs` | Placeholder: app starts |
| `presence` | `test/nebu_presence_test.exs` | Placeholder: app starts |
| `room_manager` | `test/nebu_room_test.exs` | Placeholder: app starts |
| `session_manager` | `test/nebu_session_test.exs` | Placeholder: app starts |
| `signature` | `test/nebu_signature_test.exs` | Placeholder: app starts |

### Handling `--warnings-as-errors`

`mix test --warnings-as-errors` causes mix to fail with exit code 1 if compilation emits any warnings (unused variables, deprecated calls, unresolved module attributes, etc.).

**Step 1 — Try the direct approach first:**
```makefile
test-unit-elixir:
	$(DOCKER_ELIXIR) sh -c "cd core && mix local.hex --force && mix deps.get && mix test --warnings-as-errors"
```
Run `make test-unit-elixir`. If it exits with code 0 → done.

**Step 2 — If warnings surface**, fix them in the production source, NOT in test files. Common patterns to fix:
- Unused variable: rename with leading `_` (e.g., `_unused`)
- Unused alias: remove the `alias` line
- Deprecated function: use the replacement function documented in the compiler warning

**Do NOT remove `--warnings-as-errors`** — it is an explicit acceptance criterion.

### AC4 Verification: Single Source of Truth

`DOCKER_ELIXIR` is already `elixir:1.19-alpine`. `build-core` uses the same variable:
```makefile
DOCKER_ELIXIR = docker run --rm -v $(PWD):/workspace -w /workspace elixir:1.19-alpine

build-core:
	$(DOCKER_ELIXIR) sh -c "cd core && mix local.hex --force && mix deps.get && mix compile"
```
AC4 is **already satisfied** — no code change needed for this criterion.

### Current Makefile Context (what you are editing)

File: `Makefile` (project root)

```makefile
DOCKER_GO     = docker run --rm -v $(PWD):/workspace -w /workspace golang:1.26-alpine
DOCKER_ELIXIR = docker run --rm -v $(PWD):/workspace -w /workspace elixir:1.19-alpine
DOCKER_BUF    = docker run --rm -v $(PWD):/workspace -w /workspace bufbuild/buf

...

## test-unit-elixir: Run Elixir unit tests inside container
test-unit-elixir:
	$(DOCKER_ELIXIR) sh -c "cd core && mix local.hex --force && mix test"   ← CHANGE THIS LINE
```

Only the shell command string changes. `DOCKER_ELIXIR` variable is **unchanged**.

### Parallel with Story 1-15 (Go test target)

Story 1-15 made the same style change for Go (`go test ./...` → `go test -race ./...`). That story also needed an extra tool install (`apk add gcc musl-dev`) due to Alpine constraints. For Elixir, the equivalent constraint is `mix deps.get` (no pre-installed deps in the ephemeral container). No additional Alpine packages are needed.

### Project Structure Notes

- Only file modified: `Makefile` (root), plus possibly warning fixes in `core/apps/*/lib/` modules
- `core/mix.exs`: umbrella project — `mix test` from `core/` runs all 6 apps automatically
- No new test files required — 8 test files already exist
- `--warnings-as-errors` is additive to `mix test` — same tests run, just stricter exit behavior

### References

- [Source: epics.md#Story-1.16] Full AC and user story
- [Source: architecture.md#Build-Container-Strategie] `DOCKER_ELIXIR` definition and test-unit-elixir pattern
- [Source: architecture.md#Testing-Patterns] ExUnit `describe`-Blöcke pattern
- [Source: Makefile:test-unit-elixir] Current implementation: `mix test` → change to `mix deps.get && mix test --warnings-as-errors`
- [Source: Makefile:build-core] `mix local.hex --force && mix deps.get && mix compile` — confirms `mix deps.get` is required in ephemeral container
- [Source: 1-15-go-unit-test-target.md] Parallel story: same pattern for Go test target

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

None — implementation was straightforward. `mix test --warnings-as-errors` passed on first run with no compiler warnings.

### Completion Notes List

- Updated `test-unit-elixir` Makefile target: added `mix deps.get` (required for ephemeral container dependency resolution) and `--warnings-as-errors` flag to `mix test`.
- `DOCKER_ELIXIR` variable unchanged — still `elixir:1.19-alpine`, satisfying AC4 single source of truth.
- `make test-unit-elixir` ran successfully: 21 tests across 6 umbrella apps (session_manager: 1, event_dispatcher: 16, room_manager: 1, permissions: 1, signature: 1, presence: 1), 0 failures, exit code 0.
- No compiler warnings surfaced — no production code changes required.

### File List

- `Makefile`

## Change Log

- 2026-03-25: Updated `test-unit-elixir` target — added `mix deps.get` and `--warnings-as-errors` to `mix test` command.
