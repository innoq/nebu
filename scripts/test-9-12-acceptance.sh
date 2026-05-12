#!/usr/bin/env bash
# =============================================================================
# test-9-12-acceptance.sh
# Red-phase acceptance tests for Story 9-12 — arc42 pipeline gate hardening.
#
# Usage: bash scripts/test-9-12-acceptance.sh
#
# All 6 tests FAIL before implementation (red phase).
# All 6 tests PASS after implementation (green phase).
# Exit 0 if all pass, exit 1 if any fail.
#
# Pure Bash + python3 + standard POSIX tools — no external deps.
#
# NOTE — local-only execution:
#   Tests 2 (test_pipeline_skill_has_doc_gate) and 4 (test_maintain_skill_exists)
#   inspect files under .claude/skills/ which are .gitignore'd. They will FAIL
#   in CI (clean checkouts have no .claude/skills/ tree). This script is the
#   acceptance harness for Story 9-12 and is intended to run on the developer's
#   workstation against the local skill installation, not in CI.
#
# AC mapping:
#   test_claude_md_has_arc42_gate        → AC1
#   test_pipeline_skill_has_doc_gate     → AC2
#   test_ci_docs_job_not_allow_failure   → AC3
#   test_maintain_skill_exists           → AC4
#   test_staleness_threshold_60_days     → AC5
#   test_verify_docs_passes_after_generate → AC6
# =============================================================================

set -uo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ---------------------------------------------------------------------------
# Global counters
# ---------------------------------------------------------------------------
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_NAMES=()

# ---------------------------------------------------------------------------
# Helper: run_test <function-name>
# Calls the named function, records === PASS / === FAIL.
# ---------------------------------------------------------------------------
run_test() {
    local name="$1"
    TESTS_RUN=$(( TESTS_RUN + 1 ))
    echo "--- ${name}"
    if "${name}"; then
        echo "=== PASS"
        TESTS_PASSED=$(( TESTS_PASSED + 1 ))
    else
        echo "=== FAIL"
        TESTS_FAILED=$(( TESTS_FAILED + 1 ))
        FAILED_NAMES+=("${name}")
    fi
    echo ""
}

