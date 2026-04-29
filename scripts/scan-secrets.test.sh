#!/usr/bin/env bash
# =============================================================================
# scan-secrets.test.sh
# Acceptance tests for Story 8.5 — Secret-Scan-Gate: gitleaks History-Scan
# + CI-Integration
# Test framework: pure Bash with exit codes (no external dependencies)
#
# Usage: bash scripts/scan-secrets.test.sh
#
# Tests run in isolated mktemp sandbox repos — zero impact on host repo.
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUBJECT_SCRIPT="${SCRIPT_DIR}/scan-secrets.sh"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
GITLEAKS_CONFIG="${REPO_ROOT}/.gitleaks.toml"

# ---------------------------------------------------------------------------
# Global counters
# ---------------------------------------------------------------------------
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_NAMES=()

# ---------------------------------------------------------------------------
# Cleanup registry: each test registers its sandbox dir here so the
# EXIT trap can wipe everything even if the test crashes mid-way.
# ---------------------------------------------------------------------------
declare -a _SANDBOX_DIRS=()

cleanup_all() {
    local d
    for d in "${_SANDBOX_DIRS[@]:-}"; do
        [[ -d "$d" ]] && rm -rf "$d"
    done
}
trap cleanup_all EXIT INT TERM

# ---------------------------------------------------------------------------
# Helper: setup_sandbox_repo
#
# Creates an isolated git repo in a temp directory.
# The caller receives the repo path via the global SANDBOX variable.
# ---------------------------------------------------------------------------
setup_sandbox_repo() {
    local sandbox
    sandbox="$(mktemp -d)"
    _SANDBOX_DIRS+=("$sandbox")

    git -C "$sandbox" init -q
    git -C "$sandbox" config user.name  "Test User"
    git -C "$sandbox" config user.email "test@example.com"

    # Return path to caller via global
    SANDBOX="$sandbox"
}

# ---------------------------------------------------------------------------
# Helper: assert_exit_zero
# ---------------------------------------------------------------------------
assert_exit_zero() {
    local code="$1" label="$2"
    if [[ "$code" -ne 0 ]]; then
        echo "  FAIL [$label]: expected exit 0, got $code" >&2
        return 1
    fi
    return 0
}

# ---------------------------------------------------------------------------
# Helper: assert_exit_nonzero
# ---------------------------------------------------------------------------
assert_exit_nonzero() {
    local code="$1" label="$2"
    if [[ "$code" -eq 0 ]]; then
        echo "  FAIL [$label]: expected non-zero exit, got 0" >&2
        return 1
    fi
    return 0
}

# ---------------------------------------------------------------------------
# Helper: assert_output_contains
# ---------------------------------------------------------------------------
assert_output_contains() {
    local output="$1" needle="$2" label="$3"
    if ! grep -qF "$needle" <<<"$output"; then
        echo "  FAIL [$label]: expected output to contain '${needle}'" >&2
        echo "  Actual output: ${output}" >&2
        return 1
    fi
    return 0
}

# ---------------------------------------------------------------------------
# Helper: validate_json_file
#
# Validates that a file is parseable JSON.
# Uses python3 as primary, jq as fallback.
# ---------------------------------------------------------------------------
validate_json_file() {
    local filepath="$1" label="$2"
    if [[ ! -f "$filepath" ]]; then
        echo "  FAIL [$label]: JSON file not found: ${filepath}" >&2
        return 1
    fi
    if command -v python3 >/dev/null 2>&1; then
        if ! python3 -c "import json, sys; json.load(open(sys.argv[1]))" "$filepath" 2>/dev/null; then
            echo "  FAIL [$label]: file is not valid JSON: ${filepath}" >&2
            return 1
        fi
    elif command -v jq >/dev/null 2>&1; then
        if ! jq empty "$filepath" 2>/dev/null; then
            echo "  FAIL [$label]: file is not valid JSON (jq): ${filepath}" >&2
            return 1
        fi
    else
        echo "  FAIL [$label]: neither python3 nor jq available for JSON validation" >&2
        return 1
    fi
    return 0
}

# ---------------------------------------------------------------------------
# Test runner helpers
# ---------------------------------------------------------------------------
run_test() {
    local name="$1"
    TESTS_RUN=$(( TESTS_RUN + 1 ))
    echo "--- ${name}"
    if "$name"; then
        echo "    PASS"
        TESTS_PASSED=$(( TESTS_PASSED + 1 ))
    else
        echo "    FAIL"
        TESTS_FAILED=$(( TESTS_FAILED + 1 ))
        FAILED_NAMES+=("$name")
    fi
}

