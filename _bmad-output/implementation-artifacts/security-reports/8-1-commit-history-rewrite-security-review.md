# Security Review — Story 8.1: Commit-History-Rewrite (Co-Authored-By Trailer Removal Tooling) — 2026-04-28

**Agent:** Kassandra
**Diff base:** `git diff --staged` on branch `feature/github-readiness`
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=claude-opus-4-7[1m]`

## Executive Summary

Story 8.1 delivers tooling only — a Bash script (`scripts/rewrite-coauthored-trailer.sh`), 11-test sandbox suite, and a manual runbook. The script does not execute any rewrite or push against the host repo by itself; the actual `git filter-repo` invocation is gated behind `--run` and runs only on the currently-checked-out branch. The hardcoded Python regex callback receives commit-message bytes from `git filter-repo` (no user-controlled string interpolation), and shell-quoting is correct throughout. No CRITICAL or HIGH findings. Two MEDIUM findings (missing repo-identity guard, runbook uses bare `--force` instead of `--force-with-lease`) and several LOW/INFO observations are documented below.

## Findings

### [MEDIUM] No repo-identity guard before `git filter-repo --force`

- **CWE / OWASP:** CWE-862 (Missing Authorization) / A01:2021 — Broken Access Control (operator-confusion variant)
- **Datei:** `scripts/rewrite-coauthored-trailer.sh:97-141` (function `mode_run`)
- **Beschreibung:** `mode_run` performs `git filter-repo --force` against the current repo without verifying that the repo is actually the Nebu repo. The pre-flight checks cover working-tree cleanliness, branch detachment, duplicate backup branches, and `git-filter-repo` availability — but not repo identity. If a maintainer accidentally runs the script while `cd`'d into a different (unrelated) git repository (e.g., a sibling clone, a fresh test repo, or a vendored submodule), the script will rewrite that repo's history without warning. The pre-existing backup branch enables local rollback, but if the maintainer follows the runbook's force-push step on the wrong remote, recovery becomes harder.
- **Impact:** Operator error during a manual high-stakes operation. The blast radius is bounded — the wrong-repo scenario produces a backup branch and the rewrite is local-only until force-push — but the irreversible nature of `git filter-repo --force` raises the cost of a single mis-`cd`.
- **Empfehlung:** Add a pre-flight that fails closed unless the repo is explicitly the expected Nebu repo. Two complementary options:
  1. Check `git remote get-url origin` against an expected URL pattern (e.g., contains `nebu-chat`); fail if no match.
  2. Require an explicit `--i-confirm-repo=<name>` flag, or check for a sentinel file (e.g., `git ls-files --error-unmatch _bmad-output/planning-artifacts/architecture.md`) that uniquely identifies the Nebu repo.
- **Referenz:** OWASP ASVS V1.4.1 (trust boundaries), defense-in-depth.

### [MEDIUM] Runbook uses `git push --force` instead of `--force-with-lease`

- **CWE / OWASP:** CWE-840 (Business Logic Errors) / supply-chain hygiene
- **Datei:** `scripts/REWRITE_HISTORY_RUNBOOK.md:111-115`, `:172`
- **Beschreibung:** The runbook instructs the maintainer to execute `git push --force origin main` and `git push --force origin feature/github-readiness`. Plain `--force` overwrites remote refs unconditionally — including any commits pushed to the remote between the local fetch and the rewrite (e.g., a CI bot, another maintainer, an automated tool). `--force-with-lease[=<ref>:<expected-sha>]` instead refuses the push if the remote moved, which is the standard safe alternative for history-rewrite operations.
- **Impact:** Silent overwrite of remote commits that arrived after the most recent local fetch. In the documented single-maintainer scenario the residual risk is low, but the runbook is the canonical reference for the operation and should model the safer pattern. The remote-rollback step (line 172) shares the same issue.
- **Empfehlung:** Replace `git push --force origin <branch>` with `git push --force-with-lease=<branch>:$(git rev-parse origin/<branch>) origin <branch>` (or simply `--force-with-lease` after a fresh `git fetch`). Add a line to the runbook explaining that `--force-with-lease` is the required form. Repeat for the rollback example on line 172.
- **Referenz:** Git documentation `git-push(1)` — `--force-with-lease`. Also covered in OWASP DevSecOps practices.

### [LOW] Trailer regex can over-match if a paragraph happens to start with "Co-Authored-By: Claude"

- **CWE / OWASP:** CWE-185 (Incorrect Regular Expression) — bounded impact
- **Datei:** `scripts/rewrite-coauthored-trailer.sh:140`
- **Beschreibung:** The regex `(\n\nCo-Authored-By: Claude[^\n]*)(\nCo-Authored-By: Claude[^\n]*)*` correctly anchors to a paragraph break (`\n\n`) and bounds matches to one line via `[^\n]*`. However, if a commit's body intentionally contains a paragraph that begins with the literal string `Co-Authored-By: Claude ...` (e.g., a quoted older commit message or an explanatory paragraph), the entire line will be removed. There is no validation step that compares the per-commit message before/after rewrite to confirm only trailer lines were affected.
- **Impact:** Unintentional message-content loss, only on commits whose body paragraphs literally begin with `Co-Authored-By: Claude`. In practice this is unlikely in the Nebu history (the staged diff itself is the audit), but the regex is the load-bearing logic — it cannot be unwound after force-push.
- **Empfehlung:** Two complementary mitigations:
  1. Before `--run`, dump `git log --format=%B HEAD` into a temp file and grep for `Co-Authored-By: Claude` lines whose surrounding context is *not* end-of-message — fail loudly if any non-trailer match is found, requiring manual override (e.g., `--force-non-trailer-removal`).
  2. After `--run`, add a `[content-preserved]` sub-check to `--verify` that re-runs `git log --format=%B` on both `HEAD` and `backup_branch`, strips known-trailer lines from both, and asserts byte-equality of the remaining message bodies. Currently `--verify` checks metadata but not message-body integrity beyond trailer count.
- **Referenz:** Story 8.1 risk table line "Irreversibler Datenverlust" — the mitigation listed there ("Regex exakt auf `\nCo-Authored-By: Claude[^\n]*` beschränken, mit Assertion auf Vorher-/Nachher-Diff") is partially implemented; the assertion is not.

### [LOW] Runbook does not warn that backup branch may carry historical secrets

- **CWE / OWASP:** CWE-200 (Information Exposure) — operational hygiene
- **Datei:** `scripts/REWRITE_HISTORY_RUNBOOK.md:135-145` (Step 7: Cleanup)
- **Beschreibung:** Story 8.5 (Secret Scan Gate) is documented as running *after* this rewrite. If the pre-rewrite history contains a leaked secret, the rewrite of `HEAD` does not remove it from the `backup/pre-history-rewrite-<TIMESTAMP>` branch — the entire point of the backup is to retain pre-rewrite content. The runbook's cleanup step (delete the backup branch only after Story 8.10) creates a window where a local backup branch carries secrets that have already been scrubbed from `HEAD`. If the maintainer accidentally pushes the backup branch (`git push origin --all` or similar), the secret leaks.
- **Impact:** Local-only risk in the documented workflow; becomes real only if the maintainer performs an unscoped `git push --all` or `git push --mirror`. The runbook explicitly tells the maintainer to push only specific branches, so this is largely defense-in-depth.
- **Empfehlung:** Add a one-liner near Step 5 (Force-Push) and Step 7 (Cleanup): "Never use `git push --all` or `git push --mirror` while a `backup/pre-history-rewrite-*` branch exists locally — push only specific branches by name." Optionally also: "Run Story 8.5 (Secret Scan) against `HEAD` *before* deleting the backup, then re-scan after deletion to confirm no orphan blobs remain."
- **Referenz:** GitHub guidance on history rewriting — backup branches must not be pushed.

### [INFO] Script and tests are well-isolated; no host-repo mutation paths in tests

- **Datei:** `scripts/rewrite-coauthored-trailer.test.sh:32-39, 51-62`
- **Beschreibung:** The test file uses `mktemp -d` for sandbox creation, registers each sandbox in a `_SANDBOX_DIRS` array, and installs an `EXIT` trap (`cleanup_all`) that runs even on `set -e` aborts. All `git` invocations in tests use `git -C "$SANDBOX"` to scope to the sandbox, and the few `cd "$SANDBOX"` calls happen in subshells (`$(cd "$SANDBOX" && ...)`) that do not affect the parent shell's working directory. Sandboxes are created with default `mktemp` permissions (mode 0700), and `rm -rf` is gated on `[[ -d "$d" ]]` to avoid `rm -rf ""`. No tests touch any path under the project root. This is a positive finding worth preserving.
- **Empfehlung:** None. Continue this pattern in future Bash test scripts.

### [INFO] Python message-callback is a static byte-string regex — not vulnerable to code injection

- **Datei:** `scripts/rewrite-coauthored-trailer.sh:140`
- **Beschreibung:** The `--message-callback` argument to `git filter-repo` is a hardcoded single-quoted Bash string (no `$variable` interpolation) that receives the commit-message bytes via the `message` parameter (provided by `git filter-repo` itself). The regex is `rb"..."` (a Python byte-string literal), so the substitution operates on bytes — Unicode normalization tricks, mixed-encoding payloads, and embedded NUL bytes all stay within the regex match domain and cannot escape into Python `eval`/`exec` (none are used). A malicious commit message cannot inject Python code through this path. The branch name flows into `--refs "refs/heads/${current_branch}"` — fully double-quoted; git itself rejects branch names beginning with `-` and containing `..`, so argument-parser smuggling is also blocked.
- **Empfehlung:** None.

### [INFO] No filesystem writes outside `.git/config` and the backup branch ref

- **Datei:** `scripts/rewrite-coauthored-trailer.sh:124-125`
- **Beschreibung:** `--run` writes two repo-local git-config keys (`filter-rewrite.backup-branch`, `filter-rewrite.orig-count`) and creates one branch. No writes to `~/.gitconfig`, `/tmp`, or `~/.cache`. Environment variables read from the parent shell are limited to those `git` itself consumes (`GIT_DIR`, `GIT_WORK_TREE`). No secrets are persisted.
- **Empfehlung:** None.

## Nebu Invariants Check

Most invariants are not applicable to this story (Bash tooling for one-time history rewrite, no runtime/server code). Verified the ones that do touch the diff:

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | n/a — no DB / SQL / Ecto changes |
| `reason` field on compliance access         | n/a — no compliance API changes |
| Audit-log immutability                      | n/a — no audit-log table changes |
| `instance_admin` notification (if in-scope) | n/a — no notification flow |
| OIDC token validation (`iss`/`aud`/`exp`)   | n/a — no auth code changes |
| Matrix Power Level checks                   | n/a — no room-state mutations |
| No hardcoded secrets                        | ✅ — no secrets in script, tests, runbook |
| TLS 1.3 enforcement                         | n/a — no TLS configuration |
| AES-256-GCM correctness                     | n/a — no symmetric-crypto changes |
| Ed25519 verify-before-accept                | n/a — no signature-handling code |
| No secrets in logs / error messages         | ✅ — no log lines emit secrets; SHAs are public, not secret |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 2 |
| LOW       | 2 |
| INFO      | 2 |

## Pipeline Decision

**CLEAN** — no CRITICAL or HIGH findings. Pipeline may proceed.

The two MEDIUM findings (repo-identity guard, `--force-with-lease`) should be tracked and addressed before the actual rewrite is executed against `main` (which is a separate, manual operation per Story 8.1's scope clarification — the script ships, the rewrite does not run yet). They are defense-in-depth improvements to a high-stakes manual operation, not exploit paths. The LOW findings are operational hygiene notes for the runbook author.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
