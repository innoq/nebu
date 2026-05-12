---
security_review: required
---

# Story 8.5: Secret-Scan-Gate — gitleaks History-Scan + CI-Integration

Status: ready-for-dev

## Story

**As a** maintainer preparing for the public release,
**I want** an automated scan of the entire Git history for leaked secrets (API keys, private keys, PSKs, `.env` content) and a CI gate that blocks future leaks,
**so that** I can guarantee no sensitive material enters the public record before or after the initial push to GitHub.

**Size:** S

---

## Background

Before any public push to GitHub, the full Git history must be clean of secrets. Even after the Co-Authored-By trailer rewrite (Story 8.1), any secrets that were accidentally committed — API tokens, private keys, PSKs, `.env` file contents — would be permanently visible on a public repo once pushed.

This story delivers two complementary protection layers:

1. **History-Scan** — a one-shot `gitleaks detect` run over the full Git history, to be executed before Story 8.10 (Initial Public Push). The scan must exit 0 (or document all allow-listed False-Positives with justification) before the public push proceeds.

2. **CI Gate** — a `gitleaks protect --staged` check for ongoing protection after the public push. This runs on every `feature/*` and `main` push via GitHub Actions (Story 8.6) and can be installed as a local pre-push Git hook.

**Tool choice:** `gitleaks` — Apache-2.0 licensed, well-maintained Go binary, industry standard for Open-Source repos. Not committed to the repo; installed separately (`brew install gitleaks` / `go install`).

**Known False-Positives to allow-list (identified before implementation):**

- Example tokens in `_bmad-output/` story files (not real credentials)
- TLS test keys in `gateway/internal/auth/testdata/` (test fixtures, no real authority)
- Mock OIDC configs with placeholder client secrets in test and Gherkin feature files
- Any `gitleaks-report.json` output file itself (contains redacted findings metadata)

**Dependency chain:**

- Story 8.1 delivers `scripts/rewrite-coauthored-trailer.sh` — history rewrite runs before this story's `--history` scan
- Story 8.5 (this story) must be green before Story 8.10 (Initial Public Push) starts
- Story 8.6 (GitHub Actions CI) will reference the `scripts/scan-secrets.sh --ci` script as the `secret-scan` job

---

## Acceptance Criteria

1. **`.gitleaks.toml` exists** in the repository root, contains gitleaks default rules (via `useDefault = true`) and a documented allow-list (`[[allowlist]]` entries) for each known False-Positive in this repository. Every allow-list entry has an inline comment explaining why it is safe to skip.

2. **`scripts/scan-secrets.sh` exists**, is executable, and supports three explicit modes:
   - `--history`: full history scan via `gitleaks detect --source .` — intended for pre-push one-shot verification
   - `--staged`: staged-only scan via `gitleaks protect --staged` — intended for pre-push hook
   - `--ci`: CI-optimised scan (non-interactive, no banner, JSON report written to `gitleaks-report.json`, exits 0 on clean / 1 on findings)
   Without a recognised argument: print usage and exit 1.

3. **Pre-flight check**: `scripts/scan-secrets.sh` checks for `gitleaks` in `PATH` before running any scan. If not found, the script prints a clear installation hint (`brew install gitleaks` or `go install github.com/zricethezav/gitleaks/v8@latest`) and exits with a non-zero code.

4. **History-Scan passes**: Running `scripts/scan-secrets.sh --history` on the current repository exits 0 (or, if allow-list entries are needed, every finding is documented in `.gitleaks.toml` with a comment justifying the allow-list entry and confirming the value is not a real secret).

5. **CI mode produces valid JSON report**: Running `scripts/scan-secrets.sh --ci` in a repository with at least one simulated finding produces a `gitleaks-report.json` file that is valid JSON and contains a top-level array.

6. **CI mode exits 0 on clean repo**: Running `scripts/scan-secrets.sh --ci` on a repository with no secrets produces `gitleaks-report.json` (empty array `[]`) and exits 0.

7. **Runbook exists** at `scripts/SECRET_SCAN_RUNBOOK.md` and covers:
   - How to install `gitleaks` locally
   - How to run `--history` before the public push
   - How to install `--staged` as a Git pre-push hook (`.git/hooks/pre-push` with a copy or symlink)
   - How to add a False-Positive to the allow-list in `.gitleaks.toml` (step-by-step with example)
   - What to do when `--history` finds a real secret (link back to Story 8.1 tooling for history rewrite)

