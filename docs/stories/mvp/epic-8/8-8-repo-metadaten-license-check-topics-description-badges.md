---
security_review: optional
---

# Story 8.8: Repo-Metadaten — License-Check, Topics, Description, Badges

Status: ready-for-dev

## Story

**As a** potential contributor or evaluator visiting the Nebu repository on GitHub or opencode.de,
**I want** to see a clear badge block with license, CI status, tech stack, and repo links, plus a documented setup procedure for topics and description,
**so that** I can immediately assess the project's status, technology, and compliance posture without reading the full README.

**Size:** S

---

## Background

### Strategic Context: Dual-Host Metadata

Stories 8.1–8.7 delivered commit hygiene, documentation, CI/CD, and contributor tooling for the dual-host sovereign-OSS strategy (`github.com/innoq/nebu` + `gitlab.opencode.de/nebu/nebu-server`). Story 8.8 closes the visual/metadata gap:

- Badge block in `README.md` — visible to every visitor on both platforms (Markdown renders identically on GitHub and GitLab).
- `LICENSE` verification — the Apache-2.0 file already exists in the repo root; this story confirms its presence and correctness.
- Repo-level Topics and Description — these are platform-specific settings (GitHub repo settings, GitLab project settings) that cannot be committed. A setup script + runbook ensures they are applied consistently and reproducibly after the public push in Story 8.10.

### Input Artefacts

- `tmp/badges.md` — Badge draft created during project inception. Contains correct badge snippets but an **incorrect OpenCode URL** (`opencode.de/nebu-chat/nebu`). The correct URL is `gitlab.opencode.de/nebu/nebu-server`. This story treats the draft as a reference and does not rely on its OpenCode line.
- `_bmad-output/planning-artifacts/badges-spec.md` — Created in Story 8.6. Defines CI live-badges (GitHub Actions + GitLab CI pipeline). Provides the canonical badge URLs.

### Lessons Learned from Stories 8.1–8.7

- **Verify-script design:** `REPO_ROOT=$(git rev-parse --show-toplevel)` + `cd "${REPO_ROOT}"` at the top, all paths relative (avoids false-negatives from 8.2/8.3 markdownlint cwd issue).
- **Python3 emoji scan:** use `range(0x1F000, 0x20000)` and `range(0x2600, 0x27C0)` — BSD grep Unicode false-positives fixed in 8.2.
- **Shellcheck SKIP fallback:** `if ! command -v shellcheck &>/dev/null; then echo "SKIP: shellcheck not found"; return 0; fi`.
- **markdownlint:** `cd "${REPO_ROOT}" && npx --yes markdownlint-cli <file>` — always cd first.
- **mktemp + trap:** `TMPDIR=$(mktemp -d)` + `trap 'rm -rf "${TMPDIR}"' EXIT` for any temp-file operations.
- **No emojis** in any file created by this story — not in badges (shields.io label text), not in scripts, not in runbook.
- **Anchored grep patterns** where intent is anchored (lesson from 8.4 code review finding).
- **`return 0`** (not `exit 0`) at end of each test function so the harness continues.

### Connection to Other Stories

- **Story 8.6** (CI) — Badge URLs (`ci.yml` workflow name must match) and `badges-spec.md` as input.
- **Story 8.7** (Issue/PR Templates) — CODEOWNERS + `.github/` infrastructure referenced by the Release-Readiness-Gate.
- **Story 8.9** (Release-Readiness-Gate) — `scripts/verify-repo-metadata.sh` must exit 0 before the gate can pass green.
- **Story 8.10** (Initial Public Push) — `scripts/setup-repo-metadata.sh` and `scripts/REPO_METADATA_RUNBOOK.md` are executed **after** the public push. The runbook explicitly gates on "Story 8.10 complete".

---

## Acceptance Criteria

1. **`LICENSE` file exists** in the repo root, contains the text "Apache License" and "Version 2.0" (standard Apache-2.0 text), and is a valid Apache-2.0 license per SPDX identifier `Apache-2.0`.

2. **`README.md` contains a badge block** inserted directly under the H1 title (`# Nebu`) and before the subtitle/blockquote — within the first 20 lines of the file.

3. **Badge block contains an Apache-2.0 License badge** linking to `LICENSE` (relative link) via shields.io static badge.

