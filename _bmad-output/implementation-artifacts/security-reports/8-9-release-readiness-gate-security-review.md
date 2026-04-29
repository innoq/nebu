# Security Review -- Story 8.9: Release-Readiness-Gate Master Script -- 2026-04-29

**Agent:** Kassandra
**Diff base:** `git diff --staged` on branch `feature/github-readiness`
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=claude-opus-4-7[1m]`

## Executive Summary

The release-readiness-gate is a Bash aggregator with no attacker-controlled inputs and no network calls. The diff stages five files plus two metadata updates: a 264-line master gate, a 527-line acceptance harness, a 6-step manual push checklist, an audit index for the three Epic 8 Kassandra reports, and a `.gitignore` entry for the JSON report. No application code, no migrations, no auth or crypto changes. The single structural concern is a documented behavior, not a bug: `--quick` mode skips the history secret-scan but still reports `Overall: PASS`. The Public-Push-Checklist correctly mandates `--all` for the pre-push run, so the surface area for a false-positive is governance, not script logic. No CRITICAL or HIGH findings.

## Findings

### [MEDIUM] `--quick` mode reports `Overall: PASS` while skipping the only history-level secret scan

- **CWE / OWASP:** CWE-754 (Improper Check for Unusual or Exceptional Conditions)
- **Datei:** `scripts/release-readiness-gate.sh:225-239`
- **Beschreibung:** When invoked with `--quick`, the gate calls `skip_suite "scan-secrets.sh --history"` which records `SKIP` (not `FAIL`). The aggregation at line 237 sets `overall_status="FAIL"` only when `FAIL_COUNT -gt 0`; SKIP is not counted. A `--quick` run thus prints `Overall: PASS` even though the only check that scans full git history for committed secrets was bypassed. The PUBLIC_PUSH_CHECKLIST Step 1 correctly mandates `--all`, but the gate script does not refuse to print PASS in `--quick` mode -- relying entirely on the maintainer reading the SKIP line.
- **Impact:** A maintainer who runs `--quick` for "fast iteration" and sees `Overall: PASS` may misread it as "ready to push." If the gate is the last technical control before a public force-push, a false sense of completeness could ship a repository whose history was never scanned. Bounded by the maintainer reading the summary -- not a runtime exploit path, defense-in-depth gap.
- **Empfehlung:** Either (a) print a banner in `--quick` mode immediately above the `Overall:` line stating `WARNING: --quick skipped scan-secrets.sh --history -- DO NOT use --quick for pre-public-push verification.`, or (b) introduce a distinct overall token like `Overall: PASS-PARTIAL` when `SKIP_COUNT > 0`, reserving plain `PASS` for the `--all`/`--report` paths. Option (a) is the smaller change and aligns with existing PUBLIC_PUSH_CHECKLIST.md Step 1 wording.
- **Referenz:** OWASP ASVS V14.2.5 (Security relevant logging), NIST SP 800-53 AU-12 (Audit generation completeness)

### [MEDIUM] Pre-flight TOCTOU between sub-script existence check and invocation

- **CWE / OWASP:** CWE-367 (Time-of-check Time-of-use Race Condition)
- **Datei:** `scripts/release-readiness-gate.sh:59-86, 211-229`
- **Beschreibung:** `preflight_check()` validates that all seven sub-scripts exist and are executable. Each `run_suite` invocation then re-resolves the sub-script via `bash "${REPO_ROOT}/scripts/<name>"`. Between the `[[ -f ... ]] && [[ -x ... ]]` check and the `bash` exec there is a window in which a sub-script could be deleted, replaced, or made non-executable. In Nebu's threat model the maintainer's local filesystem is trusted; an attacker with write access could simply modify `release-readiness-gate.sh` itself and bypass the entire gate -- so the TOCTOU window is not a usable attack path.
- **Impact:** Defense-in-depth gap. No realistic exploit path because the prerequisite (write access to `scripts/`) defeats the gate entirely. Documented as MEDIUM rather than LOW because Story 8.9 is the master release gate and the question of sub-script integrity was raised explicitly in the security brief.
- **Empfehlung:** Optional hardening: read each sub-script into a variable once during pre-flight (or compute and check a SHA-256 of the seven scripts), then invoke from the captured/verified copy. For MVP this is overkill -- the local-trust model handles it. Recommend deferring with the explicit note that sub-script integrity is governed by git history (signed commits, branch-protection rules from Story 8.10) rather than by the gate.
- **Referenz:** OWASP ASVS V8.3.2 (Atomic operations), CWE-367

### [LOW] Test 6 leaves `release-readiness-report.json` in REPO_ROOT until cleanup trap fires

- **CWE / OWASP:** CWE-377 (Insecure Temporary File) -- adapted; report file, not tempfile
- **Datei:** `scripts/verify-release-readiness.sh:286-293`
- **Beschreibung:** Test 6 invokes `bash "${GATE_SCRIPT}" --report` which writes `release-readiness-report.json` to `REPO_ROOT`. The test registers it in `TEMPFILES` for trap-cleanup. If the harness is killed with `kill -9` (no trap fires) or the developer ctrl-c's twice, the file is left in REPO_ROOT. The `.gitignore` entry prevents accidental commit, but a stale file from a previous run is read by the next `--report` because the gate writes with `"w"` (truncating) -- no real corruption risk, just left-over state.
- **Impact:** Cosmetic. The JSON contains only suite names, statuses (PASS/FAIL/SKIP), and exit codes -- no secrets, no environment data. The mandatory `.gitignore` entry (AC9, Test 11) prevents history pollution.
- **Empfehlung:** Optionally `rm -f "${report_file}"` before registering in TEMPFILES (Test 6 already does this at line 291). Consider adding `release-readiness-report.json` to `make clean` or a `scripts/clean.sh` if one exists. Acceptable to defer.
- **Referenz:** Hygiene; no compliance impact.

### [LOW] Sub-script paths constructed via string concatenation could be brittle if `REPO_ROOT` resolves unexpectedly

- **CWE / OWASP:** CWE-426 (Untrusted Search Path) -- adapted
- **Datei:** `scripts/release-readiness-gate.sh:24-26`
- **Beschreibung:** `REPO_ROOT` is computed via `cd "${SCRIPT_DIR}/.."` where `SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)`. If the gate script is symlinked from outside the repo (e.g., a maintainer's `~/bin` symlink), `BASH_SOURCE[0]` resolves to the symlink target's location -- so `REPO_ROOT` resolves correctly. However, if a maintainer deliberately copies (not symlinks) the gate script to a foreign location, `REPO_ROOT` would be `<foreign>/scripts/..`, and pre-flight would fail loudly (which is the intended behavior). No silent misbehavior found; documenting as LOW because the prompt asked about path construction.
- **Impact:** None. Deliberately misplaced gate script fails loudly via pre-flight.
- **Empfehlung:** No action. The fail-loud behavior is correct.
- **Referenz:** Operational hygiene.

### [INFO] No `--skip-X`, env-var bypass, or quiet mode -- gate cannot be silently disabled

- **Beschreibung:** Reviewed all 264 lines of `release-readiness-gate.sh` for bypass paths. The dispatch (`case "${MODE}"`) accepts only `--all|--quick|--report`; any other argument exits 1 with a usage error. There is no env-var like `NEBU_SKIP_GATE`, no `--skip-suite=` argument, no `if [[ -n "${CI:-}" ]]; then return 0; fi` shortcut. Pre-flight is unconditional. The only documented "fast path" is `--quick`, which is honestly named and only skips the history scan. Positive finding.
- **Empfehlung:** None.

### [INFO] No external network calls in the gate itself

- **Beschreibung:** The gate invokes only local Bash, `python3` (system interpreter for JSON serialization), and the seven local sub-scripts. No `curl`, `wget`, `gh`, `glab`, or `git fetch` in the master gate. The `gh`/`glab` CLI usage is in `setup-repo-metadata.sh` (Story 8.8), invoked manually as Step 5 of the PUBLIC_PUSH_CHECKLIST -- outside this story's scope. No supply-chain surface introduced by this story.
- **Empfehlung:** None.

### [INFO] JSON-report payload contains no secrets, no environment variables, no host metadata

- **Datei:** `scripts/release-readiness-gate.sh:138-196`
- **Beschreibung:** `release-readiness-report.json` fields: `date` (ISO-8601 UTC), `mode` (literal `all`/`quick`/`report`), `overall_status` (`PASS`/`FAIL`), `suites[].name` (hardcoded sub-script basenames), `suites[].status` (`PASS`/`FAIL`/`SKIP`), `suites[].exit_code` (integer). No hostname, no `${USER}`, no env vars, no command output, no stack traces. Even if accidentally committed (mitigated by `.gitignore` per AC9), the file leaks nothing sensitive.
- **Empfehlung:** None.

### [INFO] Force-push step in PUBLIC_PUSH_CHECKLIST.md correctly mandates `--force-with-lease`

- **Datei:** `scripts/PUBLIC_PUSH_CHECKLIST.md:68-87`
- **Beschreibung:** Step 4 explicitly states `Use \`--force-with-lease\` (never plain \`--force\`)` and shows `git push --force-with-lease="main:${EXPECTED_MAIN}" origin main` for both remotes. This addresses the Kassandra 8.1 MEDIUM-2 finding (per INDEX.md). The `EXPECTED_MAIN=$(git rev-parse origin/main)` after `git fetch` is the correct lease pattern. Positive finding.
- **Empfehlung:** None.

