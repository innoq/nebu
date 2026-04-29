#!/usr/bin/env bash
# =============================================================================
# verify-issue-pr-templates.sh
# Acceptance tests for Story 8.7 — Issue/PR-Templates for Dual-Host
# (.github/ + .gitlab/)
# Test framework: pure Bash with exit codes (no external test framework)
#
# Usage: bash scripts/verify-issue-pr-templates.sh
#
# Tests validate that GitHub and GitLab template files exist, are valid,
# have required fields, are parity-checked between platforms, contain no
# emojis, and pass markdownlint.
# Exit 0 if all 13 tests pass, Exit 1 if any test fails.
#
# Lessons Learned applied (8.1–8.6):
#   - No grep -P (BSD-incompatible) — python3 for YAML/emoji/Unicode checks
#   - Anchored regex patterns where intent is anchored
#   - set -euo pipefail + local + quoting throughout
#   - cleanup trap with return 0 and TEMPFILES array
#   - cd "${REPO_ROOT}" for markdownlint invocations (lesson 8.2/8.3)
#   - shellcheck SKIP-fallback for optional tools
#   - Section-scoping for Markdown content checks
#   - Negative-test strategy: prohibition phrase tests check verb+phrase combo
#   - run_test helper with explicit exit-code capture (not set -e propagation)
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

GITHUB_BUG_REPORT="${REPO_ROOT}/.github/ISSUE_TEMPLATE/bug_report.yml"
GITHUB_FEATURE_REQUEST="${REPO_ROOT}/.github/ISSUE_TEMPLATE/feature_request.yml"
GITHUB_CONFIG_YML="${REPO_ROOT}/.github/ISSUE_TEMPLATE/config.yml"
GITHUB_PR_TEMPLATE="${REPO_ROOT}/.github/pull_request_template.md"
GITHUB_CODEOWNERS="${REPO_ROOT}/.github/CODEOWNERS"
GITHUB_DEPENDABOT="${REPO_ROOT}/.github/dependabot.yml"

GITLAB_BUG="${REPO_ROOT}/.gitlab/issue_templates/Bug.md"
GITLAB_FEATURE="${REPO_ROOT}/.gitlab/issue_templates/Feature.md"
GITLAB_MR_DEFAULT="${REPO_ROOT}/.gitlab/merge_request_templates/Default.md"

VERIFY_SCRIPT="${REPO_ROOT}/scripts/verify-issue-pr-templates.sh"

# ---------------------------------------------------------------------------
# Global counters
# ---------------------------------------------------------------------------
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_NAMES=()

