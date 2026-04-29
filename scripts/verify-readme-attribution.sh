#!/usr/bin/env bash
# =============================================================================
# verify-readme-attribution.sh
# Verification suite for Story 8.2 — README Development Methodology section
# Test framework: pure Bash with exit codes (no external dependencies)
#
# Usage: bash scripts/verify-readme-attribution.sh
#
# Tests run READ-ONLY against README.md in the repository root.
# Exit 0 if all 7 ACs pass, Exit 1 if any fail.
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
README="${REPO_ROOT}/README.md"

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
    # Use awk: start printing after the target heading, stop at next ## heading
    awk -v h="${heading}" '
        $0 == h { found=1; next }
        found && /^## / { exit }
        found { print }
    ' "${file}"
}

# =============================================================================
# AC1: H2 heading "## Development Methodology" exists (exact spelling/casing)
#
# Given:  README.md in the repo root
# When:   grep for the exact string "^## Development Methodology$"
# Then:   exactly 1 match
# =============================================================================
test_ac1_h2_heading_present() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found at ${README}" >&2
        return 1
    fi

    local count
    count=$(grep -c "^## Development Methodology$" "${README}" || true)
    if [[ "${count}" -eq 1 ]]; then
        return 0
    else
        echo "  FAIL: expected exactly 1 occurrence of '## Development Methodology', found ${count}" >&2
        return 1
    fi
}

# =============================================================================
# AC2: The section body contains the word "BMAD"
#
# Given:  README.md after implementation
# When:   Extract "## Development Methodology" section body, grep for "BMAD"
# Then:   "BMAD" appears at least once
# =============================================================================
test_ac2_bmad_word_in_section() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found at ${README}" >&2
        return 1
    fi

    # First verify the section exists (so we don't report a false positive on empty extraction)
    local heading_count
    heading_count=$(grep -c "^## Development Methodology$" "${README}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL: section '## Development Methodology' not found — cannot check for BMAD" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section "## Development Methodology" "${README}")

    if echo "${section_body}" | grep -q "BMAD"; then
        return 0
    else
        echo "  FAIL: word 'BMAD' not found in '## Development Methodology' section body" >&2
        return 1
    fi
}

# =============================================================================
# AC3: Claude attribution sentence present in the Development Methodology section
#
# Given:  README.md after implementation
# When:   Extract section body, grep for "AI assistance via Claude"
# Then:   at least one match in the section (not file-wide)
# =============================================================================
test_ac3_claude_attribution_sentence() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found at ${README}" >&2
        return 1
    fi

    local heading_count
    heading_count=$(grep -c "^## Development Methodology$" "${README}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL: section '## Development Methodology' not found — cannot check for attribution" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section "## Development Methodology" "${README}")

    if echo "${section_body}" | grep -q "AI assistance via Claude"; then
        return 0
    else
        echo "  FAIL: attribution phrase 'AI assistance via Claude' not found in '## Development Methodology' section" >&2
        return 1
    fi
}

# =============================================================================
# AC4: CONTRIBUTING.md MARKDOWN LINK present in the section body
#
# Given:  Extract "## Development Methodology" section body
# When:   grep for Markdown link syntax: [text-containing-CONTRIBUTING.md](path-to-CONTRIBUTING.md)
# Then:   at least one match — plain-text mentions do NOT satisfy this AC
# =============================================================================
test_ac4_contributing_link_in_section() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found at ${README}" >&2
        return 1
    fi

    local heading_count
    heading_count=$(grep -c "^## Development Methodology$" "${README}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL: section '## Development Methodology' not found — cannot check for CONTRIBUTING.md link" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section "## Development Methodology" "${README}")

    # Require Markdown link syntax: [...](.../CONTRIBUTING.md) or [...](CONTRIBUTING.md)
    if echo "${section_body}" | grep -qE '\[[^]]*\]\([^)]*CONTRIBUTING\.md[^)]*\)'; then
        return 0
    else
        echo "  FAIL: Markdown link to CONTRIBUTING.md not found in section body" >&2
        echo "         Required syntax: [link-text](CONTRIBUTING.md) or [link-text](./CONTRIBUTING.md)" >&2
        return 1
    fi
}

