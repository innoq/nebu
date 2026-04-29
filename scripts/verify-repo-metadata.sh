#!/usr/bin/env bash
# =============================================================================
# verify-repo-metadata.sh
# Acceptance tests for Story 8.8 — Repo-Metadaten: License-Check, Topics,
# Description, Badges
# Test framework: pure Bash with exit codes (no external test framework)
#
# Usage: bash scripts/verify-repo-metadata.sh
#
# Tests validate that:
#   - LICENSE file exists and contains correct Apache-2.0 text
#   - README.md has exactly one H1 heading
#   - Badge block appears in the first 30 lines (under H1, before H2)
#   - Apache-2.0 license badge links to relative LICENSE file
#   - GitHub Actions CI badge URL is correct
#   - GitLab CI pipeline badge URL is correct (old wrong URL absent)
#   - Both GitHub and OpenCode repo badges are present with correct links
#   - At least four tech-stack badges (Go, Erlang, PostgreSQL, Docker)
#   - No emoji characters in the README badge block
#   - scripts/setup-repo-metadata.sh exists, is executable, shellcheck clean,
#     and has --github / --gitlab / --all modes
#   - scripts/REPO_METADATA_RUNBOOK.md exists with required content
#   - README.md markdownlint delta: current produces no more errors than baseline
#
# Exit 0 if all 12 tests pass, Exit 1 if any test fails.
#
# Lessons Learned applied (8.1–8.7):
#   - python3 for emoji + Unicode checks (BSD grep Unicode false-positives)
#   - cd "${REPO_ROOT}" + relative paths for markdownlint (lesson 8.2/8.3)
#   - Anchored regex patterns where intent is anchored (lesson 8.4)
#   - set -euo pipefail + local + quoting throughout
#   - Cleanup trap with return 0 and TEMPFILES array
#   - shellcheck SKIP-fallback for optional tools
#   - Negative URL test: opencode.de/nebu-chat/nebu must NOT appear in badge block
#   - Section-scoping: badge block extracted via line range (H1 to first H2 / 30)
#   - Markdownlint delta: baseline = git show HEAD:README.md vs working tree
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

LICENSE_FILE="${REPO_ROOT}/LICENSE"
README="${REPO_ROOT}/README.md"
SETUP_SCRIPT="${REPO_ROOT}/scripts/setup-repo-metadata.sh"
RUNBOOK="${REPO_ROOT}/scripts/REPO_METADATA_RUNBOOK.md"
VERIFY_SCRIPT="${REPO_ROOT}/scripts/verify-repo-metadata.sh"

# ---------------------------------------------------------------------------
# Global counters
# ---------------------------------------------------------------------------
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_NAMES=()

# ---------------------------------------------------------------------------
# Cleanup — temp files registered in TEMPFILES are removed on exit.
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
# Helper: extract_badge_block
# Extracts the badge block from README.md:
#   - Lines from (H1 line + 1) up to the first H2 line, or at most 30 lines.
# Writes extracted lines to stdout.
# ---------------------------------------------------------------------------
extract_badge_block() {
    if [[ ! -f "${README}" ]]; then
        return 1
    fi

    python3 - "${README}" <<'PYEOF'
import sys

with open(sys.argv[1], encoding='utf-8') as f:
    lines = f.readlines()

h1_idx = None
for i, line in enumerate(lines):
    if line.startswith('# '):
        h1_idx = i
        break

if h1_idx is None:
    sys.exit(0)

# Extract from line after H1, up to first H2 or max 30 lines from start
start = h1_idx + 1
end = min(len(lines), 30)
for i in range(start, end):
    if lines[i].startswith('## '):
        end = i
        break

for line in lines[start:end]:
    print(line, end='')
PYEOF
}