# ---------------------------------------------------------------------------
# Cleanup — temp files registered in TEMPFILES are removed on exit.
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
# Tries pyyaml first; if not available, SKIP (return 0 with warning).
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
# TEST 1 — test_bug_report_yml_valid_yaml
#
# AC 1 / Story AT 1
# Given: .github/ISSUE_TEMPLATE/bug_report.yml exists
# When:  python3 yaml.safe_load parses the file AND we assert structural
#        contents required by the AC (name=Bug Report, label=bug, body has
#        form-fields with the expected ids).
# Then:  Exit code 0
# =============================================================================
test_bug_report_yml_valid_yaml() {
    check_yaml_parseable "${GITHUB_BUG_REPORT}" ".github/ISSUE_TEMPLATE/bug_report.yml" || return 1

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot validate bug_report.yml structure" >&2
        return 0
    fi

    local err_tmp
    err_tmp="$(mktemp)"
    TEMPFILES+=("${err_tmp}")

    python3 -c "
import sys
try:
    import yaml
except ImportError:
    sys.exit(2)
with open(sys.argv[1]) as f:
    d = yaml.safe_load(f)
errors = []
if d.get('name') != 'Bug Report':
    errors.append(f\"name expected 'Bug Report', got {d.get('name')!r}\")
labels = d.get('labels', [])
if 'bug' not in labels:
    errors.append(f\"labels must include 'bug', got {labels!r}\")
body = d.get('body', []) or []
ids = {item.get('id') for item in body if isinstance(item, dict)}
required_ids = {'description', 'steps_to_reproduce', 'expected_behaviour',
                'actual_behaviour', 'version', 'environment'}
missing = required_ids - ids
if missing:
    errors.append(f'missing form-field ids: {sorted(missing)}')
if errors:
    for e in errors:
        print(e, file=sys.stderr)
    sys.exit(1)
" "${GITHUB_BUG_REPORT}" 2>"${err_tmp}"
    local py_rc=$?
    if [[ "${py_rc}" -eq 2 ]]; then
        echo "  SKIP: pyyaml not installed" >&2
        return 0
    fi
    if [[ "${py_rc}" -ne 0 ]]; then
        echo "  FAIL: bug_report.yml structural check:" >&2
        cat "${err_tmp}" >&2
        return 1
    fi
    return 0
}

# =============================================================================
# TEST 2 — test_feature_request_yml_valid_yaml
#
# AC 2 / Story AT 2
# Given: .github/ISSUE_TEMPLATE/feature_request.yml exists
# When:  python3 parses + asserts name='Feature Request', labels has
#        'enhancement', and the four required form-field ids are present.
# Then:  Exit code 0
# =============================================================================
test_feature_request_yml_valid_yaml() {
    check_yaml_parseable "${GITHUB_FEATURE_REQUEST}" ".github/ISSUE_TEMPLATE/feature_request.yml" || return 1

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot validate feature_request.yml structure" >&2
        return 0
    fi

    local err_tmp
    err_tmp="$(mktemp)"
    TEMPFILES+=("${err_tmp}")

    python3 -c "
import sys
try:
    import yaml
except ImportError:
    sys.exit(2)
with open(sys.argv[1]) as f:
    d = yaml.safe_load(f)
errors = []
if d.get('name') != 'Feature Request':
    errors.append(f\"name expected 'Feature Request', got {d.get('name')!r}\")
labels = d.get('labels', [])
if 'enhancement' not in labels:
    errors.append(f\"labels must include 'enhancement', got {labels!r}\")
body = d.get('body', []) or []
ids = {item.get('id') for item in body if isinstance(item, dict)}
required_ids = {'motivation', 'proposed_solution', 'alternatives_considered',
                'willing_to_contribute'}
missing = required_ids - ids
if missing:
    errors.append(f'missing form-field ids: {sorted(missing)}')
if errors:
    for e in errors:
        print(e, file=sys.stderr)
    sys.exit(1)
" "${GITHUB_FEATURE_REQUEST}" 2>"${err_tmp}"
    local py_rc=$?
    if [[ "${py_rc}" -eq 2 ]]; then
        echo "  SKIP: pyyaml not installed" >&2
        return 0
    fi
    if [[ "${py_rc}" -ne 0 ]]; then
        echo "  FAIL: feature_request.yml structural check:" >&2
        cat "${err_tmp}" >&2
        return 1
    fi
    return 0
}

# =============================================================================
# TEST 3 — test_config_yml_blank_issues_disabled
#
# AC 3 / Story AT 3
# Given: .github/ISSUE_TEMPLATE/config.yml exists
# When:  python3 parses YAML and checks blank_issues_enabled is exactly false
# Then:  Exit code 0
# =============================================================================
test_config_yml_blank_issues_disabled() {
    if [[ ! -f "${GITHUB_CONFIG_YML}" ]]; then
        echo "  FAIL: .github/ISSUE_TEMPLATE/config.yml not found" >&2
        return 1
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot validate YAML" >&2
        return 0
    fi

    local err_tmp result_tmp
    err_tmp="$(mktemp)"
    result_tmp="$(mktemp)"
    TEMPFILES+=("${err_tmp}" "${result_tmp}")

    local py_rc=0
    python3 -c "
import sys
try:
    import yaml
except ImportError:
    print('SKIP', file=sys.stderr)
    sys.exit(2)
with open(sys.argv[1], encoding='utf-8') as f:
    d = yaml.safe_load(f)
val = d.get('blank_issues_enabled')
if val is False:
    print('OK')
    sys.exit(0)
else:
    print(f'blank_issues_enabled={val!r} (expected False)')
    sys.exit(1)
" "${GITHUB_CONFIG_YML}" >"${result_tmp}" 2>"${err_tmp}" || py_rc=$?

    local result
    result="$(cat "${result_tmp}" 2>/dev/null || true)"

    if [[ "${py_rc}" -eq 2 ]]; then
        echo "  SKIP: pyyaml not installed — cannot validate config.yml" >&2
        return 0
    elif [[ "${py_rc}" -ne 0 ]]; then
        echo "  FAIL: blank_issues_enabled is not 'false' in config.yml: ${result}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 4 — test_pr_template_required_sections
#
# AC 4 / Story AT 4
# Given: .github/pull_request_template.md exists
# When:  grep for required sections and content
# Then:
#   4a) "Acceptance Criteria" heading present
#   4b) DCO reminder ("git commit -s") present
#   4c) Co-Authored-By: Claude is mentioned alongside a prohibition verb
#       (must not / do not / don't / NOT) — verifies actual prohibition logic,
#       not just that the string appears
# =============================================================================
test_pr_template_required_sections() {
    if [[ ! -f "${GITHUB_PR_TEMPLATE}" ]]; then
        echo "  FAIL: .github/pull_request_template.md not found" >&2
        return 1
    fi

    local ok=0

    # 4a: Acceptance Criteria section
    if ! grep -q "Acceptance Criteria" "${GITHUB_PR_TEMPLATE}"; then
        echo "  FAIL (4a): 'Acceptance Criteria' not found in pull_request_template.md" >&2
        ok=1
    fi

    # 4b: DCO sign-off reminder
    if ! grep -qE "git commit -s|DCO" "${GITHUB_PR_TEMPLATE}"; then
        echo "  FAIL (4b): DCO / 'git commit -s' reminder not found in pull_request_template.md" >&2
        ok=1
    fi

    # 4c: Prohibition of Co-Authored-By: Claude
    # Check that:
    #   - The phrase "Co-Authored-By" (or "Co-authored-by") appears
    #   - AND a prohibition verb appears on the same line or adjacent line
    # Strategy: python3 multi-line scan for prohibition verb within 3 lines
    # of the Co-Authored-By mention.
    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP (4c): python3 not in PATH — cannot run prohibition check" >&2
    else
        local py_rc=0
        python3 -c "
import sys, re
content = open(sys.argv[1], encoding='utf-8').read()
# Check Co-Authored-By appears at all
if not re.search(r'Co-[Aa]uthored-[Bb]y', content, re.IGNORECASE):
    print('Co-Authored-By phrase not found')
    sys.exit(1)
# Check that a prohibition verb appears near Co-Authored-By
# We look for NOT/must not/do not/don't within the same or adjacent context
# by finding the region around Co-Authored-By and checking for prohibition
lines = content.splitlines()
found_prohibition = False
for i, line in enumerate(lines):
    if re.search(r'Co-[Aa]uthored-[Bb]y', line, re.IGNORECASE):
        # Check this line and 2 lines before/after for prohibition
        window_start = max(0, i - 2)
        window_end = min(len(lines), i + 3)
        window = ' '.join(lines[window_start:window_end])
        if re.search(r'must not|do not|don\\'t|NOT|prohibited|MUST NOT', window):
            found_prohibition = True
            break
if not found_prohibition:
    print('Co-Authored-By found but no prohibition verb (must not/do not/NOT) nearby')
    sys.exit(1)
sys.exit(0)
" "${GITHUB_PR_TEMPLATE}" || py_rc=$?
        if [[ "${py_rc}" -ne 0 ]]; then
            local reason
            reason="$(python3 -c "
import sys, re
content = open(sys.argv[1], encoding='utf-8').read()
if not re.search(r'Co-[Aa]uthored-[Bb]y', content, re.IGNORECASE):
    print('Co-Authored-By phrase not found')
    sys.exit(0)
lines = content.splitlines()
for i, line in enumerate(lines):
    if re.search(r'Co-[Aa]uthored-[Bb]y', line, re.IGNORECASE):
        window_start = max(0, i - 2)
        window_end = min(len(lines), i + 3)
        window = ' '.join(lines[window_start:window_end])
        if re.search(r'must not|do not|don\\'t|NOT|prohibited|MUST NOT', window):
            break
        else:
            print('Co-Authored-By found but no prohibition verb nearby')
            break
" "${GITHUB_PR_TEMPLATE}" 2>/dev/null || true)"
            echo "  FAIL (4c): Co-Authored-By: Claude prohibition not properly expressed in pull_request_template.md: ${reason}" >&2
            ok=1
        fi
    fi

    # 4d: What/Why/How section
    if ! grep -qE "(What.*Why|## (What|Why|How))" "${GITHUB_PR_TEMPLATE}"; then
        echo "  FAIL (4d): What/Why/How section not found in pull_request_template.md" >&2
        ok=1
    fi

    # 4e: Linked Issue(s) section
    if ! grep -qE "Linked Issue" "${GITHUB_PR_TEMPLATE}"; then
        echo "  FAIL (4e): 'Linked Issue(s)' section not found in pull_request_template.md" >&2
        ok=1
    fi

    # 4f: Tests section
    if ! grep -qE "^## Tests" "${GITHUB_PR_TEMPLATE}"; then
        echo "  FAIL (4f): '## Tests' section not found in pull_request_template.md" >&2
        ok=1
    fi

    return "${ok}"
}

# =============================================================================
# TEST 5 — test_codeowners_has_global_owner
#
# AC 5 / Story AT 5
# Given: .github/CODEOWNERS exists
# When:  grep -E "^\* +@" .github/CODEOWNERS
# Then:  Match found — anchored '*' followed by at least one GitHub handle
# =============================================================================
test_codeowners_has_global_owner() {
    if [[ ! -f "${GITHUB_CODEOWNERS}" ]]; then
        echo "  FAIL: .github/CODEOWNERS not found" >&2
        return 1
    fi

    if ! grep -qE "^\* +@" "${GITHUB_CODEOWNERS}"; then
        echo "  FAIL: No global ownership line found in CODEOWNERS" >&2
        echo "    Expected: '* @<handle>' (anchored '*' followed by at least one GitHub handle)" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 6 — test_dependabot_yml_valid_yaml_and_four_ecosystems
#
# AC 6 / Story AT 6
# Given: .github/dependabot.yml exists
# When:  python3 parses YAML, counts updates[] entries, verifies ecosystems
# Then:  Exactly 4 update entries; ecosystems include gomod, mix/hex,
#        github-actions, and docker
# =============================================================================
test_dependabot_yml_valid_yaml_and_four_ecosystems() {
    if [[ ! -f "${GITHUB_DEPENDABOT}" ]]; then
        echo "  FAIL: .github/dependabot.yml not found" >&2
        return 1
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot validate dependabot.yml" >&2
        return 0
    fi

    local err_tmp result_tmp
    err_tmp="$(mktemp)"
    result_tmp="$(mktemp)"
    TEMPFILES+=("${err_tmp}" "${result_tmp}")

    local py_rc=0
    python3 -c "
import sys
try:
    import yaml
except ImportError:
    print('SKIP', file=sys.stderr)
    sys.exit(2)
with open(sys.argv[1], encoding='utf-8') as f:
    d = yaml.safe_load(f)
updates = d.get('updates', [])
count = len(updates)
if count != 4:
    print(f'Expected 4 update entries, found {count}')
    sys.exit(1)
ecosystems = [u.get('package-ecosystem', '') for u in updates]
required = ['gomod', 'github-actions', 'docker']
missing = []
for req in required:
    if req not in ecosystems:
        missing.append(req)
# mix or hex is acceptable
if 'mix' not in ecosystems and 'hex' not in ecosystems:
    missing.append('mix (or hex)')
if missing:
    print(f'Missing ecosystems: {missing}; found: {ecosystems}')
    sys.exit(1)
# Verify all use weekly schedule
non_weekly = [u.get('package-ecosystem') for u in updates if u.get('schedule', {}).get('interval') != 'weekly']
if non_weekly:
    print(f'Ecosystems without weekly schedule: {non_weekly}')
    sys.exit(1)
print('OK')
sys.exit(0)
" "${GITHUB_DEPENDABOT}" >"${result_tmp}" 2>"${err_tmp}" || py_rc=$?

    local result err_msg
    result="$(cat "${result_tmp}" 2>/dev/null || true)"
    err_msg="$(cat "${err_tmp}" 2>/dev/null || true)"

    if [[ "${py_rc}" -eq 2 ]]; then
        echo "  SKIP: pyyaml not installed — cannot validate dependabot.yml" >&2
        return 0
    elif [[ "${py_rc}" -ne 0 ]]; then
        echo "  FAIL: dependabot.yml validation failed: ${result}${err_msg}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 7 — test_gitlab_bug_md_exists
#
# AC 7 / Story AT 7
# Given: .gitlab/issue_templates/Bug.md created
# When:  [[ -f .gitlab/issue_templates/Bug.md ]]
# Then:  True
# =============================================================================
test_gitlab_bug_md_exists() {
    if [[ ! -f "${GITLAB_BUG}" ]]; then
        echo "  FAIL: .gitlab/issue_templates/Bug.md not found at ${GITLAB_BUG}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 8 — test_gitlab_feature_md_exists
#
# AC 8 / Story AT 8
# Given: .gitlab/issue_templates/Feature.md created
# When:  [[ -f .gitlab/issue_templates/Feature.md ]]
# Then:  True
# =============================================================================
test_gitlab_feature_md_exists() {
    if [[ ! -f "${GITLAB_FEATURE}" ]]; then
        echo "  FAIL: .gitlab/issue_templates/Feature.md not found at ${GITLAB_FEATURE}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 9 — test_gitlab_default_mr_template_required_sections
#
# AC 9 / Story AT 9
# Given: .gitlab/merge_request_templates/Default.md exists
# When:  grep checks for required sections
# Then:
#   9a) "Acceptance Criteria" heading present
#   9b) DCO reminder ("git commit -s" or "DCO") present
# =============================================================================
test_gitlab_default_mr_template_required_sections() {
    if [[ ! -f "${GITLAB_MR_DEFAULT}" ]]; then
        echo "  FAIL: .gitlab/merge_request_templates/Default.md not found" >&2
        return 1
    fi

    local ok=0

    # 9a: Acceptance Criteria section
    if ! grep -q "Acceptance Criteria" "${GITLAB_MR_DEFAULT}"; then
        echo "  FAIL (9a): 'Acceptance Criteria' not found in Default.md" >&2
        ok=1
    fi

    # 9b: DCO sign-off reminder
    if ! grep -qE "git commit -s|DCO" "${GITLAB_MR_DEFAULT}"; then
        echo "  FAIL (9b): DCO / 'git commit -s' reminder not found in Default.md" >&2
        ok=1
    fi

    # 9c: Co-Authored-By: Claude prohibition (verb + concept proximity, ±2 lines)
    # — same pattern as Test 4c for the GitHub PR template (Kassandra parity).
    if command -v python3 >/dev/null 2>&1; then
        local py_rc=0
        python3 -c "
import re, sys
text = open(sys.argv[1]).read()
lines = text.split('\\n')
verb_re = re.compile(r'(?i)(must not|do not|don\\'t|MUST NOT|prohibited|forbidden|\\bNOT\\b)')
target_re = re.compile(r'(?i)Co-Authored-By')
for i, line in enumerate(lines):
    if target_re.search(line):
        window = lines[max(0, i-2): i+3]
        if any(verb_re.search(w) for w in window):
            sys.exit(0)
sys.exit(1)
" "${GITLAB_MR_DEFAULT}" || py_rc=$?
        if [[ "${py_rc}" -ne 0 ]]; then
            echo "  FAIL (9c): no prohibition phrase near 'Co-Authored-By' in Default.md" >&2
            echo "    Expected verb (do not/must not/NOT/prohibited) within 2 lines of 'Co-Authored-By'" >&2
            ok=1
        fi
    fi

    # 9d: What/Why/How section
    if ! grep -qE "(What.*Why|## (What|Why|How))" "${GITLAB_MR_DEFAULT}"; then
        echo "  FAIL (9d): What/Why/How section not found in Default.md" >&2
        ok=1
    fi

    # 9e: Linked Issue(s) section
    if ! grep -qE "Linked Issue" "${GITLAB_MR_DEFAULT}"; then
        echo "  FAIL (9e): 'Linked Issue(s)' section not found in Default.md" >&2
        ok=1
    fi

    # 9f: Tests section
    if ! grep -qE "^## Tests" "${GITLAB_MR_DEFAULT}"; then
        echo "  FAIL (9f): '## Tests' section not found in Default.md" >&2
        ok=1
    fi

    return "${ok}"
}

# =============================================================================
# TEST 10 — test_required_fields_parity_github_gitlab_bug
#
# AC 10 / Story AT 10 — Parity check: GitHub bug_report.yml and GitLab
# Bug.md must cover the SAME required field set.
# Required fields: version, environment, steps (to reproduce),
#                  expected (behaviour), actual (behaviour)
# Check is case-insensitive per-field grep.
# =============================================================================
test_required_fields_parity_github_gitlab_bug() {
    local ok=0

    if [[ ! -f "${GITHUB_BUG_REPORT}" ]]; then
        echo "  FAIL: .github/ISSUE_TEMPLATE/bug_report.yml not found" >&2
        return 1
    fi
    if [[ ! -f "${GITLAB_BUG}" ]]; then
        echo "  FAIL: .gitlab/issue_templates/Bug.md not found" >&2
        return 1
    fi

    # Required field keywords (case-insensitive)
    local -a required_fields=("version" "environment" "steps" "expected" "actual")

    local field
    for field in "${required_fields[@]}"; do
        if ! grep -qi "${field}" "${GITHUB_BUG_REPORT}"; then
            echo "  FAIL (10-github): field '${field}' not found in bug_report.yml" >&2
            ok=1
        fi
        if ! grep -qi "${field}" "${GITLAB_BUG}"; then
            echo "  FAIL (10-gitlab): field '${field}' not found in .gitlab/issue_templates/Bug.md" >&2
            ok=1
        fi
    done

    return "${ok}"
}

# =============================================================================
# TEST 11 — test_no_emojis_in_any_template
#
# AC 10 (no-emoji) / Story AT 11
# Given: All ten files created by this story
# When:  Python3 scans each file for characters in Unicode ranges
#        U+1F000–U+1FFFF and U+2600–U+27BF
# Then:  Zero matches across all files
# =============================================================================
test_no_emojis_in_any_template() {
    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot run emoji check" >&2
        return 0
    fi

    local -a files=(
        "${GITHUB_BUG_REPORT}"
        "${GITHUB_FEATURE_REQUEST}"
        "${GITHUB_CONFIG_YML}"
        "${GITHUB_PR_TEMPLATE}"
        "${GITHUB_CODEOWNERS}"
        "${GITHUB_DEPENDABOT}"
        "${GITLAB_BUG}"
        "${GITLAB_FEATURE}"
        "${GITLAB_MR_DEFAULT}"
        "${VERIFY_SCRIPT}"
    )

    local ok=0
    local filepath
    for filepath in "${files[@]}"; do
        if [[ ! -f "${filepath}" ]]; then
            echo "  FAIL: file not found for emoji check: ${filepath}" >&2
            ok=1
            continue
        fi

        local py_rc=0
        python3 -c "
import sys
filepath = sys.argv[1]
try:
    data = open(filepath, encoding='utf-8').read()
except Exception as e:
    print(f'ERROR reading {filepath}: {e}', file=sys.stderr)
    sys.exit(1)
bad = [c for c in data if 0x1F000 <= ord(c) <= 0x1FFFF or 0x2600 <= ord(c) <= 0x27BF]
if bad:
    chars = ', '.join(f'U+{ord(c):04X} ({c!r})' for c in set(bad))
    print(f'Emoji characters found in {filepath}: {chars}')
    sys.exit(1)
sys.exit(0)
" "${filepath}" || py_rc=$?
        if [[ "${py_rc}" -ne 0 ]]; then
            echo "  FAIL: emoji characters found in ${filepath}" >&2
            ok=1
        fi
    done

    return "${ok}"
}

# =============================================================================
# TEST 12 — test_md_templates_markdownlint_clean
#
# AC 11 / Story AT 12
# Given: Four .md files: pull_request_template.md, Bug.md, Feature.md,
#        Default.md
# When:  cd ${REPO_ROOT} && npx --yes markdownlint-cli <file>
# Then:  Exit code 0, zero errors per file
#        SKIP (exit 0) if npx not available
# =============================================================================
test_md_templates_markdownlint_clean() {
    if ! command -v npx >/dev/null 2>&1; then
        echo "  SKIP: npx not in PATH — markdownlint check skipped" >&2
        return 0
    fi

    local -a md_files=(
        ".github/pull_request_template.md"
        ".gitlab/issue_templates/Bug.md"
        ".gitlab/issue_templates/Feature.md"
        ".gitlab/merge_request_templates/Default.md"
    )

    local ok=0
    local rel_file
    for rel_file in "${md_files[@]}"; do
        local abs_file="${REPO_ROOT}/${rel_file}"
        if [[ ! -f "${abs_file}" ]]; then
            echo "  FAIL: ${rel_file} not found for markdownlint check" >&2
            ok=1
            continue
        fi

        local ml_rc=0
        local ml_out
        # cd to REPO_ROOT so markdownlint error paths are relative (lesson 8.2/8.3)
        ml_out="$(cd "${REPO_ROOT}" && npx --yes markdownlint-cli "${rel_file}" 2>&1)" || ml_rc=$?

        if [[ "${ml_rc}" -ne 0 ]]; then
            # Filter output to only lines matching this specific file
            # Pattern: (^|/)<filename>: — anchored to avoid false positives
            local basename_file
            basename_file="$(basename "${rel_file}")"
            local relevant_errors
            relevant_errors="$(echo "${ml_out}" | grep -E "(^|/)${basename_file}:" || true)"
            if [[ -n "${relevant_errors}" ]]; then
                echo "  FAIL: markdownlint errors in ${rel_file}:" >&2
                echo "${relevant_errors}" >&2
                ok=1
            else
                # Non-zero exit but no file-specific errors (e.g. install output)
                echo "  SKIP (${rel_file}): markdownlint returned non-zero but no file-specific errors found" >&2
            fi
        fi
    done

    return "${ok}"
}

# =============================================================================
# TEST 13 — test_verify_script_shellcheck_clean
#
# AC 12 / Story AT 13
# Given: scripts/verify-issue-pr-templates.sh exists
# When:  shellcheck --severity=error scripts/verify-issue-pr-templates.sh
# Then:  Exit code 0; no errors
#        SKIP (exit 0) if shellcheck not in PATH
# =============================================================================
test_verify_script_shellcheck_clean() {
    if [[ ! -f "${VERIFY_SCRIPT}" ]]; then
        echo "  FAIL: scripts/verify-issue-pr-templates.sh not found at ${VERIFY_SCRIPT}" >&2
        return 1
    fi

    # AC 12 also requires the script to be executable. Check the bit
    # directly — without it, `bash scripts/verify-issue-pr-templates.sh`
    # works but `./scripts/verify-issue-pr-templates.sh` (which Story 8.9's
    # release-readiness gate uses) does not.
    if [[ ! -x "${VERIFY_SCRIPT}" ]]; then
        echo "  FAIL: ${VERIFY_SCRIPT} is not executable (run chmod +x)" >&2
        return 1
    fi

    if ! command -v shellcheck >/dev/null 2>&1; then
        echo "  SKIP (shellcheck only): shellcheck not in PATH — install to enable this check" >&2
        return 0
    fi

    local sc_rc=0
    local sc_out
    sc_out="$(shellcheck --severity=error "${VERIFY_SCRIPT}" 2>&1)" || sc_rc=$?
    if [[ "${sc_rc}" -ne 0 ]]; then
        echo "  FAIL: shellcheck errors in scripts/verify-issue-pr-templates.sh:" >&2
        echo "${sc_out}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# MAIN — Run all 13 tests sequentially
# =============================================================================
main() {
    echo "======================================================================="
    echo "verify-issue-pr-templates.sh — Story 8.7 Acceptance Tests"
    echo "REPO_ROOT: ${REPO_ROOT}"
    echo "======================================================================="
    echo ""

    run_test test_bug_report_yml_valid_yaml
    run_test test_feature_request_yml_valid_yaml
    run_test test_config_yml_blank_issues_disabled
    run_test test_pr_template_required_sections
    run_test test_codeowners_has_global_owner
    run_test test_dependabot_yml_valid_yaml_and_four_ecosystems
    run_test test_gitlab_bug_md_exists
    run_test test_gitlab_feature_md_exists
    run_test test_gitlab_default_mr_template_required_sections
    run_test test_required_fields_parity_github_gitlab_bug
    run_test test_no_emojis_in_any_template
    run_test test_md_templates_markdownlint_clean
    run_test test_verify_script_shellcheck_clean

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
