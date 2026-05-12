---
security_review: required
---

# Story 8.9: Release-Readiness-Gate — Full-Stack Test + Epic-weiter Security Review

Status: ready-for-dev

## Story

**As a** maintainer about to make the Nebu repository public,
**I want** a single master gate script that aggregates all verify tools from Stories 8.1–8.8 and produces an auditable pass/fail report,
**so that** I have one authoritative command to run before executing Story 8.10 and I cannot accidentally push a broken repository state to the public.

**Size:** M

---

## Background

### Strategic Context: The Last Technical Gate Before Public Visibility

Stories 8.1–8.8 each delivered a self-contained verify tool:

| Story | Script |
|---|---|
| 8.2 | `scripts/verify-readme-attribution.sh` |
| 8.3 | `scripts/verify-contributing.sh` |
| 8.4 | `scripts/verify-security-policy.sh` |
| 8.5 | `scripts/scan-secrets.sh --history` |
| 8.6 | `scripts/verify-ci-config.sh` |
| 8.7 | `scripts/verify-issue-pr-templates.sh` |
| 8.8 | `scripts/verify-repo-metadata.sh` |

Story 8.9 consolidates these seven tools into a single **master gate** (`scripts/release-readiness-gate.sh`) that is the final blocking check before Story 8.10 (Initial Public Push). If any sub-suite fails, `release-readiness-gate.sh` exits non-zero and the public push must not proceed.

This story operates entirely within the `feature/github-readiness` branch. The Epic 5 (compliance) and Epic 7 (admin UI) dependency noted in the sprint-status file is relevant for the _full production system_ passing `make test-integration` — that dependency is **not a hard gate for Story 8.9 itself**. Story 8.9 gates on the seven verify scripts of this branch; the integration-test step is explicitly soft (SKIP-on-error). Epic 5/7 completion is a gate for Story 8.10.

### Kassandra Reports Already Produced (Epic 8)

Three security reviews have been written during Epic 8:

| Report | Path | Classification |
|---|---|---|
| Story 8.1 | `_bmad-output/implementation-artifacts/security-reports/8-1-commit-history-rewrite-security-review.md` | CLEAN (2 MEDIUM, 2 LOW) |
| Story 8.5 | `_bmad-output/implementation-artifacts/security-reports/8-5-secret-scan-gate-security-review.md` | CLEAN (2 MEDIUM, 2 LOW) |
| Story 8.6 | `_bmad-output/implementation-artifacts/security-reports/8-6-dual-ci-security-review.md` | HIGH (H-1 fixed inline; 3 MEDIUM, 2 LOW, 3 INFO) |

Story 8.9 creates `_bmad-output/implementation-artifacts/security-reports/INDEX.md` to consolidate these into a single audit index. Story 8.10 will trigger the mandatory epic-end Kassandra run (SEC Gate 2).

### Parallel Branch Scope

This sprint (`feature/github-readiness`) is intentionally isolated from `main`. All references to `make test-integration` treat a failure as SKIP (soft), not FAIL (hard). The seven `scripts/verify-*.sh` and `scripts/scan-secrets.sh` tools are the hard gate.

### Lessons Learned from Stories 8.1–8.8

- **`REPO_ROOT=$(git rev-parse --show-toplevel)` + `cd "${REPO_ROOT}"`** at the top of every script; all paths relative (avoids false-negatives from markdownlint cwd issue in 8.2/8.3).
- **Python3 for emoji scan** — BSD grep Unicode false-positives in 8.2.
- **shellcheck SKIP fallback:** `if ! command -v shellcheck &>/dev/null; then echo "SKIP: shellcheck not found"; return 0; fi`.
- **`mktemp -d` + `trap 'rm -rf "${TMPDIR}"' EXIT`** for any temp-file sandbox operations.
- **`return 0`** (not `exit 0`) at end of each test function so the harness continues after a passing test.
- **Anchored grep patterns** where intent is anchored (lesson from 8.4 code review).
- **No emojis** in any file created by this story.
- **`--force-with-lease`** in runbook/checklist for force-push steps (Kassandra 8.1 finding).
- **JSON report** must be `.gitignore`'d — never committed (avoids accidental sensitive data in history).

---

## Acceptance Criteria

1. **`scripts/release-readiness-gate.sh` exists**, is executable (`chmod +x`), and passes `shellcheck --severity=error`.