4. **Badge block contains a GitHub Actions CI badge** with:
   - Badge image URL: `https://github.com/innoq/nebu/actions/workflows/ci.yml/badge.svg`
   - Link target: `https://github.com/innoq/nebu/actions` (or `https://github.com/innoq/nebu/actions/workflows/ci.yml`)

5. **Badge block contains a GitLab CI pipeline badge** with:
   - Badge image URL: `https://gitlab.opencode.de/nebu/nebu-server/badges/main/pipeline.svg`
   - Link target: `https://gitlab.opencode.de/nebu/nebu-server/-/pipelines`
   - (The URL `opencode.de/nebu-chat/nebu` from `tmp/badges.md` must NOT appear in the badge block.)

6. **Badge block contains both repo badges:**
   - GitHub: `[![GitHub](https://img.shields.io/badge/GitHub-innoq%2Fnebu-...)](https://github.com/innoq/nebu)`
   - OpenCode: badge linking to `https://gitlab.opencode.de/nebu/nebu-server`

7. **Badge block contains at least four tech-stack badges:** Go, Erlang/OTP, PostgreSQL, Docker — and the Sovereign Self-Hosted badge (shields.io static).

8. **No emojis** in the README badge block (Python3 Unicode range scan returns zero matches for the badge block lines). **Markdownlint-clean:** the modified `README.md` produces no new markdownlint errors (delta check against pre-story baseline).

9. **`scripts/setup-repo-metadata.sh` exists**, is executable (`chmod +x`), is shellcheck clean, and supports three modes:
   - `--github` — applies topics + description to `github.com/innoq/nebu` via `gh repo edit`
   - `--gitlab` — applies topics + description to `gitlab.opencode.de/nebu/nebu-server` via `glab project update`
   - `--all` — runs both platforms sequentially
   Topics applied: `matrix chat messaging enterprise go elixir oidc apache-2 sovereign nebu`.
   Description applied: `Nebuchadnezzar — Enterprise-grade, Matrix Client-Server API compatible chat server. Apache 2.0, no federation, horizontally scalable. Replaces Slack/Teams with full data sovereignty.`

10. **`scripts/REPO_METADATA_RUNBOOK.md` exists** and contains all four required sections:
    - When to run (gate: Story 8.10 public push must be complete first)
    - How to install and authenticate `gh` and `glab` CLIs
    - How to run `setup-repo-metadata.sh` (with example invocations)
    - Fallback: manual update via GitHub web UI and GitLab web UI

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

All tests are implemented as Bash test functions in `scripts/verify-repo-metadata.sh`. The script follows the established pattern from `scripts/verify-ci-config.sh` (Story 8.6) and `scripts/verify-issue-pr-templates.sh` (Story 8.7):

- `REPO_ROOT=$(git rev-parse --show-toplevel)` + `cd "${REPO_ROOT}"` at the top; all paths relative.
- `run_test` helper: accepts test name; increments pass/fail counters; prints per-test PASS/FAIL.
- `TMPDIR=$(mktemp -d)` + `trap 'rm -rf "${TMPDIR}"' EXIT` for any temp-file operations.
- Python3 for emoji scan and YAML validation (no external test framework).
- Exit 0 on all-pass, 1 on any failure.

---

**Test 1 — `test_license_file_exists_and_apache2`**

- Given: `LICENSE` file exists in repo root
- When: `grep -q "Apache License" LICENSE && grep -q "Version 2.0" LICENSE`
- Then: Both patterns found (exit code 0)

**Test 2 — `test_readme_has_h1`**

- Given: `README.md` exists
- When: `grep -qE "^# " README.md`
- Then: Match found (H1 heading present)

**Test 3 — `test_badge_block_under_h1_within_30_lines`**

- Given: `README.md` exists and has an H1
- When: Extract lines 1–30; assert at least one `[![` badge link is present in those lines
- Then: At least one badge pattern (`\[!\[`) found within the first 30 lines
- Note: Badge block must be directly under H1 and before the blockquote subtitle.

**Test 4 — `test_license_badge_links_to_license_file`**

- Given: Badge block present in README.md
- When: `grep -q "](LICENSE)" README.md` (case-sensitive; the badge image must link to relative `LICENSE`)
- Then: Match found

**Test 5 — `test_github_actions_badge_url`**

