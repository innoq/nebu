#!/usr/bin/env bash
# =============================================================================
# rewrite-coauthored-trailer.test.sh
# Acceptance tests for Story 8.1
# Test framework: pure Bash with exit codes (no external dependencies)
#
# Usage: bash scripts/rewrite-coauthored-trailer.test.sh
#
# Tests run in isolated mktemp sandbox repos — zero impact on host repo.
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUBJECT_SCRIPT="${SCRIPT_DIR}/rewrite-coauthored-trailer.sh"

# ---------------------------------------------------------------------------
# Global counters
# ---------------------------------------------------------------------------
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_NAMES=()

# ---------------------------------------------------------------------------
# Cleanup registry: each test registers its sandbox dir here so the
# EXIT trap can wipe everything even if the test crashes mid-way.
# ---------------------------------------------------------------------------
declare -a _SANDBOX_DIRS=()

cleanup_all() {
    for d in "${_SANDBOX_DIRS[@]:-}"; do
        [[ -d "$d" ]] && rm -rf "$d"
    done
}
trap cleanup_all EXIT

# ---------------------------------------------------------------------------
# Helper: setup_sandbox_repo
#
# Creates an isolated git repo in a temp directory.
# The caller receives the repo path via the global SANDBOX variable.
#
# Arguments (all optional, passed as key=value pairs):
#   commits — associative-array-style spec is complex; instead callers build
#             their own commits after calling setup_sandbox_repo.
# ---------------------------------------------------------------------------
setup_sandbox_repo() {
    local sandbox
    sandbox="$(mktemp -d)"
    _SANDBOX_DIRS+=("$sandbox")

    git -C "$sandbox" init -q
    git -C "$sandbox" config user.name  "Test User"
    git -C "$sandbox" config user.email "test@example.com"

    # Return path to caller via global
    SANDBOX="$sandbox"
}

# ---------------------------------------------------------------------------
# Helper: assert_exit_zero
# ---------------------------------------------------------------------------
assert_exit_zero() {
    local code="$1" label="$2"
    if [[ "$code" -ne 0 ]]; then
        echo "  FAIL [$label]: expected exit 0, got $code" >&2
        return 1
    fi
}

# ---------------------------------------------------------------------------
# Helper: assert_exit_nonzero
# ---------------------------------------------------------------------------
assert_exit_nonzero() {
    local code="$1" label="$2"
    if [[ "$code" -eq 0 ]]; then
        echo "  FAIL [$label]: expected non-zero exit, got 0" >&2
        return 1
    fi
}

# ---------------------------------------------------------------------------
# Helper: assert_output_contains
# ---------------------------------------------------------------------------
assert_output_contains() {
    local output="$1" needle="$2" label="$3"
    if ! grep -qF "$needle" <<<"$output"; then
        echo "  FAIL [$label]: expected output to contain '${needle}'" >&2
        echo "  Actual output: ${output}" >&2
        return 1
    fi
}

# ---------------------------------------------------------------------------
# Test runner helpers
# ---------------------------------------------------------------------------
run_test() {
    local name="$1"
    TESTS_RUN=$(( TESTS_RUN + 1 ))
    echo "--- ${name}"
    if "$name"; then
        echo "    PASS"
        TESTS_PASSED=$(( TESTS_PASSED + 1 ))
    else
        echo "    FAIL"
        TESTS_FAILED=$(( TESTS_FAILED + 1 ))
        FAILED_NAMES+=("$name")
    fi
}

# =============================================================================
# TEST 1 — test_dry_run_reports_correct_count
#
# AC: Story 8.1 Acceptance Test #1
# Given: Sandbox repo with 5 commits, 3 of which have Co-Authored-By: Claude trailer
# When:  scripts/rewrite-coauthored-trailer.sh --dry-run runs in sandbox repo
# Then:  Exit 0; stdout contains "would rewrite 3 commit(s)"; HEAD SHA unchanged
# =============================================================================
test_dry_run_reports_correct_count() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    local orig_sha
    # Commit 1 — no trailer
    git -C "$SANDBOX" commit --allow-empty -q \
        -m "feat: plain commit one"
    # Commit 2 — Claude trailer
    git -C "$SANDBOX" commit --allow-empty -q \
        -m "feat: commit with claude trailer one