2. **All seven verify scripts are invoked** — the gate script contains explicit calls to all seven:
   - `scripts/verify-readme-attribution.sh`
   - `scripts/verify-contributing.sh`
   - `scripts/verify-security-policy.sh`
   - `scripts/scan-secrets.sh --history`
   - `scripts/verify-ci-config.sh`
   - `scripts/verify-issue-pr-templates.sh`
   - `scripts/verify-repo-metadata.sh`

3. **Three modes are functional:**
   - `--all` (default when no flag given) — runs all seven sub-suites including secret history scan.
   - `--quick` — skips `scripts/scan-secrets.sh --history` (the time-consuming history scan); runs the six remaining verify scripts.
   - `--report` — runs `--all` and additionally writes a JSON report to `release-readiness-report.json`.

4. **Aggregated output** — the gate script prints per-sub-suite PASS/FAIL status and a final summary line showing total passed / total run and overall exit status (e.g., `GATE: 7/7 PASS` or `GATE: 5/7 FAIL`).

5. **`--report` mode writes valid JSON** to `release-readiness-report.json`. The JSON must include at minimum: `date`, `mode`, `overall_status` (`"PASS"` or `"FAIL"`), and a `suites` array with one entry per sub-script containing `name`, `status`, and `exit_code`.

6. **Pre-flight check** — before running any sub-suite, the gate script verifies that all seven verify scripts exist and are executable. If any script is missing or not executable, the gate fails immediately with a clear error message naming the missing script — it does not proceed to run partial checks.

7. **`_bmad-output/implementation-artifacts/security-reports/INDEX.md` exists** and contains an index of the three Kassandra reports for Epic 8.

8. **`scripts/PUBLIC_PUSH_CHECKLIST.md` exists** and documents the six manual steps required before and after the public push.

9. **`release-readiness-report.json` is listed in `.gitignore`** so it is never accidentally committed.