# =============================================================================
# AC5: Section order — "## Development Methodology" appears after "## Architecture"
#      and before "## Quick Start" (or equivalent "Getting Started" heading)
#
# Given:  README.md after implementation
# When:   awk collects line numbers of all H2 headings
# Then:   line(Development Methodology) > line(Architecture)
#         AND line(Development Methodology) < line(Quick Start)
# =============================================================================
test_ac5_section_order() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found at ${README}" >&2
        return 1
    fi

    local line_arch line_dev line_qs
    line_arch=$(awk '/^## Architecture$/{print NR; exit}' "${README}" || true)
    line_dev=$(awk '/^## Development Methodology$/{print NR; exit}' "${README}" || true)
    # Accept "## Quick Start" or "## Getting Started"
    line_qs=$(awk '/^## (Quick Start|Getting Started)$/{print NR; exit}' "${README}" || true)

    local ok=1

    if [[ -z "${line_arch}" ]]; then
        echo "  FAIL: '## Architecture' heading not found in README.md" >&2
        ok=0
    fi
    if [[ -z "${line_dev}" ]]; then
        echo "  FAIL: '## Development Methodology' heading not found in README.md" >&2
        ok=0
    fi
    if [[ -z "${line_qs}" ]]; then
        echo "  FAIL: '## Quick Start' / '## Getting Started' heading not found in README.md" >&2
        ok=0
    fi

    if [[ "${ok}" -eq 0 ]]; then
        return 1
    fi

    if [[ "${line_dev}" -gt "${line_arch}" ]] && [[ "${line_dev}" -lt "${line_qs}" ]]; then
        return 0
    else
        echo "  FAIL: section order incorrect" >&2
        echo "    ## Architecture       at line ${line_arch}" >&2
        echo "    ## Development Methodology at line ${line_dev}" >&2
        echo "    ## Quick Start        at line ${line_qs}" >&2
        echo "    Expected: Architecture < Development Methodology < Quick Start" >&2
        return 1
    fi
}

# =============================================================================
# AC6: No emojis in the "## Development Methodology" section body
#
# Given:  Extract "## Development Methodology" section body
# When:   Python3 scans for Unicode emoji ranges (portable across GNU/BSD)
# Then:   zero matches
#
# Note: We use Python3 instead of grep -P because BSD grep (macOS) does not
#       support -P, and grep -E with raw UTF-8 byte ranges is unreliable.
#       Python3 is part of macOS base install and always present in CI.
# =============================================================================
test_ac6_no_emojis_in_section() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found at ${README}" >&2
        return 1
    fi

    if ! command -v python3 &>/dev/null; then
        echo "  FAIL: python3 required for portable emoji detection (not found in PATH)" >&2
        return 1
    fi

    local heading_count
    heading_count=$(grep -c "^## Development Methodology$" "${README}" || true)
    if [[ "${heading_count}" -lt 1 ]]; then
        echo "  FAIL: section '## Development Methodology' not found — cannot verify no-emoji constraint" >&2
        return 1
    fi

    local section_body
    section_body=$(extract_section "## Development Methodology" "${README}")

    # Python checks code points in:
    #   U+1F000–U+1FFFF  — Supplemental Multilingual Plane (most emojis)
    #   U+2600–U+27BF    — Miscellaneous Symbols, Dingbats
    # Exit 1 (with the offending characters on stderr) if any match; exit 0 otherwise.
    local py_output py_rc=0
    py_output=$(printf '%s' "${section_body}" | python3 -c '
import sys, re
text = sys.stdin.read()
pattern = re.compile(r"[\U0001F000-\U0001FFFF\U00002600-\U000027BF]")
matches = pattern.findall(text)
if matches:
    print("found {} emoji char(s): {}".format(len(matches), " ".join(repr(m) for m in matches[:10])), file=sys.stderr)
    sys.exit(1)
sys.exit(0)
' 2>&1) || py_rc=$?

    if [[ "${py_rc}" -eq 0 ]]; then
        return 0
    else
        echo "  FAIL: emoji character(s) found in '## Development Methodology' section: ${py_output}" >&2
        return 1
    fi
}

