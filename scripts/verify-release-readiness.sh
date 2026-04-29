#!/usr/bin/env bash
# =============================================================================
# verify-release-readiness.sh
# Acceptance tests for Story 8.9 — Release-Readiness-Gate: Full-Stack Test +
# Epic-weiter Security Review
# Test framework: pure Bash with exit codes (no external test framework)
#
# Usage: bash scripts/verify-release-readiness.sh
#
# 12 tests covering:
#   1.  release-readiness-gate.sh exists and is executable
#   2.  Gate script invokes all seven sub-scripts
#   3.  Gate script has --all / --quick / --report mode flags
#   4.  Pre-flight aborts when a sub-script is missing (mktemp sandbox)
#   5.  Full run against real repo exits 0
#   6.  --report mode produces valid JSON with required fields
#   7.  security-reports/INDEX.md exists
#   8.  INDEX.md lists all three Epic 8 Kassandra reports (8-1, 8-5, 8-6)
#   9.  scripts/PUBLIC_PUSH_CHECKLIST.md exists
#  10.  PUBLIC_PUSH_CHECKLIST.md has exactly six "### Step N" headings
#  11.  release-readiness-report.json is listed in .gitignore
#  12.  shellcheck --severity=error clean on gate script + this script
#
# Exit 0 if all tests pass, Exit 1 if any test fails.
#
# Lessons Learned applied (8.1–8.8):
#   - python3 for JSON/YAML parsing (BSD grep Unicode false-positives)
#   - REPO_ROOT=$(git rev-parse --show-toplevel) + cd "${REPO_ROOT}"
#   - Anchored regex patterns where intent is anchored
#   - set -euo pipefail + local + quoting throughout
#   - Cleanup trap with return 0 + TEMPFILES array
#   - mktemp-sandbox for pre-flight negative test (Test 4)
#   - shellcheck SKIP-fallback for optional tools (Test 12)
#   - Negativ-Test: sandbox has gate script but no sub-scripts -> pre-flight fail
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

GATE_SCRIPT="${REPO_ROOT}/scripts/release-readiness-gate.sh"
SECURITY_INDEX="${REPO_ROOT}/_bmad-output/implementation-artifacts/security-reports/INDEX.md"
PUSH_CHECKLIST="${REPO_ROOT}/scripts/PUBLIC_PUSH_CHECKLIST.md"
GITIGNORE="${REPO_ROOT}/.gitignore"
VERIFY_SELF="${REPO_ROOT}/scripts/verify-release-readiness.sh"

# ---------------------------------------------------------------------------
# Global counters
# ---------------------------------------------------------------------------
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_NAMES=()