Co-Authored-By: Claude <noreply@anthropic.com>"
    # Commit 3 — human co-author
    git -C "$SANDBOX" commit --allow-empty -q \
        -m "feat: human co-author commit

Co-Authored-By: Alice Human <alice@example.com>"
    # Commit 4 — Claude trailer (variant model name)
    git -C "$SANDBOX" commit --allow-empty -q \
        -m "feat: commit with claude trailer two

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
    # Commit 5 — Claude trailer
    git -C "$SANDBOX" commit --allow-empty -q \
        -m "feat: commit with claude trailer three

Co-Authored-By: Claude <noreply@anthropic.com>"

    orig_sha="$(git -C "$SANDBOX" rev-parse HEAD)"

    local output exit_code
    output="$(cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --dry-run 2>&1)" || exit_code=$?
    exit_code="${exit_code:-0}"

    assert_exit_zero "$exit_code" "dry-run exit code" || return 1
    assert_output_contains "$output" "would rewrite 3 commit(s)" "dry-run count message" || return 1

    local after_sha
    after_sha="$(git -C "$SANDBOX" rev-parse HEAD)"
    if [[ "$orig_sha" != "$after_sha" ]]; then
        echo "  FAIL: HEAD SHA changed during --dry-run (before: $orig_sha, after: $after_sha)" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 2 — test_run_removes_claude_trailer_only
#
# AC: Story 8.1 Acceptance Test #2
# Given: Sandbox repo with Claude trailer commit AND Alice human trailer commit
# When:  scripts/rewrite-coauthored-trailer.sh --run runs in sandbox repo
# Then:  Zero Claude trailers remain; Alice's Co-Authored-By trailer is intact
# =============================================================================
test_run_removes_claude_trailer_only() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    # Commit 1 — Claude trailer
    git -C "$SANDBOX" commit --allow-empty -q \
        -m "feat: commit with claude

Co-Authored-By: Claude <noreply@anthropic.com>"
    # Commit 2 — human co-author
    git -C "$SANDBOX" commit --allow-empty -q \
        -m "feat: commit with alice

Co-Authored-By: Alice Human <alice@example.com>"

    local exit_code=0
    cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --run >/dev/null 2>&1 || exit_code=$?

    assert_exit_zero "$exit_code" "run exit code" || return 1

    # Check raw commit objects on HEAD only (not --all, to avoid counting
    # backup-branch history which intentionally retains the original trailers).
    local claude_count
    claude_count="$(git -C "$SANDBOX" log HEAD --format="%B" | grep -c "Co-Authored-By: Claude" || true)"
    if [[ "$claude_count" -ne 0 ]]; then
        echo "  FAIL: expected 0 Claude trailers in HEAD commits after rewrite, found $claude_count line(s)" >&2
        return 1
    fi

    # The backup branch SHOULD still contain the original Claude trailer
    # (that is the entire point of the backup — it is a rollback point).
    local backup_branch
    backup_branch="$(git -C "$SANDBOX" branch --list "backup/pre-history-rewrite-*" | sed 's/^[* ]*//' | head -1)"
    if [[ -n "$backup_branch" ]]; then
        local backup_claude_count
        backup_claude_count="$(git -C "$SANDBOX" log "$backup_branch" --format="%B" | grep -c "Co-Authored-By: Claude" || true)"
        if [[ "$backup_claude_count" -eq 0 ]]; then
            echo "  FAIL: backup branch should still contain the original Claude trailer (backup integrity)" >&2
            return 1
        fi
    fi

    # Alice's trailer must be preserved on HEAD
    local alice_count
    alice_count="$(git -C "$SANDBOX" log HEAD --format="%B" | grep -c "Co-Authored-By: Alice" || true)"
    if [[ "$alice_count" -eq 0 ]]; then
        echo "  FAIL: Alice's Co-Authored-By trailer was removed (it should be preserved)" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 3 — test_run_creates_backup_branch
#
# AC: Story 8.1 Acceptance Test #3
# Given: Sandbox repo with HEAD at $ORIG_SHA
# When:  scripts/rewrite-coauthored-trailer.sh --run
# Then:  A branch backup/pre-history-rewrite-<timestamp> exists and points to $ORIG_SHA
# =============================================================================
test_run_creates_backup_branch() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    git -C "$SANDBOX" commit --allow-empty -q \
        -m "feat: initial commit