10. **`shellcheck --severity=error` clean** on `scripts/release-readiness-gate.sh` (verified by the acceptance test suite's Test 12).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

All tests are implemented as Bash test functions in `scripts/verify-release-readiness.sh`. The script follows the established pattern from `scripts/verify-ci-config.sh` (Story 8.6) and `scripts/verify-repo-metadata.sh` (Story 8.8):

- `REPO_ROOT=$(git rev-parse --show-toplevel)` + `cd "${REPO_ROOT}"` at the top; all paths relative.
- `run_test` helper: accepts test name; increments pass/fail counters; prints per-test `PASS` / `FAIL`.
- `TMPDIR=$(mktemp -d)` + `trap 'rm -rf "${TMPDIR}"' EXIT` for sandbox operations.
- No external test framework.
- Exit 0 on all-pass, exit 1 on any failure.

---

**Test 1 — `test_release_gate_script_exists_and_executable`**

- Given: repo root is accessible
- When: `[[ -f scripts/release-readiness-gate.sh ]] && [[ -x scripts/release-readiness-gate.sh ]]`
- Then: both conditions true (file exists, executable bit set)

---

**Test 2 — `test_release_gate_invokes_all_seven_scripts`**

- Given: `scripts/release-readiness-gate.sh` exists
- When: grep for each of the seven script references inside the gate script:
  - `grep -q "verify-readme-attribution.sh" scripts/release-readiness-gate.sh`
  - `grep -q "verify-contributing.sh" scripts/release-readiness-gate.sh`
  - `grep -q "verify-security-policy.sh" scripts/release-readiness-gate.sh`
  - `grep -q "scan-secrets.sh" scripts/release-readiness-gate.sh`
  - `grep -q "verify-ci-config.sh" scripts/release-readiness-gate.sh`
  - `grep -q "verify-issue-pr-templates.sh" scripts/release-readiness-gate.sh`
  - `grep -q "verify-repo-metadata.sh" scripts/release-readiness-gate.sh`
- Then: all seven greps return exit code 0

---

**Test 3 — `test_release_gate_three_modes`**

- Given: `scripts/release-readiness-gate.sh` exists
- When (3a): `grep -q "\-\-all" scripts/release-readiness-gate.sh`
- Then (3a): match found
- When (3b): `grep -q "\-\-quick" scripts/release-readiness-gate.sh`
- Then (3b): match found
- When (3c): `grep -q "\-\-report" scripts/release-readiness-gate.sh`
- Then (3c): match found

---

**Test 4 — `test_release_gate_preflight_aborts_when_subscript_missing`**

- Given: a sandbox directory (`mktemp -d`) containing a copy of `release-readiness-gate.sh` but with none of the seven sub-scripts present
- When: `bash release-readiness-gate.sh --all` is run from inside the sandbox
- Then: exit code is non-zero AND output contains the word `missing` or `not found` (case-insensitive)
- Note: the sandbox must be constructed so that the PATH and `REPO_ROOT` used inside the gate script resolve to the sandbox rather than the live repo. Use an env-var override or a minimal stub directory.

---

**Test 5 — `test_release_gate_full_run_against_real_repo`**

- Given: the live `feature/github-readiness` branch with all seven verify scripts in place
- When: `bash scripts/release-readiness-gate.sh --all` is run from `REPO_ROOT`
- Then: exit code is 0 AND output contains `GATE:` followed by `PASS`
- On failure: the test prints the full output of the gate run to stderr for diagnosis
- Note: this is the most important test — a non-zero exit here means at least one of the seven sub-suites is failing in the real repository

---

**Test 6 — `test_release_gate_report_mode_produces_valid_json`**

- Given: a temporary working directory with `REPO_ROOT` pointing to the real repo
- When: `bash scripts/release-readiness-gate.sh --report` is run (outputs `release-readiness-report.json` in the working directory, or `REPO_ROOT` — test must locate the output file correctly)
- Then (6a): `release-readiness-report.json` exists after the run
- Then (6b): `python3 -c "import json, sys; d=json.load(open('release-readiness-report.json')); assert 'date' in d and 'overall_status' in d and 'suites' in d"` exits 0
- Then (6c): `overall_status` value is either `"PASS"` or `"FAIL"` (string, not null)
- Then (6d): `suites` is a non-empty JSON array
- Note: clean up `release-readiness-report.json` after the test (the file is `.gitignore`'d but should not be left in the working tree)

---

**Test 7 — `test_security_reports_index_exists`**

- Given: repo root accessible
- When: `[[ -f _bmad-output/implementation-artifacts/security-reports/INDEX.md ]]`
- Then: true (file exists)

---

**Test 8 — `test_security_reports_index_lists_all_three`**

- Given: `_bmad-output/implementation-artifacts/security-reports/INDEX.md` exists
- When (8a): `grep -q "8-1" _bmad-output/implementation-artifacts/security-reports/INDEX.md`
- Then (8a): match found (Story 8.1 report referenced)
- When (8b): `grep -q "8-5" _bmad-output/implementation-artifacts/security-reports/INDEX.md`
- Then (8b): match found (Story 8.5 report referenced)
- When (8c): `grep -q "8-6" _bmad-output/implementation-artifacts/security-reports/INDEX.md`
- Then (8c): match found (Story 8.6 report referenced)

---

**Test 9 — `test_public_push_checklist_exists`**

- Given: repo root accessible
- When: `[[ -f scripts/PUBLIC_PUSH_CHECKLIST.md ]]`
- Then: true (file exists)

---

**Test 10 — `test_public_push_checklist_has_six_steps`**

- Given: `scripts/PUBLIC_PUSH_CHECKLIST.md` exists
- When: `grep -cE "^### Step [0-9]" scripts/PUBLIC_PUSH_CHECKLIST.md`
- Then: count equals 6
- Note: the grep pattern `^### Step \d` matches Markdown H3 headings of the form `### Step 1`, `### Step 2`, etc. The test asserts exactly 6 such headings exist.

---

**Test 11 — `test_release_readiness_report_in_gitignore`**

- Given: `.gitignore` exists in repo root
- When: `grep -q "release-readiness-report\.json" .gitignore`
- Then: match found

---

**Test 12 — `test_shellcheck_clean`**

- Given: `scripts/release-readiness-gate.sh` exists
- When: `shellcheck --severity=error scripts/release-readiness-gate.sh` (SKIP with exit 0 if `shellcheck` not in PATH)
- Then: exit code 0; no shellcheck errors
- Note: also run shellcheck on `scripts/verify-release-readiness.sh` itself as a bonus check (SKIP-on-missing follows same pattern)

---

**Persistenz-Strategie:** Not applicable — static files and shell scripts; no application state, no crash/restart test required.

---

## Risks and Mitigations

| Risk | Severity | Mitigation |
|---|---|---|
| **Sub-suite exit codes may be non-zero for reasons other than failures** (e.g., `scan-secrets.sh` returning `2` for "no gitleaks binary") | MEDIUM | Gate script must distinguish between "tool not found / SKIP" and "tool found, found violations". Document exit-code semantics per sub-script in Implementation Notes. |
| **`--report` mode writes `release-readiness-report.json` to REPO_ROOT** — if a developer commits it accidentally, the report (containing sub-suite output) becomes part of history | LOW | AC9 and Test 11 enforce `.gitignore`. Additionally, add a note to `PUBLIC_PUSH_CHECKLIST.md` warning not to commit the report. |
| **Pre-flight false positive** — if the gate script resolves `REPO_ROOT` incorrectly (e.g., run from a subdirectory or a symlinked path), the pre-flight check reports scripts as missing when they exist | LOW | Use `REPO_ROOT=$(git rev-parse --show-toplevel)` in the gate script itself; resolve all sub-script paths relative to that value. |
| **`scan-secrets.sh --history` slow on large history** (Story 8.5 established a gitleaks binary dependency) | LOW | `--quick` mode explicitly skips the history scan for faster iteration. Document in checklist that `--all` (not `--quick`) is required for the final pre-push run. |
| **False green: a sub-suite's verify script has a bug and exits 0 even when the thing it tests is broken** | MEDIUM | Test 5 (`test_release_gate_full_run_against_real_repo`) exercises the gate against the real live repository. If a sub-suite script is silently broken, this test will catch it indirectly when paired with the sub-suite's own unit tests. |
| **security-reports/INDEX.md becomes stale** after future stories add more reports | INFO | INDEX.md is a point-in-time audit index for Epic 8. Document this explicitly in the file header. The epic-end Kassandra run (Story 8.10) supersedes it. |

---

## Implementation Notes

### `scripts/release-readiness-gate.sh` — Design

```bash
#!/usr/bin/env bash
# release-readiness-gate.sh — Master release-readiness gate for Story 8.9.
# Aggregates all verify scripts from Stories 8.2–8.8.
# Usage: bash scripts/release-readiness-gate.sh [--all|--quick|--report]
#
# Exit codes:
#   0 — all enabled sub-suites passed
#   1 — one or more sub-suites failed, or pre-flight failed
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

MODE="${1:---all}"

SUITES=(
  "verify-readme-attribution.sh"
  "verify-contributing.sh"
  "verify-security-policy.sh"
  "verify-ci-config.sh"
  "verify-issue-pr-templates.sh"
  "verify-repo-metadata.sh"
)
SCAN_SECRETS="scripts/scan-secrets.sh"

# Pre-flight: verify all sub-scripts exist and are executable
# ... (fail fast if any missing)

# Run sub-suites depending on mode
# --quick skips scan-secrets.sh --history
# --report runs --all then writes release-readiness-report.json

# Aggregated output:
#   [PASS] verify-readme-attribution.sh
#   [FAIL] verify-contributing.sh
#   ...
#   GATE: 6/7 FAIL
```

Key design decisions:
- Each sub-suite is run in a subshell; its exit code captured. Output is shown live (not buffered) so failures are immediately visible.
- `--quick` mode sets a flag that skips the `scan-secrets.sh --history` invocation. The summary still shows `[SKIP] scan-secrets.sh --history`.
- `--report` mode collects per-suite results into a JSON structure and writes `"${REPO_ROOT}/release-readiness-report.json"` after all suites complete.
- The gate script does NOT use `set -e` for the sub-suite calls — it must capture non-zero exits without aborting. Use `|| true` + explicit exit-code capture (`sub_exit=$?`).

### `release-readiness-report.json` — Structure

```json
{
  "date": "2026-04-28",
  "mode": "all",
  "overall_status": "PASS",
  "suites": [
    {
      "name": "verify-readme-attribution.sh",
      "status": "PASS",
      "exit_code": 0
    },
    {
      "name": "scan-secrets.sh --history",
      "status": "PASS",
      "exit_code": 0
    }
  ]
}
```

The file is written to `"${REPO_ROOT}/release-readiness-report.json"` (repo root, not `scripts/`). It is `.gitignore`'d.

### `scripts/PUBLIC_PUSH_CHECKLIST.md` — Six Steps

The checklist is a Markdown document with six `### Step N` headings, following the pattern of `scripts/REPO_METADATA_RUNBOOK.md` (Story 8.8) and `scripts/REWRITE_HISTORY_RUNBOOK.md` (Story 8.1).

Required steps:

1. Run `bash scripts/release-readiness-gate.sh --all` — must exit 0.
2. Execute `scripts/rewrite-coauthored-trailer.sh --history --run` (Story 8.1 tooling) — manual maintainer action.
3. Run `scripts/rewrite-coauthored-trailer.sh --verify` to confirm the rewrite.
4. Force-push to BOTH remotes with `--force-with-lease` (GitHub + opencode.de).
5. Run `scripts/setup-repo-metadata.sh --all` (Story 8.8) to apply topics and description on both platforms.
6. Configure branch protection on both platforms (Story 8.10).

Notes on Step 4: use `git push --force-with-lease=main:$(git rev-parse origin/main) origin main` — never bare `--force` (Kassandra 8.1 MEDIUM-2 finding).

### `_bmad-output/implementation-artifacts/security-reports/INDEX.md` — Structure

The INDEX.md is a point-in-time audit index for Epic 8 Kassandra reports. It lists each report with its path, overall classification, and open finding counts.

Columns: `Story`, `Report File`, `Classification`, `CRITICAL`, `HIGH`, `MEDIUM`, `LOW`, `Open MEDIUMs/LOWs (status)`.

It explicitly states in its header that:
- This index covers only SEC Gate 1 per-story reviews.
- The mandatory SEC Gate 2 epic-end review is executed in Story 8.10.
- Open MEDIUMs/LOWs are either fixed inline (noted in sprint-status) or deferred with explicit justification.

---

## Files to Create / Modify

| File | Action | Note |
|---|---|---|
| `scripts/release-readiness-gate.sh` | CREATE | Executable; `--all` / `--quick` / `--report` modes; pre-flight; aggregated output |
| `scripts/PUBLIC_PUSH_CHECKLIST.md` | CREATE | 6 steps (`### Step N` headings); no emojis |
| `scripts/verify-release-readiness.sh` | CREATE | 12 acceptance tests; exit 0 on all-pass |
| `_bmad-output/implementation-artifacts/security-reports/INDEX.md` | CREATE | Lists 3 Kassandra reports for Epic 8; point-in-time audit index |
| `.gitignore` | MODIFY | Add `release-readiness-report.json` |
| `_bmad-output/implementation-artifacts/sprint-status-epic-8.yaml` | UPDATE | `8-9-...` → `done` after implementation complete |

**Explicitly NOT part of this story:**

- Any changes to the seven sub-verify scripts themselves (they are owned by their respective stories).
- Actual execution of `scripts/rewrite-coauthored-trailer.sh --run` (owned by Story 8.1 runbook; executed manually by maintainer before public push).
- Actual execution of `scripts/setup-repo-metadata.sh` (gated on Story 8.10).
- The epic-end Kassandra security review (SEC Gate 2) — that is Story 8.10's gate.
- Any changes to application source code (`gateway/`, `core/`).
- `make test-integration` is soft (SKIP-on-failure); no implementation effort for integration test infra.

---

## Context: Epic 8

Epic 8 prepares Nebu for public release on `github.com/innoq/nebu` and `gitlab.opencode.de/nebu/nebu-server`. Story 8.9 is the **last technical implementation story** before the actual public push. After this story is `done`, the repository has:

- A single master gate (`release-readiness-gate.sh`) that aggregates all seven verify tools into one executable pre-push check.
- A documented six-step public-push checklist (`PUBLIC_PUSH_CHECKLIST.md`) that any maintainer can follow.
- An audit index of all Epic 8 Kassandra reports (`security-reports/INDEX.md`).
- `release-readiness-report.json` correctly `.gitignore`'d.

Story 8.10 (Initial Public Push) is the only remaining story. It consumes the output of Story 8.9 directly: the checklist's Step 1 is `bash scripts/release-readiness-gate.sh --all` and Story 8.10 is gated on that command exiting 0.

Dependencies:

- **Stories 8.2–8.8** (all `done`) — the seven verify scripts must exist and be executable.
- **Story 8.5** (`scan-secrets.sh`) — the `--history` mode invoked by `--all`.
- **Story 8.10** (Initial Public Push) — depends on Story 8.9 being `done`; do not start 8.10 until `release-readiness-gate.sh --all` exits 0.