# =============================================================================
# TEST 1 — test_history_detects_synthetic_aws_key
#
# AC: Story 8.5 Acceptance Test #1
# Given: Sandbox Git repo with a commit containing AKIAIOSFODNN7EXAMPLE
#        (synthetic AWS Access Key ID — official AWS documentation example,
#         format matches gitleaks default rule, NOT a real credential)
# When:  scripts/scan-secrets.sh --history runs in the sandbox repo
# Then:  Exit code != 0; gitleaks-report.json is NOT written (non-CI mode)
# =============================================================================
test_history_detects_synthetic_aws_key() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    # Create an initial empty commit so the repo is valid
    git -C "$SANDBOX" commit --allow-empty -q -m "chore: init"

    # Create a file containing the official AWS example key
    # AKIAIOSFODNN7EXAMPLE is from https://docs.aws.amazon.com/IAM/latest/UserGuide/security-creds.html
    # It is listed as an example, not a real credential.
    printf 'AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n' > "${SANDBOX}/config.env"
    git -C "$SANDBOX" add config.env
    git -C "$SANDBOX" commit -q -m "chore: add example aws key"

    local report_file="${SANDBOX}/gitleaks-report.json"
    local exit_code=0
    (cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --history) >/dev/null 2>&1 || exit_code=$?

    assert_exit_nonzero "$exit_code" "--history exit code on secret repo" || return 1

    # --history mode must NOT write a report file (only --ci mode writes one)
    if [[ -f "$report_file" ]]; then
        echo "  FAIL: --history mode must not write gitleaks-report.json" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 2 — test_staged_blocks_unstaged_secret
#
# AC: Story 8.5 Acceptance Test #2
# Given: Sandbox Git repo with a staged (but not yet committed) file
#        containing a synthetic secret pattern
# When:  scripts/scan-secrets.sh --staged runs in the sandbox repo
# Then:  Exit code 1; staged finding is detected
# =============================================================================
test_staged_blocks_unstaged_secret() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    # Initial commit so the repo has a HEAD
    git -C "$SANDBOX" commit --allow-empty -q -m "chore: init"

    # Stage a file with a synthetic AWS key — do NOT commit it
    printf 'AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n' > "${SANDBOX}/secrets.env"
    git -C "$SANDBOX" add secrets.env

    local exit_code=0
    (cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --staged) >/dev/null 2>&1 || exit_code=$?

    assert_exit_nonzero "$exit_code" "--staged exit code on staged secret" || return 1

    return 0
}

# =============================================================================
# TEST 3 — test_staged_passes_whitelisted_path
#
# AC: Story 8.5 Acceptance Test #3
# Given: Sandbox Git repo with project .gitleaks.toml copied in (which
#        allow-lists _bmad-output/), staged file at _bmad-output/test-example.md
#        containing a synthetic token
# When:  scripts/scan-secrets.sh --staged runs in the sandbox repo
# Then:  Exit code 0; the whitelisted path is not reported as a finding
# =============================================================================
test_staged_passes_whitelisted_path() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    # Also need the config to exist for this test
    if [[ ! -f "$GITLEAKS_CONFIG" ]]; then
        echo "  FAIL: .gitleaks.toml not found at ${GITLEAKS_CONFIG}" >&2
        return 1
    fi

    setup_sandbox_repo

    # Initial commit so the repo has a HEAD
    git -C "$SANDBOX" commit --allow-empty -q -m "chore: init"

    # Note: scan-secrets.sh resolves CONFIG_FILE relative to its own location
    # (the project repo), so the sandbox uses the host project's .gitleaks.toml
    # by design. The "_bmad-output/" path is allow-listed in that config, so
    # the staged secret below is expected to pass --staged.
    mkdir -p "${SANDBOX}/_bmad-output"
    printf 'AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n' > "${SANDBOX}/_bmad-output/test-example.md"
    git -C "$SANDBOX" add _bmad-output/test-example.md

    local exit_code=0
    (cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --staged) >/dev/null 2>&1 || exit_code=$?

    assert_exit_zero "$exit_code" "--staged exit code on allow-listed path" || return 1

    return 0
}