### [INFO] INDEX.md is git-tracked -- audit-trail tampering visible in history

- **Datei:** `_bmad-output/implementation-artifacts/security-reports/INDEX.md`
- **Beschreibung:** The audit index lives in the staged diff and will be committed. The three referenced Kassandra reports (`8-1-...`, `8-5-...`, `8-6-...`) are all present in `_bmad-output/implementation-artifacts/security-reports/` and are git-tracked. Any retroactive edit to suppress findings would be visible via `git log -p` on the report file. The header explicitly states this is a point-in-time index and that SEC Gate 2 in Story 8.10 supersedes it as the authoritative epic-end audit. Audit integrity rests on git history, branch protection (Story 8.10), and signed commits -- aligned with Nebu's documented audit model.
- **Empfehlung:** None.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | n/a    |
| `reason` field on compliance access         | n/a    |
| Audit-log immutability                      | ✅      |
| `instance_admin` notification (if in-scope) | n/a    |
| OIDC token validation (`iss`/`aud`/`exp`)   | n/a    |
| Matrix Power Level checks                   | n/a    |
| No hardcoded secrets                        | ✅      |
| TLS 1.3 enforcement                         | n/a    |
| AES-256-GCM correctness                     | n/a    |
| Ed25519 verify-before-accept                | n/a    |
| No secrets in logs / error messages         | ✅      |

