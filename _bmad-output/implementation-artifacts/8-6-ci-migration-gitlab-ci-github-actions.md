---
security_review: required
---

# Story 8.6: Dual CI — GitHub Actions + GitLab CI for Sovereign OSS

Status: ready-for-dev

## Story

**As a** maintainer publishing Nebu as sovereign open-source software,
**I want** both a GitHub Actions workflow and an updated GitLab CI pipeline that cover identical job topologies — using the same shared scripts,
**so that** Nebu is continuously verified on both `github.com/innoq/nebu` and `gitlab.opencode.de/nebu/nebu-server` without duplicating test logic.

**Size:** M

---

## Background

### Strategic Pivot: Bidirectional Publication (Sovereign OSS)

The original story title ("CI-Migration GitLab CI → GitHub Actions") is no longer accurate. Nebu is published to two hosts simultaneously:

- `github.com/innoq/nebu` — primary public GitHub repo, driven by GitHub Actions
- `gitlab.opencode.de/nebu/nebu-server` — sovereign mirror on opencode.de (German public-sector open-source infrastructure), driven by GitLab CI

Both CI platforms must provide equivalent quality gates. Neither replaces the other. The YAML slug `8-6-ci-migration-gitlab-ci-github-actions` is kept for historical continuity.

### Existing GitLab CI

`.gitlab-ci.yml` at the repo root already defines three stages: `build-ci-images`, `unit`, `integration`. The unit-test jobs (`unit-tests-go`, `unit-tests-elixir`) run directly in custom CI images pinned at Go 1.26 and Elixir 1.19. The integration test uses Docker-in-Docker. This story expands and aligns `.gitlab-ci.yml` with the new job topology while creating the parallel GitHub Actions workflow.

### DRY Principle — Single-Source Scripts

All test, lint, and verify logic lives in scripts in the repository:

- `make test-unit-go` — Go unit tests with race detector
- `make test-unit-elixir` — Elixir unit tests with `--warnings-as-errors`
- `scripts/scan-secrets.sh --ci` — gitleaks CI mode (Story 8.5)
- `scripts/verify-readme-attribution.sh` — verifies README (Story 8.2)
- `scripts/verify-contributing.sh` — verifies CONTRIBUTING.md (Story 8.3)
- `scripts/verify-security-policy.sh` — verifies SECURITY.md (Story 8.4)

Both CIs invoke the same scripts. No CI-specific logic is embedded in workflow YAML. This is the architectural contract for this story.

### Supply-Chain Security Consideration

GitHub Actions workflows that use `uses: actions/...@<tag>` or third-party actions introduce supply-chain risk. All actions must be pinned to a full commit SHA, not a mutable tag. `security_review: required` applies.

---

## Acceptance Criteria

1. **`.github/workflows/ci.yml` exists** and is valid YAML. It defines the following jobs: `lint-go`, `test-unit-go`, `test-unit-elixir`, `secret-scan`, `verify-docs`. All jobs use pinned tool versions (no `:latest` image tags, no mutable action tags — actions pinned to full commit SHA). Triggers: `push: branches: [main, "feature/**"]` and `pull_request:`.

2. **`.gitlab-ci.yml` exists and is updated** with the same job topology. The existing `unit-tests-go` and `unit-tests-elixir` jobs are preserved (possibly renamed to match parity: `test-unit-go`, `test-unit-elixir`). New jobs added: `lint-go`, `secret-scan`, `verify-docs`. Stage layout uses `stages:` with at least: `lint`, `unit`, `scan`, `verify` (integration stage retained). Branch trigger rules cover `main` and `feature/**` branches.

3. **Both CIs invoke the same scripts (DRY contract):**
   - `secret-scan` job: calls `scripts/scan-secrets.sh --ci`
   - `verify-docs` job: calls all three verify scripts (`scripts/verify-readme-attribution.sh`, `scripts/verify-contributing.sh`, `scripts/verify-security-policy.sh`)
   - `test-unit-go` job: calls `make test-unit-go` (or equivalent `go test -race ./...`)
   - `test-unit-elixir` job: calls `make test-unit-elixir` (or equivalent `mix test --warnings-as-errors`)
   - No CI-platform-specific test logic — scripts are the single source of truth.

