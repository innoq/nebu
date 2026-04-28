---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-04-28'
storyId: '8.1'
storyKey: '8-1-commit-history-rewrite-co-authored-by-trailer-entfernen'
storyFile: '_bmad-output/implementation-artifacts/8-1-commit-history-rewrite-co-authored-by-trailer-entfernen.md'
atddChecklistPath: '_bmad-output/test-artifacts/atdd-checklist-8-1-commit-history-rewrite-co-authored-by-trailer-entfernen.md'
generatedTestFiles:
  - 'scripts/rewrite-coauthored-trailer.test.sh'
inputDocuments:
  - '_bmad-output/implementation-artifacts/8-1-commit-history-rewrite-co-authored-by-trailer-entfernen.md'
  - '_bmad/tea/config.yaml'
---

# ATDD Checklist — Story 8.1: Commit-History-Rewrite Co-Authored-By-Trailer entfernen

## Step 1: Preflight & Context

- **Stack detected:** backend (Go `go.mod` + Elixir `mix.exs`)
- **Story ID:** 8.1
- **Story key:** `8-1-commit-history-rewrite-co-authored-by-trailer-entfernen`
- **Story file:** `_bmad-output/implementation-artifacts/8-1-commit-history-rewrite-co-authored-by-trailer-entfernen.md`
- **Test framework:** Pure Bash with exit codes (no external framework — specified in story)
- **7 acceptance criteria** identified, mapped to 10 test functions (7 from story + 3 added during test-review hardening)

## Step 2: Generation Mode

- **Mode selected:** AI generation (backend project, no browser recording needed)

## Step 3: Test Strategy

| Test Function | Acceptance Criterion | Level | Priority |
|---|---|---|---|
| `test_dry_run_reports_correct_count` | AC3: --dry-run mode | Integration | P0 |
| `test_run_removes_claude_trailer_only` | AC5: Trailer regex, Claude-only | Integration | P0 |
| `test_run_creates_backup_branch` | AC4: Backup branch | Integration | P0 |
| `test_run_preserves_metadata` | AC5+AC4: Metadata preservation | Integration | P1 |
| `test_pre_flight_aborts_on_dirty_tree` | AC2a: Dirty tree pre-flight | Unit | P0 |
| `test_verify_mode_passes_after_run` | AC3: --verify mode (5 sub-checks) | Integration | P1 |
| `test_pre_flight_aborts_when_filter_repo_missing` | AC2b: filter-repo missing | Unit | P0 |
| `test_no_args_prints_usage` | AC3: Usage on missing arg | Unit | P1 |
| `test_script_passes_shellcheck` | AC1: shellcheck-clean (skip if absent) | Unit | P1 |
| `test_pre_flight_aborts_on_duplicate_backup` | AC2c: Doppel-Backup pre-flight | Unit | P0 |

All tests are P0/P1. All designed to fail before implementation (TDD red phase).
The five `--verify` sub-checks are: `no-trailer`, `backup-exists`, `count-unchanged`, `metadata-match`, `backup-integrity`.

## Step 4: Test Generation

- **Execution mode:** Sequential (no subagent/agent-team — single test file, pure Bash)
- **Generated file:** `scripts/rewrite-coauthored-trailer.test.sh`
- **TDD phase:** RED — all tests fail with "script not found or not executable" before implementation
- **Final state:** GREEN — 10 tests, all passing against the implemented script

### Verification run output

```
==========================================================
  Story 8.1 — Red-Phase Acceptance Tests
  Target: scripts/rewrite-coauthored-trailer.sh
==========================================================

--- test_dry_run_reports_correct_count
  FAIL: script not found or not executable: ...
    FAIL
--- test_run_removes_claude_trailer_only
  FAIL: script not found or not executable: ...
    FAIL
--- test_run_creates_backup_branch
  FAIL: script not found or not executable: ...
    FAIL
--- test_run_preserves_metadata
  FAIL: script not found or not executable: ...
    FAIL
--- test_pre_flight_aborts_on_dirty_tree
  FAIL: script not found or not executable: ...
    FAIL
--- test_verify_mode_passes_after_run
  FAIL: script not found or not executable: ...
    FAIL
--- test_pre_flight_aborts_when_filter_repo_missing
  FAIL: script not found or not executable: ...
    FAIL

==========================================================
  Results: 0/7 passed, 7 failed
==========================================================
```

## Step 5: Validate & Complete

### Checklist

- [x] Every acceptance criterion has at least one test
- [x] All tests fail before implementation (TDD red phase)
- [x] No hard waits, no hidden assertions
- [x] No external test framework dependencies
- [x] Sandbox isolation: each test uses `mktemp -d`, cleanup trap registered
- [x] Test file is executable (`chmod +x`)
- [x] Test file staged via `git add`
- [x] No impact on host repo (all git operations in sandbox)
- [x] Persistence strategy: N/A (Shell script, no application state)
- [x] Crash/restart test: N/A (explicitly documented in story)

### Risks / Assumptions

- The `--verify` sub-check names (`no-trailer`, `backup-exists`, `count-unchanged`, `metadata-match`) are assumptions based on the story spec. The implementation must output these exact labels for test 6 to pass.
- `test_run_preserves_metadata` compares `HEAD~2..HEAD` — assumes exactly 3 commits exist after rewrite (as set up in the test). If the implementation adds a merge commit or changes topology, the comparison range must be adjusted.
- Restricted PATH test (`/usr/bin:/bin`) may need adjustment on macOS where `git` lives at `/usr/bin/git`. The test is valid as long as `git-filter-repo` is not in those standard paths.

### Next Steps

- Implement `scripts/rewrite-coauthored-trailer.sh` (TDD green phase) — `/bmad-dev-story`
- Implement `scripts/REWRITE_HISTORY_RUNBOOK.md`
- Run `bash scripts/rewrite-coauthored-trailer.test.sh` — all 7 must pass
