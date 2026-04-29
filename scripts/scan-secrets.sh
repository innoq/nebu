#!/usr/bin/env bash
# =============================================================================
# scripts/scan-secrets.sh
# Secret-scan wrapper for nebu-chat using gitleaks.
#
# Modes:
#   --history   Full git history scan (pre-push, one-shot verification)
#   --staged    Staged-changes-only scan (pre-push hook)
#   --ci        CI mode: non-interactive, JSON report written to
#               gitleaks-report.json in the current directory, exits 0/1
#
# Usage: scan-secrets.sh [--history | --staged | --ci | --help]
#
# Pre-flight: gitleaks must be installed.
#   brew install gitleaks
#   go install github.com/zricethezav/gitleaks/v8@latest
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Config lives in the project repo root (always).
CONFIG_FILE="${REPO_ROOT}/.gitleaks.toml"

# Report is written relative to the current working directory so that tests
# running in mktemp sandboxes receive the report in their sandbox directory.
REPORT_FILE="./gitleaks-report.json"

# ---------------------------------------------------------------------------
# Pre-flight: verify gitleaks is installed
# ---------------------------------------------------------------------------
preflight_check() {
    if ! command -v gitleaks >/dev/null 2>&1; then
        echo "ERROR: gitleaks not found in PATH." >&2
        echo "Install via: brew install gitleaks" >&2
        echo "        or: go install github.com/zricethezav/gitleaks/v8@latest" >&2
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# Pre-flight: warn if config file is missing (non-fatal)
# ---------------------------------------------------------------------------
config_check() {
    if [[ ! -f "${CONFIG_FILE}" ]]; then
        echo "WARNING: ${CONFIG_FILE} not found." >&2
        echo "         gitleaks will use built-in default rules." >&2
    fi
}

# ---------------------------------------------------------------------------
# mode_history — full git history scan
#   Intended for pre-push one-shot verification.
#   Does NOT write a JSON report (use --ci for that).
# ---------------------------------------------------------------------------
mode_history() {
    preflight_check
    config_check
    local config_args=()
    if [[ -f "${CONFIG_FILE}" ]]; then
        config_args=(--config "${CONFIG_FILE}")
    fi
    gitleaks detect \
        --source "." \
        "${config_args[@]}" \
        --no-banner \
        --redact
}

# ---------------------------------------------------------------------------
# mode_staged — staged-only scan
#   Intended for pre-push / pre-commit hook usage.
#   Does NOT write a JSON report.
# ---------------------------------------------------------------------------
mode_staged() {
    preflight_check
    config_check
    local config_args=()
    if [[ -f "${CONFIG_FILE}" ]]; then
        config_args=(--config "${CONFIG_FILE}")
    fi
    gitleaks protect \
        --staged \
        --source "." \
        "${config_args[@]}" \
        --no-banner \
        --redact
}

# ---------------------------------------------------------------------------
# mode_ci — CI-optimised scan
#   Non-interactive, writes JSON report to gitleaks-report.json in the
#   current working directory.  Exits 0 on clean / 1 on findings.
#   The report file is ALWAYS written (empty array on no findings).
# ---------------------------------------------------------------------------
mode_ci() {
    preflight_check
    config_check
    local config_args=()
    if [[ -f "${CONFIG_FILE}" ]]; then
        config_args=(--config "${CONFIG_FILE}")
    fi
    local exit_code=0
    gitleaks detect \
        --source "." \
        "${config_args[@]}" \
        --no-banner \
        --redact \
        --report-format json \
        --report-path "${REPORT_FILE}" \
        || exit_code=$?
    exit "${exit_code}"
}

# ---------------------------------------------------------------------------
# usage
# ---------------------------------------------------------------------------
usage() {
    echo "Usage: $(basename "$0") [--history | --staged | --ci | --help]" >&2
    echo "" >&2
    echo "  --history   Full git history scan (run before public push)" >&2
    echo "              Intended for pre-push one-shot verification." >&2
    echo "  --staged    Staged-changes-only scan (pre-push hook)" >&2
    echo "              Intended for use in .git/hooks/pre-push." >&2
    echo "  --ci        CI mode: JSON report to gitleaks-report.json," >&2
    echo "              exits 0 on clean / 1 on findings." >&2
    echo "" >&2
    echo "Config:  ${CONFIG_FILE}" >&2
    echo "Report:  ./gitleaks-report.json  (--ci mode only)" >&2
    echo "" >&2
    echo "Install gitleaks:" >&2
    echo "  brew install gitleaks" >&2
    echo "  go install github.com/zricethezav/gitleaks/v8@latest" >&2
}

# ---------------------------------------------------------------------------
# Dispatch
# ---------------------------------------------------------------------------
case "${1:-}" in
    --history) mode_history ;;
    --staged)  mode_staged  ;;
    --ci)      mode_ci      ;;
    --help)
        usage
        exit 0
        ;;
    *)
        usage
        exit 1
        ;;
esac