- Given: Badge block present in README.md
- When: `grep -q "github.com/innoq/nebu/actions" README.md`
- Then: Match found (covers both the badge image URL and the link target)

**Test 6 — `test_gitlab_ci_badge_url`**

- Given: Badge block present in README.md
- When (6a): `grep -q "gitlab.opencode.de/nebu/nebu-server/badges/main/pipeline.svg" README.md`
- Then (6a): Match found
- When (6b): `grep -q "gitlab.opencode.de/nebu/nebu-server/-/pipelines" README.md`
- Then (6b): Match found
- When (6c): `grep -qv "opencode.de/nebu-chat" README.md` — old incorrect URL must NOT appear in badge block
- Then (6c): Exit code 0 (pattern absent in badge block lines)
- Note: 6c scans only the badge block lines (lines 1–30) to avoid false positives from references elsewhere.

**Test 7 — `test_both_repo_badges_present`**

- Given: Badge block present in README.md
- When (7a): `grep -q "github.com/innoq/nebu)" README.md` (GitHub repo badge link target)
- Then (7a): Match found
- When (7b): `grep -q "gitlab.opencode.de/nebu/nebu-server)" README.md` (OpenCode repo badge link target)
- Then (7b): Match found

**Test 8 — `test_tech_stack_badges_present`**

- Given: Badge block present in README.md
- When: Count how many of the following strings appear (case-insensitive) in lines 1–30: `Go`, `Erlang`, `PostgreSQL`, `Docker`
- Then: All four present (count == 4); additionally `Sovereign` or `sovereign` appears at least once

**Test 9 — `test_no_emojis_in_readme_badge_block`**

- Given: `README.md` exists
- When: Python3 extracts the first 30 lines; scans each character for Unicode ranges `0x1F000–0x1FFFF` and `0x2600–0x27BF`
- Then: Zero emoji characters found

**Test 10 — `test_setup_script_exists_executable_and_shellcheck_clean`**

- Given: `scripts/setup-repo-metadata.sh` exists
- When (10a): `[[ -x scripts/setup-repo-metadata.sh ]]`
- Then (10a): True (executable bit set)
- When (10b): `shellcheck --severity=error scripts/setup-repo-metadata.sh` (SKIP with exit 0 if shellcheck not in PATH)
- Then (10b): Exit code 0; no shellcheck errors
- When (10c): `grep -q "\-\-github" scripts/setup-repo-metadata.sh && grep -q "\-\-gitlab" scripts/setup-repo-metadata.sh && grep -q "\-\-all" scripts/setup-repo-metadata.sh`
- Then (10c): All three mode flags present

**Test 11 — `test_runbook_exists_and_required_content`**

- Given: `scripts/REPO_METADATA_RUNBOOK.md` exists
- When (11a): `grep -q "gh" scripts/REPO_METADATA_RUNBOOK.md`
- Then (11a): Match found (`gh` CLI mentioned)
- When (11b): `grep -q "glab" scripts/REPO_METADATA_RUNBOOK.md`
- Then (11b): Match found (`glab` CLI mentioned)
- When (11c): `grep -qi "story 8.10\|story 8-10" scripts/REPO_METADATA_RUNBOOK.md`
- Then (11c): Match found (explicit gate statement referencing Story 8.10)

**Test 12 — `test_verify_script_shellcheck_clean`**

- Given: `scripts/verify-repo-metadata.sh` exists
- When: `shellcheck --severity=error scripts/verify-repo-metadata.sh` (SKIP with exit 0 if shellcheck not in PATH)
- Then: Exit code 0; no shellcheck errors

**Persistenz-Strategie:** Not applicable — static files and shell scripts; no application state, no crash/restart test required.

---

## Risks and Mitigations

