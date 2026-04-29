#!/usr/bin/env bash
# =============================================================================
# ci-local.sh
# Run CI jobs locally via Docker — mirrors GitHub Actions / GitLab CI exactly.
#
# Uses the same Docker images as .gitlab-ci.yml (single source of truth).
# Calling any job here produces the same result as the pipeline.
#
# Usage:
#   scripts/ci-local.sh                # run all jobs sequentially
#   scripts/ci-local.sh --lint         # run lint-go only
#   scripts/ci-local.sh --test-go      # run test-unit-go only
#   scripts/ci-local.sh --test-elixir  # run test-unit-elixir only
#   scripts/ci-local.sh --scan         # run secret-scan only
#   scripts/ci-local.sh --verify       # run verify-docs only
#   scripts/ci-local.sh --all          # run all jobs (same as no args)
#
# Jobs: lint-go  test-unit-go  test-unit-elixir  secret-scan  verify-docs
#
# Requires: Docker in PATH
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ---------------------------------------------------------------------------
# Image versions — keep in sync with the Makefile (DOCKER_GO/DOCKER_ELIXIR)
# Public docker.io images so the script works on a fresh clone without
# access to the GitLab Container Registry.
# ---------------------------------------------------------------------------
GO_IMAGE="golang:1.26-alpine"
ELIXIR_IMAGE="elixir:1.19-alpine"

# ---------------------------------------------------------------------------
# Pre-flight: Docker must be in PATH
# ---------------------------------------------------------------------------
preflight_check() {
    if ! command -v docker >/dev/null 2>&1; then
        echo "ERROR: docker not found in PATH." >&2
        echo "  Install Docker Desktop or Docker Engine and ensure it is running." >&2
        echo "  See https://docs.docker.com/get-docker/" >&2
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# docker_run — mount repo into /workspace and run a command
# ---------------------------------------------------------------------------
docker_run() {
    docker run --rm \
        -v "${REPO_ROOT}:/workspace" \
        -w /workspace \
        "$@"
}

# ---------------------------------------------------------------------------
# Individual job functions
# ---------------------------------------------------------------------------
job_lint_go() {
    echo "==> lint-go"
    docker_run "${GO_IMAGE}" \
        sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go vet ./..."
}

job_test_unit_go() {
    echo "==> test-unit-go"
    docker_run "${GO_IMAGE}" \
        sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go test -race ./..."
}

job_test_unit_elixir() {
    echo "==> test-unit-elixir"
    docker_run "${ELIXIR_IMAGE}" \
        sh -c "cd core && mix local.hex --force && mix local.rebar --force && mix deps.get && mix test --warnings-as-errors"
}

job_secret_scan() {
    echo "==> secret-scan"
    "${SCRIPT_DIR}/scan-secrets.sh" --ci
}

job_verify_docs() {
    echo "==> verify-docs"
    "${SCRIPT_DIR}/verify-readme-attribution.sh"
    "${SCRIPT_DIR}/verify-contributing.sh"
    "${SCRIPT_DIR}/verify-security-policy.sh"
}

run_all() {
    job_lint_go
    job_test_unit_go
    job_test_unit_elixir
    job_secret_scan
    job_verify_docs
}

# ---------------------------------------------------------------------------
# Usage
# ---------------------------------------------------------------------------
usage() {
    local rc="${1:-1}"
    cat >&2 <<EOF
Usage: $(basename "$0") [MODE]

Modes:
  (no args)        run all jobs sequentially
  --all            run all jobs sequentially
  --lint           run lint-go
  --test-go        run test-unit-go
  --test-elixir    run test-unit-elixir
  --scan           run secret-scan
  --verify         run verify-docs
  --help, -h       show this message

Requires Docker in PATH. Uses Docker images that mirror the Makefile
(public docker.io: golang:1.26-alpine, elixir:1.19-alpine).
EOF
    exit "${rc}"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
MODE="${1:-all}"

# --help / -h work without docker being installed
case "${MODE}" in
    --help | -h)
        usage 0
        ;;
esac

preflight_check

case "${MODE}" in
    --all | all)
        run_all
        ;;
    --lint)
        job_lint_go
        ;;
    --test-go)
        job_test_unit_go
        ;;
    --test-elixir)
        job_test_unit_elixir
        ;;
    --scan)
        job_secret_scan
        ;;
    --verify)
        job_verify_docs
        ;;
    *)
        echo "Unknown mode: ${MODE}" >&2
        usage 1
        ;;
esac