Co-Authored-By: Claude <noreply@anthropic.com>"

    local orig_sha
    orig_sha="$(git -C "$SANDBOX" rev-parse HEAD)"

    local output exit_code=0
    output="$(cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --run 2>&1)" || exit_code=$?

    assert_exit_zero "$exit_code" "run exit code" || return 1

    # Find the backup branch by listing branches matching the expected prefix
    local backup_branch
    backup_branch="$(git -C "$SANDBOX" branch --list "backup/pre-history-rewrite-*" | sed 's/^[* ]*//' | head -1)"

    if [[ -z "$backup_branch" ]]; then
        echo "  FAIL: no branch matching 'backup/pre-history-rewrite-*' found" >&2
        return 1
    fi

    local backup_sha
    backup_sha="$(git -C "$SANDBOX" rev-parse "$backup_branch")"
    if [[ "$backup_sha" != "$orig_sha" ]]; then
        echo "  FAIL: backup branch $backup_branch points to $backup_sha, expected $orig_sha" >&2
        return 1
    fi

    # The branch name must also appear in stdout (AC4: output for rollback docs)
    assert_output_contains "$output" "backup/pre-history-rewrite-" "branch name in stdout" || return 1

    return 0
}

# =============================================================================
# TEST 4 — test_run_preserves_metadata
#
# AC: Story 8.1 Acceptance Test #4
# Given: Sandbox repo with 3 Claude-trailer commits, each with explicit
#        --author and GIT_COMMITTER_* env set to deterministic values
# When:  scripts/rewrite-coauthored-trailer.sh --run
# Then:  author name/email/date and committer name/email/date are identical
#        character-for-character to the backup branch entries
# =============================================================================
test_run_preserves_metadata() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    # Create 3 commits with fixed, controlled metadata
    local i
    for i in 1 2 3; do
        GIT_COMMITTER_NAME="Committer ${i}" \
        GIT_COMMITTER_EMAIL="committer${i}@example.com" \
        GIT_COMMITTER_DATE="2026-01-0${i}T12:00:00+0000" \
        git -C "$SANDBOX" commit --allow-empty -q \
            --author="Author ${i} <author${i}@example.com>" \
            --date="2026-01-0${i}T10:00:00+0000" \
            -m "feat: commit ${i} with claude trailer

Co-Authored-By: Claude <noreply@anthropic.com>"
    done

    local exit_code=0
    cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --run >/dev/null 2>&1 || exit_code=$?

    assert_exit_zero "$exit_code" "run exit code" || return 1

    # Find backup branch
    local backup_branch
    backup_branch="$(git -C "$SANDBOX" branch --list "backup/pre-history-rewrite-*" | sed 's/^[* ]*//' | head -1)"

    if [[ -z "$backup_branch" ]]; then
        echo "  FAIL: no backup branch found for metadata comparison" >&2
        return 1
    fi

    # Compare metadata of rewritten commits vs backup branch.
    # Use the full 3-commit range so all rewritten commits are validated.
    local after_meta backup_meta
    after_meta="$(git -C "$SANDBOX" log --format="%an|%ae|%ai|%cn|%ce|%ci" HEAD)"
    backup_meta="$(git -C "$SANDBOX" log --format="%an|%ae|%ai|%cn|%ce|%ci" "$backup_branch")"

    if [[ "$after_meta" != "$backup_meta" ]]; then
        echo "  FAIL: metadata mismatch after rewrite" >&2
        echo "  After rewrite: $after_meta" >&2
        echo "  Backup branch: $backup_meta" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 5 — test_pre_flight_aborts_on_dirty_tree
#
# AC: Story 8.1 Acceptance Test #5
# Given: Sandbox repo with an uncommitted change (untracked file)
# When:  scripts/rewrite-coauthored-trailer.sh --run
# Then:  Exit != 0; stderr contains "working tree not clean"; repo state unchanged
# =============================================================================
test_pre_flight_aborts_on_dirty_tree() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    git -C "$SANDBOX" commit --allow-empty -q -m "feat: baseline commit"
    local orig_sha
    orig_sha="$(git -C "$SANDBOX" rev-parse HEAD)"

    # Introduce a dirty working tree
    echo "foo" > "${SANDBOX}/untracked.txt"

    local stderr_output exit_code=0
    stderr_output="$(cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --run 2>&1 >/dev/null)" || exit_code=$?

    assert_exit_nonzero "$exit_code" "dirty-tree abort exit code" || return 1
    assert_output_contains "$stderr_output" "working tree not clean" "dirty-tree error message" || return 1

    local after_sha
    after_sha="$(git -C "$SANDBOX" rev-parse HEAD)"
    if [[ "$orig_sha" != "$after_sha" ]]; then
        echo "  FAIL: repo HEAD changed despite pre-flight abort (before: $orig_sha, after: $after_sha)" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 6 — test_verify_mode_passes_after_run