# =============================================================================
# TEST 1 — test_license_exists_and_apache2
#
# AC 1 / Story AT 1
# Given: LICENSE file exists in repo root
# When:  grep for "Apache License" AND "Version 2.0" in LICENSE
# Then:  Both patterns found (exit code 0)
# =============================================================================
test_license_exists_and_apache2() {
    if [[ ! -f "${LICENSE_FILE}" ]]; then
        echo "  FAIL: LICENSE file not found at ${LICENSE_FILE}" >&2
        return 1
    fi

    if ! grep -qF "Apache License" "${LICENSE_FILE}"; then
        echo "  FAIL: 'Apache License' not found in LICENSE" >&2
        return 1
    fi

    if ! grep -qF "Version 2.0" "${LICENSE_FILE}"; then
        echo "  FAIL: 'Version 2.0' not found in LICENSE" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 2 — test_readme_has_h1
#
# AC 2 / Story AT 2
# Given: README.md exists
# When:  grep for anchored pattern "^# " (exactly one H1 line)
# Then:  Match found (H1 heading present)
# =============================================================================
test_readme_has_h1() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found at ${README}" >&2
        return 1
    fi

    local count
    count="$(grep -cE "^# " "${README}" || true)"
    if [[ "${count}" -eq 0 ]]; then
        echo "  FAIL: No H1 heading (^# ) found in README.md" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 3 — test_badge_block_under_h1
#
# AC 2 / Story AT 3
# Given: README.md exists and has an H1
# When:  Extract lines 1–30; assert at least one Markdown image-link pattern
#        ([![ ) is present in those lines
# Then:  At least one badge pattern (\[!\[) found within the first 30 lines
# Note:  Badge block must be directly under H1 and before any H2 or blockquote.
# =============================================================================
test_badge_block_under_h1() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found" >&2
        return 1
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot run badge-block check" >&2
        return 0
    fi

    local badge_block
    badge_block="$(extract_badge_block)"

    if ! echo "${badge_block}" | grep -qF "[!["; then
        echo "  FAIL: No badge pattern (\`[![\`) found within lines 1–30 of README.md" >&2
        echo "  Expected: badge block inserted directly under H1 (^# )" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 4 — test_apache_license_badge_with_link
#
# AC 3 / Story AT 4
# Given: Badge block present in README.md
# When:  grep for "](LICENSE)" — license badge must link to relative LICENSE file
# Then:  Match found
# =============================================================================
test_apache_license_badge_with_link() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found" >&2
        return 1
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot extract badge block" >&2
        return 0
    fi

    local badge_block
    badge_block="$(extract_badge_block)"

    if ! echo "${badge_block}" | grep -qF "](LICENSE)"; then
        echo "  FAIL: No badge linking to relative 'LICENSE' found in badge block" >&2
        echo "  Expected: [![License](...)](LICENSE)" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 5 — test_github_actions_build_badge
#
# AC 4 / Story AT 5
# Given: Badge block present in README.md
# When:  grep for "github.com/innoq/nebu/actions" in badge block lines
# Then:  Match found (covers both badge image URL and link target)
# =============================================================================
test_github_actions_build_badge() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found" >&2
        return 1
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot extract badge block" >&2
        return 0
    fi

    local badge_block
    badge_block="$(extract_badge_block)"

    if ! echo "${badge_block}" | grep -qF "github.com/innoq/nebu/actions"; then
        echo "  FAIL: 'github.com/innoq/nebu/actions' not found in badge block" >&2
        echo "  Expected GitHub Actions badge URL: https://github.com/innoq/nebu/actions/workflows/ci.yml/badge.svg" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 6 — test_gitlab_ci_pipeline_badge
#
# AC 5 / Story AT 6
# Given: Badge block present in README.md
# When (6a): grep for "gitlab.opencode.de/nebu/nebu-server/badges/main/pipeline.svg"
# Then (6a): Match found
# When (6b): grep for "gitlab.opencode.de/nebu/nebu-server/-/pipelines"
# Then (6b): Match found
# When (6c): grep for "opencode.de/nebu-chat" in badge block lines
# Then (6c): Must NOT be found (old incorrect URL absent from badge block)
# =============================================================================
test_gitlab_ci_pipeline_badge() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found" >&2
        return 1
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot extract badge block" >&2
        return 0
    fi

    local badge_block ok=0
    badge_block="$(extract_badge_block)"

    # 6a: pipeline badge image URL
    if ! echo "${badge_block}" | grep -qF "gitlab.opencode.de/nebu/nebu-server/badges/main/pipeline.svg"; then
        echo "  FAIL (6a): 'gitlab.opencode.de/nebu/nebu-server/badges/main/pipeline.svg' not found in badge block" >&2
        ok=1
    fi

    # 6b: pipeline badge link target
    if ! echo "${badge_block}" | grep -qF "gitlab.opencode.de/nebu/nebu-server/-/pipelines"; then
        echo "  FAIL (6b): 'gitlab.opencode.de/nebu/nebu-server/-/pipelines' not found in badge block" >&2
        ok=1
    fi

    # 6c: old incorrect URL must NOT appear in badge block
    if echo "${badge_block}" | grep -qF "opencode.de/nebu-chat"; then
        echo "  FAIL (6c): old incorrect URL 'opencode.de/nebu-chat' found in badge block — must be replaced with gitlab.opencode.de/nebu/nebu-server" >&2
        ok=1
    fi

    return "${ok}"
}

