# Secret Scan Runbook

This document describes how to use `scripts/scan-secrets.sh` before and
after the initial public push of the nebu-chat repository.

**Pre-push gate (mandatory):** `scripts/scan-secrets.sh --history` MUST exit
0 before Story 8.10 (Initial Public Push) may proceed. If the scan reports
findings, use the allow-list procedure (if the value is a confirmed
non-secret test fixture) or use the history-rewrite tooling from Story 8.1
(if the value is a real secret that must be removed from history).

---

## When to Run scan-secrets

| Situation | Command |
|---|---|
| Before any public push (one-time gate) | `--history` |
| After each local commit (optional, fast) | `--staged` |
| On every CI push to feature/* and main | `--ci` |

---

## Installing gitleaks Locally

gitleaks is not committed to the repository. Install it once with:

```bash
# macOS
brew install gitleaks

# Go toolchain (any platform)
go install github.com/zricethezav/gitleaks/v8@latest
```

Verify the installation:

```bash
gitleaks version
```

Expected output example: `8.30.1`

---

## Running the History Scan Before the Public Push

Before Story 8.10 (Initial Public Push), run:

```bash
scripts/scan-secrets.sh --history
```

This executes a full `gitleaks detect` over the entire git history.

- Exit 0 — no findings. Proceed to Story 8.10.
- Exit 1 — findings reported. See sections below for resolution.

**Pre-push gate (mandatory):** This scan MUST exit 0 before Story 8.10
(Initial Public Push) may proceed.

---

## Installing the Staged-Scan as a Pre-Push Hook

Install `--staged` as a Git hook so it runs automatically before every
`git push`.

**Important:** Do NOT symlink or copy `scan-secrets.sh` directly to
`.git/hooks/pre-push`. Git invokes the hook as
`pre-push <remote-name> <remote-url>`, while `scan-secrets.sh` expects
exactly one of `--history`, `--staged`, `--ci`. A direct symlink will
exit 1 on every push and the maintainer will end up disabling the hook
or using `--no-verify`. Use the dedicated wrapper below instead.

Create `.git/hooks/pre-push` with the following content:

```bash
#!/usr/bin/env bash
# .git/hooks/pre-push
# Runs scan-secrets.sh --staged before each push. Hook arguments
# (remote name, remote URL) and stdin (refs being pushed) are ignored —
# the staged-scan is independent of the push target.
set -e
REPO_ROOT="$(git rev-parse --show-toplevel)"
exec "${REPO_ROOT}/scripts/scan-secrets.sh" --staged
```

Then make it executable:

```bash
chmod +x .git/hooks/pre-push
```

To uninstall the hook:

```bash
rm .git/hooks/pre-push
```

### Bypass and audit trail

`git push --no-verify` skips client-side hooks entirely. Do not use
`--no-verify` unless you have just run `scripts/scan-secrets.sh --history`
manually and confirmed it is clean. The CI-side `--ci` job (Story 8.6)
runs unconditionally on every push and is the actual gate — the hook
is only an early-warning convenience.

---

## Adding a False-Positive to the Allow-List

Follow this procedure when `--history` or `--staged` reports a finding that
you have confirmed is NOT a real secret (e.g., a test fixture, documentation
example, or placeholder value):

### Step 1 — Identify the finding

Run the scan and note:

- The file path (e.g., `gateway/features/auth.feature`)
- The rule ID (e.g., `aws-access-key-id-custom`)
- Why the value is safe (e.g., "synthetic client_secret for local Dex")

### Step 2 — Open `.gitleaks.toml`

The allow-list lives in the `[allowlist]` section at the bottom of
`.gitleaks.toml`.

### Step 3 — Add a path entry

Add the file path or directory pattern to the `paths` array. Scope the
pattern to the minimum necessary path:

```toml
[allowlist]
paths = [
  # ... existing entries ...

  # (a) Matches placeholder client_secret in OIDC Gherkin feature files.
  # (b) Synthetic value used against a local Dex test instance only —
  #     never a real OIDC provider.
  # (c) gateway/features/auth.feature
  '''gateway/features/auth\.feature''',
]
```

Each entry MUST have a comment stating:

- (a) what the pattern matches
- (b) why the matched value is not a real secret
- (c) which file or path it lives in

### Step 4 — Re-run the scan

```bash
scripts/scan-secrets.sh --history
```

Verify that exit code is now 0 and the finding is suppressed.

### Step 5 — Commit the change

```bash
git add .gitleaks.toml
git commit -m "chore(security): allow-list <path> — <reason>"
```

---

## What to Do When --history Finds a Real Secret

If `--history` reports a finding that IS a real secret (API token, private
key, PSK, password):

1. Do NOT push to either public remote (GitHub or opencode).
2. Immediately rotate / revoke the secret at its provider.
3. Use the history-rewrite tooling from Story 8.1
   (`scripts/rewrite-coauthored-trailer.sh`) or `git filter-repo` to remove
   the secret from the full git history.
4. Re-run `scripts/scan-secrets.sh --history` after the rewrite.
5. Proceed to Story 8.10 only when the scan exits 0.

See `scripts/REWRITE_HISTORY_RUNBOOK.md` for the full history-rewrite
procedure.

---

## CI Integration

`scripts/scan-secrets.sh --ci` is the command invoked by both CI
pipelines (defined in Story 8.6):

- **GitHub Actions** — `.github/workflows/ci.yml`, `secret-scan` job.
- **GitLab CI** — `.gitlab-ci.yml`, `secret-scan` stage.

Nebu publishes to both `github.com/innoq/nebu` and
`gitlab.opencode.de/nebu/nebu-server` as a sovereign-OSS dual-host
setup, so the same scan must succeed on both pipelines.

The `--ci` mode:

- Runs a full history scan.
- Writes findings to `gitleaks-report.json` in the current directory
  (empty array `[]` when no findings are present).
- Exits 0 on a clean scan, 1 if findings are present.

`gitleaks-report.json` is listed in `.gitignore` and will never be
committed.

Both CI workflows upload `gitleaks-report.json` as a build artifact
for audit purposes.

---

## Quick Reference

```bash
# Full history scan (before public push)
scripts/scan-secrets.sh --history

# Staged-only scan (pre-push hook)
scripts/scan-secrets.sh --staged

# CI mode (used by GitHub Actions + GitLab CI, writes JSON report)
scripts/scan-secrets.sh --ci

# Show usage
scripts/scan-secrets.sh --help
```