#
# AC: Story 8.1 Acceptance Test #6
# Given: Sandbox repo after a successful --run
# When:  scripts/rewrite-coauthored-trailer.sh --verify
# Then:  Exit 0; all sub-checks report PASS in stdout
# =============================================================================
test_verify_mode_passes_after_run() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    git -C "$SANDBOX" commit --allow-empty -q \
        -m "feat: commit for verify test

Co-Authored-By: Claude <noreply@anthropic.com>"

    # Run --run first
    local run_exit=0
    cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --run >/dev/null 2>&1 || run_exit=$?
    if [[ "$run_exit" -ne 0 ]]; then
        echo "  FAIL: --run failed (exit $run_exit) — cannot test --verify" >&2
        return 1
    fi

    # Now run --verify
    local verify_output verify_exit=0
    verify_output="$(cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --verify 2>&1)" || verify_exit=$?

    assert_exit_zero "$verify_exit" "--verify exit code" || return 1

    # All five sub-checks must appear as PASS
    local check
    for check in "no-trailer" "backup-exists" "count-unchanged" "metadata-match" "backup-integrity"; do
        if ! grep -q "PASS" <<<"$(grep "$check" <<<"$verify_output")"; then
            echo "  FAIL: sub-check '$check' did not report PASS" >&2
            echo "  --verify output: $verify_output" >&2
            return 1
        fi
    done

    return 0
}

# =============================================================================
# TEST 7 — test_pre_flight_aborts_when_filter_repo_missing
#
# AC: Story 8.1 Acceptance Test #7
# Given: Sandbox repo, but PATH contains no git-filter-repo binary
# When:  scripts/rewrite-coauthored-trailer.sh --run with restricted PATH
# Then:  Exit != 0; stderr contains "git-filter-repo not installed"
# =============================================================================
test_pre_flight_aborts_when_filter_repo_missing() {
    # RED PHASE: subject script does not exist yet — this test must fail
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    git -C "$SANDBOX" commit --allow-empty -q -m "feat: baseline for filter-repo test"

    # Use a PATH that has git and basic tools but no git-filter-repo
    local restricted_path="/usr/bin:/bin"

    local stderr_output exit_code=0
    stderr_output="$(cd "$SANDBOX" && PATH="$restricted_path" bash "$SUBJECT_SCRIPT" --run 2>&1 >/dev/null)" || exit_code=$?

    assert_exit_nonzero "$exit_code" "filter-repo-missing abort exit code" || return 1
    assert_output_contains "$stderr_output" "git-filter-repo not installed" "filter-repo error message" || return 1

    return 0
}

# =============================================================================
# TEST 8 — test_no_args_prints_usage
#
# AC: Story 8.1 AC 3 (ohne Argument: Usage + Exit 1)
# Given: Script invoked with no arguments
# When:  bash scripts/rewrite-coauthored-trailer.sh
# Then:  Exit 1; output contains "Usage:"
# =============================================================================
test_no_args_prints_usage() {
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    local output exit_code=0
    output="$(bash "$SUBJECT_SCRIPT" 2>&1)" || exit_code=$?

    assert_exit_nonzero "$exit_code" "no-args exit code" || return 1
    assert_output_contains "$output" "Usage:" "usage message present" || return 1

    return 0
}

# =============================================================================
# TEST 9 — test_script_passes_shellcheck
#
# AC: Story 8.1 AC 1 (shellcheck ohne Errors)
# Given: shellcheck is in PATH (skip if absent)
# When:  shellcheck --severity=error scripts/rewrite-coauthored-trailer.sh
# Then:  Exit 0
# =============================================================================
test_script_passes_shellcheck() {
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    if ! command -v shellcheck >/dev/null 2>&1; then
        echo "  SKIP: shellcheck not in PATH"
        # Exit 0 so the test suite doesn't fail in CI environments without shellcheck
        return 0
    fi

    local output exit_code=0
    output="$(shellcheck --severity=error "$SUBJECT_SCRIPT" 2>&1)" || exit_code=$?

    if [[ "$exit_code" -ne 0 ]]; then
        echo "  FAIL: shellcheck reported errors:" >&2
        echo "$output" >&2
        return 1
    fi

    return 0
}