# =============================================================================
# AC7: markdownlint — no NEW errors introduced by the Development Methodology section
#
# The baseline README already contains lint errors (pre-existing). This test
# measures the error count BEFORE the new section exists (git show HEAD:README.md)
# and AFTER (the working-tree README.md). It FAILS only if the new section adds
# additional errors beyond the baseline.
#
# In the red phase (section not yet written) the test counts any errors in the
# current README. If the current README already has errors, we record that count
# as the "baseline" (= current state). The test passes when the final README has
# <= baseline errors.
#
# Strategy: try npx markdownlint-cli; fall back to Docker; SKIP if neither available.
# =============================================================================

# Helper: run markdownlint against a file, return the error count (line count of output)
_markdownlint_error_count() {
    local file="$1"
    local count=0
    if command -v npx &>/dev/null; then
        set +e
        count=$(npx --yes markdownlint-cli "${file}" 2>&1 | grep -c " error " || true)
        set -e
    elif command -v docker &>/dev/null; then
        local rel_file
        rel_file=$(basename "${file}")
        set +e
        count=$(docker run --rm -v "${REPO_ROOT}:/work" davidanson/markdownlint-cli2:latest "${rel_file}" 2>&1 | grep -c " error " || true)
        set -e
    else
        echo "__UNAVAILABLE__"
        return
    fi
    echo "${count}"
}

test_ac7_markdownlint_clean() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found at ${README}" >&2
        return 1
    fi

    # Check tool availability
    if ! command -v npx &>/dev/null && ! command -v docker &>/dev/null; then
        echo "  SKIP: neither 'npx' nor 'docker' found — markdownlint check cannot run" >&2
        echo "        Install markdownlint-cli ('npm i -g markdownlint-cli') or Docker to enable this check." >&2
        # In the red phase treat SKIP as FAIL so the suite does not silently green
        return 1
    fi

    # Baseline: count errors in the committed (HEAD) version of README.md
    local baseline_readme baseline_count
    baseline_readme="$(mktemp /tmp/readme-baseline-XXXX.md)"
    # Ensure tempfile is removed even if a later step aborts (set -e).
    # We register an EXIT trap once per script invocation (idempotent: appended to TEMPFILES list).
    TEMPFILES+=("${baseline_readme}")
    if git -C "${REPO_ROOT}" show HEAD:README.md > "${baseline_readme}" 2>/dev/null; then
        baseline_count=$(_markdownlint_error_count "${baseline_readme}")
    else
        # No HEAD README (unlikely), use 0 as baseline
        baseline_count=0
    fi
    rm -f "${baseline_readme}"

    # Current working-tree error count
    local current_count
    current_count=$(_markdownlint_error_count "${README}")

    echo "    Baseline (HEAD) lint errors: ${baseline_count}" >&2
    echo "    Current (working-tree) lint errors: ${current_count}" >&2

    if [[ "${current_count}" -le "${baseline_count}" ]]; then
        return 0
    else
        local new_errors=$(( current_count - baseline_count ))
        echo "  FAIL: ${new_errors} new markdownlint error(s) introduced (baseline=${baseline_count}, current=${current_count})" >&2
        # Show diff of errors for diagnosis
        if command -v npx &>/dev/null; then
            set +e
            npx --yes markdownlint-cli "${README}" 2>&1 | tail -30 >&2
            set -e
        fi
        return 1
    fi
}

# =============================================================================
# MAIN — Run all tests sequentially
# =============================================================================
echo "======================================================================="
echo "verify-readme-attribution.sh — Story 8.2 Acceptance Criteria"
echo "README: ${README}"
echo "======================================================================="
echo ""

run_test test_ac1_h2_heading_present
run_test test_ac2_bmad_word_in_section
run_test test_ac3_claude_attribution_sentence
run_test test_ac4_contributing_link_in_section
run_test test_ac5_section_order
run_test test_ac6_no_emojis_in_section
run_test test_ac7_markdownlint_clean

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