| Risk | Severity | Mitigation |
|---|---|---|
| **`gh`/`glab` token exposure in logs** — setup script could echo tokens if verbose mode is active | LOW | Script must not print tokens. Use `gh` and `glab` directly (they read from environment/keychain). Add a comment in script and runbook warning against `set -x` in CI for this script. |
| **GitHub Actions badge URL mismatch** — badge image URL must match the workflow `name:` field exactly | LOW | CI workflow name is `CI` per `.github/workflows/ci.yml` (Story 8.6). Badge URL uses the filename `ci.yml`, not the name. Both forms work; use filename-based URL. |
| **GitLab pipeline badge branch** — `badges/main/pipeline.svg` requires the default branch to be `main` | LOW | Repo default branch is `main`. Document in runbook that this must be verified post-push. |
| **`tmp/badges.md` incorrect OpenCode URL** — the draft file has `opencode.de/nebu-chat/nebu` | MEDIUM | Story explicitly uses `gitlab.opencode.de/nebu/nebu-server`. Test 6c verifies the old URL is absent from the badge block. `tmp/badges.md` is updated as cleanup but is not the source of truth for implementation. |
| **Markdownlint MD041** — H1 must be the first line for markdownlint; badges between H1 and body may trigger rules | LOW | Place badges on the line immediately after the H1 (no blank line between H1 and badges), or use an HTML comment separator. Verify with markdownlint delta check (Test 12 analog in verify script). |
| **`setup-repo-metadata.sh` glab API key name** — `glab project update` vs `glab repo edit` syntax varies by version | LOW | Script must document required `glab` version. Use `glab project update --topics` form; add version note in runbook. |

---

## Implementation Notes

### Badge Block Order and Placement

Insert the badge block on the line directly after `# Nebu` in `README.md`, before the existing blockquote subtitle. Use a single blank line between the H1 and the badge block (markdownlint MD022 requires blank lines around headings; the badge block sits below that blank line).

Recommended badge row grouping:

```markdown
<!-- Status -->
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](LICENSE)
[![CI](https://github.com/innoq/nebu/actions/workflows/ci.yml/badge.svg)](https://github.com/innoq/nebu/actions)
[![pipeline status](https://gitlab.opencode.de/nebu/nebu-server/badges/main/pipeline.svg)](https://gitlab.opencode.de/nebu/nebu-server/-/pipelines)

<!-- Repos -->
[![GitHub](https://img.shields.io/badge/GitHub-innoq%2Fnebu-181717?style=flat-square&logo=github&logoColor=white)](https://github.com/innoq/nebu)
[![OpenCode](https://img.shields.io/badge/OpenCode-nebu%2Fnebu--server-2a6fff?style=flat-square)](https://gitlab.opencode.de/nebu/nebu-server)

<!-- Tech stack -->
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Erlang/OTP](https://img.shields.io/badge/Erlang%2FOTP-27+-A90533?style=flat-square&logo=erlang&logoColor=white)](https://www.erlang.org)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16+-336791?style=flat-square&logo=postgresql&logoColor=white)](https://postgresql.org)
[![Docker](https://img.shields.io/badge/Docker-compose-2496ED?style=flat-square&logo=docker&logoColor=white)](docker-compose.yml)

<!-- Protocol -->
[![Matrix](https://img.shields.io/badge/Matrix-Client--Server%20API-0DBD8B?style=flat-square&logo=matrix&logoColor=white)](https://spec.matrix.org/latest/client-server-api/)
[![OIDC](https://img.shields.io/badge/OIDC-Keycloak%20ready-E8572A?style=flat-square)](#authentication)
[![TLS](https://img.shields.io/badge/TLS-1.3%20everywhere-185FA5?style=flat-square)](#security)
[![Sovereign](https://img.shields.io/badge/deployment-sovereign%20self--hosted-0F6E56?style=flat-square)](#deployment)
```

Notes:
- No emojis in label text or logo alt text.
- `shields.io` logo parameter does not count as an emoji — it is a slug string.
- HTML comment lines (`<!-- ... -->`) do not render in GitHub/GitLab Markdown and do not trigger markdownlint errors when kept short.
- CI/pipeline badges will show "no status" until the first pipeline run after public push — this is acceptable and documented in `badges-spec.md`.

### `scripts/setup-repo-metadata.sh` — Design