4. **Tool versions are explicitly pinned** (not `:latest`) in both CI configurations:
   - Go: `1.26` (image tag or `go-version: "1.26"`)
   - Elixir: `1.19` / OTP 27 (image tag)
   - gitleaks: installed via explicit version download in GitHub Actions (e.g., `v8.x.y`), referenced via `$CI_REGISTRY_IMAGE/ci-go:1.26` custom image in GitLab (which pre-installs gitleaks at a pinned version)
   - GitHub Actions: all `uses:` steps pinned to full commit SHA (not semver tag)

5. **Caching is configured** in both CIs for build-speed:
   - GitHub Actions: `actions/cache` for Go module cache (`~/.cache/go`), mix deps cache (`~/.mix`, `~/.hex`)
   - GitLab CI: `cache:` blocks with `key: "$CI_COMMIT_REF_SLUG"` for Go module and mix dependency directories

6. **`gitleaks-report.json` is uploaded as a CI artifact** in both configurations:
   - GitHub Actions: `uses: actions/upload-artifact` for `gitleaks-report.json` (uploaded even on failure via `if: always()`)
   - GitLab CI: `artifacts:` block on `secret-scan` job with `when: always` and `paths: [gitleaks-report.json]`

7. **Branch triggers are correctly scoped:**
   - GitHub Actions: `on: push: branches: [main, "feature/**"]` and `on: pull_request:` (all branches)
   - GitLab CI: `rules:` with `if: '$CI_COMMIT_BRANCH == "main"'`, `if: '$CI_COMMIT_BRANCH =~ /^feature\//'`, and `if: '$CI_PIPELINE_SOURCE == "merge_request_event"'`

8. **`scripts/ci-local.sh` exists and is executable**. The script runs the complete CI job suite locally using Docker containers equivalent to the CI environments. It accepts an optional `--job <jobname>` argument to run a single job. Exit code reflects the combined pass/fail status. The script is shellcheck clean (`shellcheck --severity=error`).

9. **`scripts/verify-ci-config.sh` exists and is executable**. It validates both CI configuration files and the local script using the test cases defined in the Acceptance Tests section. Exit code 0 means all tests pass. The script uses `mktemp -d` sandboxes where file isolation is needed, has a `trap ... EXIT` cleanup, is shellcheck clean, and does not use `grep -P` or other non-BSD-portable features.

10. **`_bmad-output/planning-artifacts/badges-spec.md` exists** with placeholder badge Markdown for GitHub Actions CI status and GitLab CI status for both repo URLs. This file is consumed by Story 8.8 — it is _not_ inserted into `README.md` by this story.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

All tests are implemented as Bash test functions in `scripts/verify-ci-config.sh`. Tests use `mktemp -d` sandboxes for any file-isolation operations. A `trap cleanup EXIT` ensures sandbox removal even on early exit. No external test framework is required. The script exits 0 on all-pass, 1 on any failure, and prints a per-test PASS/FAIL summary.

1. **`test_github_ci_yml_is_valid_yaml`** — Bash, `python3 -c "import yaml, sys; yaml.safe_load(sys.stdin)"` parse check
   - Given: `.github/workflows/ci.yml` exists in the repository
   - When: `python3 -c "import yaml, sys; yaml.safe_load(sys.stdin)" < .github/workflows/ci.yml`
   - Then: Exit code 0; no YAML parse error

2. **`test_gitlab_ci_yml_is_valid_yaml`** — Bash, same python3 parse check
   - Given: `.gitlab-ci.yml` exists in the repository
   - When: `python3 -c "import yaml, sys; yaml.safe_load(sys.stdin)" < .gitlab-ci.yml`
   - Then: Exit code 0; no YAML parse error

3. **`test_github_ci_has_required_jobs`** — Bash, grep against `.github/workflows/ci.yml`
   - Given: `.github/workflows/ci.yml` exists
   - When: grep for job identifiers: `lint-go`, `test-unit-go`, `test-unit-elixir`, `secret-scan`, `verify-docs`
   - Then: All five job names are present (anchored match: `^  <jobname>:`)

4. **`test_gitlab_ci_has_required_jobs`** — Bash, grep against `.gitlab-ci.yml`
   - Given: `.gitlab-ci.yml` exists
   - When: grep for job identifiers: `lint-go`, `test-unit-go`, `test-unit-elixir`, `secret-scan`, `verify-docs`
   - Then: All five job names are present (anchored match)