Most invariants are not applicable -- this story ships only Bash scripts and Markdown documents, no application code, no DB migrations, no auth/crypto code, no Matrix handlers. The applicable invariants pass:

- **Audit-log immutability:** INDEX.md and the three referenced Kassandra reports are git-tracked; future edits leave a history trail. Branch protection (Story 8.10) will prevent force-push to `main`.
- **No hardcoded secrets:** Reviewed all five new files; no API keys, tokens, or credentials. The gate does not read or print env vars.
- **No secrets in logs:** Gate output prints script names, statuses, exit codes. The JSON report contains the same fields. No env-var dumping, no `set -x`, no command-output capture into the report.

## Dependency Scan

Not applicable -- diff contains no `go.sum` / `go.mod` / `mix.lock` / `mix.exs` / `rebar.lock` changes. The gate uses only system `python3` (no pip dependencies) and `shellcheck` (optional, SKIP-fallback). No supply-chain surface introduced.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 2 |
| LOW       | 2 |
| INFO      | 4 |

## Pipeline Decision

**CLEAN** -- no CRITICAL / HIGH findings. Pipeline may proceed to commit and merge.

The two MEDIUM findings are defense-in-depth observations:
- M-1 (`--quick` masquerading as PASS) is governance-mitigated via PUBLIC_PUSH_CHECKLIST Step 1; recommend a one-line banner in `--quick` mode as cheap follow-up.
- M-2 (TOCTOU pre-flight) is not exploitable in Nebu's local-trust model; recommend deferring with explicit risk acceptance.

Both should be tracked in sprint-status as known deferrals. Neither blocks Story 8.10. The mandatory SEC Gate 2 epic-end Kassandra review in Story 8.10 will revisit these against the full Epic 8 diff.

---

*Generated by Kassandra -- BMAD Security Review Agent. This report is an immutable audit artifact -- do not edit retrospectively; create a new review if re-analysis is required.*
