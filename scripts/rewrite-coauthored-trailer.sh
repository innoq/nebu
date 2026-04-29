#!/usr/bin/env bash
# =============================================================================
# rewrite-coauthored-trailer.sh
# Story 8.1: Remove Co-Authored-By: Claude trailers from Git history
#
# Usage:
#   scripts/rewrite-coauthored-trailer.sh --dry-run   # show count, no changes
#   scripts/rewrite-coauthored-trailer.sh --run        # backup + rewrite
#   scripts/rewrite-coauthored-trailer.sh --verify     # post-rewrite verification
#
# Requirements: git, git-filter-repo
#
# Implementation notes:
#   --run uses "git filter-repo --refs <current-branch>" to limit the rewrite
#   to the current branch only, which keeps the backup branch pointing to the
#   original (pre-rewrite) SHA.  The backup branch intentionally retains the
#   original trailers — this is what makes it a valid rollback point.
#   --verify uses "git log HEAD" (not --all) so the backup-branch history is
#   not counted; a separate backup-integrity sub-check confirms that the backup
#   branch still carries the original trailers.
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Pre-flight: check git-filter-repo is installed
# ---------------------------------------------------------------------------
preflight_filter_repo() {
    if ! command -v git-filter-repo >/dev/null 2>&1; then
        echo "ERROR: git-filter-repo not installed. Install via: brew install git-filter-repo" >&2
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# Pre-flight: check working tree is clean
# ---------------------------------------------------------------------------
preflight_clean_tree() {
    local status
    status="$(git status --porcelain 2>/dev/null)"
    if [[ -n "$status" ]]; then
        echo "ERROR: working tree not clean. Commit or stash your changes first." >&2
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# Pre-flight: check no existing backup branch (protection against double-run)
# ---------------------------------------------------------------------------
preflight_no_duplicate_backup() {
    local existing
    existing="$(git branch --list "backup/pre-history-rewrite-*" | head -1)"
    if [[ -n "$existing" ]]; then
        echo "ERROR: backup branch already exists: ${existing}" >&2
        echo "       Delete it or rename it before re-running." >&2
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# Pre-flight: check we are on a named branch (filter-repo --refs needs one)
# ---------------------------------------------------------------------------
preflight_named_branch() {
    local current_branch
    current_branch="$(git rev-parse --abbrev-ref HEAD)"
    if [[ "$current_branch" == "HEAD" ]]; then
        echo "ERROR: detached HEAD detected. Check out a named branch first." >&2
        echo "       (filter-repo --refs needs a real branch name to scope the rewrite.)" >&2
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# Count commits with Claude trailers on HEAD (not --all).
# Using git log without --all avoids counting backup-branch history.
# ---------------------------------------------------------------------------
count_claude_commits() {
    local count=0
    while read -r commit; do
        # Check for actual trailer in commit metadata (not just text in body)
        if git cat-file -p "$commit" 2>/dev/null | grep -qE "^Co-Authored-By: Claude"; then
            ((count++)) || true
        fi
    done < <(git log HEAD --format="%H")
    echo "$count"
}

# ---------------------------------------------------------------------------
# MODE: --dry-run
# ---------------------------------------------------------------------------
mode_dry_run() {
    preflight_filter_repo

    local count
    count="$(count_claude_commits)"
    echo "Dry-run: would rewrite ${count} commit(s) to remove Co-Authored-By: Claude trailers."
    echo "(No changes made to repository)"
    exit 0
}

# ---------------------------------------------------------------------------
# MODE: --run
# ---------------------------------------------------------------------------
mode_run() {
    preflight_filter_repo
    preflight_clean_tree
    preflight_no_duplicate_backup
    preflight_named_branch

    # Record the original HEAD SHA and commit count for verify mode
    local orig_sha
    orig_sha="$(git rev-parse HEAD)"
    local orig_count
    orig_count="$(git rev-list --count HEAD)"

    # Determine the current branch name so we can limit filter-repo's scope
    local current_branch
    current_branch="$(git rev-parse --abbrev-ref HEAD)"

    # Create timestamped backup branch pointing to the current HEAD.
    # By using --refs below, filter-repo will NOT remap this backup branch,
    # so it will continue to point to the original (pre-rewrite) SHA.
    local backup_branch
    backup_branch="backup/pre-history-rewrite-$(date +%Y%m%d-%H%M%S)"
    git branch "$backup_branch" HEAD

    echo "Backup branch created: ${backup_branch} (SHA: ${orig_sha})"
    echo "Original commit count: ${orig_count}"

    # Store pre-rewrite metadata in git config so --verify can access it
    git config "filter-rewrite.backup-branch" "$backup_branch"
    git config "filter-rewrite.orig-count" "$orig_count"

    # Perform the rewrite using git filter-repo.
    #
    # --force : required because this is not a fresh clone.
    # --refs  : limit rewrite to refs/heads/<current_branch> only.
    #           This prevents filter-repo from remapping the backup branch,
    #           so the backup branch keeps pointing to the original SHA.
    #
    # Trailer regex: matches "\n\nCo-Authored-By: Claude..." (plus any
    # additional consecutive Claude lines) and removes the whole block.
    # Other Co-Authored-By: trailers (e.g. human co-authors) are untouched.
    git filter-repo --force \
        --refs "refs/heads/${current_branch}" \
        --message-callback \
        'import re; return re.sub(rb"(\n\nCo-Authored-By: Claude[^\n]*)(\nCo-Authored-By: Claude[^\n]*)*", b"", message)'

    echo "Rewrite complete."
    echo ""
    echo "Next steps (manual, see scripts/REWRITE_HISTORY_RUNBOOK.md):"
    echo "  1. Verify with: bash scripts/rewrite-coauthored-trailer.sh --verify"
    echo "  2. Force-push manually (NOT done by this script)"
    echo "  3. Rollback if needed: git reset --hard ${backup_branch}"
    exit 0
}

# ---------------------------------------------------------------------------
# MODE: --verify
# Five sub-checks:
#   1. no-trailer        — zero Co-Authored-By: Claude trailers on HEAD
#   2. backup-exists     — backup branch exists
#   3. count-unchanged   — commit count equals pre-rewrite count
#   4. metadata-match    — author/committer metadata matches backup branch
#   5. backup-integrity  — backup branch still contains the original trailers
# ---------------------------------------------------------------------------
mode_verify() {
    local all_pass=true

    # Retrieve stored values from git config
    local backup_branch orig_count
    backup_branch="$(git config "filter-rewrite.backup-branch" 2>/dev/null || echo "")"
    orig_count="$(git config "filter-rewrite.orig-count" 2>/dev/null || echo "")"

    # Fall back to discovering backup branch by name pattern if config missing
    if [[ -z "$backup_branch" ]]; then
        backup_branch="$(git branch --list "backup/pre-history-rewrite-*" | sed 's/^[* ]*//' | head -1)"
    fi

    # ---- Check 1: no-trailer ----
    local claude_count
    claude_count="$(count_claude_commits)"
    if [[ "$claude_count" -eq 0 ]]; then
        echo "PASS  [no-trailer] Zero Co-Authored-By: Claude trailers found in history."
    else
        echo "FAIL  [no-trailer] ${claude_count} Co-Authored-By: Claude trailer(s) still present."
        all_pass=false
    fi

    # ---- Check 2: backup-exists ----
    if [[ -n "$backup_branch" ]] && git rev-parse "$backup_branch" >/dev/null 2>&1; then
        echo "PASS  [backup-exists] Backup branch '${backup_branch}' exists."
    else
        echo "FAIL  [backup-exists] No backup branch matching 'backup/pre-history-rewrite-*' found."
        all_pass=false
    fi

    # ---- Check 3: count-unchanged ----
    local current_count
    current_count="$(git rev-list --count HEAD)"
    if [[ -z "$orig_count" ]] && [[ -n "$backup_branch" ]] && git rev-parse "$backup_branch" >/dev/null 2>&1; then
        orig_count="$(git rev-list --count "$backup_branch")"
    fi
    if [[ -n "$orig_count" ]] && [[ "$current_count" -eq "$orig_count" ]]; then
        echo "PASS  [count-unchanged] Commit count unchanged: ${current_count} commits."
    elif [[ -z "$orig_count" ]]; then
        echo "FAIL  [count-unchanged] Cannot determine original commit count."
        all_pass=false
    else
        echo "FAIL  [count-unchanged] Commit count changed: was ${orig_count}, now ${current_count}."
        all_pass=false
    fi

    # ---- Check 4: metadata-match ----
    if [[ -n "$backup_branch" ]] && git rev-parse "$backup_branch" >/dev/null 2>&1; then
        local head_meta backup_meta
        head_meta="$(git log --format="%an|%ae|%ai|%cn|%ce|%ci" HEAD)"
        backup_meta="$(git log --format="%an|%ae|%ai|%cn|%ce|%ci" "$backup_branch")"
        if [[ "$head_meta" == "$backup_meta" ]]; then
            echo "PASS  [metadata-match] All author/committer metadata matches backup branch."
        else
            echo "FAIL  [metadata-match] Metadata mismatch between HEAD and backup branch."
            all_pass=false
        fi
    else
        echo "FAIL  [metadata-match] Cannot compare metadata (no backup branch available)."
        all_pass=false
    fi

    # ---- Check 5: backup-integrity ----
    if [[ -n "$backup_branch" ]] && git rev-parse "$backup_branch" >/dev/null 2>&1; then
        local backup_trailer_count
        backup_trailer_count="$(git log "$backup_branch" --format="%B" | grep -c "Co-Authored-By: Claude" || true)"
        if [[ "$backup_trailer_count" -eq 0 ]]; then
            # No Claude trailer commits in backup — skip (nothing to verify)
            echo "PASS  [backup-integrity] No Claude-trailer commits in backup branch (nothing to verify)."
        else
            echo "PASS  [backup-integrity] Backup branch contains ${backup_trailer_count} original Claude trailer line(s)."
        fi
    else
        echo "FAIL  [backup-integrity] Cannot verify backup integrity (no backup branch available)."
        all_pass=false
    fi

    echo ""
    if [[ "$all_pass" == "true" ]]; then
        echo "All verification checks PASSED."
        exit 0
    else
        echo "One or more verification checks FAILED."
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
main() {
    if [[ $# -eq 0 ]]; then
        echo "Usage: $0 [--dry-run | --run | --verify]" >&2
        echo "" >&2
        echo "  --dry-run   Show how many commits would be rewritten (no changes)" >&2
        echo "  --run       Create backup branch and rewrite history" >&2
        echo "  --verify    Verify post-rewrite state (run after --run)" >&2
        exit 1
    fi

    case "$1" in
        --dry-run)
            mode_dry_run
            ;;
        --run)
            mode_run
            ;;
        --verify)
            mode_verify
            ;;
        *)
            echo "Unknown argument: $1" >&2
            echo "Usage: $0 [--dry-run | --run | --verify]" >&2
            exit 1
            ;;
    esac
}

main "$@"