# =============================================================================
# TEST 1 — test_claude_md_has_arc42_gate  (AC1)
#
# Given:  CLAUDE.md has been updated for Story 9-12
# When:   grep searches CLAUDE.md for the bmad-generate-arc42 TEA table row
# Then:   exits 0 (row is present)
#
# Checks:
#   - CLAUDE.md contains "/bmad-generate-arc42"
#   - The row appears in the TEA Quick Reference table
#   - The row marks the step as mandatory (Yes)
# =============================================================================
test_claude_md_has_arc42_gate() {
    local claude_md="${REPO_ROOT}/CLAUDE.md"
    local failed=0

    if [[ ! -f "${claude_md}" ]]; then
        echo "  FAIL: CLAUDE.md not found at ${claude_md}" >&2
        return 1
    fi

    # AC1: /bmad-generate-arc42 must appear in CLAUDE.md
    if ! grep -q "bmad-generate-arc42" "${claude_md}"; then
        echo "  FAIL: CLAUDE.md does not contain 'bmad-generate-arc42'" >&2
        failed=1
    else
        # Must be inside a table row (line contains '|')
        if ! grep "bmad-generate-arc42" "${claude_md}" | grep -q "|"; then
            echo "  FAIL: 'bmad-generate-arc42' found in CLAUDE.md but not in a table row (missing '|')" >&2
            failed=1
        fi
        # Must be marked as mandatory — row should contain "Yes"
        if ! grep "bmad-generate-arc42" "${claude_md}" | grep -q "Yes"; then
            echo "  FAIL: 'bmad-generate-arc42' table row does not contain 'Yes' (mandatory marker)" >&2
            failed=1
        fi
    fi

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# TEST 2 — test_pipeline_skill_has_doc_gate  (AC2)
#
# Given:  bmad-pipeline skill has been updated for Story 9-12
# When:   .claude/skills/bmad-pipeline/SKILL.md is inspected
# Then:   it contains a doc-generation step that:
#           - invokes /bmad-generate-arc42 (or bmad-generate-arc42)
#           - references docs/.arc42-manifest.json
#           - references a 24h / generated_at freshness check
# =============================================================================
test_pipeline_skill_has_doc_gate() {
    local skill_file="${REPO_ROOT}/.claude/skills/bmad-pipeline/SKILL.md"
    local failed=0

    if [[ ! -f "${skill_file}" ]]; then
        echo "  FAIL: .claude/skills/bmad-pipeline/SKILL.md not found" >&2
        return 1
    fi

    # Must reference the arc42 generation step
    if ! grep -q "bmad-generate-arc42" "${skill_file}"; then
        echo "  FAIL: bmad-pipeline/SKILL.md does not reference 'bmad-generate-arc42'" >&2
        failed=1
    fi

    # Must reference the manifest for freshness verification
    if ! grep -q "arc42-manifest.json" "${skill_file}"; then
        echo "  FAIL: bmad-pipeline/SKILL.md does not reference 'arc42-manifest.json'" >&2
        failed=1
    fi

    # Must reference a freshness / staleness check (24h or generated_at)
    if ! grep -qE "24h|generated_at|24 h" "${skill_file}"; then
        echo "  FAIL: bmad-pipeline/SKILL.md does not reference a 24h freshness check (24h or generated_at)" >&2
        failed=1
    fi

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# TEST 3 — test_ci_docs_job_not_allow_failure  (AC3)
#
# Given:  CI files have been updated for Story 9-12
# When:   .github/workflows/ci.yml is inspected
# Then:   the docs job does NOT have continue-on-error: true
#         (GitHub Actions equivalent of allow_failure: true)
# When:   .gitlab-ci.yml is inspected
# Then:   the verify-docs job has allow_failure: false (or omits the key)
#         and does NOT have allow_failure: true
#
# The test explicitly requires allow_failure: false to be stated (not just absent)
# to force a deliberate opt-in by the implementer.
# =============================================================================
test_ci_docs_job_not_allow_failure() {
    local github_ci="${REPO_ROOT}/.github/workflows/ci.yml"
    local gitlab_ci="${REPO_ROOT}/.gitlab-ci.yml"
    local failed=0

    # --- GitHub Actions ---
    if [[ ! -f "${github_ci}" ]]; then
        echo "  FAIL: .github/workflows/ci.yml not found" >&2
        failed=1
    else
        # Must NOT have continue-on-error: true in the verify-docs job block
        # Extract the verify-docs job section (from "verify-docs:" to next job or EOF)
        local github_docs_block
        github_docs_block=$(awk '/^  verify-docs:/{found=1} found{print} /^  [a-z].*:$/ && !/^  verify-docs:/{if(found) exit}' "${github_ci}")

        if echo "${github_docs_block}" | grep -q "continue-on-error: true"; then
            echo "  FAIL: .github/workflows/ci.yml: verify-docs job still has 'continue-on-error: true'" >&2
            failed=1
        fi

        # Must explicitly have continue-on-error: false (AC3 requires explicit false)
        if ! echo "${github_docs_block}" | grep -q "continue-on-error: false"; then
            echo "  FAIL: .github/workflows/ci.yml: verify-docs job does not have 'continue-on-error: false'" >&2
            failed=1
        fi
    fi

    # --- GitLab CI ---
    if [[ ! -f "${gitlab_ci}" ]]; then
        echo "  FAIL: .gitlab-ci.yml not found" >&2
        failed=1
    else
        # Extract the verify-docs job section
        local gitlab_docs_block
        gitlab_docs_block=$(awk '/^verify-docs:/{found=1} found{print} /^[a-z].*:$/ && !/^verify-docs:/{if(found) exit}' "${gitlab_ci}")

        if echo "${gitlab_docs_block}" | grep -q "allow_failure: true"; then
            echo "  FAIL: .gitlab-ci.yml: verify-docs job still has 'allow_failure: true'" >&2
            failed=1
        fi

        # Must explicitly have allow_failure: false (AC3 requires explicit false)
        if ! echo "${gitlab_docs_block}" | grep -q "allow_failure: false"; then
            echo "  FAIL: .gitlab-ci.yml: verify-docs job does not have 'allow_failure: false'" >&2
            failed=1
        fi
    fi

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# TEST 4 — test_maintain_skill_exists  (AC4)
#
# Given:  Story 9-12 has been implemented
# When:   .claude/skills/bmad-maintain-arc42/ is inspected
# Then:   directory exists with customize.toml and a skill .md file; the skill:
#           - references "git diff" (delta detection)
#           - references "_bmad-output/planning-artifacts" (source scope)
#           - references "arc42-manifest.json" (manifest update)
# =============================================================================
test_maintain_skill_exists() {
    local skill_dir="${REPO_ROOT}/.claude/skills/bmad-maintain-arc42"
    local failed=0

    if [[ ! -d "${skill_dir}" ]]; then
        echo "  FAIL: .claude/skills/bmad-maintain-arc42/ directory does not exist" >&2
        return 1
    fi

    # customize.toml must be present
    if [[ ! -f "${skill_dir}/customize.toml" ]]; then
        echo "  FAIL: .claude/skills/bmad-maintain-arc42/customize.toml not found" >&2
        failed=1
    fi

    # At least one .md skill file must be present
    local skill_file_count
    skill_file_count=$(find "${skill_dir}" -maxdepth 1 -name "*.md" | wc -l | tr -d ' ')
    if [[ "${skill_file_count}" -eq 0 ]]; then
        echo "  FAIL: no .md skill file found in .claude/skills/bmad-maintain-arc42/" >&2
        failed=1
    else
        # Find the skill .md file for content checks
        local skill_md
        skill_md=$(find "${skill_dir}" -maxdepth 1 -name "*.md" | head -1)

        # Must use git diff for delta detection
        if ! grep -q "git diff" "${skill_md}"; then
            echo "  FAIL: bmad-maintain-arc42 skill does not reference 'git diff' (delta detection)" >&2
            failed=1
        fi

        # Must scope to planning-artifacts source directory
        if ! grep -q "_bmad-output/planning-artifacts" "${skill_md}"; then
            echo "  FAIL: bmad-maintain-arc42 skill does not reference '_bmad-output/planning-artifacts'" >&2
            failed=1
        fi

        # Must update the manifest
        if ! grep -q "arc42-manifest.json" "${skill_md}"; then
            echo "  FAIL: bmad-maintain-arc42 skill does not reference 'arc42-manifest.json'" >&2
            failed=1
        fi
    fi

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# TEST 5 — test_staleness_threshold_60_days  (AC5)
#
# Given:  scripts/verify-docs.sh has been updated for Story 9-12
# When:   verify-docs.sh is inspected
# Then:   the staleness threshold is 60 days (not 180):
#           - "180" must NOT appear as a threshold value in the freshness check
#           - "60" must appear as the threshold value
#           - The check is a CI-blocking error (not a warning-only)
# =============================================================================
test_staleness_threshold_60_days() {
    local verify_script="${REPO_ROOT}/scripts/verify-docs.sh"
    local failed=0

    if [[ ! -f "${verify_script}" ]]; then
        echo "  FAIL: scripts/verify-docs.sh not found" >&2
        return 1
    fi

    # 60-day threshold must be present in the timedelta context (not just any "60")
    if ! grep -q "timedelta(days=60)" "${verify_script}"; then
        echo "  FAIL: scripts/verify-docs.sh does not contain 'timedelta(days=60)' (staleness threshold)" >&2
        failed=1
    fi

    # 180-day threshold must be gone (replaced by 60)
    # Check specifically in the timedelta/threshold context (not just any "180")
    if grep -q "timedelta(days=180)" "${verify_script}"; then
        echo "  FAIL: scripts/verify-docs.sh still contains 'timedelta(days=180)' (must be replaced with 60)" >&2
        failed=1
    fi

    # The staleness check must be a blocking error, not a warning
    # verify-docs.sh currently returns 0 even on staleness (WARNING only).
    # After AC5, it must return non-zero on staleness.
    # Check: the staleness section must NOT contain "This is a warning, not a failure"
    if grep -q "warning, not a failure" "${verify_script}"; then
        echo "  FAIL: scripts/verify-docs.sh still contains staleness-as-warning text ('warning, not a failure')" >&2
        echo "        After AC5 the threshold must be a CI-blocking error" >&2
        failed=1
    fi

    # The comment "~2 epic cadence" must be present (per AC5 spec)
    if ! grep -q "epic cadence" "${verify_script}"; then
        echo "  FAIL: scripts/verify-docs.sh missing '# ~2 epic cadence' comment (required by AC5)" >&2
        failed=1
    fi

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# TEST 6 — test_verify_docs_passes_after_generate  (AC6)
#
# Given:  docs/.arc42-manifest.json exists with a recent generated_at timestamp
#         (simulating a fresh /bmad-generate-arc42 run)
# When:   scripts/verify-docs.sh is executed
# Then:   exits 0
#
# If the manifest does not exist or is stale, this test is skipped with a note
# (the full AC6 requires a real arc42 run; in red phase the script itself may
# still have the 180-day threshold, which would make it pass even with old docs —
# but once AC5 tightens to 60 days, old docs fail, so this test validates the
# post-generate state is clean).
#
# Implementation note: this test writes a temporary manifest with a fresh
# generated_at, runs verify-docs.sh, then restores the original.
# =============================================================================
test_verify_docs_passes_after_generate() {
    local verify_script="${REPO_ROOT}/scripts/verify-docs.sh"
    local manifest="${REPO_ROOT}/docs/.arc42-manifest.json"
    local failed=0

    if [[ ! -f "${verify_script}" ]]; then
        echo "  FAIL: scripts/verify-docs.sh not found" >&2
        return 1
    fi

    if [[ ! -x "${verify_script}" ]]; then
        echo "  FAIL: scripts/verify-docs.sh exists but is not executable" >&2
        return 1
    fi

    if [[ ! -f "${manifest}" ]]; then
        echo "  SKIP: docs/.arc42-manifest.json not found — run /bmad-generate-arc42 first" >&2
        echo "        (This test is a no-op until the manifest exists)" >&2
        # In red phase the manifest may not exist; report as FAIL so the test is truly red
        echo "  FAIL: manifest required for AC6 verification" >&2
        return 1
    fi

    # Patch: temporarily rewrite all generated_at timestamps to now (fresh run simulation)
    local now_ts
    now_ts=$(python3 -c "from datetime import datetime, timezone; print(datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'))")

    # File-based backup with trap so an early abort still restores the manifest.
    local manifest_backup_file="${manifest}.bak.$$"
    cp "${manifest}" "${manifest_backup_file}"
    # shellcheck disable=SC2064
    trap "mv -f '${manifest_backup_file}' '${manifest}' 2>/dev/null || true" EXIT INT TERM

    python3 - "${manifest}" "${now_ts}" <<'PYEOF'
import json, sys
path, now_ts = sys.argv[1], sys.argv[2]
with open(path) as fh:
    data = json.load(fh)
data["generated_at"] = now_ts
for key in data.get("files", {}):
    data["files"][key]["generated_at"] = now_ts
with open(path, "w") as fh:
    json.dump(data, fh, indent=2)
PYEOF

    # Run verify-docs.sh
    set +e
    "${verify_script}"
    local rc=$?
    set -e

    # Restore original manifest and clear the trap
    mv -f "${manifest_backup_file}" "${manifest}"
    trap - EXIT INT TERM

    if [[ "${rc}" -ne 0 ]]; then
        echo "  FAIL: scripts/verify-docs.sh exited ${rc} after fresh-manifest simulation (expected 0)" >&2
        failed=1
    fi

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# MAIN — Run all 6 acceptance tests sequentially
# =============================================================================
echo "======================================================================="
echo "test-9-12-acceptance.sh — Story 9-12 Acceptance Tests"
echo "arc42 Pipeline Gate Hardening"
echo "Repo root: ${REPO_ROOT}"
echo "======================================================================="
echo ""

run_test test_claude_md_has_arc42_gate
run_test test_pipeline_skill_has_doc_gate
run_test test_ci_docs_job_not_allow_failure
run_test test_maintain_skill_exists
run_test test_staleness_threshold_60_days
run_test test_verify_docs_passes_after_generate

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