# =============================================================================
# TEST 4 — test_ci_produces_valid_json_on_finding
#
# AC: Story 8.5 Acceptance Test #4
# Given: Sandbox Git repo with a commit containing AKIAIOSFODNN7EXAMPLE
# When:  scripts/scan-secrets.sh --ci runs in the sandbox repo
# Then:  Exit code 1; gitleaks-report.json exists and is valid JSON with
#        a non-empty array
# =============================================================================
test_ci_produces_valid_json_on_finding() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    # Create commit with synthetic secret
    printf 'AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n' > "${SANDBOX}/config.env"
    git -C "$SANDBOX" add config.env
    git -C "$SANDBOX" commit -q -m "chore: add example aws key"

    local report_file="${SANDBOX}/gitleaks-report.json"
    local exit_code=0
    (cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --ci) >/dev/null 2>&1 || exit_code=$?

    assert_exit_nonzero "$exit_code" "--ci exit code on repo with secret" || return 1

    # Report file must exist and be valid JSON
    validate_json_file "$report_file" "gitleaks-report.json" || return 1

    # The JSON must contain a non-empty array (at least one finding)
    local array_len=0
    if command -v python3 >/dev/null 2>&1; then
        array_len="$(python3 -c "import json, sys; data=json.load(open(sys.argv[1])); print(len(data) if isinstance(data, list) else -1)" "$report_file" 2>/dev/null || echo 0)"
    elif command -v jq >/dev/null 2>&1; then
        array_len="$(jq 'length' "$report_file" 2>/dev/null || echo 0)"
    fi

    if [[ "$array_len" -lt 1 ]]; then
        echo "  FAIL: gitleaks-report.json must contain at least one finding, got length=${array_len}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 5 — test_ci_exits_zero_on_clean_repo
#
# AC: Story 8.5 Acceptance Test #5
# Given: Sandbox Git repo with commits containing no secret patterns
# When:  scripts/scan-secrets.sh --ci runs in the sandbox repo
# Then:  Exit code 0; gitleaks-report.json exists and contains [] or
#        empty JSON array
# =============================================================================
test_ci_exits_zero_on_clean_repo() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    # Create commits with no secrets
    printf 'Hello from a clean file with no secrets.\n' > "${SANDBOX}/readme.txt"
    git -C "$SANDBOX" add readme.txt
    git -C "$SANDBOX" commit -q -m "chore: add clean readme"

    printf 'DATABASE_URL=postgres://localhost/myapp_test\n' > "${SANDBOX}/config.txt"
    git -C "$SANDBOX" add config.txt
    git -C "$SANDBOX" commit -q -m "chore: add placeholder config"

    local report_file="${SANDBOX}/gitleaks-report.json"
    local exit_code=0
    (cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --ci) >/dev/null 2>&1 || exit_code=$?

    assert_exit_zero "$exit_code" "--ci exit code on clean repo" || return 1

    # Report file must exist and be valid JSON
    validate_json_file "$report_file" "gitleaks-report.json on clean repo" || return 1

    # The JSON must be an empty array (or null, which gitleaks emits when
    # there are no findings). Anything else — non-zero array length, malformed
    # output, or a JSON parser failure — is a FAIL.
    local array_len=""
    if command -v python3 >/dev/null 2>&1; then
        array_len="$(python3 -c "
import json, sys
data = json.load(open(sys.argv[1]))
if data is None or (isinstance(data, list) and len(data) == 0):
    print(0)
elif isinstance(data, list):
    print(len(data))
else:
    sys.exit(1)
" "$report_file" 2>/dev/null)" || array_len=""
    elif command -v jq >/dev/null 2>&1; then
        array_len="$(jq 'if . == null then 0 elif type == "array" then length else error("not array or null") end' "$report_file" 2>/dev/null)" || array_len=""
    else
        echo "  FAIL: neither python3 nor jq available to validate clean-repo JSON shape" >&2
        return 1
    fi

    if [[ -z "$array_len" ]]; then
        echo "  FAIL: gitleaks-report.json is not an empty array or null on a clean repo" >&2
        return 1
    fi
    if [[ "$array_len" -ne 0 ]]; then
        echo "  FAIL: expected zero findings on clean repo, got ${array_len}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 6 — test_pre_flight_aborts_when_gitleaks_missing
#
# AC: Story 8.5 Acceptance Test #6
# Given: PATH restricted to /usr/bin:/bin so gitleaks is not available
# When:  scripts/scan-secrets.sh --history is invoked
# Then:  Exit code != 0; stderr contains installation hint
#        ("gitleaks not installed" or "brew install gitleaks")
# =============================================================================
test_pre_flight_aborts_when_gitleaks_missing() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    git -C "$SANDBOX" commit --allow-empty -q -m "chore: init"

    # Restrict PATH so gitleaks is not reachable
    local restricted_path="/usr/bin:/bin"

    local stderr_output exit_code=0
    stderr_output="$(cd "$SANDBOX" && PATH="$restricted_path" bash "$SUBJECT_SCRIPT" --history 2>&1 >/dev/null)" || exit_code=$?

    assert_exit_nonzero "$exit_code" "gitleaks-missing abort exit code" || return 1

    # stderr must contain either the "not found" message or the install hint
    local found_hint=0
    if grep -qiF "gitleaks" <<<"$stderr_output"; then
        found_hint=1
    fi
    if [[ "$found_hint" -eq 0 ]]; then
        echo "  FAIL: stderr must reference gitleaks (install hint or error message)" >&2
        echo "  Actual stderr: ${stderr_output}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 7 — test_allowlist_entry_suppresses_known_finding
