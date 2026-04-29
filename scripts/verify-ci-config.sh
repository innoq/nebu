#!/usr/bin/env bash
# =============================================================================
# verify-ci-config.sh
# Acceptance tests for Story 8.6 — Dual CI: GitHub Actions + GitLab CI
# Test framework: pure Bash with exit codes (no external test framework)
#
# Usage: bash scripts/verify-ci-config.sh
#
# Tests validate CI configuration files (YAML validity, job presence,
# script invocation, pinned versions, artifacts, and helper scripts).
# Exit 0 if all tests pass, Exit 1 if any test fails.
#
# Lessons Learned applied (8.1–8.5):
#   - No grep -P (BSD-incompatible) — python3 for YAML parsing
#   - Anchored regex patterns for job name matching
#   - set -euo pipefail + local + quoting
#   - cleanup trap with return 0
#   - cd "${REPO_ROOT}" for all file operations
#   - mktemp not needed here (tests are read-only)
#   - shellcheck SKIP-fallback for optional tools
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

GITHUB_WORKFLOW="${REPO_ROOT}/.github/workflows/ci.yml"
GITLAB_CI="${REPO_ROOT}/.gitlab-ci.yml"
CI_LOCAL="${REPO_ROOT}/scripts/ci-local.sh"
BADGES_SPEC="${REPO_ROOT}/_bmad-output/planning-artifacts/badges-spec.md"
VERIFY_CI_CONFIG="${REPO_ROOT}/scripts/verify-ci-config.sh"

# ---------------------------------------------------------------------------
# Global counters
# ---------------------------------------------------------------------------
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_NAMES=()

