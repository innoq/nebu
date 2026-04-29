#!/usr/bin/env bash
# =============================================================================
# release-readiness-gate.sh
# Master release-readiness gate for Story 8.9.
# Aggregates all verify scripts from Stories 8.2-8.8 into a single gate.
#
# Usage: bash scripts/release-readiness-gate.sh [--all|--quick|--report]
#
# Modes:
#   --all    (default) Run all seven sub-suites including history scan.
#   --quick  Skip scripts/scan-secrets.sh --history (faster iteration).
#   --report Run --all and write JSON report to release-readiness-report.json.
#
# Exit codes:
#   0 -- all enabled sub-suites passed
#   1 -- one or more sub-suites failed, or pre-flight failed
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

# ---------------------------------------------------------------------------
# Mode
# ---------------------------------------------------------------------------
MODE="${1:---all}"

case "${MODE}" in
    --all|--quick|--report) ;;
    *)
        echo "ERROR: unknown mode '${MODE}'" >&2
        echo "Usage: $(basename "$0") [--all|--quick|--report]" >&2
        exit 1
        ;;
esac

# ---------------------------------------------------------------------------
# Sub-scripts to check and run
# ---------------------------------------------------------------------------
SUITES=(
    "scripts/verify-readme-attribution.sh"
    "scripts/verify-contributing.sh"
    "scripts/verify-security-policy.sh"
    "scripts/verify-ci-config.sh"
    "scripts/verify-issue-pr-templates.sh"
    "scripts/verify-repo-metadata.sh"
)
SCAN_SECRETS_SCRIPT="scripts/scan-secrets.sh"

# ---------------------------------------------------------------------------
# Pre-flight: verify all sub-scripts exist and are executable.
# Fail immediately if any are missing -- do not run partial checks.
# ---------------------------------------------------------------------------
preflight_check() {
    local failed=0
    local s

    for s in "${SUITES[@]}"; do
        if [[ ! -f "${REPO_ROOT}/${s}" ]]; then
            echo "ERROR: sub-script missing: ${s}" >&2
            failed=1
        elif [[ ! -x "${REPO_ROOT}/${s}" ]]; then
            echo "ERROR: sub-script not executable: ${s}" >&2
            failed=1
        fi
    done

    if [[ ! -f "${REPO_ROOT}/${SCAN_SECRETS_SCRIPT}" ]]; then
        echo "ERROR: sub-script missing: ${SCAN_SECRETS_SCRIPT}" >&2
        failed=1
    elif [[ ! -x "${REPO_ROOT}/${SCAN_SECRETS_SCRIPT}" ]]; then
        echo "ERROR: sub-script not executable: ${SCAN_SECRETS_SCRIPT}" >&2
        failed=1
    fi

    if [[ "${failed}" -ne 0 ]]; then
        echo "Pre-flight failed: one or more sub-scripts are missing or not" \
             "executable. Cannot proceed." >&2
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# run_suite
# Runs a single sub-script, captures its exit code, prints PASS/FAIL.
# Sets global arrays SUITE_NAMES and SUITE_RESULTS.
# Uses || true so set -e does not abort the gate on a failing sub-suite.
# ---------------------------------------------------------------------------
SUITE_NAMES=()
SUITE_STATUSES=()
SUITE_EXIT_CODES=()
PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0

run_suite() {
    local name="$1"
    local cmd=("${@:2}")
    local rc=0

    echo ""
    echo "--- ${name}"
    "${cmd[@]}" || rc=$?

    SUITE_NAMES+=("${name}")
    SUITE_EXIT_CODES+=("${rc}")

    if [[ "${rc}" -eq 0 ]]; then
        echo "[PASS] ${name}"
        SUITE_STATUSES+=("PASS")
        PASS_COUNT=$(( PASS_COUNT + 1 ))
    else
        echo "[FAIL] ${name} (exit ${rc})"
        SUITE_STATUSES+=("FAIL")
        FAIL_COUNT=$(( FAIL_COUNT + 1 ))
    fi
}

skip_suite() {
    local name="$1"
    echo ""
    echo "[SKIP] ${name}"
    SUITE_NAMES+=("${name}")
    SUITE_STATUSES+=("SKIP")
    SUITE_EXIT_CODES+=(0)
    SKIP_COUNT=$(( SKIP_COUNT + 1 ))
}

# ---------------------------------------------------------------------------
# write_report
# Writes release-readiness-report.json to REPO_ROOT.
# ---------------------------------------------------------------------------
write_report() {
    local overall_status="$1"
    local report_file="${REPO_ROOT}/release-readiness-report.json"
    local mode_value

    case "${MODE}" in
        --all)    mode_value="all" ;;
        --quick)  mode_value="quick" ;;
        --report) mode_value="all" ;;
        *)        mode_value="all" ;;
    esac

    local date_str
    date_str="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

    # Build suites JSON entries
    local suites_json=""
    local i
    for i in "${!SUITE_NAMES[@]}"; do
        local name="${SUITE_NAMES[${i}]}"
        local status="${SUITE_STATUSES[${i}]}"
        local exit_code="${SUITE_EXIT_CODES[${i}]}"
        local entry
        entry="    {\"name\": \"${name}\", \"status\": \"${status}\", \"exit_code\": ${exit_code}}"
        if [[ "${i}" -gt 0 ]]; then
            suites_json="${suites_json},"$'\n'
        fi
        suites_json="${suites_json}${entry}"
    done

    python3 - "${report_file}" "${date_str}" "${mode_value}" \
        "${overall_status}" "${suites_json}" <<'PYEOF'