8. **Allow-list entries have justifications**: Every `[[allowlist]]` entry in `.gitleaks.toml` has an inline comment. The comment states (a) what the pattern matches, (b) why the matched value is not a real secret, (c) which file or path it lives in.

9. **Shellcheck clean**: `scripts/scan-secrets.sh` passes `shellcheck --severity=error` with zero errors.

10. **Story 8.10 gate**: `scripts/SECRET_SCAN_RUNBOOK.md` explicitly states that `scripts/scan-secrets.sh --history` must exit 0 before Story 8.10 (Initial Public Push) may proceed.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

All tests run in isolated `mktemp -d` sandbox Git repos — no writes to the host repository. Tests are implemented as Bash test functions in `scripts/scan-secrets.sh.test.sh` (or `scripts/scan-secrets.test.sh`) with explicit exit codes. No external test framework required.

1. **`test_history_detects_aws_key_in_sandbox_commit`** — Bash, isolated mktemp repo
   - Given: A sandbox Git repo with a commit containing the string `AKIAIOSFODNN7EXAMPLE` (synthetic AWS Access Key ID — format matches gitleaks default rule, not a real credential)
   - When: `scripts/scan-secrets.sh --history` runs in the sandbox repo
   - Then: Exit code 1; stdout or stderr references the finding; `gitleaks-report.json` is NOT written (non-CI mode)

2. **`test_staged_blocks_secret_before_commit`** — Bash, isolated mktemp repo
   - Given: A sandbox Git repo with a staged (but not yet committed) file containing `AKIAIOSFODNN7EXAMPLE`
   - When: `scripts/scan-secrets.sh --staged` runs in the sandbox repo
   - Then: Exit code 1; output references the staged finding

3. **`test_staged_allows_whitelisted_pattern`** — Bash, isolated mktemp repo
   - Given: A sandbox Git repo with a staged file at path `_bmad-output/test-example.md` containing a token that matches a gitleaks default rule AND is covered by an allow-list path pattern in `.gitleaks.toml` (e.g., `_bmad-output/`)
   - When: `scripts/scan-secrets.sh --staged` runs in the sandbox repo (with the project `.gitleaks.toml` present)
   - Then: Exit code 0; the whitelisted path is not reported as a finding

4. **`test_ci_produces_valid_json_on_finding`** — Bash, isolated mktemp repo
   - Given: A sandbox Git repo with a commit containing `AKIAIOSFODNN7EXAMPLE`
   - When: `scripts/scan-secrets.sh --ci` runs in the sandbox repo
   - Then: Exit code 1; `gitleaks-report.json` exists and is parseable as JSON (verified via `python3 -m json.tool` or `jq .`); the file contains a non-empty array

5. **`test_ci_exits_zero_on_clean_repo`** — Bash, isolated mktemp repo
   - Given: A sandbox Git repo with commits containing no secret patterns
   - When: `scripts/scan-secrets.sh --ci` runs in the sandbox repo
   - Then: Exit code 0; `gitleaks-report.json` exists and contains `[]` or is an empty JSON array

6. **`test_preflight_fails_when_gitleaks_missing`** — Bash, isolated mktemp repo
   - Given: A sandbox environment where `gitleaks` is NOT present in `PATH` (achieved via `PATH=/usr/bin:/bin` override)
   - When: `scripts/scan-secrets.sh --history` is invoked
   - Then: Exit code != 0; stderr contains an installation hint (`brew install gitleaks` or `go install`)

7. **`test_allowlist_entry_suppresses_known_false_positive`** — Bash, isolated mktemp repo
   - Given: A sandbox Git repo with a commit in a path matching the allow-list pattern (e.g., `gateway/internal/auth/testdata/`) containing a test key string
   - When: `scripts/scan-secrets.sh --history` runs with the project `.gitleaks.toml`
   - Then: Exit code 0; the test-key path is not reported as a finding

8. **`test_shellcheck_clean`** — Bash, direct shellcheck invocation (no sandbox needed)
   - Given: `scripts/scan-secrets.sh` exists
   - When: `shellcheck --severity=error scripts/scan-secrets.sh` is run
   - Then: Exit code 0; no errors printed

**Persistenz-Strategie:** Not applicable — shell script with no application state. No crash/restart test required.

---

## Risks & Mitigations