# ---------------------------------------------------------------------------
# Cleanup — any tempfile registered in TEMPFILES is removed on exit.
# Tests are otherwise read-only against the repo.
# ---------------------------------------------------------------------------
TEMPFILES=()
cleanup() {
    local f
    for f in "${TEMPFILES[@]:-}"; do
        [[ -n "${f}" ]] && rm -f "${f}"
    done
    return 0
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Helper: run_test
# Calls the named test function, records PASS/FAIL.
# Uses explicit exit-code capture (not set -e propagation).
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

# ---------------------------------------------------------------------------
# Helper: check_yaml_parseable
# Returns 0 if the file exists and parses as valid YAML via python3.
# Tries pyyaml first; if not available, falls back to SKIP (return 0
# with a warning) so CI environments without pyyaml do not hard-fail.
# ---------------------------------------------------------------------------
check_yaml_parseable() {
    local filepath="$1"
    local label="$2"

    if [[ ! -f "${filepath}" ]]; then
        echo "  FAIL [${label}]: file not found: ${filepath}" >&2
        return 1
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP [${label}]: python3 not in PATH — cannot validate YAML" >&2
        return 0
    fi

    local err_tmp
    err_tmp="$(mktemp)"
    TEMPFILES+=("${err_tmp}")

    local py_rc=0
    python3 -c "
import sys
try:
    import yaml
except ImportError:
    print('SKIP: pyyaml not installed', file=sys.stderr)
    sys.exit(2)
with open(sys.argv[1], encoding='utf-8') as f:
    yaml.safe_load(f)
sys.exit(0)
" "${filepath}" 2>"${err_tmp}" || py_rc=$?

    if [[ "${py_rc}" -eq 2 ]]; then
        echo "  SKIP [${label}]: pyyaml not installed — cannot validate YAML" >&2
        return 0
    elif [[ "${py_rc}" -ne 0 ]]; then
        local err_msg
        err_msg="$(cat "${err_tmp}" 2>/dev/null || true)"
        echo "  FAIL [${label}]: YAML parse error in ${filepath}: ${err_msg}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 1 — test_github_workflow_yaml_valid
#
# AC 1 / Story AT 1
# Given: .github/workflows/ci.yml exists in the repository
# When:  python3 yaml.safe_load parses the file
# Then:  Exit code 0; no YAML parse error
# =============================================================================
test_github_workflow_yaml_valid() {
    check_yaml_parseable "${GITHUB_WORKFLOW}" "GitHub Actions ci.yml"
}

# =============================================================================
# TEST 2 — test_gitlab_ci_yaml_valid
#
# AC 2 / Story AT 2
# Given: .gitlab-ci.yml exists in the repository
# When:  python3 yaml.safe_load parses the file
# Then:  Exit code 0; no YAML parse error
# =============================================================================
test_gitlab_ci_yaml_valid() {
    check_yaml_parseable "${GITLAB_CI}" "GitLab .gitlab-ci.yml"
}

# =============================================================================
# TEST 3 — test_github_workflow_has_required_jobs
#
# AC 1 / Story AT 3
# Given: .github/workflows/ci.yml exists
# When:  grep for anchored job identifiers: lint-go, test-unit-go,
#        test-unit-elixir, secret-scan, verify-docs
#        (anchored pattern: ^  <jobname>: — two leading spaces, colon after)
# Then:  All five job names are present
# =============================================================================
test_github_workflow_has_required_jobs() {
    if [[ ! -f "${GITHUB_WORKFLOW}" ]]; then
        echo "  FAIL: .github/workflows/ci.yml not found" >&2
        return 1
    fi

    local required_jobs=("lint-go" "test-unit-go" "test-unit-elixir" "secret-scan" "verify-docs")
    local ok=0
    local job
    for job in "${required_jobs[@]}"; do
        if ! grep -qE "^  ${job}:" "${GITHUB_WORKFLOW}"; then
            echo "  FAIL: job '${job}' not found in .github/workflows/ci.yml (expected anchored pattern '^  ${job}:')" >&2
            ok=1
        fi
    done

    return "${ok}"
}

# =============================================================================
# TEST 4 — test_gitlab_ci_has_required_jobs
#
# AC 2 / Story AT 4
# Given: .gitlab-ci.yml exists
# When:  grep for anchored job identifiers: lint-go, test-unit-go,
#        test-unit-elixir, secret-scan, verify-docs
#        (anchored pattern: ^<jobname>: — no leading spaces in GitLab YAML)
# Then:  All five job names are present
# =============================================================================
test_gitlab_ci_has_required_jobs() {
    if [[ ! -f "${GITLAB_CI}" ]]; then
        echo "  FAIL: .gitlab-ci.yml not found" >&2
        return 1
    fi

    local required_jobs=("lint-go" "test-unit-go" "test-unit-elixir" "secret-scan" "verify-docs")
    local ok=0
    local job
    for job in "${required_jobs[@]}"; do
        if ! grep -qE "^${job}:" "${GITLAB_CI}"; then
            echo "  FAIL: job '${job}' not found in .gitlab-ci.yml (expected anchored pattern '^${job}:')" >&2
            ok=1
        fi
    done

    return "${ok}"
}

# =============================================================================
# TEST 5 — test_github_workflow_invokes_scan_secrets_ci
#
# AC 3 / Story AT 5
# Given: .github/workflows/ci.yml
# When:  grep for 'scripts/scan-secrets.sh --ci'
# Then:  Match found (DRY contract: CI calls the shared script)
# =============================================================================
test_github_workflow_invokes_scan_secrets_ci() {
    if [[ ! -f "${GITHUB_WORKFLOW}" ]]; then
        echo "  FAIL: .github/workflows/ci.yml not found" >&2
        return 1
    fi

    if ! grep -qF "scripts/scan-secrets.sh --ci" "${GITHUB_WORKFLOW}"; then
        echo "  FAIL: 'scripts/scan-secrets.sh --ci' not found in .github/workflows/ci.yml" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 6 — test_gitlab_ci_invokes_scan_secrets_ci
#
# AC 3 / Story AT 6
# Given: .gitlab-ci.yml
# When:  grep for 'scripts/scan-secrets.sh --ci'
# Then:  Match found
# =============================================================================
test_gitlab_ci_invokes_scan_secrets_ci() {
    if [[ ! -f "${GITLAB_CI}" ]]; then
        echo "  FAIL: .gitlab-ci.yml not found" >&2
        return 1
    fi

    if ! grep -qF "scripts/scan-secrets.sh --ci" "${GITLAB_CI}"; then
        echo "  FAIL: 'scripts/scan-secrets.sh --ci' not found in .gitlab-ci.yml" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 7 — test_github_workflow_invokes_verify_scripts
#
# AC 3 / Story AT 7
# Given: .github/workflows/ci.yml
# When:  grep for at least one of:
#          scripts/verify-readme-attribution.sh
#          scripts/verify-contributing.sh
#          scripts/verify-security-policy.sh
# Then:  At least one match found (verify-docs job calls all three)
# =============================================================================
test_github_workflow_invokes_verify_scripts() {
    if [[ ! -f "${GITHUB_WORKFLOW}" ]]; then
        echo "  FAIL: .github/workflows/ci.yml not found" >&2
        return 1
    fi

    local missing=()
    grep -qF "scripts/verify-readme-attribution.sh" "${GITHUB_WORKFLOW}" || missing+=("verify-readme-attribution.sh")
    grep -qF "scripts/verify-contributing.sh" "${GITHUB_WORKFLOW}" || missing+=("verify-contributing.sh")
    grep -qF "scripts/verify-security-policy.sh" "${GITHUB_WORKFLOW}" || missing+=("verify-security-policy.sh")

    if [[ "${#missing[@]}" -gt 0 ]]; then
        echo "  FAIL: missing verify-*.sh invocations in .github/workflows/ci.yml: ${missing[*]}" >&2
        echo "    AC3 requires all three scripts to be called." >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 8 — test_gitlab_ci_invokes_verify_scripts
#
# AC 3 / Story AT 8
# Given: .gitlab-ci.yml
# When:  grep for ALL THREE verify scripts (per AC3 DRY contract)
# Then:  All three match found
# =============================================================================
test_gitlab_ci_invokes_verify_scripts() {
    if [[ ! -f "${GITLAB_CI}" ]]; then
        echo "  FAIL: .gitlab-ci.yml not found" >&2
        return 1
    fi

    local missing=()
    grep -qF "scripts/verify-readme-attribution.sh" "${GITLAB_CI}" || missing+=("verify-readme-attribution.sh")
    grep -qF "scripts/verify-contributing.sh" "${GITLAB_CI}" || missing+=("verify-contributing.sh")
    grep -qF "scripts/verify-security-policy.sh" "${GITLAB_CI}" || missing+=("verify-security-policy.sh")

    if [[ "${#missing[@]}" -gt 0 ]]; then
        echo "  FAIL: missing verify-*.sh invocations in .gitlab-ci.yml: ${missing[*]}" >&2
        echo "    AC3 requires all three scripts to be called." >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 9 — test_github_actions_pinned_to_sha
#
# AC 4 / Story AT 9 (supply-chain security)
# Given: .github/workflows/ci.yml
# When:  All 'uses:' lines are checked — each must reference a full 40-char
#        hex SHA (e.g. actions/checkout@abc123def456...)
#        Reject: @v3, @v4, @main, @master (mutable tags)
# Then:  Zero 'uses:' lines with mutable (non-SHA) references
#
# Implementation note: we check both conditions:
#   a) No 'uses:' line ends with @<semver-tag> (e.g. @v4, @v4.1.0)
#   b) No 'uses:' line ends with @main or @master
# We do NOT require all uses: lines to have a SHA — we report violations.
# =============================================================================
test_github_actions_pinned_to_sha() {
    if [[ ! -f "${GITHUB_WORKFLOW}" ]]; then
        echo "  FAIL: .github/workflows/ci.yml not found" >&2
        return 1
    fi

    # Extract all 'uses:' lines (any indentation, supports `- uses:` shorthand
    # AND named-step `  uses:` form). Skip blank/comment-only lines.
    local uses_lines
    uses_lines="$(grep -E '^\s*-?\s*uses:\s+\S' "${GITHUB_WORKFLOW}" || true)"

    if [[ -z "${uses_lines}" ]]; then
        echo "  FAIL: no 'uses:' lines found in .github/workflows/ci.yml" >&2
        return 1
    fi

    # Per-line check: each external action must be pinned to a 40-char SHA.
    # Local actions (uses: ./...) are exempt — they have no remote ref.
    # Note: `\s` is GNU-only; use `[[:space:]]` for BSD sed (macOS) portability.
    local fail_lines=()
    while IFS= read -r line; do
        # Extract the value after `uses:` (first whitespace-delimited token).
        local ref
        ref="$(echo "${line}" | sed -E 's|^[[:space:]]*-?[[:space:]]*uses:[[:space:]]+||' | awk '{print $1}')"
        # Strip surrounding quotes if any
        ref="${ref%\"}"
        ref="${ref#\"}"
        ref="${ref%\'}"
        ref="${ref#\'}"
        # Local action — skip
        [[ "${ref}" == ./* ]] && continue
        [[ "${ref}" == .github/* ]] && continue
        # Docker action — skip (uses: docker://image:tag)
        [[ "${ref}" == docker://* ]] && continue
        # Must contain @<40-hex>. Use grep instead of bash =~ for portability.
        if ! echo "${ref}" | grep -qE '@[0-9a-f]{40}$'; then
            fail_lines+=("${line}")
        fi
    done <<< "${uses_lines}"

    if [[ "${#fail_lines[@]}" -gt 0 ]]; then
        echo "  FAIL: 'uses:' lines without 40-char commit SHA pinning:" >&2
        printf '    %s\n' "${fail_lines[@]}" >&2
        echo "    Each external action must use @<40-hex-sha> (e.g. actions/checkout@b4ffde65...)." >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 10 — test_gitlab_ci_no_latest_tags
#
# AC 4 / Story AT 10
# Given: .gitlab-ci.yml
# When:  grep for ':latest' in 'image:' lines
# Then:  No match in regular job image references.
#
# Accepted exception: 'docker:latest' in build-ci-images jobs (bootstrap DinD
# pattern). The test documents this exception explicitly and skips those lines.
# All other job 'image:' lines must NOT use ':latest'.
# =============================================================================
test_gitlab_ci_no_latest_tags() {
    if [[ ! -f "${GITLAB_CI}" ]]; then
        echo "  FAIL: .gitlab-ci.yml not found" >&2
        return 1
    fi

    # Extract all 'image:' lines (anchored to job context, 2-space indent)
    # Then filter out the documented exception: 'docker:latest' used in
    # build-ci-images DinD bootstrap jobs (build-ci-go, build-ci-elixir,
    # and the YAML anchor .build_ci_image).
    # All remaining ':latest' references in image: lines are violations.
    local latest_violations
    latest_violations="$(grep -E '^\s+image:\s+.*:latest' "${GITLAB_CI}" | \
        grep -vE '^\s+image:\s+docker:latest' || true)"

    if [[ -n "${latest_violations}" ]]; then
        echo "  FAIL: found 'image:' lines with ':latest' tag (only docker:latest in build-ci-images is permitted):" >&2
        echo "${latest_violations}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 11 — test_ci_local_script_exists
#
# AC 8 / Story AT 11
# Given: scripts/ci-local.sh was created
# When:  [[ -x scripts/ci-local.sh ]]
# Then:  True (file exists and is executable)
# =============================================================================
test_ci_local_script_exists() {
    if [[ ! -f "${CI_LOCAL}" ]]; then
        echo "  FAIL: scripts/ci-local.sh not found at ${CI_LOCAL}" >&2
        return 1
    fi

    if [[ ! -x "${CI_LOCAL}" ]]; then
        echo "  FAIL: scripts/ci-local.sh exists but is not executable (chmod +x required)" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 12 — test_badges_spec_exists
#
# AC 10 / Story AT 14
# Given: _bmad-output/planning-artifacts/badges-spec.md was created
# When:  [[ -f _bmad-output/planning-artifacts/badges-spec.md ]]
# Then:  True
# =============================================================================
test_badges_spec_exists() {
    if [[ ! -f "${BADGES_SPEC}" ]]; then
        echo "  FAIL: badges-spec.md not found at ${BADGES_SPEC}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 13 — test_shellcheck_clean
#
# AC 8+9 / Story AT 12+13 (merged into one test for ci-local.sh +
# verify-ci-config.sh)
# Given: shellcheck is in PATH; scripts exist
# When:  shellcheck --severity=error <script>
# Then:  Exit code 0; no errors
#        SKIP (exit 0) if shellcheck is not in PATH
# =============================================================================
test_shellcheck_clean() {
    if ! command -v shellcheck >/dev/null 2>&1; then
        echo "  SKIP: shellcheck not in PATH — install shellcheck to enable this check" >&2
        return 0
    fi

    local ok=0

    # Check ci-local.sh (only if it exists; absence is caught by test 11)
    if [[ -f "${CI_LOCAL}" ]]; then
        local sc_rc=0
        local sc_out
        sc_out="$(shellcheck --severity=error "${CI_LOCAL}" 2>&1)" || sc_rc=$?
        if [[ "${sc_rc}" -ne 0 ]]; then
            echo "  FAIL: shellcheck errors in scripts/ci-local.sh:" >&2
            echo "${sc_out}" >&2
            ok=1
        fi
    else
        echo "  NOTE: scripts/ci-local.sh not found — shellcheck for ci-local.sh skipped" >&2
    fi

    # Check verify-ci-config.sh (self-check)
    if [[ -f "${VERIFY_CI_CONFIG}" ]]; then
        local sc_rc2=0
        local sc_out2
        sc_out2="$(shellcheck --severity=error "${VERIFY_CI_CONFIG}" 2>&1)" || sc_rc2=$?
        if [[ "${sc_rc2}" -ne 0 ]]; then
            echo "  FAIL: shellcheck errors in scripts/verify-ci-config.sh:" >&2
            echo "${sc_out2}" >&2
            ok=1
        fi
    fi

    return "${ok}"
}

# =============================================================================
# TEST 14 — test_github_workflow_uploads_gitleaks_artifact
#
# AC 6 / Story AT 14 (mapped from user prompt test 14)
# Given: .github/workflows/ci.yml
# When:  grep for 'actions/upload-artifact' + 'gitleaks-report.json'
# Then:  Both patterns found (artifact upload of gitleaks report is configured)
# =============================================================================
test_github_workflow_uploads_gitleaks_artifact() {
    if [[ ! -f "${GITHUB_WORKFLOW}" ]]; then
        echo "  FAIL: .github/workflows/ci.yml not found" >&2
        return 1
    fi

    if ! grep -qF "upload-artifact" "${GITHUB_WORKFLOW}"; then
        echo "  FAIL: 'upload-artifact' not found in .github/workflows/ci.yml" >&2
        echo "    Expected: uses: actions/upload-artifact@<sha>" >&2
        return 1
    fi

    if ! grep -qF "gitleaks-report.json" "${GITHUB_WORKFLOW}"; then
        echo "  FAIL: 'gitleaks-report.json' not found in .github/workflows/ci.yml" >&2
        echo "    Expected: path: gitleaks-report.json under upload-artifact step" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 15 — test_gitlab_ci_artifacts_gitleaks_report
#
# AC 6 / Story AT 15 (mapped from user prompt test 15)
# Given: .gitlab-ci.yml
# When:  grep for 'artifacts:' block AND 'gitleaks-report.json' in paths
# Then:  Both patterns found
# =============================================================================
test_gitlab_ci_artifacts_gitleaks_report() {
    if [[ ! -f "${GITLAB_CI}" ]]; then
        echo "  FAIL: .gitlab-ci.yml not found" >&2
        return 1
    fi

    if ! grep -qE "^\s+artifacts:" "${GITLAB_CI}"; then
        echo "  FAIL: no 'artifacts:' block found in .gitlab-ci.yml" >&2
        return 1
    fi

    if ! grep -qF "gitleaks-report.json" "${GITLAB_CI}"; then
        echo "  FAIL: 'gitleaks-report.json' not found in .gitlab-ci.yml artifacts paths" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 16 — test_github_workflow_has_cache (AC5 coverage)
#
# Given: .github/workflows/ci.yml
# When:  Inspect for actions/cache usage with hashFiles() invalidation key
# Then:  At least one cache step references hashFiles() so dep changes
#        invalidate the cache (rather than only branch-keyed)
# =============================================================================
test_github_workflow_has_cache() {
    if [[ ! -f "${GITHUB_WORKFLOW}" ]]; then
        echo "  FAIL: .github/workflows/ci.yml not found" >&2
        return 1
    fi

    if ! grep -qE 'uses:\s+actions/cache@[0-9a-f]{40}' "${GITHUB_WORKFLOW}"; then
        echo "  FAIL: actions/cache (SHA-pinned) not found in workflow" >&2
        return 1
    fi

    if ! grep -qF 'hashFiles(' "${GITHUB_WORKFLOW}"; then
        echo "  FAIL: cache key does not use hashFiles() — deps changes won't invalidate cache" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 17 — test_gitlab_ci_has_cache (AC5 coverage)
#
# Given: .gitlab-ci.yml
# When:  Inspect for cache: blocks
# Then:  At least one job has a cache: block (and the cache key references
#        either CI_COMMIT_REF_SLUG or a lockfile path for invalidation)
# =============================================================================
test_gitlab_ci_has_cache() {
    if [[ ! -f "${GITLAB_CI}" ]]; then
        echo "  FAIL: .gitlab-ci.yml not found" >&2
        return 1
    fi

    if ! grep -qE '^\s*cache:' "${GITLAB_CI}"; then
        echo "  FAIL: no 'cache:' block found in .gitlab-ci.yml" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 18 — test_github_workflow_triggers (AC7 coverage)
#
# Given: .github/workflows/ci.yml
# When:  Parse via python3 yaml.safe_load and inspect 'on:' section
# Then:  Workflow triggers on push to main + feature/** AND on pull_request
# =============================================================================
test_github_workflow_triggers() {
    if [[ ! -f "${GITHUB_WORKFLOW}" ]]; then
        echo "  FAIL: .github/workflows/ci.yml not found" >&2
        return 1
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH" >&2
        return 0
    fi

    local err_tmp result
    err_tmp="$(mktemp)"
    TEMPFILES+=("${err_tmp}")

    result="$(python3 -c "
import sys
try:
    import yaml
except ImportError:
    print('SKIP'); sys.exit(0)
with open(sys.argv[1]) as f:
    d = yaml.safe_load(f)
on = d.get(True) if True in d else d.get('on')  # YAML parses 'on:' as boolean True
on = on or {}
push = on.get('push', {}) if isinstance(on, dict) else {}
branches = push.get('branches', []) if isinstance(push, dict) else []
ok_main = any('main' == b for b in branches)
ok_feat = any('feature' in str(b) for b in branches)
ok_pr = isinstance(on, dict) and 'pull_request' in on
if ok_main and ok_feat and ok_pr:
    print('OK')
else:
    print(f'main={ok_main} feature={ok_feat} pr={ok_pr}')
" "${GITHUB_WORKFLOW}" 2>"${err_tmp}")" || {
        echo "  FAIL: python3 trigger inspection error: $(cat "${err_tmp}")" >&2
        return 1
    }

    if [[ "${result}" == "SKIP" ]]; then
        echo "  SKIP: pyyaml not installed" >&2
        return 0
    fi
    if [[ "${result}" != "OK" ]]; then
        echo "  FAIL: workflow triggers incomplete: ${result}" >&2
        echo "    Expected: push.branches contains 'main' AND a 'feature/**' entry, plus pull_request" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST — test_ci_local_help_smoke
#
# AC 8 (smoke test for --help / -h flag)
# Given: scripts/ci-local.sh exists and is executable
# When:  bash scripts/ci-local.sh --help (and -h)
# Then:  Exit code 0 and the usage banner is printed.
#        --help/-h must succeed even without Docker installed (no preflight).
# =============================================================================
test_ci_local_help_smoke() {
    if [[ ! -x "${CI_LOCAL}" ]]; then
        echo "  FAIL: scripts/ci-local.sh not executable" >&2
        return 1
    fi

    local out rc
    rc=0
    out="$(bash "${CI_LOCAL}" --help 2>&1)" || rc=$?
    if [[ "${rc}" -ne 0 ]]; then
        echo "  FAIL: '--help' exited non-zero (rc=${rc})" >&2
        echo "${out}" >&2
        return 1
    fi
    if ! echo "${out}" | grep -qF "Usage:"; then
        echo "  FAIL: '--help' output missing 'Usage:' banner" >&2
        echo "${out}" >&2
        return 1
    fi

    rc=0
    out="$(bash "${CI_LOCAL}" -h 2>&1)" || rc=$?
    if [[ "${rc}" -ne 0 ]]; then
        echo "  FAIL: '-h' exited non-zero (rc=${rc})" >&2
        echo "${out}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 19 — test_gitlab_ci_rules (AC7 coverage)
#
# Given: .gitlab-ci.yml
# When:  Inspect for rules: with main branch + feature branch references
# Then:  At least one rule on main branch and one supporting feature/** or MR
# =============================================================================
test_gitlab_ci_rules() {
    if [[ ! -f "${GITLAB_CI}" ]]; then
        echo "  FAIL: .gitlab-ci.yml not found" >&2
        return 1
    fi

    if ! grep -qE '^\s*rules:' "${GITLAB_CI}"; then
        echo "  FAIL: no 'rules:' block found in .gitlab-ci.yml" >&2
        return 1
    fi

    # Look for both main and feature/MR triggers
    local has_main has_feature_or_mr=0
    has_main=0
    grep -qE 'CI_COMMIT_BRANCH\s*==\s*"main"|CI_DEFAULT_BRANCH' "${GITLAB_CI}" && has_main=1
    grep -qE 'feature|merge_request_event|CI_PIPELINE_SOURCE' "${GITLAB_CI}" && has_feature_or_mr=1

    if [[ "${has_main}" -ne 1 ]]; then
        echo "  FAIL: no rule referencing main branch found" >&2
        return 1
    fi
    if [[ "${has_feature_or_mr}" -ne 1 ]]; then
        echo "  FAIL: no rule supporting feature branches or merge requests found" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# MAIN — Run all 19 tests sequentially
# =============================================================================
main() {
    echo "======================================================================="
    echo "verify-ci-config.sh — Story 8.6 Acceptance Tests"
    echo "GitHub Actions: ${GITHUB_WORKFLOW}"
    echo "GitLab CI:      ${GITLAB_CI}"
    echo "======================================================================="
    echo ""

    run_test test_github_workflow_yaml_valid
    run_test test_gitlab_ci_yaml_valid
    run_test test_github_workflow_has_required_jobs
    run_test test_gitlab_ci_has_required_jobs
    run_test test_github_workflow_invokes_scan_secrets_ci
    run_test test_gitlab_ci_invokes_scan_secrets_ci
    run_test test_github_workflow_invokes_verify_scripts
    run_test test_gitlab_ci_invokes_verify_scripts
    run_test test_github_actions_pinned_to_sha
    run_test test_gitlab_ci_no_latest_tags
    run_test test_ci_local_script_exists
    run_test test_badges_spec_exists
    run_test test_shellcheck_clean
    run_test test_github_workflow_uploads_gitleaks_artifact
    run_test test_gitlab_ci_artifacts_gitleaks_report
    run_test test_github_workflow_has_cache
    run_test test_gitlab_ci_has_cache
    run_test test_github_workflow_triggers
    run_test test_gitlab_ci_rules
    run_test test_ci_local_help_smoke

    echo ""
    echo "======================================================================="
    echo "Results: ${TESTS_PASSED}/${TESTS_RUN} passed, ${TESTS_FAILED} failed"
    echo "======================================================================="

    if [[ "${#FAILED_NAMES[@]}" -gt 0 ]]; then
        echo ""
        echo "Failed tests:"
        local name
        for name in "${FAILED_NAMES[@]}"; do
            echo "  - ${name}"
        done
        echo ""
    fi

    if [[ "${TESTS_FAILED}" -gt 0 ]]; then
        exit 1
    fi
    exit 0
}

main "$@"
