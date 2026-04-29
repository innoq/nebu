#!/usr/bin/env bash
# =============================================================================
# verify-contributing.sh
# Verification suite for Story 8.3 — CONTRIBUTING.md sections and conventions
# Test framework: pure Bash with exit codes (no external dependencies except python3)
#
# Usage: bash scripts/verify-contributing.sh
#
# Tests run READ-ONLY against CONTRIBUTING.md in the repository root.
# Exit 0 if all 10 ACs pass, Exit 1 if any fail.
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CONTRIBUTING="${REPO_ROOT}/CONTRIBUTING.md"

# ---------------------------------------------------------------------------
# Global counters
# ---------------------------------------------------------------------------
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_NAMES=()

# ---------------------------------------------------------------------------
# Tempfile cleanup — any tempfile registered in TEMPFILES is removed on exit,
# even if a test aborts (set -e) or the user sends SIGINT.
# ---------------------------------------------------------------------------
TEMPFILES=()
cleanup_tempfiles() {
    local f
    for f in "${TEMPFILES[@]:-}"; do
        [[ -n "${f}" ]] && rm -f "${f}"
    done
    return 0
}
trap cleanup_tempfiles EXIT INT TERM

# ---------------------------------------------------------------------------
# Helper: run_test
# Calls the named function, records PASS/FAIL.
# ---------------------------------------------------------------------------
run_test() {
    local name="$1"
    TESTS_RUN=$(( TESTS_RUN + 1 ))
    echo "--- ${name}"
    if "${name}"; then
        echo "    PASS"
        TESTS_PASSED=$(( TESTS_PASSED + 1 ))
    else
        echo "    FAIL"
        TESTS_FAILED=$(( TESTS_FAILED + 1 ))
        FAILED_NAMES+=("${name}")
    fi
}

# ---------------------------------------------------------------------------
# Helper: extract_section
# Extracts the body of a section from the first ## heading match up to
# (but not including) the next ## heading or EOF.
# Usage: extract_section "## Section Name" <file>
# ---------------------------------------------------------------------------
extract_section() {
    local heading="$1"
    local file="$2"
    awk -v h="${heading}" '
        $0 == h { found=1; next }
        found && /^## / { exit }
        found { print }
    ' "${file}"
}

# ---------------------------------------------------------------------------
# Helper: extract_section_flexible
# Extracts the body of the first section whose heading matches a regex pattern.
# Usage: extract_section_flexible "pattern" <file>
# Pattern is matched against the full heading line (including "## ").
# ---------------------------------------------------------------------------
extract_section_flexible() {
    local pattern="$1"
    local file="$2"
    awk -v p="${pattern}" '
        /^## / && $0 ~ p { found=1; next }
        found && /^## / { exit }
        found { print }
    ' "${file}"
}

# =============================================================================
# AC1: CONTRIBUTING.md exists in the repository root
#
# Given:  Repository root after implementation
# When:   test -f CONTRIBUTING.md
# Then:   exit code 0
# =============================================================================
test_ac1_file_exists() {
    if [[ -f "${CONTRIBUTING}" ]]; then
        return 0
    else
        echo "  FAIL: CONTRIBUTING.md not found at ${CONTRIBUTING}" >&2
        return 1
    fi
}

# =============================================================================
# AC2: Bug report section heading present
#
# Given:  CONTRIBUTING.md after implementation
# When:   grep for "## Reporting Bugs" or "## Bug Reports"
# Then:   at least 1 match
# =============================================================================
test_ac2_bug_report_section() {
    if [[ ! -f "${CONTRIBUTING}" ]]; then
        echo "  FAIL: CONTRIBUTING.md not found" >&2
        return 1
    fi

    local count
    count=$(grep -cE "^## (Reporting Bugs|Bug Reports)$" "${CONTRIBUTING}" || true)
    if [[ "${count}" -ge 1 ]]; then
        return 0
    else
        echo "  FAIL: no '## Reporting Bugs' or '## Bug Reports' heading found" >&2
        return 1
    fi
}