| Risiko | Schwere | Mitigation |
|---|---|---|
| **Over-broad allow-list** — an allow-list regex that is too wide suppresses real secrets silently | HOCH | Every allow-list entry must be path-scoped (`paths`) or regex-scoped to the minimum required pattern. Justification comment required (AC 8). Allow-list reviewed in security review gate. |
| **Real secret in history not caught because gitleaks rule does not cover it** | MITTEL | gitleaks default ruleset covers ~170 patterns. For custom PSKs and internal tokens (which have no standard pattern), the Runbook instructs the maintainer to do an additional manual `git log -p` grep before Story 8.10. |
| **gitleaks not installed on CI runner** | NIEDRIG | Story 8.6 (GitHub Actions) installs gitleaks via `brew` or the official Docker image. The `--ci` mode script is self-contained and only requires the binary in PATH. |
| **`gitleaks-report.json` accidentally committed** | NIEDRIG | `.gitignore` must include `gitleaks-report.json`. Verified in AC 5 test setup. |
| **False-Positive flood delays pre-push workflow** | NIEDRIG | Allow-list populated upfront (before implementation) based on known test fixtures. Developer can expand via documented Runbook procedure. |

---

## Implementation Notes

### `.gitleaks.toml` — Outline

```toml
# .gitleaks.toml
# gitleaks configuration for nebu-chat
# See: https://github.com/zricethezav/gitleaks#configuration

title = "nebu-chat gitleaks config"

useDefault = true  # include all default detection rules

# Allow-list for known false-positives in this repository.
# Each entry MUST have an inline comment explaining:
#   (a) what the pattern matches
#   (b) why the matched value is not a real secret
#   (c) which file/path it lives in

[[allowlist]]
description = "BMAD story files — example tokens used in documentation only"
paths = [
  '''_bmad-output/.*'''
]
# (a) Matches any token-like string in BMAD output files
# (b) These are placeholder/example values written in story descriptions, not real credentials
# (c) _bmad-output/ directory (planning + implementation artifacts)

[[allowlist]]
description = "TLS test keys in gateway auth testdata"
paths = [
  '''gateway/internal/auth/testdata/.*'''
]
# (a) Matches private key PEM blocks in test fixture files
# (b) These are self-signed test certificates with no real CA authority, generated for unit tests only
# (c) gateway/internal/auth/testdata/

[[allowlist]]
description = "Mock OIDC client secrets in Gherkin feature files and test configs"
paths = [
  '''gateway/features/.*''',
  '''gateway/.*_test\.go'''
]
# (a) Matches placeholder client_secret values in OIDC test scenarios
# (b) These are synthetic values used against a local Dex test instance, never a real OIDC provider
# (c) gateway/features/ and *_test.go files throughout gateway/
```

### `scripts/scan-secrets.sh` — Outline

```bash
#!/usr/bin/env bash
# scripts/scan-secrets.sh
# Secret-scan wrapper for nebu-chat using gitleaks.
# Modes:
#   --history   Full history scan (pre-push, one-shot)
#   --staged    Staged-only scan (pre-push hook)
#   --ci        CI mode: non-interactive, JSON report, exit 0/1
#
# Usage: scan-secrets.sh [--history | --staged | --ci]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPORT_FILE="${REPO_ROOT}/gitleaks-report.json"
CONFIG_FILE="${REPO_ROOT}/.gitleaks.toml"

# Pre-flight: verify gitleaks is installed
preflight_check() {
  if ! command -v gitleaks >/dev/null 2>&1; then
    echo "ERROR: gitleaks not found in PATH." >&2
    echo "Install via: brew install gitleaks" >&2
    echo "        or: go install github.com/zricethezav/gitleaks/v8@latest" >&2
    exit 1
  fi
}

mode_history() {
  preflight_check
  gitleaks detect \
    --source "${REPO_ROOT}" \
    --config "${CONFIG_FILE}" \
    --no-banner \
    --redact \
    --log-opts="--all"
}

mode_staged() {
  preflight_check
  gitleaks protect \
    --staged \
    --source "${REPO_ROOT}" \
    --config "${CONFIG_FILE}" \
    --no-banner \
    --redact
}

mode_ci() {
  preflight_check
  local exit_code=0
  gitleaks detect \
    --source "${REPO_ROOT}" \
    --config "${CONFIG_FILE}" \
    --no-banner \
    --redact \
    --log-opts="--all" \
    --report-format json \
    --report-path "${REPORT_FILE}" \
    || exit_code=$?
  exit "${exit_code}"
}

case "${1:-}" in
  --history) mode_history ;;
  --staged)  mode_staged  ;;
  --ci)      mode_ci      ;;
  *)
    echo "Usage: $(basename "$0") [--history | --staged | --ci]" >&2
    echo "" >&2
    echo "  --history   Full git history scan (run before public push)" >&2
    echo "  --staged    Staged-changes-only scan (use as pre-push hook)" >&2
    echo "  --ci        CI mode: JSON report to gitleaks-report.json, exit 0/1" >&2
    exit 1
    ;;
esac
```

