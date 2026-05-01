#!/usr/bin/env bash
# =============================================================================
# test-docs-acceptance.sh
# Red-phase acceptance tests for Story 9.1 — arc42 docs generation from BMAD
# artifacts.
#
# Usage: bash scripts/test-docs-acceptance.sh
#
# All 9 tests FAIL before implementation (red phase).
# All 9 tests PASS after implementation (green phase).
# Exit 0 if all pass, exit 1 if any fail.
#
# Pure Bash + python3 + standard POSIX tools — no external deps.
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
# TEST 1 — test_all_arc42_files_exist  (AC1, AC6)
#
# Given:  /bmad-generate-arc42 has been run
# When:   scripts/verify-docs.sh executes
# Then:   exits 0; all required files present and non-empty
#
# Checks:
#   - scripts/verify-docs.sh exists and is executable
#   - Running it exits 0
#   - All required docs/ files exist and are non-empty (≥200 bytes)
# =============================================================================
test_all_arc42_files_exist() {
    local verify_script="${REPO_ROOT}/scripts/verify-docs.sh"
    local failed=0

    # Check verify-docs.sh exists and is executable
    if [[ ! -f "${verify_script}" ]]; then
        echo "  FAIL: scripts/verify-docs.sh does not exist" >&2
        failed=1
    elif [[ ! -x "${verify_script}" ]]; then
        echo "  FAIL: scripts/verify-docs.sh exists but is not executable" >&2
        failed=1
    fi

    # Required files (all must be non-empty ≥200 bytes)
    local required_files=(
        "docs/.arc42-manifest.json"
        "docs/architecture/README.md"
        "docs/architecture/02-constraints.md"
        "docs/architecture/03-context.md"
        "docs/architecture/04-solution-strategy.md"
        "docs/architecture/05-building-blocks.md"
        "docs/architecture/06-runtime.md"
        "docs/architecture/07-deployment.md"
        "docs/architecture/08-concepts.md"
        "docs/architecture/09-decisions.md"
        "docs/architecture/10-quality.md"
        "docs/architecture/11-risks.md"
        "docs/architecture/12-glossary.md"
        "docs/architecture/adr/ADR-001-elixir-otp.md"
        "docs/architecture/adr/ADR-002-no-redis-nats.md"
        "docs/architecture/adr/ADR-003-content-hash-event-id.md"
        "docs/architecture/adr/ADR-004-horde-registry.md"
        "docs/architecture/adr/ADR-005-grpc-eventbus.md"
        "docs/architecture/adr/ADR-006-message-buffer-drain.md"
        "docs/architecture/adr/ADR-007-ed25519-x25519-keypairs.md"
        "docs/architecture/adr/ADR-008-node-registration-psk.md"
        "docs/architecture/adr/ADR-009-openapi-spec-first.md"
        "docs/architecture/adr/ADR-010-fts-strategy.md"
        "docs/architecture/adr/ADR-011-managed-e2ee-key-escrow.md"
        "docs/getting-started.md"
        "docs/matrix-api-scope.md"
        "docs/roadmap.md"
    )

    local min_bytes=200
    local f size
    for f in "${required_files[@]}"; do
        local full_path="${REPO_ROOT}/${f}"
        if [[ ! -f "${full_path}" ]]; then
            echo "  FAIL: required file missing: ${f}" >&2
            failed=1
        else
            size=$(wc -c < "${full_path}" | tr -d ' ')
            if [[ "${size}" -lt "${min_bytes}" ]]; then
                echo "  FAIL: file too small (${size} bytes, need ≥${min_bytes}): ${f}" >&2
                failed=1
            fi
        fi
    done

    if [[ "${failed}" -eq 1 ]]; then
        return 1
    fi

    # Run verify-docs.sh; must exit 0
    set +e
    "${verify_script}"
    local rc=$?
    set -e
    if [[ "${rc}" -ne 0 ]]; then
        echo "  FAIL: scripts/verify-docs.sh exited ${rc} (expected 0)" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 2 — test_manifest_is_valid_json  (AC5)
#
# Given:  skill ran successfully
# When:   python3 parses docs/.arc42-manifest.json
# Then:   exits 0 (valid JSON) AND contains required keys:
#           generated_at, source_artifacts (array), files (object)
# =============================================================================
test_manifest_is_valid_json() {
    local manifest="${REPO_ROOT}/docs/.arc42-manifest.json"

    if [[ ! -f "${manifest}" ]]; then
        echo "  FAIL: docs/.arc42-manifest.json does not exist" >&2
        return 1
    fi

    local py_script
    py_script=$(cat <<'PYEOF'
import json, sys

path = sys.argv[1]
try:
    with open(path) as fh:
        data = json.load(fh)
except json.JSONDecodeError as e:
    print("JSON parse error: " + str(e), file=sys.stderr)
    sys.exit(1)

errors = []

if "generated_at" not in data:
    errors.append("missing key: generated_at")

if "source_artifacts" not in data:
    errors.append("missing key: source_artifacts")
elif not isinstance(data["source_artifacts"], list):
    errors.append("source_artifacts must be a JSON array")

if "files" not in data:
    errors.append("missing key: files")
elif not isinstance(data["files"], dict):
    errors.append("files must be a JSON object")
else:
    for rel_path, entry in data["files"].items():
        if "editable" not in entry:
            errors.append("files." + rel_path + ": missing 'editable' key")
        elif not isinstance(entry["editable"], bool):
            errors.append("files." + rel_path + ": 'editable' must be a boolean")
        if "generated_at" not in entry:
            errors.append("files." + rel_path + ": missing 'generated_at' key")

if errors:
    for e in errors:
        print("  " + e, file=sys.stderr)
    sys.exit(1)

print("Manifest valid")
PYEOF
)

    local py_output
    local py_rc=0
    py_output=$(python3 -c "${py_script}" "${manifest}" 2>&1) || py_rc=$?

    if [[ "${py_rc}" -ne 0 ]]; then
        echo "  FAIL: docs/.arc42-manifest.json validation failed:" >&2
        echo "${py_output}" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 3 — test_readme_coming_soon_links_removed  (AC2)
#
# Given:  story is complete
# When:   grep searches README.md for "coming soon"
# Then:   count is 0 (no remaining placeholders)
# =============================================================================
test_readme_coming_soon_links_removed() {
    local readme="${REPO_ROOT}/README.md"

    if [[ ! -f "${readme}" ]]; then
        echo "  FAIL: README.md not found at ${readme}" >&2
        return 1
    fi

    local count
    count=$(grep -ci "coming soon" "${readme}" || true)

    if [[ "${count}" -gt 0 ]]; then
        echo "  FAIL: found ${count} occurrence(s) of 'coming soon' in README.md" >&2
        grep -in "coming soon" "${readme}" | while IFS= read -r line; do
            echo "    ${line}" >&2
        done
        return 1
    fi

    # Additionally verify the five target files that were placeholder-linked
    # now actually exist
    local linked_files=(
        "docs/getting-started.md"
        "docs/architecture/README.md"
        "docs/architecture/adr/ADR-001-elixir-otp.md"
        "docs/matrix-api-scope.md"
        "docs/roadmap.md"
    )
    local failed=0
    local f
    for f in "${linked_files[@]}"; do
        if [[ ! -f "${REPO_ROOT}/${f}" ]]; then
            echo "  FAIL: README formerly-placeholder file does not exist: ${f}" >&2
            failed=1
        fi
    done

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# TEST 4 — test_adr_files_have_required_sections  (AC4)
#
# Given:  skill ran
# When:   each ADR-001..ADR-009 file is read
# Then:   each contains "## Status", "## Context", "## Decision", "## Consequences"
#
# ADR-010 and ADR-011 are placeholders — checked for existence + "Proposed" only.
# =============================================================================
test_adr_files_have_required_sections() {
    local adr_dir="${REPO_ROOT}/docs/architecture/adr"
    local failed=0

    # Indexed arrays avoid octal-interpretation issues with leading-zero numbers
    local adr_filenames=(
        "ADR-001-elixir-otp.md"
        "ADR-002-no-redis-nats.md"
        "ADR-003-content-hash-event-id.md"
        "ADR-004-horde-registry.md"
        "ADR-005-grpc-eventbus.md"
        "ADR-006-message-buffer-drain.md"
        "ADR-007-ed25519-x25519-keypairs.md"
        "ADR-008-node-registration-psk.md"
        "ADR-009-openapi-spec-first.md"
    )

    local required_sections=("## Status" "## Context" "## Decision" "## Consequences")

    local filename full_path section
    for filename in "${adr_filenames[@]}"; do
        full_path="${adr_dir}/${filename}"

        if [[ ! -f "${full_path}" ]]; then
            echo "  FAIL: ADR file missing: docs/architecture/adr/${filename}" >&2
            failed=1
            continue
        fi

        for section in "${required_sections[@]}"; do
            if ! grep -qF "${section}" "${full_path}"; then
                echo "  FAIL: ${filename}: missing section '${section}'" >&2
                failed=1
            fi
        done
    done

    # ADR-010 and ADR-011: placeholders — must exist and contain "Proposed"
    local placeholder_adrs=(
        "ADR-010-fts-strategy.md"
        "ADR-011-managed-e2ee-key-escrow.md"
    )
    local ph
    for ph in "${placeholder_adrs[@]}"; do
        local ph_path="${adr_dir}/${ph}"
        if [[ ! -f "${ph_path}" ]]; then
            echo "  FAIL: placeholder ADR missing: docs/architecture/adr/${ph}" >&2
            failed=1
        elif ! grep -qi "Proposed" "${ph_path}"; then
            echo "  FAIL: ${ph}: does not contain 'Proposed' status" >&2
            failed=1
        fi
    done

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# TEST 5 — test_verify_docs_ci_job_registered  (AC7)
#
# Given:  story is complete
# When:   .github/workflows/ci.yml and .gitlab-ci.yml are inspected
# Then:   both contain "verify-docs" (referencing the CI docs job)
# =============================================================================
test_verify_docs_ci_job_registered() {
    local github_ci="${REPO_ROOT}/.github/workflows/ci.yml"
    local gitlab_ci="${REPO_ROOT}/.gitlab-ci.yml"
    local failed=0

    if [[ ! -f "${github_ci}" ]]; then
        echo "  FAIL: .github/workflows/ci.yml does not exist" >&2
        failed=1
    elif ! grep -q "verify-docs" "${github_ci}"; then
        echo "  FAIL: 'verify-docs' not found in .github/workflows/ci.yml" >&2
        failed=1
    fi

    if [[ ! -f "${gitlab_ci}" ]]; then
        echo "  FAIL: .gitlab-ci.yml does not exist" >&2
        failed=1
    elif ! grep -q "verify-docs" "${gitlab_ci}"; then
        echo "  FAIL: 'verify-docs' not found in .gitlab-ci.yml" >&2
        failed=1
    fi

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# TEST 6 — test_skill_dir_exists  (AC8)
#
# Given:  story is complete
# When:   .claude/skills/bmad-generate-arc42/ is inspected
# Then:   directory exists with both customize.toml and a skill file
# =============================================================================
test_skill_dir_exists() {
    local skill_dir="${REPO_ROOT}/.claude/skills/bmad-generate-arc42"
    local failed=0

    if [[ ! -d "${skill_dir}" ]]; then
        echo "  FAIL: .claude/skills/bmad-generate-arc42 directory does not exist" >&2
        return 1
    fi

    if [[ ! -f "${skill_dir}/customize.toml" ]]; then
        echo "  FAIL: .claude/skills/bmad-generate-arc42/customize.toml not found" >&2
        failed=1
    fi

    # Accept any .md file as the skill file (skill.md is the canonical name)
    local skill_file_count
    skill_file_count=$(find "${skill_dir}" -maxdepth 1 -name "*.md" | wc -l | tr -d ' ')
    if [[ "${skill_file_count}" -eq 0 ]]; then
        echo "  FAIL: no .md skill file found in .claude/skills/bmad-generate-arc42/" >&2
        failed=1
    fi

    # customize.toml must contain arc42_section_map
    if [[ -f "${skill_dir}/customize.toml" ]]; then
        if ! grep -q "arc42_section_map" "${skill_dir}/customize.toml"; then
            echo "  FAIL: customize.toml does not define arc42_section_map" >&2
            failed=1
        fi
    fi

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# TEST 7 — test_arc42_section_completeness  (AC3)
#
# Given:  skill ran
# When:   each arc42 section file (README.md + 02..12) is read
# Then:   each contains a level-1 heading with section number,
#         a _Source: footer line, and is at least 800 bytes
# =============================================================================
test_arc42_section_completeness() {
    local arch_dir="${REPO_ROOT}/docs/architecture"
    local failed=0

    local section_files=(
        "README.md"
        "02-constraints.md"
        "03-context.md"
        "04-solution-strategy.md"
        "05-building-blocks.md"
        "06-runtime.md"
        "07-deployment.md"
        "08-concepts.md"
        "09-decisions.md"
        "10-quality.md"
        "11-risks.md"
        "12-glossary.md"
    )

    local f full_path
    for f in "${section_files[@]}"; do
        full_path="${arch_dir}/${f}"
        if [[ ! -f "${full_path}" ]]; then
            echo "  FAIL: arc42 section file missing: docs/architecture/${f}" >&2
            failed=1
            continue
        fi

        # level-1 heading with section number (e.g. "# 1 " or "# 02 ")
        if ! grep -qE "^# [0-9]" "${full_path}"; then
            echo "  FAIL: ${f}: no level-1 heading with section number (expected '# N...')" >&2
            failed=1
        fi

        # _Source: footer line (traceability back to BMAD artifacts)
        if ! grep -q "_Source:" "${full_path}"; then
            echo "  FAIL: ${f}: missing '_Source:' footer line" >&2
            failed=1
        fi

        # substantive content — require ≥800 bytes (filters out stub-only files)
        local size
        size=$(wc -c < "${full_path}" | tr -d ' ')
        if [[ "${size}" -lt 800 ]]; then
            echo "  FAIL: ${f}: file too small (${size} bytes, need ≥800 for substantive content)" >&2
            failed=1
        fi
    done

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# TEST 8 — test_getting_started_complete  (AC9)
#
# Given:  story is complete
# When:   docs/getting-started.md is read
# Then:   contains all mandatory sub-sections per AC9:
#           Prerequisites, make setup, make dev, /etc/hosts,
#           Element (client), Troubleshoot
# =============================================================================
test_getting_started_complete() {
    local gs="${REPO_ROOT}/docs/getting-started.md"

    if [[ ! -f "${gs}" ]]; then
        echo "  FAIL: docs/getting-started.md does not exist" >&2
        return 1
    fi

    local failed=0

    local keywords=("Prerequisites" "make setup" "make dev" "/etc/hosts" "Element" "Troubleshoot")
    local kw
    for kw in "${keywords[@]}"; do
        if ! grep -q "${kw}" "${gs}"; then
            echo "  FAIL: docs/getting-started.md: missing keyword '${kw}'" >&2
            failed=1
        fi
    done

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# TEST 9 — test_matrix_api_scope_complete  (AC10)
#
# Given:  story is complete
# When:   docs/matrix-api-scope.md is read
# Then:   contains endpoint table (/_matrix/ paths), Intentionally Excluded
#         section, and Current Stubs section
# =============================================================================
test_matrix_api_scope_complete() {
    local scope="${REPO_ROOT}/docs/matrix-api-scope.md"

    if [[ ! -f "${scope}" ]]; then
        echo "  FAIL: docs/matrix-api-scope.md does not exist" >&2
        return 1
    fi

    local failed=0

    # endpoint table — at least one /_matrix/ path
    if ! grep -q "/_matrix/" "${scope}"; then
        echo "  FAIL: docs/matrix-api-scope.md: no /_matrix/ endpoint entries found" >&2
        failed=1
    fi

    # stub markers (🔶 or the word Stub)
    if ! grep -qE "🔶|Stub" "${scope}"; then
        echo "  FAIL: docs/matrix-api-scope.md: no stub markers (🔶 or 'Stub') found" >&2
        failed=1
    fi

    # intentionally excluded section
    if ! grep -qi "Intentionally [Ee]xcluded" "${scope}"; then
        echo "  FAIL: docs/matrix-api-scope.md: missing 'Intentionally Excluded' section" >&2
        failed=1
    fi

    # current stubs / E2EE stubs section (matches "## Current Stubs", "## Current E2EE Stubs", etc.)
    if ! grep -qiE "^##.*[Ss]tubs" "${scope}"; then
        echo "  FAIL: docs/matrix-api-scope.md: missing 'Current Stubs' section" >&2
        failed=1
    fi

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# MAIN — Run all 9 acceptance tests sequentially
# =============================================================================
echo "======================================================================="
echo "test-docs-acceptance.sh — Story 9.1 Acceptance Tests"
echo "Repo root: ${REPO_ROOT}"
echo "======================================================================="
echo ""

run_test test_all_arc42_files_exist
run_test test_manifest_is_valid_json
run_test test_readme_coming_soon_links_removed
run_test test_adr_files_have_required_sections
run_test test_verify_docs_ci_job_registered
run_test test_skill_dir_exists
run_test test_arc42_section_completeness
run_test test_getting_started_complete
run_test test_matrix_api_scope_complete

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