# =============================================================================
# TEST 10 — test_pre_flight_aborts_on_duplicate_backup
#
# AC: Story 8.1 AC 2c (Doppel-Backup-Branch Pre-flight)
# Given: Sandbox repo with an existing backup/pre-history-rewrite-* branch
# When:  scripts/rewrite-coauthored-trailer.sh --run
# Then:  Exit != 0; stderr contains "backup branch already exists"
# =============================================================================
test_pre_flight_aborts_on_duplicate_backup() {
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    git -C "$SANDBOX" commit --allow-empty -q \
        -m "feat: initial commit

Co-Authored-By: Claude <noreply@anthropic.com>"

    # Manually create a backup branch that matches the preflight pattern
    git -C "$SANDBOX" branch "backup/pre-history-rewrite-already-exists" HEAD

    local stderr_output exit_code=0
    stderr_output="$(cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --run 2>&1 >/dev/null)" || exit_code=$?

    assert_exit_nonzero "$exit_code" "duplicate-backup abort exit code" || return 1
    assert_output_contains "$stderr_output" "backup branch already exists" "duplicate-backup error message" || return 1

    return 0
}

# =============================================================================
# TEST 11 — test_pre_flight_aborts_on_detached_head
#
# AC: Story 8.1 AC 2 (additional pre-flight: filter-repo --refs needs a named branch)
# Given: Sandbox repo on a detached HEAD
# When:  scripts/rewrite-coauthored-trailer.sh --run
# Then:  Exit != 0; stderr contains "detached HEAD"
# =============================================================================
test_pre_flight_aborts_on_detached_head() {
    if [[ ! -x "$SUBJECT_SCRIPT" ]]; then
        echo "  FAIL: script not found or not executable: ${SUBJECT_SCRIPT}" >&2
        return 1
    fi

    setup_sandbox_repo

    git -C "$SANDBOX" commit --allow-empty -q \
        -m "feat: initial commit

Co-Authored-By: Claude <noreply@anthropic.com>"
    git -C "$SANDBOX" commit --allow-empty -q -m "feat: second commit"

    # Detach HEAD by checking out the SHA directly
    git -C "$SANDBOX" checkout -q --detach HEAD

    local stderr_output exit_code=0
    stderr_output="$(cd "$SANDBOX" && bash "$SUBJECT_SCRIPT" --run 2>&1 >/dev/null)" || exit_code=$?

    assert_exit_nonzero "$exit_code" "detached-head abort exit code" || return 1
    assert_output_contains "$stderr_output" "detached HEAD" "detached-head error message" || return 1

    return 0
}

# =============================================================================
# MAIN
# =============================================================================
main() {
    echo "=========================================================="
    echo "  Story 8.1 — Acceptance Tests"
    echo "  Target: ${SUBJECT_SCRIPT}"
    echo "=========================================================="
    echo ""

    run_test test_dry_run_reports_correct_count
    run_test test_run_removes_claude_trailer_only
    run_test test_run_creates_backup_branch
    run_test test_run_preserves_metadata
    run_test test_pre_flight_aborts_on_dirty_tree
    run_test test_verify_mode_passes_after_run
    run_test test_pre_flight_aborts_when_filter_repo_missing
    run_test test_no_args_prints_usage
    run_test test_script_passes_shellcheck
    run_test test_pre_flight_aborts_on_duplicate_backup
    run_test test_pre_flight_aborts_on_detached_head

    echo ""
    echo "=========================================================="
    echo "  Results: ${TESTS_PASSED}/${TESTS_RUN} passed, ${TESTS_FAILED} failed"
    echo "=========================================================="

    if [[ "${#FAILED_NAMES[@]}" -gt 0 ]]; then
        echo ""
        echo "  Failed tests:"
        for name in "${FAILED_NAMES[@]}"; do
            echo "    - ${name}"
        done
        echo ""
    fi

    if [[ "$TESTS_FAILED" -gt 0 ]]; then
        exit 1
    fi
    exit 0
}

main "$@"