### Pre-Push Hook Snippet (for Runbook)

```bash
#!/usr/bin/env bash
# .git/hooks/pre-push  (copy or symlink from scripts/scan-secrets.sh)
# Runs gitleaks --staged check before each push.

REPO_ROOT="$(git rev-parse --show-toplevel)"
"${REPO_ROOT}/scripts/scan-secrets.sh" --staged
```

Installation:

```bash
cp scripts/scan-secrets.sh .git/hooks/pre-push
chmod +x .git/hooks/pre-push
# or use a symlink:
ln -sf ../../scripts/scan-secrets.sh .git/hooks/pre-push
```

### Adding a False-Positive to the Allow-List (Runbook Procedure)

1. Run `scripts/scan-secrets.sh --history` and note the reported file path and rule ID.
2. Open `.gitleaks.toml`.
3. Add a new `[[allowlist]]` entry scoped to the minimum required path or regex.
4. Add an inline comment with three statements: what the pattern matches, why it is safe, where it lives.
5. Re-run `scripts/scan-secrets.sh --history` — verify the finding is suppressed and exit code is 0.
6. Commit `.gitleaks.toml` with a commit message explaining the allow-list addition.

### `.gitignore` addition

```
# gitleaks report output
gitleaks-report.json
```

### Story 8.10 Gate

`scripts/SECRET_SCAN_RUNBOOK.md` must include the following gate statement:

> **Pre-push gate (mandatory):** `scripts/scan-secrets.sh --history` MUST exit 0 before Story 8.10 (Initial Public Push) may proceed. If the scan reports findings, use the allow-list procedure (if the value is a confirmed non-secret test fixture) or use the history-rewrite tooling from Story 8.1 (if the value is a real secret that must be removed from history).

---

## Files to Create / Modify

| File / Action | Description |
|---|---|
| `.gitleaks.toml` (NEW) | gitleaks config: `useDefault = true` + documented allow-list for known False-Positives |
| `scripts/scan-secrets.sh` (NEW) | Bash wrapper: `--history` / `--staged` / `--ci` modes, pre-flight gitleaks check |
| `scripts/scan-secrets.test.sh` (NEW) | Bash test suite: 9 test functions in mktemp sandbox repos (AC 1–10) |
| `scripts/SECRET_SCAN_RUNBOOK.md` (NEW) | Manual runbook: install, pre-push run, pre-push hook setup, allow-list procedure, Story 8.10 gate |
| `.gitignore` (MODIFY) | Add `gitleaks-report.json` to prevent accidental commit of report output |
| `_bmad-output/implementation-artifacts/sprint-status-epic-8.yaml` (MODIFY) | Story 8-5 status: `backlog` -> `ready-for-dev` |

---

## Context: Epic 8

Epic 8 transfers the Nebu repository from GitLab (private) to GitHub (public, Apache 2.0).

Story 8.5 is a **prerequisite for Story 8.10** (Initial Public Push): the history scan must be green before the public push proceeds.

**Dependencies:**

- **Story 8.1** (History Rewrite Tooling) — the rewrite runs before this story's `--history` scan; a post-rewrite history is cleaner and may need fewer allow-list entries
- **Story 8.6** (GitHub Actions CI) — will reference `scripts/scan-secrets.sh --ci` as the `secret-scan` CI job; the script interface defined here is the contract

**Downstream dependency:**

- **Story 8.10** (Initial Public Push) — gated on `--history` exit 0

**BMad Method (BMAD — Build More Architect Dreams):** This story follows the standard BMAD TDD cycle: failing tests in `scripts/scan-secrets.test.sh` are written first (Gate 1), implementation brings them green (Gate 2), code review + test review (Gate 3), security review required (Gate 4 — `security_review: required`).