# ---------------------------------------------------------------------------
# Cleanup — temp files and directories registered in TEMPFILES are removed
# on exit. Sandbox directories created in Test 4 and Test 6 are also covered.
# ---------------------------------------------------------------------------
TEMPFILES=()
cleanup() {
    local f
    for f in "${TEMPFILES[@]:-}"; do
        [[ -n "${f}" ]] && rm -rf "${f}"
    done
    return 0
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Helper: run_test
# Calls the named test function, records PASS/FAIL.
# Uses explicit exit-code capture so set -e does not abort the harness.
# ---------------------------------------------------------------------------
run_test() {
    local name="$1"
    TESTS_RUN=$(( TESTS_RUN + 1 ))
    echo "--- ${name}"
    local rc=0
    "${name}" || rc=$?
    if [[ "${rc}" -eq 0 ]]; then
        echo "    PASS"
        TESTS_PASSED=$(( TESTS_PASSED + 1 ))
    else
        echo "    FAIL"
        TESTS_FAILED=$(( TESTS_FAILED + 1 ))
        FAILED_NAMES+=("${name}")
    fi
}

# =============================================================================
# TEST 1 — test_release_gate_script_exists_and_executable
#
# AC 1
# Given: repo root is accessible
# When:  check -f and -x on scripts/release-readiness-gate.sh
# Then:  both conditions true
# =============================================================================
test_release_gate_script_exists_and_executable() {
    if [[ ! -f "${GATE_SCRIPT}" ]]; then
        echo "    ERROR: scripts/release-readiness-gate.sh does not exist" >&2
        return 1
    fi
    if [[ ! -x "${GATE_SCRIPT}" ]]; then
        echo "    ERROR: scripts/release-readiness-gate.sh is not executable" >&2
        return 1
    fi
    return 0
}

# =============================================================================
# TEST 2 — test_release_gate_invokes_all_seven_scripts
#
# AC 2
# Given: scripts/release-readiness-gate.sh exists
# When:  grep for each of the seven sub-script references
# Then:  all seven greps return exit code 0
# =============================================================================
test_release_gate_invokes_all_seven_scripts() {
    if [[ ! -f "${GATE_SCRIPT}" ]]; then
        echo "    ERROR: scripts/release-readiness-gate.sh does not exist — cannot check sub-script references" >&2
        return 1
    fi

    local all_found=0
    local scripts=(
        "verify-readme-attribution.sh"
        "verify-contributing.sh"
        "verify-security-policy.sh"
        "scan-secrets.sh"
        "verify-ci-config.sh"
        "verify-issue-pr-templates.sh"
        "verify-repo-metadata.sh"
    )
    for s in "${scripts[@]}"; do
        if ! grep -q "${s}" "${GATE_SCRIPT}"; then
            echo "    ERROR: '${s}' not referenced in scripts/release-readiness-gate.sh" >&2
            all_found=1
        fi
    done
    return "${all_found}"
}

# =============================================================================
# TEST 3 — test_release_gate_three_modes
#
# AC 3
# Given: scripts/release-readiness-gate.sh exists
# When:  grep for --all, --quick, --report flags inside the gate script
# Then:  all three match
# =============================================================================
test_release_gate_three_modes() {
    if [[ ! -f "${GATE_SCRIPT}" ]]; then
        echo "    ERROR: scripts/release-readiness-gate.sh does not exist — cannot check mode flags" >&2
        return 1
    fi

    local all_found=0
    local modes=("--all" "--quick" "--report")
    for mode in "${modes[@]}"; do
        if ! grep -qF -- "${mode}" "${GATE_SCRIPT}"; then
            echo "    ERROR: mode flag '${mode}' not found in scripts/release-readiness-gate.sh" >&2
            all_found=1
        fi
    done
    return "${all_found}"
}

# =============================================================================
# TEST 4 — test_release_gate_preflight_aborts_when_subscript_missing
#
# AC 6 (negative)
# Given: a mktemp sandbox containing only release-readiness-gate.sh (copied
#        from REPO_ROOT), with NO sub-scripts present
# When:  bash <sandbox>/scripts/release-readiness-gate.sh --all
# Then:  exit code != 0 AND output contains "missing" or "not found"
#        (case-insensitive match)
#
# Note:  The sandbox is constructed so that SCRIPT_DIR inside the gate
#        script resolves to <sandbox>/scripts/. The gate script discovers
#        REPO_ROOT via cd "${SCRIPT_DIR}/.." which becomes <sandbox>/.
#        The sub-scripts do NOT exist inside the sandbox, so pre-flight
#        must abort.
# =============================================================================
test_release_gate_preflight_aborts_when_subscript_missing() {
    if [[ ! -f "${GATE_SCRIPT}" ]]; then
        echo "    ERROR: scripts/release-readiness-gate.sh does not exist — cannot run sandbox test" >&2
        return 1
    fi

    # Create isolated sandbox
    local sandbox
    sandbox="$(mktemp -d)"
    TEMPFILES+=("${sandbox}")

    # Replicate minimal directory structure: sandbox/scripts/
    mkdir -p "${sandbox}/scripts"

    # Copy only the gate script — deliberately omit all seven sub-scripts
    cp "${GATE_SCRIPT}" "${sandbox}/scripts/release-readiness-gate.sh"

    # Run from sandbox, capture output and exit code
    local output
    local rc=0
    output="$(cd "${sandbox}" && bash scripts/release-readiness-gate.sh --all 2>&1)" || rc=$?

    if [[ "${rc}" -eq 0 ]]; then
        echo "    ERROR: gate script exited 0 in sandbox (expected non-zero pre-flight failure)" >&2
        echo "    Output was: ${output}" >&2
        return 1
    fi

    # Output must mention missing or not found (case-insensitive)
    if ! echo "${output}" | grep -qiE "(missing|not found)"; then
        echo "    ERROR: gate script exited non-zero but output does not mention 'missing' or 'not found'" >&2
        echo "    Output was: ${output}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 5 — test_release_gate_full_run_against_real_repo
#
# AC 4 (integration)
# Given: live feature/github-readiness branch with all seven verify scripts
# When:  bash scripts/release-readiness-gate.sh --quick
#        (--quick skips history scan for speed; still exercises all other suites)
# Then:  exit code 0 AND output contains "GATE:" followed by "PASS"
# On failure: full gate output is printed to stderr for diagnosis
# =============================================================================
test_release_gate_full_run_against_real_repo() {
    if [[ ! -f "${GATE_SCRIPT}" ]]; then
        echo "    ERROR: scripts/release-readiness-gate.sh does not exist" >&2
        return 1
    fi

    local output
    local rc=0
    output="$(cd "${REPO_ROOT}" && bash scripts/release-readiness-gate.sh --quick 2>&1)" || rc=$?

    if [[ "${rc}" -ne 0 ]]; then
        echo "    ERROR: release-readiness-gate.sh --quick exited ${rc} (expected 0)" >&2
        echo "--- gate output ---" >&2
        echo "${output}" >&2
        echo "--- end gate output ---" >&2
        return 1
    fi

    if ! echo "${output}" | grep -qE "GATE:.*PASS"; then
        echo "    ERROR: gate output does not contain 'GATE:.*PASS'" >&2
        echo "--- gate output ---" >&2
        echo "${output}" >&2
        echo "--- end gate output ---" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 6 — test_release_gate_report_mode_produces_valid_json
#
# AC 5
# Given: --report mode runs all suites and writes release-readiness-report.json
# When:  bash scripts/release-readiness-gate.sh --report (run from a temp dir
#        with REPO_ROOT set so the report lands in a predictable location)
# Then (6a): release-readiness-report.json exists after the run
# Then (6b): python3 validates JSON structure with date, overall_status, suites
# Then (6c): overall_status is "PASS" or "FAIL"
# Then (6d): suites is a non-empty array
# Note:  report file is cleaned up via TEMPFILES after the test
# =============================================================================
test_release_gate_report_mode_produces_valid_json() {
    if [[ ! -f "${GATE_SCRIPT}" ]]; then
        echo "    ERROR: scripts/release-readiness-gate.sh does not exist" >&2
        return 1
    fi

    # Run from REPO_ROOT; the gate script writes report to REPO_ROOT
    local report_file="${REPO_ROOT}/release-readiness-report.json"
    # Register for cleanup regardless of test outcome
    TEMPFILES+=("${report_file}")

    # Remove any stale report from a previous run
    rm -f "${report_file}"

    local rc=0
    bash "${GATE_SCRIPT}" --report >/dev/null 2>&1 || rc=$?
    # --report exits non-zero if any suite fails; that is acceptable for this
    # test — we only care about the JSON structure, not the gate result.

    # 6a: report file must exist
    if [[ ! -f "${report_file}" ]]; then
        echo "    ERROR: release-readiness-report.json was not created by --report mode" >&2
        return 1
    fi

    # 6b + 6c + 6d: validate JSON structure with python3
    local py_rc=0
    python3 - "${report_file}" <<'PYEOF' || py_rc=$?
import json, sys

with open(sys.argv[1], encoding='utf-8') as f:
    d = json.load(f)

errors = []
if 'date' not in d:
    errors.append("missing field: date")
if 'mode' not in d:
    errors.append("missing field: mode")
elif d['mode'] not in ("all", "quick", "report"):
    errors.append(f"mode must be all/quick/report (CLI dashes stripped), got: {d['mode']!r}")
if 'overall_status' not in d:
    errors.append("missing field: overall_status")
elif d['overall_status'] not in ("PASS", "FAIL"):
    errors.append(f"overall_status must be PASS or FAIL, got: {d['overall_status']!r}")
if 'suites' not in d:
    errors.append("missing field: suites")
elif not isinstance(d['suites'], list) or len(d['suites']) == 0:
    errors.append("suites must be a non-empty array")

if errors:
    for e in errors:
        print(f"ERROR: {e}", file=sys.stderr)
    sys.exit(1)
PYEOF

    if [[ "${py_rc}" -ne 0 ]]; then
        echo "    ERROR: release-readiness-report.json failed JSON validation" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 7 — test_security_reports_index_exists
#
# AC 7
# Given: repo root accessible
# When:  -f check on _bmad-output/implementation-artifacts/security-reports/INDEX.md
# Then:  true
# =============================================================================
test_security_reports_index_exists() {
    if [[ ! -f "${SECURITY_INDEX}" ]]; then
        echo "    ERROR: _bmad-output/implementation-artifacts/security-reports/INDEX.md does not exist" >&2
        return 1
    fi
    return 0
}

# =============================================================================
# TEST 8 — test_security_reports_index_lists_all_three
#
# AC 7
# Given: security-reports/INDEX.md exists
# When:  grep for 8-1, 8-5, 8-6 references
# Then:  all three found
# =============================================================================
test_security_reports_index_lists_all_three() {
    if [[ ! -f "${SECURITY_INDEX}" ]]; then
        echo "    ERROR: INDEX.md does not exist — skipping content check" >&2
        return 1
    fi

    local all_found=0
    local reports=("8-1" "8-5" "8-6")
    for r in "${reports[@]}"; do
        if ! grep -q "${r}" "${SECURITY_INDEX}"; then
            echo "    ERROR: INDEX.md does not mention report '${r}'" >&2
            all_found=1
        fi
    done
    return "${all_found}"
}

# =============================================================================
# TEST 9 — test_public_push_checklist_exists
#
# AC 8
# Given: repo root accessible
# When:  -f check on scripts/PUBLIC_PUSH_CHECKLIST.md
# Then:  true
# =============================================================================
test_public_push_checklist_exists() {
    if [[ ! -f "${PUSH_CHECKLIST}" ]]; then
        echo "    ERROR: scripts/PUBLIC_PUSH_CHECKLIST.md does not exist" >&2
        return 1
    fi
    return 0
}

# =============================================================================
# TEST 10 — test_public_push_checklist_has_six_steps
#
# AC 8
# Given: scripts/PUBLIC_PUSH_CHECKLIST.md exists
# When:  grep -cE "^### Step [0-9]" on the checklist
# Then:  count equals exactly 6
# Note:  anchored pattern ^### Step [0-9] matches Markdown H3 step headings
# =============================================================================
test_public_push_checklist_has_six_steps() {
    if [[ ! -f "${PUSH_CHECKLIST}" ]]; then
        echo "    ERROR: scripts/PUBLIC_PUSH_CHECKLIST.md does not exist — cannot count steps" >&2
        return 1
    fi

    local count
    count="$(grep -cE "^### Step [0-9]" "${PUSH_CHECKLIST}" || true)"
    if [[ "${count}" -ne 6 ]]; then
        echo "    ERROR: expected 6 '### Step N' headings in PUBLIC_PUSH_CHECKLIST.md, found ${count}" >&2
        return 1
    fi
    return 0
}

# =============================================================================
# TEST 11 — test_release_readiness_report_in_gitignore
#
# AC 9
# Given: .gitignore exists in repo root
# When:  grep for "release-readiness-report.json" (escaped dot) in .gitignore
# Then:  match found
# =============================================================================
test_release_readiness_report_in_gitignore() {
    if [[ ! -f "${GITIGNORE}" ]]; then
        echo "    ERROR: .gitignore does not exist in repo root" >&2
        return 1
    fi

    if ! grep -qE "release-readiness-report\.json" "${GITIGNORE}"; then
        echo "    ERROR: 'release-readiness-report.json' is not listed in .gitignore" >&2
        return 1
    fi
    return 0
}

# =============================================================================
# TEST 12 — test_shellcheck_clean
#
# AC 10
# Given: scripts/release-readiness-gate.sh exists
# When:  shellcheck --severity=error on gate script and this verify script
# Then:  exit code 0 (no errors)
# SKIP:  if shellcheck is not installed (with exit 0 — no hard dep on tool)
# =============================================================================
test_shellcheck_clean() {
    if ! command -v shellcheck &>/dev/null; then
        echo "    SKIP: shellcheck not found in PATH"
        return 0
    fi

    local all_clean=0

    if [[ -f "${GATE_SCRIPT}" ]]; then
        local sc_rc=0
        shellcheck --severity=error "${GATE_SCRIPT}" || sc_rc=$?
        if [[ "${sc_rc}" -ne 0 ]]; then
            echo "    ERROR: shellcheck found errors in scripts/release-readiness-gate.sh" >&2
            all_clean=1
        fi
    else
        echo "    SKIP (shellcheck on gate): scripts/release-readiness-gate.sh does not exist"
    fi

    # Bonus: shellcheck this verify script itself
    if [[ -f "${VERIFY_SELF}" ]]; then
        local sc_self_rc=0
        shellcheck --severity=error "${VERIFY_SELF}" || sc_self_rc=$?
        if [[ "${sc_self_rc}" -ne 0 ]]; then
            echo "    ERROR: shellcheck found errors in scripts/verify-release-readiness.sh" >&2
            all_clean=1
        fi
    fi

    return "${all_clean}"
}

# =============================================================================
# MAIN — run all 12 tests
# =============================================================================
main() {
    echo "============================================================"
    echo "verify-release-readiness.sh — Story 8.9 Acceptance Tests"
    echo "REPO_ROOT: ${REPO_ROOT}"
    echo "============================================================"
    echo ""

    run_test "test_release_gate_script_exists_and_executable"
    run_test "test_release_gate_invokes_all_seven_scripts"
    run_test "test_release_gate_three_modes"
    run_test "test_release_gate_preflight_aborts_when_subscript_missing"
    run_test "test_release_gate_full_run_against_real_repo"
    run_test "test_release_gate_report_mode_produces_valid_json"
    run_test "test_security_reports_index_exists"
    run_test "test_security_reports_index_lists_all_three"
    run_test "test_public_push_checklist_exists"
    run_test "test_public_push_checklist_has_six_steps"
    run_test "test_release_readiness_report_in_gitignore"
    run_test "test_shellcheck_clean"

    echo ""
    echo "============================================================"
    echo "Results: ${TESTS_PASSED}/${TESTS_RUN} passed, ${TESTS_FAILED} failed"

    if [[ "${TESTS_FAILED}" -gt 0 ]]; then
        echo "FAILED tests:"
        local n
        for n in "${FAILED_NAMES[@]}"; do
            echo "  - ${n}"
        done
        echo "============================================================"
        return 1
    fi

    echo "ALL TESTS PASSED"
    echo "============================================================"
    return 0
}

main