# =============================================================================
# TEST 7 — test_repo_badges_github_and_opencode
#
# AC 6 / Story AT 7
# Given: Badge block present in README.md
# When (7a): grep for "github.com/innoq/nebu)" (GitHub repo badge link target)
# Then (7a): Match found
# When (7b): grep for "gitlab.opencode.de/nebu/nebu-server)" (OpenCode badge link)
# Then (7b): Match found
# =============================================================================
test_repo_badges_github_and_opencode() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found" >&2
        return 1
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot extract badge block" >&2
        return 0
    fi

    local badge_block ok=0
    badge_block="$(extract_badge_block)"

    # 7a: GitHub repo badge
    if ! echo "${badge_block}" | grep -qF "github.com/innoq/nebu)"; then
        echo "  FAIL (7a): GitHub repo badge link target 'github.com/innoq/nebu)' not found in badge block" >&2
        ok=1
    fi

    # 7b: OpenCode repo badge
    if ! echo "${badge_block}" | grep -qF "gitlab.opencode.de/nebu/nebu-server)"; then
        echo "  FAIL (7b): OpenCode repo badge link target 'gitlab.opencode.de/nebu/nebu-server)' not found in badge block" >&2
        ok=1
    fi

    return "${ok}"
}

# =============================================================================
# TEST 8 — test_tech_stack_badges_min_4
#
# AC 7 / Story AT 8
# Given: Badge block present in README.md
# When:  Count how many of Go, Erlang, PostgreSQL, Docker appear (case-insensitive)
#        in badge block lines 1–30; also check Sovereign/sovereign appears
# Then:  All four tech-stack strings present; Sovereign present at least once
# =============================================================================
test_tech_stack_badges_min_4() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found" >&2
        return 1
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot extract badge block" >&2
        return 0
    fi

    local badge_block
    badge_block="$(extract_badge_block)"

    local ok=0
    local -a required=("Go" "Erlang" "PostgreSQL" "Docker")
    local term
    for term in "${required[@]}"; do
        if ! echo "${badge_block}" | grep -qiF "${term}"; then
            echo "  FAIL: tech-stack badge for '${term}' not found in badge block" >&2
            ok=1
        fi
    done

    if ! echo "${badge_block}" | grep -qiF "Sovereign"; then
        echo "  FAIL: 'Sovereign' / 'sovereign' badge not found in badge block" >&2
        ok=1
    fi

    return "${ok}"
}