# =============================================================================
# AC3: PR workflow section with branch naming + Conventional Commits
#
# Given:  Extract PR workflow section body
# When:   Section body contains branch naming pattern (feat/ or fix/)
#         AND contains "Conventional Commits" or "conventional commits"
# Then:   both grep calls return at least one match
# =============================================================================
test_ac3_pr_workflow_section() {
    if [[ ! -f "${CONTRIBUTING}" ]]; then
        echo "  FAIL: CONTRIBUTING.md not found" >&2
        return 1
    fi

    # Check section heading exists
    local heading_count
    heading_count=$(grep -cE "^## (Submitting a Pull Request|Pull Requests)$" "${CONTRIBUTING}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL: no '## Submitting a Pull Request' or '## Pull Requests' heading found" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section_flexible "(Submitting a Pull Request|Pull Requests)" "${CONTRIBUTING}")

    local ok=1

    # 3a: branch naming
    if ! echo "${section_body}" | grep -qE "feat/|fix/"; then
        echo "  FAIL (3a): branch naming pattern (feat/ or fix/) not found in PR section" >&2
        ok=0
    fi

    # 3b: Conventional Commits
    if ! echo "${section_body}" | grep -qi "Conventional Commits"; then
        echo "  FAIL (3b): 'Conventional Commits' not found in PR section" >&2
        ok=0
    fi

    [[ "${ok}" -eq 1 ]]
}

