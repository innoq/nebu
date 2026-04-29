#!/usr/bin/env bash
# =============================================================================
# verify-security-policy.sh
# Verification suite for Story 8.4 — SECURITY.md Vulnerability-Disclosure-Policy
# Test framework: pure Bash with exit codes (no external dependencies except python3)
#
# Usage: bash scripts/verify-security-policy.sh
#
# Tests run READ-ONLY against SECURITY.md in the repository root.
# Exit 0 if all 10 ACs pass, Exit 1 if any fail.
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
SECURITY="${REPO_ROOT}/SECURITY.md"

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
# AC1: SECURITY.md exists in the repository root
#
# Given:  Repository root after implementation
# When:   test -f SECURITY.md
# Then:   exit code 0
# =============================================================================
test_ac1_file_exists() {
    if [[ -f "${SECURITY}" ]]; then
        return 0
    else
        echo "  FAIL: SECURITY.md not found at ${SECURITY}" >&2
        return 1
    fi
}

# =============================================================================
# AC2: Supported Versions section with Markdown table
#
# Given:  SECURITY.md after implementation
# When:   2a: grep for exactly "## Supported Versions" heading
#         2b: section body contains a Markdown table row (pipe character)
# Then:   2a: >= 1 match
#         2b: at least one "|" present in section body
# =============================================================================
test_ac2_supported_versions_section() {
    if [[ ! -f "${SECURITY}" ]]; then
        echo "  FAIL: SECURITY.md not found" >&2
        return 1
    fi

    local heading_count
    heading_count=$(grep -cE "^## Supported Versions$" "${SECURITY}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL (2a): no '## Supported Versions' heading found" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section "## Supported Versions" "${SECURITY}")

    if ! echo "${section_body}" | grep -q "|"; then
        echo "  FAIL (2b): no Markdown table ('|') found in '## Supported Versions' section" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# AC3: Reporting section with GitHub Advisories mention and no-public-issues note
#
# Given:  SECURITY.md after implementation
# When:   3a: grep for heading starting with "## Reporting" (anchored)
#         3b: section body contains "GitHub Security Advisories" or "Security Advisories"
#         3c: file contains language prohibiting reporting via public issues
# Then:   all three checks pass
#
# Note: AC3c uses dual sub-assertions per Lessons-Learned principle 9:
#       (a) "not" / "do not" / "never" / "must not" appears in the file, and
#       (b) the combination of prohibition + "public" + "issue/bug" is present.
# =============================================================================
test_ac3_reporting_section() {
    if [[ ! -f "${SECURITY}" ]]; then
        echo "  FAIL: SECURITY.md not found" >&2
        return 1
    fi

    # 3a: heading anchored to "## Reporting" (matches "## Reporting a Vulnerability"
    # and "## Reporting Vulnerabilities" but NOT "## Reporting Bug Reports" — see
    # Lessons-Learned principle 3 on anchored headings).
    local heading_count
    heading_count=$(grep -cE "^## Reporting" "${SECURITY}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL (3a): no heading starting with '## Reporting' found" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section_flexible "^## Reporting" "${SECURITY}")

    local ok=1

    # 3b: GitHub Security Advisories mentioned in section body
    if ! echo "${section_body}" | grep -qi "Security Advisories"; then
        echo "  FAIL (3b): 'Security Advisories' not found in Reporting section body" >&2
        ok=0
    fi

    # 3c: Prohibition against public issues — dual sub-assertion:
    #   (3c-i)  an explicit prohibition INSTRUCTION exists ("do not report",
    #           "must not report", "never report") — not just any "not"
    #   (3c-ii) combined pattern: prohibition adjacent to "public" + "issue/bug"
    if ! grep -qiE "(do not|must not|never|don't) (report|file|open|submit|disclose)" "${SECURITY}"; then
        echo "  FAIL (3c-i): no explicit prohibition instruction found in SECURITY.md" >&2
        echo "    Expected: e.g. 'Do not report ...', 'Must not file ...', 'Never disclose ...'" >&2
        ok=0
    fi

    if ! grep -qiE "not.*public.*(issue|bug)|public.*(issue|bug).*not|(do not|must not|never).*public.*(issue|bug)" "${SECURITY}"; then
        echo "  FAIL (3c-ii): no combined prohibition-of-public-issues pattern found in SECURITY.md" >&2
        echo "    Expected: e.g. 'Do not report ... via public GitHub Issues'" >&2
        ok=0
    fi

    [[ "${ok}" -eq 1 ]]
}

# =============================================================================
# AC4: Response SLA — three timing commitments present
#
# Given:  SECURITY.md after implementation
# When:   4a: file contains "72" (acknowledgment window in hours)
#         4b: file contains "7 day" or "seven day" (initial assessment window)
#         4c: file contains "14 day" or "fourteen day" (fix timeline)
# Then:   all three checks pass
# =============================================================================
test_ac4_response_sla() {
    if [[ ! -f "${SECURITY}" ]]; then
        echo "  FAIL: SECURITY.md not found" >&2
        return 1
    fi

    local ok=1

    # 4a: 72 hours acknowledgment
    local count72
    count72=$(grep -c "72" "${SECURITY}" || true)
    if [[ "${count72}" -lt 1 ]]; then
        echo "  FAIL (4a): '72' (acknowledgment within 72 hours) not found in SECURITY.md" >&2
        ok=0
    fi

    # 4b: 7 days / seven days — initial assessment
    if ! grep -qiE "7.?day|seven.?day" "${SECURITY}"; then
        echo "  FAIL (4b): '7 day' or 'seven day' (initial assessment window) not found in SECURITY.md" >&2
        ok=0
    fi

    # 4c: 14 days / fourteen days — fix timeline
    if ! grep -qiE "14.?day|fourteen.?day" "${SECURITY}"; then
        echo "  FAIL (4c): '14 day' or 'fourteen day' (fix timeline) not found in SECURITY.md" >&2
        ok=0
    fi

    [[ "${ok}" -eq 1 ]]
}

# =============================================================================
# AC5: Disclosure Policy section with window in valid range (60–120 days)
#
# Given:  SECURITY.md after implementation
# When:   5a: grep for heading starting with "## Disclosure"
#         5b: file contains a disclosure window in accepted range (60, 90, or 120 days)
#         5c: section body describes what happens when window expires
#             (public disclosure proceeds)
# Then:   all checks pass
# =============================================================================
test_ac5_disclosure_policy_section() {
    if [[ ! -f "${SECURITY}" ]]; then
        echo "  FAIL: SECURITY.md not found" >&2
        return 1
    fi

    # 5a: heading
    local heading_count
    heading_count=$(grep -cE "^## Disclosure" "${SECURITY}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL (5a): no heading starting with '## Disclosure' found" >&2
        return 1
    fi

    local ok=1

    # 5b: window in valid range — accept 60, 90, or 120 days
    if ! grep -qiE "90.?day|60.?day|120.?day" "${SECURITY}"; then
        echo "  FAIL (5b): no disclosure window in valid range (60/90/120 days) found in SECURITY.md" >&2
        ok=0
    fi

    # 5c: what happens when window expires — public disclosure proceeds
    local section_body
    section_body=$(extract_section_flexible "^## Disclosure" "${SECURITY}")
    if ! echo "${section_body}" | grep -qiE "public|disclose|disclosure"; then
        echo "  FAIL (5c): Disclosure Policy section does not describe outcome when window expires (expected 'public disclosure')" >&2
        ok=0
    fi

    [[ "${ok}" -eq 1 ]]
}

# =============================================================================
# AC6: Scope section with at least three vulnerability categories
#
# Given:  Extract "## Scope" or "## In Scope" section body
# When:   Section body contains >= 3 of the required vulnerability categories
# Then:   match count >= 3
#
# Categories counted: auth, inject, crypto, remote code, RCE, sensitiv,
#                     privilege, escalat
# Each unique category stem is counted once (not per line).
# =============================================================================
test_ac6_scope_section() {
    if [[ ! -f "${SECURITY}" ]]; then
        echo "  FAIL: SECURITY.md not found" >&2
        return 1
    fi

    local heading_count
    heading_count=$(grep -cE "^## (Scope|In Scope)$" "${SECURITY}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL: no '## Scope' or '## In Scope' heading found" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section_flexible "(^## Scope$|^## In Scope$)" "${SECURITY}")

    # Count each category that appears at least once in the section body.
    local count=0
    echo "${section_body}" | grep -qi "auth"         && count=$(( count + 1 ))
    echo "${section_body}" | grep -qi "inject"       && count=$(( count + 1 ))
    echo "${section_body}" | grep -qi "crypto"       && count=$(( count + 1 ))
    echo "${section_body}" | grep -qiE "remote code|RCE" && count=$(( count + 1 ))
    echo "${section_body}" | grep -qi "sensitiv"     && count=$(( count + 1 ))
    echo "${section_body}" | grep -qiE "privilege|escalat" && count=$(( count + 1 ))

    if [[ "${count}" -ge 3 ]]; then
        return 0
    else
        echo "  FAIL: only ${count} vulnerability category/categories found in '## Scope' section (need >= 3 of: auth, inject, crypto, RCE/remote-code, sensitive, privilege/escalation)" >&2
        return 1
    fi
}

# =============================================================================
# AC7: Out of Scope section with required entries
#
# Given:  Extract "## Out of Scope" section body
# When:   7a: section body contains "denial" or "DoS" (denial-of-service entry)
#         7b: section body contains "third-party" or "upstream"
#         7c: section body contains "federat" (no-federation entry)
# Then:   all three checks pass
# =============================================================================
test_ac7_out_of_scope_section() {
    if [[ ! -f "${SECURITY}" ]]; then
        echo "  FAIL: SECURITY.md not found" >&2
        return 1
    fi

    local heading_count
    heading_count=$(grep -cE "^## Out of Scope$" "${SECURITY}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL: no '## Out of Scope' heading found" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section "## Out of Scope" "${SECURITY}")

    local ok=1

    # 7a: denial of service
    if ! echo "${section_body}" | grep -qiE "denial|DoS"; then
        echo "  FAIL (7a): 'denial' or 'DoS' (denial-of-service out-of-scope entry) not found in '## Out of Scope' section" >&2
        ok=0
    fi

    # 7b: third-party / upstream dependencies
    if ! echo "${section_body}" | grep -qiE "third.party|upstream"; then
        echo "  FAIL (7b): 'third-party' or 'upstream' not found in '## Out of Scope' section" >&2
        ok=0
    fi

    # 7c: federation explicitly out of scope
    if ! echo "${section_body}" | grep -qi "federat"; then
        echo "  FAIL (7c): 'federat' (federation out-of-scope entry) not found in '## Out of Scope' section" >&2
        ok=0
    fi

    [[ "${ok}" -eq 1 ]]
}

# =============================================================================
# AC8: Recognition / acknowledgment section present with honest no-bounty statement
#
# Given:  SECURITY.md after implementation
# When:   8a: grep for heading "## Recognition", "## Hall of Fame", "## Acknowledgments",
#             or "## Acknowledgements"
#         8b: the section body (or full file) contains language about NO bug bounty
#         8c: section body describes public credit / acknowledgment for reporters
# Then:   all three checks pass
# =============================================================================
test_ac8_recognition_section() {
    if [[ ! -f "${SECURITY}" ]]; then
        echo "  FAIL: SECURITY.md not found" >&2
        return 1
    fi

    # 8a: heading
    local heading_count
    heading_count=$(grep -cE "^## (Recognition|Hall of Fame|Acknowledgments|Acknowledgements)$" "${SECURITY}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL (8a): no '## Recognition', '## Hall of Fame', '## Acknowledgments', or '## Acknowledgements' heading found" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section_flexible "(^## Recognition$|^## Hall of Fame$|^## Acknowledgments$|^## Acknowledgements$)" "${SECURITY}")

    local ok=1

    # 8b: honest no-bounty statement
    if ! echo "${section_body}" | grep -qiE "no.*bounty|bounty.*program|no.*bug bounty|does not.*bounty|not.*operate.*bounty|cannot offer"; then
        echo "  FAIL (8b): no honest 'no bug bounty' statement found in recognition section" >&2
        echo "    Expected language like: 'no bug bounty program', 'does not operate a bounty', 'cannot offer financial rewards'" >&2
        ok=0
    fi

    # 8c: public credit / acknowledgment for reporters
    if ! echo "${section_body}" | grep -qiE "credit|acknowledged|acknowledgment|release notes|recognition"; then
        echo "  FAIL (8c): no mention of public credit/acknowledgment for reporters in recognition section" >&2
        ok=0
    fi

    [[ "${ok}" -eq 1 ]]
}

# =============================================================================
# AC9: No emojis in SECURITY.md
#
# Given:  Full SECURITY.md content
# When:   Python3 scans for Unicode emoji ranges (U+1F000-U+1FFFF, U+2600-U+27BF)
# Then:   zero matches
#
# Note: We use Python3 instead of grep -P because BSD grep (macOS) does not
#       support -P, and grep -E with raw UTF-8 byte ranges is unreliable.
#       Python3 is part of macOS base install and always present in CI.
# =============================================================================
test_ac9_no_emojis() {
    if [[ ! -f "${SECURITY}" ]]; then
        echo "  FAIL: SECURITY.md not found" >&2
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
' "${SECURITY}" 2>&1) || py_rc=$?

    if [[ "${py_rc}" -eq 0 ]]; then
        return 0
    else
        echo "  FAIL: emoji character(s) found in SECURITY.md: ${py_output}" >&2
        return 1
    fi
}

# =============================================================================
# AC10: markdownlint clean — SECURITY.md is a new file, expected 0 errors
#
# Strategy: try npx markdownlint-cli; fall back to Docker; SKIP if neither available.
# New file: no baseline comparison needed — target is 0 errors.
#
# Note: cd to REPO_ROOT before running markdownlint-cli so that the emitted
#       paths are relative (e.g. "SECURITY.md:5 ...") and the grep pattern
#       "(^|/)SECURITY\.md:" matches regardless of invocation directory.
#       This is the 8.3 trick from Lessons-Learned principle 5.
# =============================================================================
test_ac10_markdownlint_clean() {
    if [[ ! -f "${SECURITY}" ]]; then
        echo "  FAIL: SECURITY.md not found" >&2
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
        lint_output=$(cd "${REPO_ROOT}" && npx --yes markdownlint-cli SECURITY.md 2>&1)
        error_count=$(echo "${lint_output}" | grep -cE "(^|/)SECURITY\.md:" || true)
        set -e
    elif command -v docker &>/dev/null; then
        set +e
        lint_output=$(docker run --rm -v "${REPO_ROOT}:/work" -w /work davidanson/markdownlint-cli2:latest "SECURITY.md" 2>&1)
        error_count=$(echo "${lint_output}" | grep -cE "(^|/)SECURITY\.md:" || true)
        set -e
    fi

    echo "    markdownlint errors: ${error_count}" >&2

    if [[ "${error_count}" -eq 0 ]]; then
        return 0
    else
        echo "  FAIL: ${error_count} markdownlint error(s) in SECURITY.md" >&2
        echo "${lint_output}" | head -30 >&2
        return 1
    fi
}

# =============================================================================
# MAIN — Run all tests sequentially
# =============================================================================
echo "======================================================================="
echo "verify-security-policy.sh — Story 8.4 Acceptance Criteria"
echo "SECURITY.md: ${SECURITY}"
echo "======================================================================="
echo ""

run_test test_ac1_file_exists
run_test test_ac2_supported_versions_section
run_test test_ac3_reporting_section
run_test test_ac4_response_sla
run_test test_ac5_disclosure_policy_section
run_test test_ac6_scope_section
run_test test_ac7_out_of_scope_section
run_test test_ac8_recognition_section
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