```bash
#!/usr/bin/env bash
# setup-repo-metadata.sh — Set GitHub/GitLab repo topics and description.
# Run AFTER Story 8.10 public push. See scripts/REPO_METADATA_RUNBOOK.md.
# WARNING: Do not run with 'set -x' in CI — tokens may be echoed.
set -euo pipefail

DESCRIPTION="Nebuchadnezzar — Enterprise-grade, Matrix Client-Server API compatible chat server. Apache 2.0, no federation, horizontally scalable. Replaces Slack/Teams with full data sovereignty."
TOPICS="matrix chat messaging enterprise go elixir oidc apache-2 sovereign nebu"

usage() {
  echo "Usage: $0 [--github | --gitlab | --all]"
  echo ""
  echo "  --github   Apply metadata to github.com/innoq/nebu (requires 'gh' CLI authenticated)"
  echo "  --gitlab   Apply metadata to gitlab.opencode.de/nebu/nebu-server (requires 'glab' CLI authenticated)"
  echo "  --all      Apply to both platforms sequentially"
  exit 1
}

apply_github() { ... }
apply_gitlab() { ... }

case "${1:-}" in
  --github) apply_github ;;
  --gitlab) apply_gitlab ;;
  --all)    apply_github && apply_gitlab ;;
  *)        usage ;;
esac
```

### `scripts/REPO_METADATA_RUNBOOK.md` — Required Sections

1. **When to run** — Explicit statement: "Run this script only after Story 8.10 (Initial Public Push) is complete and both `github.com/innoq/nebu` and `gitlab.opencode.de/nebu/nebu-server` exist as public repositories."
2. **Install and authenticate CLIs** — `gh` (GitHub CLI) + `glab` (GitLab CLI), version requirements, auth commands.
3. **Invocation** — Example commands for `--github`, `--gitlab`, `--all`; expected output.
4. **Fallback: manual via Web UI** — Step-by-step for GitHub Settings and GitLab Project Settings.

### `tmp/badges.md` — Cleanup

Update the OpenCode badge line in `tmp/badges.md` from `opencode.de/nebu-chat/nebu` to `gitlab.opencode.de/nebu/nebu-server` to eliminate the incorrect URL from the repository. This is a one-line fix; the file is a reference artefact, not an authoritative spec.

---

## Files to Create / Modify

| File | Action | Note |
|---|---|---|
| `README.md` | MODIFY | Insert badge block after H1 |
| `LICENSE` | VERIFY (no change expected) | Already exists; Apache-2.0 text confirmed |
| `scripts/setup-repo-metadata.sh` | CREATE | Executable; `--github`/`--gitlab`/`--all` modes |
| `scripts/REPO_METADATA_RUNBOOK.md` | CREATE | 4 required sections; no emojis |
| `scripts/verify-repo-metadata.sh` | CREATE | 12 tests; exit 0 on all-pass |
| `tmp/badges.md` | MODIFY | Fix incorrect OpenCode URL (1-line cleanup) |
| `_bmad-output/implementation-artifacts/sprint-status-epic-8.yaml` | UPDATE | `8-8-...` → `done` after merge |

**Explicitly NOT part of this story:**

- Any changes to CI workflow files (`.github/workflows/`, `.gitlab-ci.yml`).
- Any changes to `CONTRIBUTING.md`, `SECURITY.md`, or other documentation files.
- Actual execution of `setup-repo-metadata.sh` — that is gated on Story 8.10.
- Adding SPDX license headers to source files (`gateway/cmd/gateway/main.go`, `core/apps/*/lib/*.ex`) — deferred; SPDX-License-Identifier convention noted in story background, not enforced here.
- Any application source code changes.

---

## Context: Epic 8

Epic 8 prepares Nebu for public release on GitHub and the opencode.de GitLab mirror. Story 8.8 is the penultimate preparation story before the Release-Readiness-Gate (8.9) and the Initial Public Push (8.10). After this story, the repository presents a complete, professional face: license confirmed, CI/CD visible via live badges, tech stack advertised, and dual-host repo links prominent. The setup script and runbook ensure that platform-level metadata (topics, description) are applied reproducibly by any maintainer following documented steps.

Dependencies:

- **Story 8.6** (CI) — `badges-spec.md` and the `ci.yml` workflow name used in the GitHub Actions badge URL.
- **Story 8.7** (Issue/PR Templates) — Predecessor story; no direct dependency, but badge block references `CONTRIBUTING.md` implicitly via the Contributions-Welcome badge pattern.
- **Story 8.9** (Release-Readiness-Gate) — `scripts/verify-repo-metadata.sh` must exit 0 before the gate passes green.
- **Story 8.10** (Initial Public Push) — `setup-repo-metadata.sh` is executed after this story's gate; the runbook explicitly documents this dependency.