5. **`test_github_ci_calls_scan_secrets`** — Bash, grep
   - Given: `.github/workflows/ci.yml`
   - When: grep for `scripts/scan-secrets.sh --ci`
   - Then: Match found

6. **`test_gitlab_ci_calls_scan_secrets`** — Bash, grep
   - Given: `.gitlab-ci.yml`
   - When: grep for `scripts/scan-secrets.sh --ci`
   - Then: Match found

7. **`test_github_ci_calls_verify_scripts`** — Bash, grep
   - Given: `.github/workflows/ci.yml`
   - When: grep for at least one of: `verify-readme-attribution.sh`, `verify-contributing.sh`, `verify-security-policy.sh`
   - Then: At least one match found

8. **`test_gitlab_ci_calls_verify_scripts`** — Bash, grep
   - Given: `.gitlab-ci.yml`
   - When: grep for at least one of the three verify scripts
   - Then: At least one match found

9. **`test_github_ci_no_latest_tags`** — Bash, grep for `:latest` in GitHub Actions YAML
   - Given: `.github/workflows/ci.yml`
   - When: grep for `:latest`
   - Then: No match (`:latest` is forbidden; all images and action references must be pinned)

10. **`test_gitlab_ci_no_latest_tags`** — Bash, grep for `:latest` in GitLab CI YAML
    - Given: `.gitlab-ci.yml`
    - When: grep for `:latest` in job `image:` lines
    - Then: No match in job image references (the existing `docker:latest` in `build-ci-images` jobs may require a carve-out; the test must explicitly document any exception)

11. **`test_ci_local_sh_exists_and_is_executable`** — Bash, file system check
    - Given: `scripts/ci-local.sh` was created
    - When: `[[ -x scripts/ci-local.sh ]]`
    - Then: True

12. **`test_ci_local_sh_shellcheck_clean`** — Bash, shellcheck invocation
    - Given: `scripts/ci-local.sh` exists and shellcheck is in PATH
    - When: `shellcheck --severity=error scripts/ci-local.sh`
    - Then: Exit code 0; no errors

13. **`test_verify_ci_config_sh_shellcheck_clean`** — Bash, shellcheck invocation
    - Given: `scripts/verify-ci-config.sh` exists
    - When: `shellcheck --severity=error scripts/verify-ci-config.sh`
    - Then: Exit code 0; no errors

14. **`test_badges_spec_exists`** — Bash, file system check
    - Given: `_bmad-output/planning-artifacts/badges-spec.md` was created
    - When: `[[ -f _bmad-output/planning-artifacts/badges-spec.md ]]`
    - Then: True