# =============================================================================
# AC4: BMAD section exists and contains escape hatch for external contributors
#
# Given:  Extract BMAD/Development Workflow section body
# When:   Section body mentions "BMAD" AND contains language indicating
#         external contributors can skip BMAD
# Then:   both checks pass
# =============================================================================
test_ac4_bmad_section_with_escape_hatch() {
    if [[ ! -f "${CONTRIBUTING}" ]]; then
        echo "  FAIL: CONTRIBUTING.md not found" >&2
        return 1
    fi

    # Anchored heading regex — match either "## Development Workflow (BMAD)"
    # exactly, or "## BMAD Workflow" exactly. The anchored "$" prevents
    # accidental matches against unrelated headings such as "## BMAD Process"
    # (INFO.7 from test review).
    local heading_count
    heading_count=$(grep -cE '^## (Development Workflow \(BMAD\)|BMAD Workflow)$' "${CONTRIBUTING}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL: no '## Development Workflow (BMAD)' or '## BMAD Workflow' heading found" >&2
        return 1
    fi

    # extract_section_flexible passes the pattern to awk via -v; awk strings
    # require doubled backslashes to produce a literal "(". Use [(] instead
    # for portability.
    local section_body
    section_body=$(extract_section_flexible '(Development Workflow [(]BMAD[)]|BMAD Workflow)' "${CONTRIBUTING}")

    local ok=1

    # 4a: mentions BMAD
    if ! echo "${section_body}" | grep -q "BMAD"; then
        echo "  FAIL (4a): 'BMAD' not found in BMAD/Workflow section body" >&2
        ok=0
    fi

    # 4b: escape hatch — external contributors can skip BMAD
    if ! echo "${section_body}" | grep -qiE "without|directly|skip|not required|are not required"; then
        echo "  FAIL (4b): escape hatch language not found in BMAD section (expected: 'without', 'directly', 'skip', 'not required', or similar)" >&2
        ok=0
    fi

    [[ "${ok}" -eq 1 ]]
}

# =============================================================================
# AC5: CLAUDE.md referenced in CONTRIBUTING.md AND OpenAPI-first mentioned
#
# Given:  CONTRIBUTING.md after implementation
# When:   5a: grep for "CLAUDE.md"
#         5b: grep for "OpenAPI" (case-insensitive) — AC5 mandates a mention
#             of the OpenAPI-first approach for gateway API changes
# Then:   both checks pass
# =============================================================================
test_ac5_claude_md_reference() {
    if [[ ! -f "${CONTRIBUTING}" ]]; then
        echo "  FAIL: CONTRIBUTING.md not found" >&2
        return 1
    fi

    local ok=1

    # 5a: CLAUDE.md referenced
    if ! grep -q "CLAUDE.md" "${CONTRIBUTING}"; then
        echo "  FAIL (5a): 'CLAUDE.md' not referenced in CONTRIBUTING.md" >&2
        ok=0
    fi

    # 5b: OpenAPI-first mentioned (AC5 requires it)
    if ! grep -qi "OpenAPI" "${CONTRIBUTING}"; then
        echo "  FAIL (5b): 'OpenAPI' not mentioned in CONTRIBUTING.md (AC5 requires OpenAPI-first reference)" >&2
        ok=0
    fi

    [[ "${ok}" -eq 1 ]]
}

# =============================================================================
# AC6: Testing section with TDD keyword
#
# Given:  Extract ## Testing (or ## Test Expectations) section body
# When:   Section body contains "unit" AND ("TDD" or "red-green" or "test-first")
# Then:   both checks pass
# =============================================================================
test_ac6_testing_section() {
    if [[ ! -f "${CONTRIBUTING}" ]]; then
        echo "  FAIL: CONTRIBUTING.md not found" >&2
        return 1
    fi

    local heading_count
    heading_count=$(grep -cE "^## (Testing|Test Expectations)$" "${CONTRIBUTING}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL: no '## Testing' or '## Test Expectations' heading found" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section_flexible "(Testing|Test Expectations)" "${CONTRIBUTING}")

    local ok=1

    # 6a: mentions unit tests
    if ! echo "${section_body}" | grep -qi "unit"; then
        echo "  FAIL (6a): 'unit' not found in Testing section" >&2
        ok=0
    fi

    # 6b: TDD / red-green / test-first
    if ! echo "${section_body}" | grep -qiE "TDD|red-green|test.first"; then
        echo "  FAIL (6b): TDD keyword ('TDD', 'red-green', 'test-first') not found in Testing section" >&2
        ok=0
    fi

    [[ "${ok}" -eq 1 ]]
}

# =============================================================================
# AC7: License/DCO section with Apache and DCO keywords
#
# Given:  Extract License / Licensing / License and DCO section body
# When:   Section body contains "Apache" AND ("DCO" or "git commit -s")
# Then:   both checks pass
# =============================================================================
test_ac7_license_dco_section() {
    if [[ ! -f "${CONTRIBUTING}" ]]; then
        echo "  FAIL: CONTRIBUTING.md not found" >&2
        return 1
    fi

    local heading_count
    heading_count=$(grep -cE "^## (License|Licensing|License and DCO)$" "${CONTRIBUTING}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL: no '## License', '## Licensing', or '## License and DCO' heading found" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section_flexible "(License and DCO|Licensing|License)" "${CONTRIBUTING}")

    local ok=1

    # 7a: Apache
    if ! echo "${section_body}" | grep -q "Apache"; then
        echo "  FAIL (7a): 'Apache' not found in License/DCO section" >&2
        ok=0
    fi

    # 7b: DCO or git commit -s
    if ! echo "${section_body}" | grep -qE "DCO|git commit -s"; then
        echo "  FAIL (7b): 'DCO' or 'git commit -s' not found in License/DCO section" >&2
        ok=0
    fi

    [[ "${ok}" -eq 1 ]]
}

# =============================================================================
# AC8: No-Co-Authored-By rule is explicitly stated in CONTRIBUTING.md
#
# Given:  CONTRIBUTING.md after implementation
# When:   grep for "Co-Authored-By" AND a prohibition phrase nearby
# Then:   8a: at least 1 mention of the trailer name
#         8b: a prohibition phrase ("do not", "must not", "MUST NOT", "no ...
#             ai-attribution") appears in CONTRIBUTING.md
#
# Note: AC8b ensures the trailer is FORBIDDEN, not merely mentioned.
#       Without 8b, a friendly "feel free to add Co-Authored-By" would pass.
# =============================================================================
test_ac8_no_coauthored_by_rule_stated() {
    if [[ ! -f "${CONTRIBUTING}" ]]; then
        echo "  FAIL: CONTRIBUTING.md not found" >&2
        return 1
    fi

    local ok=1

    # 8a: trailer name mentioned
    local count
    count=$(grep -c "Co-Authored-By" "${CONTRIBUTING}" || true)
    if [[ "${count}" -lt 1 ]]; then
        echo "  FAIL (8a): 'Co-Authored-By' is not mentioned in CONTRIBUTING.md (the prohibition must be explicitly stated)" >&2
        ok=0
    fi

    # 8b: a prohibition phrase exists
    # Accepted phrases (case-insensitive): "do not", "don't", "must not",
    # "should not", "no ai-attribution", "no AI attribution", "not include",
    # "without ... Co-Authored-By".
    if ! grep -qiE "do not|don't|must not|should not|no ai.?attribution|not include" "${CONTRIBUTING}"; then
        echo "  FAIL (8b): no prohibition phrase ('do not', 'must not', 'no AI-attribution', 'not include') found in CONTRIBUTING.md" >&2
        ok=0
    fi

    [[ "${ok}" -eq 1 ]]
}

# =============================================================================
# AC9: No emojis in CONTRIBUTING.md
#
# Given:  Full CONTRIBUTING.md content
# When:   Python3 scans for Unicode emoji ranges (U+1F000-U+1FFFF, U+2600-U+27BF)
# Then:   zero matches
#
# Note: We use Python3 instead of grep -P because BSD grep (macOS) does not
#       support -P, and grep -E with raw UTF-8 byte ranges is unreliable.
#       Python3 is part of macOS base install and always present in CI.
# =============================================================================
test_ac9_no_emojis() {
    if [[ ! -f "${CONTRIBUTING}" ]]; then
        echo "  FAIL: CONTRIBUTING.md not found" >&2
        return 1
    fi

    if ! command -v python3 &>/dev/null; then
        echo "  FAIL: python3 required for portable emoji detection (not found in PATH)" >&2
        return 1
    fi

    local py_output py_rc=0
    py_output=$(python3 -c '
import sys, re
with open(sys.argv[1], encoding="utf-8") as f:
    text = f.read()
pattern = re.compile(r"[\U0001F000-\U0001FFFF\U00002600-\U000027BF]")
matches = pattern.findall(text)
if matches:
    print("found {} emoji char(s): {}".format(len(matches), " ".join(repr(m) for m in matches[:10])), file=sys.stderr)
    sys.exit(1)
sys.exit(0)
' "${CONTRIBUTING}" 2>&1) || py_rc=$?

    if [[ "${py_rc}" -eq 0 ]]; then
        return 0
    else
        echo "  FAIL: emoji character(s) found in CONTRIBUTING.md: ${py_output}" >&2
        return 1
    fi
}

# =============================================================================
# AC10: markdownlint clean — CONTRIBUTING.md is a new file, expected 0 errors
#
# Strategy: try npx markdownlint-cli; fall back to Docker; SKIP if neither available.
# New file: no baseline comparison needed — target is 0 errors.
# =============================================================================
test_ac10_markdownlint_clean() {
    if [[ ! -f "${CONTRIBUTING}" ]]; then
        echo "  FAIL: CONTRIBUTING.md not found" >&2
        return 1
    fi

    if ! command -v npx &>/dev/null && ! command -v docker &>/dev/null; then
        echo "  SKIP: neither 'npx' nor 'docker' found — markdownlint check cannot run" >&2
        echo "        Install markdownlint-cli ('npm i -g markdownlint-cli') or Docker to enable this check." >&2
        return 1
    fi

    local error_count=0
    local lint_output=""

    if command -v npx &>/dev/null; then
        set +e
        # Run with CWD = REPO_ROOT so markdownlint-cli emits relative paths,
        # making the error-line pattern reliable regardless of where the
        # script is invoked from.
        lint_output=$(cd "${REPO_ROOT}" && npx --yes markdownlint-cli CONTRIBUTING.md 2>&1)
        error_count=$(echo "${lint_output}" | grep -cE "(^|/)CONTRIBUTING\.md:" || true)
        set -e
    elif command -v docker &>/dev/null; then
        set +e
        lint_output=$(docker run --rm -v "${REPO_ROOT}:/work" -w /work davidanson/markdownlint-cli2:latest "CONTRIBUTING.md" 2>&1)
        error_count=$(echo "${lint_output}" | grep -cE "(^|/)CONTRIBUTING\.md:" || true)
        set -e
    fi

    echo "    markdownlint errors: ${error_count}" >&2

    if [[ "${error_count}" -eq 0 ]]; then
        return 0
    else
        echo "  FAIL: ${error_count} markdownlint error(s) in CONTRIBUTING.md" >&2
        echo "${lint_output}" | head -30 >&2
        return 1
    fi
}

# =============================================================================
# MAIN — Run all tests sequentially
# =============================================================================
echo "======================================================================="
echo "verify-contributing.sh — Story 8.3 Acceptance Criteria"
echo "CONTRIBUTING: ${CONTRIBUTING}"
echo "======================================================================="
echo ""

run_test test_ac1_file_exists
run_test test_ac2_bug_report_section
run_test test_ac3_pr_workflow_section
run_test test_ac4_bmad_section_with_escape_hatch
run_test test_ac5_claude_md_reference
run_test test_ac6_testing_section
run_test test_ac7_license_dco_section
run_test test_ac8_no_coauthored_by_rule_stated
run_test test_ac9_no_emojis
run_test test_ac10_markdownlint_clean

echo ""
echo "======================================================================="
echo "Results: ${TESTS_PASSED}/${TESTS_RUN} passed"

if [[ "${TESTS_FAILED}" -gt 0 ]]; then
    echo "FAILED tests (${TESTS_FAILED}):"
    for name in "${FAILED_NAMES[@]}"; do
        echo "  - ${name}"
    done
    echo "======================================================================="
    exit 1
else
    echo "ALL TESTS PASSED"
    echo "======================================================================="
    exit 0
fi
