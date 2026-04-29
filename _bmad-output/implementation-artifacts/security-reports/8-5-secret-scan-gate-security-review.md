# Security Review — 8-5-secret-scan-gate-gitleaks-history-ci-integration — 2026-04-28

**Agent:** Kassandra
**Diff base:** `git diff --staged` (HEAD of `feature/github-readiness`)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7-1m`

## Executive Summary

Story 8.5 delivers the gitleaks gatekeeper for the public push (Story 8.10). The script surface is small, hardened by `set -euo pipefail`, takes no user-controlled arguments to the `gitleaks` binary, and uses `--redact` consistently — secret values do not reach stdout, stderr, or the JSON report. No CRITICAL or HIGH findings. Two MEDIUM findings concern the broad path allowlists (`_bmad/.*`, `_bmad-output/.*`) and a documentation defect in the pre-push-hook installation — neither is exploitable today but both can degrade the protection the story is meant to provide. The synthetic AWS Access Key in the test suite (`AKIAIOSFODNN7EXAMPLE`) is verified as the official AWS documentation example, not a real credential.

## Findings

### [MEDIUM] Allow-list paths `_bmad/.*` and `_bmad-output/.*` are broad enough to silently absorb a real secret

- **CWE / OWASP:** CWE-1100 (Insufficient Encapsulation of Sensitive Configuration) / A05:2021 Security Misconfiguration
- **Datei:** `.gitleaks.toml:55, .gitleaks.toml:61`
- **Beschreibung:** The allowlist paths `_bmad/.*` and `_bmad-output/.*` whitelist ALL token-shaped strings under those directories. The justification — "no real credentials are stored here" — is true today by convention, but is not enforced by tooling. `_bmad-output/` is the dumping ground for planning artifacts, story drafts, retrospectives, and review reports; over the lifetime of the project, a maintainer pasting a curl/Postman snippet from an OIDC test, an actual `.env` block from a debug session, or a real AWS-test-account key into a planning note would be silently swallowed by the very tool whose job is to catch it. The two patterns are also unanchored (no leading `^`) — they match anywhere in the path, so a hypothetical sibling directory like `vendor/_bmad-output-mirror/` would also be allow-listed.
- **Impact:** A real secret committed to a documentation file inside `_bmad/` or `_bmad-output/` would pass `--history`, pass the Story 8.10 gate, and land on `github.com/innoq/nebu` and `gitlab.opencode.de/nebu/nebu-server` — where it is permanently visible and indexable. Reputational risk if exploited, bounded probability today (low likelihood of misuse), MEDIUM overall — defense-in-depth gap, not an active exploit.
- **Empfehlung:** Two complementary tightenings:
  1. Anchor both patterns: `'''^_bmad/.*'''` and `'''^_bmad-output/.*'''` to prevent accidental subpath matches.
  2. Add a regex-level allowlist that explicitly suppresses ONLY documentation-shaped patterns (e.g., the `AKIAIOSFODNN7EXAMPLE` literal, `dummy-`, `placeholder-`, `example-`, `test-token-` prefixes) and remove the path-wide allowlist. The path-wide allowlist trades detection coverage for convenience; a narrowed regex-based allowlist preserves both. As a third line of defense, a pre-commit hook that grep-matches high-entropy strings in `_bmad-output/*.md` would catch what `.gitleaks.toml` deliberately ignores.
- **Referenz:** OWASP ASVS V14.2.1 (configuration hardening), CWE-1100

### [MEDIUM] Symlinking `scripts/scan-secrets.sh` directly to `.git/hooks/pre-push` produces a hook that fails on every push

- **CWE / OWASP:** CWE-1059 (Insufficient Technical Documentation)
- **Datei:** `scripts/SECRET_SCAN_RUNBOOK.md:69-77`
- **Beschreibung:** The Runbook's "Option A" instructs `ln -sf ../../scripts/scan-secrets.sh .git/hooks/pre-push`. Git invokes the pre-push hook with `<remote> <url>` as argv (per `githooks(5)`), not `--staged`. `scan-secrets.sh` dispatches on `$1`; `<remote>` does not match `--history|--staged|--ci`, so the catch-all `*)` arm prints usage and exits 1. **Every `git push` will be blocked with a usage message, regardless of whether secrets are present.** Option B (`cp` instead of `ln`) has the same defect. Only the third snippet (the dedicated wrapper that explicitly calls `"${REPO_ROOT}/scripts/scan-secrets.sh" --staged`) actually works. Maintainers who follow Option A will encounter spurious failures and likely either uninstall the hook or push with `--no-verify` — both leave the local protection layer absent.
- **Impact:** Loss of a defense-in-depth layer. The CI-side `--ci` gate still catches secrets on push to GitHub/GitLab, so the public push gate (Story 8.10) is not compromised. But the local pre-push hook is the first opportunity to catch a secret before it reaches a remote build log, and the broken instruction collapses that layer for any maintainer following the documented happy-path.
- **Empfehlung:** Remove Options A and B. Make the dedicated-hook-file snippet the single recommended path. Rename it to "the only supported installation method" and delete the symlink/copy options. Optionally, add `scripts/install-pre-push-hook.sh` that writes the wrapper to `.git/hooks/pre-push` and chmods it.
- **Referenz:** Git pre-push hook contract — https://git-scm.com/docs/githooks#_pre_push

### [LOW] `*/testdata/*` allowlist pattern matches anywhere in the tree

- **CWE / OWASP:** CWE-1100 / A05:2021
- **Datei:** `.gitleaks.toml:90`
- **Beschreibung:** The pattern `'''.*testdata/.*'''` allow-lists any directory containing the substring `testdata`. This is broader than the explicit `gateway/internal/auth/testdata/.*` entry that already covers the known case. Any future vendored dependency, third-party include, or generated path containing `testdata` (e.g., `vendor/.../testdata/`, `node_modules/.../testdata/`) would inherit the suppression — even if such a path contained a real credential checked in by a third party.
- **Impact:** Defense-in-depth gap. No exploit path today; the project does not vendor third-party code with `testdata` directories. Worth tightening before the project's vendor surface grows.
- **Empfehlung:** Remove the generic `*/testdata/*` line. Rely on the path-specific `gateway/internal/auth/testdata/.*` and `core/apps/.*/test/.*\.exs$` entries already present. Add specific testdata paths only when a concrete false positive forces the issue, with the same (a)(b)(c) justification comment.
- **Referenz:** Principle of least privilege applied to suppression rules.

### [LOW] `git push --no-verify` bypass is not documented in the Runbook

- **CWE / OWASP:** CWE-1059 (Insufficient Documentation of Bypass)
- **Datei:** `scripts/SECRET_SCAN_RUNBOOK.md` (entire file — gap, no specific line)
- **Beschreibung:** A maintainer with commit access can bypass the local pre-push hook with `git push --no-verify`. This is standard git behavior, not a defect of this story. However, the Runbook does not state explicitly that the hook is bypassable, that the CI gate is the authoritative gate, and that bypassing the local hook leaves only one safety net. Given the story's audit-trail framing ("Story 8.10 may not proceed unless `--history` exits 0"), the bypass path should be acknowledged in the Runbook so that compliance-relevant decisions about local-vs-CI gating are not based on the assumption that the local hook is unbypassable.
- **Impact:** Transparency gap, not an exploit. CI gate remains the binding constraint.
- **Empfehlung:** Add a "Bypass and Authoritative Gate" section to the Runbook stating: "The local pre-push hook is bypassable via `git push --no-verify`. The authoritative gate is the CI `--ci` job (Story 8.6) which runs on every push to `main` and `feature/*` and cannot be bypassed by client-side flags."
- **Referenz:** N/A — documentation hygiene.

### [INFO] `gitleaks-report.json` write target is CWD-relative — bounded path-traversal surface

- **CWE / OWASP:** CWE-22 (Path Traversal — informational only) / A01:2021
- **Datei:** `scripts/scan-secrets.sh:32`
- **Beschreibung:** The script writes its report to `./gitleaks-report.json` (CWD-relative, by design — the test harness depends on this). On a CI runner, the CWD is the checked-out PR workspace. A hostile PR could in principle commit a symlink at `gitleaks-report.json` pointing to a sensitive workspace file, then `gitleaks` would overwrite the symlink target via `--report-path`. In practice the runner workspace is ephemeral and contains nothing sensitive at scan time, and the artifact upload would only expose the runner's own scratch files. Listed for transparency only — no concrete attack path against Nebu's threat model.
- **Impact:** None today. Worth re-evaluating if CI ever gives `gitleaks` access to a path outside the workspace (e.g., a mounted secret).
- **Empfehlung:** No action. Optional hardening: `--report-path "$(pwd)/gitleaks-report.json"` and a `[ -L gitleaks-report.json ] && rm gitleaks-report.json` guard before writing.

### [INFO] `--redact` is consistently applied — JSON report does not contain plaintext secrets

- **CWE / OWASP:** Positive finding — counter to CWE-532 (Sensitive Info in Log)
- **Datei:** `scripts/scan-secrets.sh:72, 91, 113`
- **Beschreibung:** All three modes pass `--redact` to gitleaks. gitleaks replaces the matched secret value with `REDACTED` in stdout, stderr, and the JSON report. The build artifact uploaded by GitHub Actions / GitLab CI therefore contains finding metadata (file, line, rule ID) but **never the secret value**. This is the correct posture for an artifact retention path that may live for 90 days on a public CI host.
- **Impact:** Positive — no secret leakage via CI artifact channel.

### [INFO] Synthetic AWS key `AKIAIOSFODNN7EXAMPLE` verified as official AWS documentation example

- **CWE / OWASP:** N/A — verification only
- **Datei:** `scripts/scan-secrets.test.sh:172, 213, 257, 287, 457`
- **Beschreibung:** `AKIAIOSFODNN7EXAMPLE` is the canonical AWS documentation example key (https://docs.aws.amazon.com/IAM/latest/UserGuide/security-creds.html). It has the AKIA-prefix and 16 trailing alphanumerics required by gitleaks' default rule, but is publicly disclosed by AWS as a non-credential. Use in the test suite is appropriate.
- **Impact:** None — confirms the test fixture is a non-secret.

### [INFO] Custom AWS rule with lowered entropy threshold is correctly designed

- **CWE / OWASP:** N/A
- **Datei:** `.gitleaks.toml:32-37`
- **Beschreibung:** The custom rule deliberately bypasses the default entropy filter to ensure the AWS documentation example (which has low entropy by accident of containing repeated characters) is also caught. `secretGroup = 1` correctly identifies the captured key (group 1 is the key itself, not the prefix anchor `(?:^|[^A-Z0-9])`). With `--redact`, the matched key is replaced before output. The minor risk of false positives from documentation strings (e.g., `AKIAEXAMPLE...` in a markdown file) is bounded by the path allowlists and is acceptable for a tool whose job is high recall.
- **Impact:** None — design verified correct.

### [INFO] Exit code propagation is fail-safe

- **CWE / OWASP:** N/A
- **Datei:** `scripts/scan-secrets.sh:108-117`
- **Beschreibung:** `--ci` mode captures gitleaks exit codes via `|| exit_code=$?` and propagates the actual code. gitleaks exits 0 (clean), 1 (findings), 126/127 (binary/permission errors), 2 (config error). All non-zero codes propagate, which means a misconfigured or broken gitleaks fails the CI gate rather than passing it silently. Correct behavior.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | N/A — no DB code in diff |
| `reason` field on compliance access         | N/A — no compliance access path |
| Audit-log immutability                      | N/A — no audit code in diff |
| `instance_admin` notification (if in-scope) | N/A — no admin-notify path |
| OIDC token validation (`iss`/`aud`/`exp`)   | N/A — no OIDC code in diff |
| Matrix Power Level checks                   | N/A — no Matrix code in diff |
| No hardcoded secrets                        | ✅ — only synthetic AWS doc example, verified |
| TLS 1.3 enforcement                         | N/A — no TLS path in diff |
| AES-256-GCM correctness                     | N/A — no crypto in diff |
| Ed25519 verify-before-accept                | N/A — no signature path in diff |
| No secrets in logs / error messages         | ✅ — `--redact` consistently applied |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 2 |
| LOW       | 2 |
| INFO      | 4 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

The two MEDIUM findings (broad allowlist paths, broken pre-push-hook installation instructions) should be tracked as Story 8.5 follow-ups or rolled into Story 8.10's pre-push checklist. They do not block the Story 8.5 commit and do not invalidate the Story 8.10 gate, because:

- The CI-side `--ci` job (Story 8.6) is the authoritative gate; it cannot be bypassed by `--no-verify` or by allow-list scope.
- Today there are no real secrets in `_bmad/` or `_bmad-output/` — verified by running `--history` against the current tree (covered by AC 4).
- The pre-push-hook documentation defect is a usability issue that degrades a defense-in-depth layer; the primary detection layer (CI) is unaffected.

Recommended follow-up: a small Story 8.5.1 or a checklist item in Story 8.10 to (a) anchor the `_bmad*` allowlist paths, (b) replace path-wide BMAD allowlisting with a documentation-pattern regex allowlist, (c) fix the Runbook to recommend only the dedicated-wrapper hook installation, and (d) add the `--no-verify` bypass acknowledgment.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