15. **`test_act_probe_run_if_available`** — Bash, conditional
    - Given: `act` (https://github.com/nektos/act) is installed in PATH
    - When: `act --list` runs against `.github/workflows/ci.yml`
    - Then: Exit code 0 and jobs listed; SKIP (exit 0) when `act` is not found in PATH

**Persistenz-Strategie:** Not applicable — CI configuration files and shell scripts with no application state. No crash/restart test required.

---

## Risks and Mitigations

| Risk | Severity | Mitigation |
|---|---|---|
| **Unpinned GitHub Actions** — mutable action tags (`@v3`, `@main`) allow supply-chain injection | HIGH | All `uses:` steps must be pinned to full commit SHA. Verified in AC 4 and test 9. Security review gate required. |
| **Secrets leaked via environment variables in CI logs** | HIGH | No secrets printed in logs; `scan-secrets.sh --ci` runs non-interactively. `DOCKER_DRIVER` and CI image tokens are CI-provided, not hard-coded. |
| **`ci-local.sh` diverges from actual CI over time** | MEDIUM | `ci-local.sh` must invoke the same make targets and scripts used in the workflow YAML — no inline commands duplicated. Review in code review gate. |
| **GitLab `docker:latest` in build-ci-images jobs** | LOW | Existing `build-ci-images` jobs use `docker:latest` for the DinD builder; this is a one-off bootstrap pattern and is documented as an accepted exception in `test_gitlab_ci_no_latest_tags`. |
| **Platform-specific caching keys cause stale deps** | LOW | Cache keys include `${{ hashFiles('**/go.sum') }}` (GitHub) and `$CI_COMMIT_REF_SLUG` with a hash suffix (GitLab) to invalidate on dependency changes. |

---

## Implementation Notes

### `.github/workflows/ci.yml` — Outline

```yaml
name: CI

on:
  push:
    branches: [main, "feature/**"]
  pull_request:

env:
  GO_VERSION: "1.26"
  ELIXIR_VERSION: "1.19"
  OTP_VERSION: "27"
  GITLEAKS_VERSION: "8.24.3"  # pin explicit release

jobs:

  lint-go:
    runs-on: ubuntu-22.04
    steps:
      - uses: actions/checkout@<full-sha>           # pin SHA, not tag
      - uses: actions/setup-go@<full-sha>
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - name: golangci-lint
        run: cd gateway && go vet ./...             # or golangci-lint run

  test-unit-go:
    runs-on: ubuntu-22.04
    steps:
      - uses: actions/checkout@<full-sha>
      - uses: actions/setup-go@<full-sha>
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - name: Run Go unit tests
        run: make test-unit-go

  test-unit-elixir:
    runs-on: ubuntu-22.04
    steps:
      - uses: actions/checkout@<full-sha>
      - uses: erlef/setup-beam@<full-sha>
        with:
          elixir-version: ${{ env.ELIXIR_VERSION }}
          otp-version: ${{ env.OTP_VERSION }}
      - uses: actions/cache@<full-sha>
        with:
          path: |
            core/deps
            core/_build
          key: mix-${{ hashFiles('core/mix.lock') }}
      - name: Run Elixir unit tests
        run: make test-unit-elixir

  secret-scan:
    runs-on: ubuntu-22.04
    steps:
      - uses: actions/checkout@<full-sha>
        with:
          fetch-depth: 0                            # full history for gitleaks
      - name: Install gitleaks
        run: |
          curl -sSfL \
            "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz" \
            | tar -xz -C /usr/local/bin gitleaks
      - name: Run secret scan
        run: scripts/scan-secrets.sh --ci
      - uses: actions/upload-artifact@<full-sha>
        if: always()
        with:
          name: gitleaks-report
          path: gitleaks-report.json

  verify-docs:
    runs-on: ubuntu-22.04
    steps:
      - uses: actions/checkout@<full-sha>
      - name: Verify README attribution
        run: scripts/verify-readme-attribution.sh
      - name: Verify CONTRIBUTING.md
        run: scripts/verify-contributing.sh
      - name: Verify SECURITY.md
        run: scripts/verify-security-policy.sh
```

### `.gitlab-ci.yml` — Updated Outline

The existing file defines stages `build-ci-images`, `unit`, `integration`. Expand to:

```yaml
stages:
  - build-ci-images
  - lint
  - unit
  - scan
  - verify
  - integration

variables:
  DOCKER_DRIVER: overlay2
  CI_GO_IMAGE: $CI_REGISTRY_IMAGE/ci-go:1.26
  CI_ELIXIR_IMAGE: $CI_REGISTRY_IMAGE/ci-elixir:1.19
  GITLEAKS_VERSION: "8.24.3"

# existing build-ci-go and build-ci-elixir jobs — unchanged

lint-go:
  stage: lint
  image: $CI_GO_IMAGE
  script:
    - cd gateway && go vet ./...
  cache:
    key: "go-$CI_COMMIT_REF_SLUG"
    paths: [/root/go/pkg/mod]
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
    - if: '$CI_COMMIT_BRANCH =~ /^feature\//'
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'

test-unit-go:
  stage: unit
  image: $CI_GO_IMAGE
  script:
    - cd gateway && go test -race ./...
  cache:
    key: "go-$CI_COMMIT_REF_SLUG"
    paths: [/root/go/pkg/mod]
  rules:  # same rules as above

test-unit-elixir:
  stage: unit
  image: $CI_ELIXIR_IMAGE
  script:
    - cd core && mix deps.get && mix test --warnings-as-errors
  cache:
    key: "mix-$CI_COMMIT_REF_SLUG"
    paths: [core/deps, core/_build, ~/.hex, ~/.mix]
  rules:  # same rules as above

secret-scan:
  stage: scan
  image: $CI_GO_IMAGE   # has curl; gitleaks installed via script
  before_script:
    - curl -sSfL
        "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz"
        | tar -xz -C /usr/local/bin gitleaks
  script:
    - scripts/scan-secrets.sh --ci
  artifacts:
    when: always
    paths: [gitleaks-report.json]
  rules:  # same rules as above

verify-docs:
  stage: verify
  image: $CI_GO_IMAGE
  script:
    - scripts/verify-readme-attribution.sh
    - scripts/verify-contributing.sh
    - scripts/verify-security-policy.sh
  rules:  # same rules as above

# existing integration-test job — unchanged
```

### `scripts/ci-local.sh` — Outline

```bash
#!/usr/bin/env bash
# scripts/ci-local.sh
# Run CI jobs locally via Docker — equivalent to GitHub Actions / GitLab CI.
#
# Usage:
#   ci-local.sh                  # run all jobs
#   ci-local.sh --job <jobname>  # run a single job
#
# Jobs: lint-go  test-unit-go  test-unit-elixir  secret-scan  verify-docs

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

GO_IMAGE="golang:1.26-alpine"
ELIXIR_IMAGE="elixir:1.19-otp-27-alpine"

DOCKER_RUN="docker run --rm -v ${REPO_ROOT}:/workspace -w /workspace"

job_lint_go()        { ${DOCKER_RUN} "${GO_IMAGE}" sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go vet ./..."; }
job_test_unit_go()   { ${DOCKER_RUN} "${GO_IMAGE}" sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go test -race ./..."; }
job_test_unit_elixir() { ${DOCKER_RUN} "${ELIXIR_IMAGE}" sh -c "cd core && mix local.hex --force && mix deps.get && mix test --warnings-as-errors"; }
job_secret_scan()    { "${SCRIPT_DIR}/scan-secrets.sh" --ci; }
job_verify_docs()    {
  "${SCRIPT_DIR}/verify-readme-attribution.sh"
  "${SCRIPT_DIR}/verify-contributing.sh"
  "${SCRIPT_DIR}/verify-security-policy.sh"
}

# dispatch
case "${1:-all}" in
  --job)
    shift
    case "${1:-}" in
      lint-go)           job_lint_go ;;
      test-unit-go)      job_test_unit_go ;;
      test-unit-elixir)  job_test_unit_elixir ;;
      secret-scan)       job_secret_scan ;;
      verify-docs)       job_verify_docs ;;
      *) echo "Unknown job: ${1:-}. Valid: lint-go test-unit-go test-unit-elixir secret-scan verify-docs" >&2; exit 1 ;;
    esac
    ;;
  all)
    job_lint_go
    job_test_unit_go
    job_test_unit_elixir
    job_secret_scan
    job_verify_docs
    ;;
  *)
    echo "Usage: $(basename "$0") [--job <jobname>]" >&2
    exit 1
    ;;
esac
```

### `scripts/verify-ci-config.sh` — Structural Notes

Following the pattern established by `scripts/verify-readme-attribution.sh`, `verify-contributing.sh`, and `verify-security-policy.sh`:

- Functions named `test_<description>` with `PASS`/`FAIL` output
- `SANDBOX` via `mktemp -d`; `cleanup()` called via `trap cleanup EXIT`
- Counter `FAILURES=0`; final `exit "${FAILURES}"` (clamped to 1 if > 1)
- BSD-portable: use `grep -E` (not `grep -P`), `python3` for JSON/YAML, no `sed -i` without extension
- Anchored regex in grep patterns (e.g., `^  lint-go:` not `lint-go`)
- Shellcheck annotation `# shellcheck source=/dev/null` for dynamic sources if needed
- Returns 0 (success) / 1 (failure) — `set -euo pipefail` at top but test functions use explicit exit-code capture, not `set -e` propagation

### `_bmad-output/planning-artifacts/badges-spec.md` — Content

The file must contain:
- Badge Markdown for GitHub Actions CI: `[![CI](https://github.com/innoq/nebu/actions/workflows/ci.yml/badge.svg)](https://github.com/innoq/nebu/actions/workflows/ci.yml)`
- Badge Markdown for GitLab CI: `[![pipeline status](https://gitlab.opencode.de/nebu/nebu-server/badges/main/pipeline.svg)](https://gitlab.opencode.de/nebu/nebu-server/-/commits/main)`
- A note that these badges are consumed by Story 8.8 and must not be inserted into `README.md` by this story.

### Lessons Learned from Stories 8.1–8.5

The following patterns from previous stories in this epic apply to all new scripts in this story:

1. **mktemp sandbox tests** — every test that creates or modifies files uses `SANDBOX="$(mktemp -d)"` with a `trap 'rm -rf "${SANDBOX}"' EXIT`. Tests do not write to the host repo working tree.
2. **cleanup-trap with return 0** — the cleanup function does `rm -rf "${SANDBOX}"; return 0` to avoid masking the test exit code.
3. **BSD-portability** — no `grep -P`, no `sed -i` without an extension argument, no GNU-only flags. Python 3 is used for any JSON or YAML parsing.
4. **Anchored regex** — all `grep` patterns that match job names in YAML are anchored (e.g., `^  lint-go:`). Unanchored patterns on job names can match comments or descriptions and produce false positives.
5. **shellcheck clean** — all new scripts pass `shellcheck --severity=error` before the story can be marked `done`. Tests 12 and 13 in the Acceptance Tests section enforce this.
6. **No section-scoping complexity** — this story's verify script validates YAML structure, not Markdown sections, so no section-scoping logic from 8.2/8.3/8.4 is needed.

### GitHub Actions SHA Pinning Reference

When writing `.github/workflows/ci.yml`, look up current commit SHAs for these actions on their respective GitHub release pages:

| Action | Typical tag | Must pin to: |
|---|---|---|
| `actions/checkout` | `v4` | full SHA of v4.x.y release commit |
| `actions/setup-go` | `v5` | full SHA of v5.x.y release commit |
| `erlef/setup-beam` | `v1` | full SHA of v1.x.y release commit |
| `actions/cache` | `v4` | full SHA of v4.x.y release commit |
| `actions/upload-artifact` | `v4` | full SHA of v4.x.y release commit |

Use the GitHub web UI (`/<owner>/<repo>/releases`) or `gh release list` to find the current SHA.

---

## Files to Create / Modify

| File / Action | Description |
|---|---|
| `.github/workflows/ci.yml` (NEW) | GitHub Actions CI: `lint-go`, `test-unit-go`, `test-unit-elixir`, `secret-scan`, `verify-docs`; all tools pinned, caching configured, gitleaks artifact |
| `.gitlab-ci.yml` (MODIFY) | Expand existing file: add `lint`, `scan`, `verify` stages and corresponding jobs; preserve existing `build-ci-images` and `integration-test` jobs |
| `scripts/ci-local.sh` (NEW) | Docker-based local CI runner for all five jobs; `--job` flag for single-job execution; shellcheck clean |
| `scripts/verify-ci-config.sh` (NEW) | Bash test suite: 15 tests (YAML validity, job presence, script invocation, no `:latest`, shellcheck, badges-spec existence, optional act probe) |
| `_bmad-output/planning-artifacts/badges-spec.md` (NEW) | Placeholder badge Markdown for GitHub Actions + GitLab CI — consumed by Story 8.8, NOT inserted in README here |
| `_bmad-output/implementation-artifacts/sprint-status-epic-8.yaml` (MODIFY) | Status `8-6-ci-migration-gitlab-ci-github-actions`: `backlog` → `ready-for-dev` |

**Explicitly out of scope for this story:**

- No `README.md` edits (badges inserted in Story 8.8)
- No `.github/` issue or PR templates (Story 8.7)
- No branch protection setup (Story 8.10)
- No integration test job changes (job preserved as-is from existing `.gitlab-ci.yml`)

---

## Context: Epic 8

Epic 8 transfers Nebu from a private GitLab repository to simultaneous public presence on GitHub and opencode.de (GitLab). Story 8.6 is the CI layer of that dual-host strategy.

**Dependency chain:**

- **Story 8.5** (Secret Scan Gate) delivered `scripts/scan-secrets.sh --ci`. Story 8.6 calls it as the `secret-scan` CI job. The script interface is already stable.
- **Story 8.2 / 8.3 / 8.4** delivered `scripts/verify-*.sh`. Story 8.6 calls all three in the `verify-docs` CI job.
- **Story 8.8** (Badges) reads `_bmad-output/planning-artifacts/badges-spec.md` produced here.
- **Story 8.10** (Initial Public Push) requires working CI on both platforms before the public push proceeds. Branch-protection setup in Story 8.10 references the job names defined in this story's `.github/workflows/ci.yml`.

**BMad Method (BMAD — Build More Architect Dreams):** This story follows the standard BMAD TDD cycle: `scripts/verify-ci-config.sh` is written first with all 15 tests failing (Gate 1 / ATDD), the CI YAML files and helper scripts are implemented until all tests pass (Gate 2), followed by code review including test review (Gate 3) and a mandatory security review (Gate 4 — `security_review: required`).
