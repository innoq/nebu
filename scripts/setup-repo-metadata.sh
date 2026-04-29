#!/usr/bin/env bash
# =============================================================================
# setup-repo-metadata.sh
# Apply GitHub/GitLab repo topics and description after the public push.
#
# Run AFTER Story 8.10 (Initial Public Push) is complete.
# See scripts/REPO_METADATA_RUNBOOK.md for prerequisites and instructions.
#
# WARNING: Do not run with 'set -x' in CI — tokens may be echoed in logs.
#
# Usage:
#   scripts/setup-repo-metadata.sh --github   Apply to github.com/innoq/nebu
#   scripts/setup-repo-metadata.sh --gitlab   Apply to gitlab.opencode.de/nebu/nebu-server
#   scripts/setup-repo-metadata.sh --all      Apply to both platforms sequentially
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
GITHUB_REPO="innoq/nebu"
GITLAB_REPO="nebu/nebu-server"
GITLAB_HOST="gitlab.opencode.de"

DESCRIPTION="Nebuchadnezzar -- Enterprise-grade, Matrix Client-Server API \
compatible chat server. Apache 2.0, no federation, horizontally scalable. \
Replaces Slack/Teams with full data sovereignty."

# Topics for GitHub (space-separated, each as a --add-topic argument)
GITHUB_TOPICS=(
    matrix
    chat
    messaging
    enterprise
    go
    elixir
    oidc
    apache-2
    sovereign
    nebu
)

# Topics for GitLab (comma-separated string)
GITLAB_TOPICS="matrix,chat,messaging,enterprise,go,elixir,oidc,apache-2,sovereign,nebu"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log() {
    printf '[setup-repo-metadata] %s\n' "$*"
}

require_cmd() {
    local cmd="$1"
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        printf 'ERROR: %s not found in PATH.\n' "${cmd}" >&2
        printf 'Install instructions: see scripts/REPO_METADATA_RUNBOOK.md\n' >&2
        return 1
    fi
}

# ---------------------------------------------------------------------------
# apply_github
# Applies description and topics to github.com/innoq/nebu via the gh CLI.
# Requires: gh authenticated with repo write permissions.
# ---------------------------------------------------------------------------
apply_github() {
    require_cmd gh

    log "Applying metadata to github.com/${GITHUB_REPO} ..."

    # Build --add-topic arguments
    local topic_args=()
    local t
    for t in "${GITHUB_TOPICS[@]}"; do
        topic_args+=("--add-topic" "${t}")
    done

    gh repo edit "${GITHUB_REPO}" \
        --description "${DESCRIPTION}" \
        "${topic_args[@]}"

    log "GitHub metadata applied."
}

# ---------------------------------------------------------------------------
# apply_gitlab
# Applies description and topics to gitlab.opencode.de/nebu/nebu-server
# via the glab CLI.
# Requires: glab authenticated against gitlab.opencode.de.
# ---------------------------------------------------------------------------
apply_gitlab() {
    require_cmd glab

    log "Applying metadata to ${GITLAB_HOST}/${GITLAB_REPO} ..."

    GLAB_HOST="${GITLAB_HOST}" glab project update \
        "${GITLAB_REPO}" \
        --description "${DESCRIPTION}" \
        --topics "${GITLAB_TOPICS}"

    log "GitLab metadata applied."
}

# ---------------------------------------------------------------------------
# usage
# ---------------------------------------------------------------------------
usage() {
    printf 'Usage: %s [--github | --gitlab | --all]\n' "$0"
    printf '\n'
    printf '  --github   Apply metadata to github.com/%s\n' "${GITHUB_REPO}"
    printf '             Requires: gh CLI authenticated with repo write permissions\n'
    printf '  --gitlab   Apply metadata to %s/%s\n' "${GITLAB_HOST}" "${GITLAB_REPO}"
    printf '             Requires: glab CLI authenticated against %s\n' "${GITLAB_HOST}"
    printf '  --all      Apply to both platforms sequentially\n'
    printf '\n'
    printf 'Run only AFTER Story 8.10 (Initial Public Push) is complete.\n'
    printf 'See scripts/REPO_METADATA_RUNBOOK.md for full instructions.\n'
    return 1
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
case "${1:-}" in
    --github) apply_github ;;
    --gitlab) apply_gitlab ;;
    --all)
        apply_github
        apply_gitlab
        ;;
    *)
        usage
        ;;
esac