import json, sys

report_file = sys.argv[1]
date_str    = sys.argv[2]
mode_value  = sys.argv[3]
overall_s   = sys.argv[4]
suites_raw  = sys.argv[5]

# Parse the suites JSON manually from the shell-constructed fragment
# by wrapping it in an array.
suites_json = f"[{suites_raw}]" if suites_raw.strip() else "[]"
suites = json.loads(suites_json)

report = {
    "date":           date_str,
    "mode":           mode_value,
    "overall_status": overall_s,
    "suites":         suites,
}

with open(report_file, "w", encoding="utf-8") as f:
    json.dump(report, f, indent=2)
    f.write("\n")

print(f"Report written: {report_file}")
PYEOF
}

# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------
main() {
    echo "============================================================"
    echo "release-readiness-gate.sh -- Story 8.9"
    echo "Mode: ${MODE}"
    echo "REPO_ROOT: ${REPO_ROOT}"
    echo "============================================================"

    preflight_check

    # Run the six verify scripts (always)
    run_suite "verify-readme-attribution.sh" \
        bash "${REPO_ROOT}/scripts/verify-readme-attribution.sh"
    run_suite "verify-contributing.sh" \
        bash "${REPO_ROOT}/scripts/verify-contributing.sh"
    run_suite "verify-security-policy.sh" \
        bash "${REPO_ROOT}/scripts/verify-security-policy.sh"
    run_suite "verify-ci-config.sh" \
        bash "${REPO_ROOT}/scripts/verify-ci-config.sh"
    run_suite "verify-issue-pr-templates.sh" \
        bash "${REPO_ROOT}/scripts/verify-issue-pr-templates.sh"
    run_suite "verify-repo-metadata.sh" \
        bash "${REPO_ROOT}/scripts/verify-repo-metadata.sh"

    # History scan: run for --all and --report; skip for --quick
    if [[ "${MODE}" == "--quick" ]]; then
        skip_suite "scan-secrets.sh --history"
    else
        run_suite "scan-secrets.sh --history" \
            bash "${REPO_ROOT}/scripts/scan-secrets.sh" --history
    fi

    # ---------------------------------------------------------------------------
    # Summary
    # ---------------------------------------------------------------------------
    local total=$(( PASS_COUNT + FAIL_COUNT + SKIP_COUNT ))
    local overall_status="PASS"
    if [[ "${FAIL_COUNT}" -gt 0 ]]; then
        overall_status="FAIL"
    fi

    echo ""
    echo "============================================================"
    echo "Summary:"
    local i
    for i in "${!SUITE_NAMES[@]}"; do
        echo "  [${SUITE_STATUSES[${i}]}] ${SUITE_NAMES[${i}]}"
    done
    echo ""
    echo "GATE: ${PASS_COUNT}/${total} PASS (${FAIL_COUNT} FAIL, ${SKIP_COUNT} SKIP)"
    if [[ "${MODE}" == "--quick" ]]; then
        echo ""
        echo "WARNING: --quick mode skipped scan-secrets.sh --history."
        echo "         Do NOT treat this as release-ready. Run --all"
        echo "         (with gitleaks installed) before public push."
        echo "         See PUBLIC_PUSH_CHECKLIST.md Step 1."
        echo ""
    fi
    echo "Overall: ${overall_status}"
    echo "============================================================"

    # Write JSON report if requested
    if [[ "${MODE}" == "--report" ]]; then
        write_report "${overall_status}"
    fi

    if [[ "${overall_status}" == "FAIL" ]]; then
        exit 1
    fi
    exit 0
}

main
