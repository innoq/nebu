#!/usr/bin/env bash
# =============================================================================
# verify-docs.sh
# CI check for Story 9-11 — arc42 docs presence, size, and manifest freshness.
#
# NOTE on DRY: the manifest JSON validation block is intentionally duplicated
# with scripts/test-docs-acceptance.sh — both scripts are zero-dependency,
# self-contained Bash + python3 entry points (CI vs. acceptance harness).
# A shared helper would couple them; the duplication is ~30 LOC and stable.
#
# Usage: bash scripts/verify-docs.sh
#
# Exit 0 if all checks pass, exit 1 if any FAIL.
# Prints per-check PASS/FAIL.
# =============================================================================

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ---------------------------------------------------------------------------
# Counters
# ---------------------------------------------------------------------------
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_NAMES=()

# ---------------------------------------------------------------------------
# run_test <name> — calls the function, records PASS/FAIL
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

# ---------------------------------------------------------------------------
# Required files list (must exist and be ≥200 bytes)
# ---------------------------------------------------------------------------
REQUIRED_FILES=(
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

# =============================================================================
# CHECK 1 — All required files exist and are non-empty (≥200 bytes)
# =============================================================================
check_files_exist_and_nonempty() {
    local failed=0
    local min_bytes=200

    for f in "${REQUIRED_FILES[@]}"; do
        local full_path="${REPO_ROOT}/${f}"
        if [[ ! -f "${full_path}" ]]; then
            echo "  FAIL: missing required file: ${f}" >&2
            failed=1
        else
            local size
            size=$(wc -c < "${full_path}" | tr -d ' ')
            if [[ "${size}" -lt "${min_bytes}" ]]; then
                echo "  FAIL: file too small (${size} bytes, need ≥${min_bytes}): ${f}" >&2
                failed=1
            fi
        fi
    done

    [[ "${failed}" -eq 0 ]] || return 1
    return 0
}

# =============================================================================
# CHECK 2 — .arc42-manifest.json is valid JSON
# =============================================================================
check_manifest_valid_json() {
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

print("Manifest valid: " + str(len(data.get("files", {}))) + " file entries")
PYEOF
)

    local output rc=0
    output=$(python3 -c "${py_script}" "${manifest}" 2>&1) || rc=$?

    if [[ "${rc}" -ne 0 ]]; then
        echo "  FAIL: manifest validation failed:" >&2
        echo "${output}" >&2
        return 1
    fi

    echo "  ${output}"
    return 0
}

# =============================================================================
# CHECK 3 — editable:false files have generated_at within last 60 days
#           (CI-blocking error — stale docs fail the pipeline)
#           ~2 epic cadence; increase if release cadence slows
# =============================================================================
check_manifest_freshness() {
    local manifest="${REPO_ROOT}/docs/.arc42-manifest.json"

    if [[ ! -f "${manifest}" ]]; then
        echo "  SKIP: manifest not found" >&2
        return 0
    fi

    local py_script
    py_script=$(cat <<'PYEOF'
import json, sys
from datetime import datetime, timezone, timedelta

path = sys.argv[1]
try:
    with open(path) as fh:
        data = json.load(fh)
except Exception as e:
    print("Cannot parse manifest: " + str(e), file=sys.stderr)
    sys.exit(0)  # Don't fail on parse error here (check_manifest_valid_json handles it)

now = datetime.now(timezone.utc)
# ~2 epic cadence; increase if release cadence slows
cutoff = now - timedelta(days=60)
stale = []

files = data.get("files", {})
for rel_path, entry in files.items():
    if entry.get("editable", True):
        continue  # skip editable files
    gen_at_str = entry.get("generated_at", "")
    if not gen_at_str:
        continue
    try:
        # Accept ISO 8601 with Z suffix
        gen_at = datetime.fromisoformat(gen_at_str.replace("Z", "+00:00"))
        if gen_at < cutoff:
            stale.append(rel_path + " (generated: " + gen_at_str + ")")
    except ValueError:
        pass  # unparseable timestamp — skip

if stale:
    print("ERROR: " + str(len(stale)) + " auto-generated file(s) are >60 days old:", file=sys.stderr)
    for s in stale:
        print("  " + s, file=sys.stderr)
    print("Run /bmad-generate-arc42 to refresh.", file=sys.stderr)
    sys.exit(1)
else:
    print("All auto-generated files are within 60 days.")
PYEOF
)

    local output rc=0
    output=$(python3 -c "${py_script}" "${manifest}" 2>&1) || rc=$?

    if [[ "${rc}" -ne 0 ]]; then
        echo "  FAIL: arc42 docs staleness check failed:" >&2
        echo "${output}" >&2
        return 1
    fi

    echo "  ${output}"
    return 0
}

# =============================================================================
# MAIN
# =============================================================================
echo "======================================================================="
echo "verify-docs.sh — Nebu arc42 Documentation CI Check"
echo "Repo root: ${REPO_ROOT}"
echo "======================================================================="
echo ""

run_test check_files_exist_and_nonempty
run_test check_manifest_valid_json
run_test check_manifest_freshness

echo "======================================================================="
echo "Results: ${TESTS_PASSED}/${TESTS_RUN} passed"

if [[ "${TESTS_FAILED}" -gt 0 ]]; then
    echo "FAILED checks (${TESTS_FAILED}):"
    for name in "${FAILED_NAMES[@]}"; do
        echo "  - ${name}"
    done
    echo "======================================================================="
    exit 1
else
    echo "ALL CHECKS PASSED"
    echo "======================================================================="
    exit 0
fi