#
# AC: Story 8.5 Acceptance Test #7
# Given: Sandbox Git repo with a commit in a path matching the allow-list
#        pattern (gateway/internal/auth/testdata/) containing a test key
#        string, and project .gitleaks.toml present
# When:  scripts/scan-secrets.sh --history runs in the sandbox repo
# Then:  Exit code 0; the allow-listed path is not reported as a finding
# =============================================================================
test_allowlist_entry_suppresses_known_finding() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    # Also need the config to exist for this test
    if [[ ! -f "$GITLEAKS_CONFIG" ]]; then
        echo "  FAIL: .gitleaks.toml not found at ${GITLEAKS_CONFIG}" >&2
        return 1
    fi

    setup_sandbox_repo

    # scan-secrets.sh resolves CONFIG_FILE from its own SCRIPT_DIR (the host
    # project root), so the sandbox does not need its own .gitleaks.toml — the
    # host config's allowlist for gateway/internal/auth/testdata/ applies.
    mkdir -p "${SANDBOX}/gateway/internal/auth/testdata"
    printf 'AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n' \
        > "${SANDBOX}/gateway/internal/auth/testdata/test-config.env"
    git -C "$SANDBOX" add .
    git -C "$SANDBOX" commit -q -m "chore: add test fixture with synthetic key"

    local exit_code=0
    (cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --history) >/dev/null 2>&1 || exit_code=$?

    assert_exit_zero "$exit_code" "--history exit code with allow-listed path" || return 1

    return 0
}

# =============================================================================
# TEST 8 — test_script_passes_shellcheck
#
# AC: Story 8.5 Acceptance Test #8 / AC 9
# Given: shellcheck is in PATH (SKIP if absent)
# When:  shellcheck --severity=error scripts/scan-secrets.sh
# Then:  Exit 0; no errors printed
# =============================================================================
test_script_passes_shellcheck() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    if ! command -v shellcheck >/dev/null 2>&1; then
        echo "  SKIP: shellcheck not in PATH — install shellcheck to enable this check"
        # Exit 0 so the overall suite does not fail in environments without shellcheck
        return 0
    fi

    local output exit_code=0
    output="$(shellcheck --severity=error "$SUBJECT_SCRIPT" 2>&1)" || exit_code=$?

    if [[ "$exit_code" -ne 0 ]]; then
        echo "  FAIL: shellcheck reported errors:" >&2
        echo "$output" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 9 — test_runbook_exists_and_references_story_8_10_gate
#
# AC: Story 8.5 AC7 (runbook exists) + AC10 (Story 8.10 gate statement)
# Given: Repo after implementation
# When:  Check scripts/SECRET_SCAN_RUNBOOK.md
# Then:  File exists AND contains a reference to "Story 8.10"
# =============================================================================
test_runbook_exists_and_references_story_8_10_gate() {
    local runbook="${REPO_ROOT}/scripts/SECRET_SCAN_RUNBOOK.md"

    if [[ ! -f "$runbook" ]]; then
        echo "  FAIL: SECRET_SCAN_RUNBOOK.md not found at ${runbook}" >&2
        return 1
    fi

    if ! grep -qF "Story 8.10" "$runbook"; then
        echo "  FAIL: SECRET_SCAN_RUNBOOK.md does not reference 'Story 8.10' gate" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# MAIN
# =============================================================================
main() {
    echo "=========================================================="
    echo "  Story 8.5 — Acceptance Tests"
    echo "  Target: ${SUBJECT_SCRIPT}"
    echo "=========================================================="
    echo ""

    run_test test_history_detects_synthetic_aws_key
    run_test test_staged_blocks_unstaged_secret
    run_test test_staged_passes_whitelisted_path
    run_test test_ci_produces_valid_json_on_finding
    run_test test_ci_exits_zero_on_clean_repo
    run_test test_pre_flight_aborts_when_gitleaks_missing
    run_test test_allowlist_entry_suppresses_known_finding
    run_test test_script_passes_shellcheck
    run_test test_runbook_exists_and_references_story_8_10_gate

    echo ""
    echo "=========================================================="
    echo "  Results: ${TESTS_PASSED}/${TESTS_RUN} passed, ${TESTS_FAILED} failed"
    echo "=========================================================="

    if [[ "${#FAILED_NAMES[@]}" -gt 0 ]]; then
        echo ""
        echo "  Failed tests:"
        local name
        for name in "${FAILED_NAMES[@]}"; do
            echo "    - ${name}"
        done
        echo ""
    fi

    if [[ "$TESTS_FAILED" -gt 0 ]]; then
        exit 1
    fi
    exit 0
}

main "$@"