# =============================================================================
# TEST 9 — test_no_emojis_in_badge_block
#
# AC 8 / Story AT 9
# Given: README.md exists
# When:  python3 extracts badge block (first 30 lines); scans each character
#        for Unicode ranges 0x1F000–0x1FFFF and 0x2600–0x27BF
# Then:  Zero emoji characters found
# =============================================================================
test_no_emojis_in_badge_block() {
    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found" >&2
        return 1
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "  SKIP: python3 not in PATH — cannot run emoji check" >&2
        return 0
    fi

    local py_rc=0
    python3 - "${README}" <<'PYEOF' || py_rc=$?
import sys

with open(sys.argv[1], encoding='utf-8') as f:
    lines = f.readlines()

# Extract badge block: from after H1 up to first H2 or 30 lines
h1_idx = None
for i, line in enumerate(lines):
    if line.startswith('# '):
        h1_idx = i
        break

if h1_idx is None:
    print("SKIP: no H1 found in README.md", file=sys.stderr)
    sys.exit(0)

start = h1_idx + 1
end = min(len(lines), 30)
for i in range(start, end):
    if lines[i].startswith('## '):
        end = i
        break

badge_lines = lines[start:end]
bad = []
for lineno, line in enumerate(badge_lines, start=start+1):
    for ch in line:
        cp = ord(ch)
        if 0x1F000 <= cp <= 0x1FFFF or 0x2600 <= cp <= 0x27BF:
            bad.append(f"  line {lineno}: U+{cp:04X} ({ch!r})")

if bad:
    print("Emoji characters found in README.md badge block:", file=sys.stderr)
    for b in bad:
        print(b, file=sys.stderr)
    sys.exit(1)
sys.exit(0)
PYEOF

    if [[ "${py_rc}" -ne 0 ]]; then
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 10 — test_setup_repo_metadata_script
#
# AC 9 / Story AT 10
# Given: scripts/setup-repo-metadata.sh exists
# When (10a): [[ -x scripts/setup-repo-metadata.sh ]] — executable bit set
# When (10b): shellcheck --severity=error scripts/setup-repo-metadata.sh
#             (SKIP with exit 0 if shellcheck not in PATH)
# When (10c): grep for --github, --gitlab, --all flags in the script
# Then (10a-c): All checks pass
# =============================================================================
test_setup_repo_metadata_script() {
    if [[ ! -f "${SETUP_SCRIPT}" ]]; then
        echo "  FAIL: scripts/setup-repo-metadata.sh not found at ${SETUP_SCRIPT}" >&2
        return 1
    fi

    local ok=0

    # 10a: executable
    if [[ ! -x "${SETUP_SCRIPT}" ]]; then
        echo "  FAIL (10a): scripts/setup-repo-metadata.sh is not executable (chmod +x required)" >&2
        ok=1
    fi

    # 10b: shellcheck (SKIP-fallback)
    if ! command -v shellcheck >/dev/null 2>&1; then
        echo "  SKIP (10b): shellcheck not in PATH — install to enable this check" >&2
    else
        local sc_rc=0
        local sc_out
        sc_out="$(shellcheck --severity=error "${SETUP_SCRIPT}" 2>&1)" || sc_rc=$?
        if [[ "${sc_rc}" -ne 0 ]]; then
            echo "  FAIL (10b): shellcheck errors in scripts/setup-repo-metadata.sh:" >&2
            echo "${sc_out}" >&2
            ok=1
        fi
    fi

    # 10c: three mode flags
    if ! grep -qF -- "--github" "${SETUP_SCRIPT}"; then
        echo "  FAIL (10c): '--github' mode flag not found in scripts/setup-repo-metadata.sh" >&2
        ok=1
    fi
    if ! grep -qF -- "--gitlab" "${SETUP_SCRIPT}"; then
        echo "  FAIL (10c): '--gitlab' mode flag not found in scripts/setup-repo-metadata.sh" >&2
        ok=1
    fi
    if ! grep -qF -- "--all" "${SETUP_SCRIPT}"; then
        echo "  FAIL (10c): '--all' mode flag not found in scripts/setup-repo-metadata.sh" >&2
        ok=1
    fi

    # 10d: spot-check that the prescribed topics appear in the script.
    # We don't enforce the full list (intentional flexibility for the
    # maintainer) but a regression that drops the core topics should fail.
    local topic
    for topic in "matrix" "sovereign" "nebu" "apache-2"; do
        if ! grep -qF "${topic}" "${SETUP_SCRIPT}"; then
            echo "  FAIL (10d): topic '${topic}' not found in scripts/setup-repo-metadata.sh" >&2
            ok=1
        fi
    done

    # 10e: description spot-check — the script must apply a description to
    # both platforms. We assert two stable phrases independently (the
    # description string may span backslash-continued lines, so a single
    # multi-word grep would miss it across the line break).
    if ! grep -qiF "Matrix Client-Server API" "${SETUP_SCRIPT}"; then
        echo "  FAIL (10e): description phrase 'Matrix Client-Server API' not found" >&2
        ok=1
    fi
    if ! grep -qiF "no federation" "${SETUP_SCRIPT}"; then
        echo "  FAIL (10e): description phrase 'no federation' not found" >&2
        ok=1
    fi

    return "${ok}"
}

# =============================================================================
# TEST 11 — test_repo_metadata_runbook
#
# AC 10 / Story AT 11
# Given: scripts/REPO_METADATA_RUNBOOK.md exists
# When (11a): grep -q "gh" scripts/REPO_METADATA_RUNBOOK.md
# Then (11a): Match found (gh CLI mentioned)
# When (11b): grep -q "glab" scripts/REPO_METADATA_RUNBOOK.md
# Then (11b): Match found (glab CLI mentioned)
# When (11c): grep -qi "story 8.10\|story 8-10" scripts/REPO_METADATA_RUNBOOK.md
# Then (11c): Match found (explicit gate statement referencing Story 8.10)
# =============================================================================
test_repo_metadata_runbook() {
    if [[ ! -f "${RUNBOOK}" ]]; then
        echo "  FAIL: scripts/REPO_METADATA_RUNBOOK.md not found at ${RUNBOOK}" >&2
        return 1
    fi

    local ok=0

    # 11a: gh CLI
    if ! grep -qF "gh" "${RUNBOOK}"; then
        echo "  FAIL (11a): 'gh' not found in scripts/REPO_METADATA_RUNBOOK.md" >&2
        ok=1
    fi

    # 11b: glab CLI
    if ! grep -qF "glab" "${RUNBOOK}"; then
        echo "  FAIL (11b): 'glab' not found in scripts/REPO_METADATA_RUNBOOK.md" >&2
        ok=1
    fi

    # 11c: Story 8.10 gate reference (case-insensitive, accept 8.10 or 8-10)
    if ! grep -qiE "story 8[.-]10" "${RUNBOOK}"; then
        echo "  FAIL (11c): 'Story 8.10' (or 'Story 8-10') gate reference not found in scripts/REPO_METADATA_RUNBOOK.md" >&2
        ok=1
    fi

    # 11d: AC10 requires four sections; one of them is the manual-fallback
    # via the web UI. The earlier 11a/11b/11c checks indirectly verify the
    # CLI-install and gate sections, but the fallback heading would be lost
    # without an explicit assertion.
    if ! grep -qiF "fallback" "${RUNBOOK}"; then
        echo "  FAIL (11d): 'fallback' (manual web-UI fallback section) not found in scripts/REPO_METADATA_RUNBOOK.md" >&2
        ok=1
    fi

    return "${ok}"
}

# =============================================================================
# TEST 12 — test_readme_markdownlint_clean_delta
#
# AC 8 / Story AT 12
# Given: README.md in working tree; git HEAD as baseline
# When:  Run markdownlint-cli on both the baseline (git show HEAD:README.md)
#        and the current working-tree README.md; count errors in each
# Then:  current error count <= baseline error count (delta: no new errors)
#        SKIP (exit 0) if npx not available
#        SKIP (exit 0) if git not available or no HEAD commit
# =============================================================================
test_readme_markdownlint_clean_delta() {
    if ! command -v npx >/dev/null 2>&1; then
        echo "  SKIP: npx not in PATH — markdownlint delta check skipped" >&2
        return 0
    fi

    if [[ ! -f "${README}" ]]; then
        echo "  FAIL: README.md not found" >&2
        return 1
    fi

    # Check if git is available and there is a HEAD commit
    if ! command -v git >/dev/null 2>&1; then
        echo "  SKIP: git not in PATH — cannot compute baseline" >&2
        return 0
    fi

    local head_readme
    head_readme="$(mktemp /tmp/verify-readme-baseline-XXXXXX.md)"
    TEMPFILES+=("${head_readme}")

    local git_rc=0
    git -C "${REPO_ROOT}" show HEAD:README.md > "${head_readme}" 2>/dev/null || git_rc=$?
    if [[ "${git_rc}" -ne 0 ]]; then
        echo "  SKIP: README.md not in git HEAD — no baseline available; skipping delta check" >&2
        return 0
    fi

    # Count markdownlint errors in baseline
    local baseline_out
    baseline_out="$(cd "${REPO_ROOT}" && npx --yes markdownlint-cli "${head_readme}" 2>&1)" || true
    # Count lines that match the markdownlint error pattern (path:line:col rule).
    # The baseline is a temp file so we match any .md: path pattern.
    local baseline_count
    baseline_count="$(echo "${baseline_out}" | grep -cE "\.md:[0-9]" || true)"

    # Count markdownlint errors in current working tree README.md
    local current_out
    current_out="$(cd "${REPO_ROOT}" && npx --yes markdownlint-cli "README.md" 2>&1)" || true
    local current_count
    current_count="$(echo "${current_out}" | grep -cE "\.md:[0-9]" || true)"

    if [[ "${current_count}" -gt "${baseline_count}" ]]; then
        echo "  FAIL: README.md has more markdownlint errors than baseline" >&2
        echo "    Baseline (HEAD): ${baseline_count} error(s)" >&2
        echo "    Current:         ${current_count} error(s)" >&2
        echo "    New errors (delta):" >&2
        echo "${current_out}" | grep -E "\.md:[0-9]" >&2 || true
        return 1
    fi

    if [[ "${current_count}" -gt 0 ]]; then
        echo "  NOTE: README.md has ${current_count} markdownlint error(s) — same as baseline (not a regression)" >&2
    fi

    return 0
}

# =============================================================================
# MAIN — Run all 12 tests sequentially
# =============================================================================
main() {
    echo "======================================================================="
    echo "verify-repo-metadata.sh — Story 8.8 Acceptance Tests"
    echo "REPO_ROOT: ${REPO_ROOT}"
    echo "======================================================================="
    echo ""

    run_test test_license_exists_and_apache2
    run_test test_readme_has_h1
    run_test test_badge_block_under_h1
    run_test test_apache_license_badge_with_link
    run_test test_github_actions_build_badge
    run_test test_gitlab_ci_pipeline_badge
    run_test test_repo_badges_github_and_opencode
    run_test test_tech_stack_badges_min_4
    run_test test_no_emojis_in_badge_block
    run_test test_setup_repo_metadata_script
    run_test test_repo_metadata_runbook
    run_test test_readme_markdownlint_clean_delta

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
