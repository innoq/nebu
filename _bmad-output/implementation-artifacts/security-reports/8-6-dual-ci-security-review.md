# Security Review — Story 8.6 (Dual CI: GitHub Actions + GitLab CI)

- **Reviewer:** Kassandra (bmad-security-review)
- **Date:** 2026-04-28
- **Branch:** feature/github-readiness
- **Story:** `_bmad-output/implementation-artifacts/8-6-ci-migration-gitlab-ci-github-actions.md`
- **Frontmatter:** `security_review: required`
- **Scope:** Staged diff (`git diff --staged`)
- **Frameworks applied (as lenses):** OWASP Top 10 (A06 Vulnerable & Outdated Components, A08 Software & Data Integrity), CWE-829 (Inclusion of Functionality from Untrusted Source), CWE-494 (Download of Code Without Integrity Check), CWE-78 (OS Command Injection), CWE-200 (Information Exposure), STRIDE (Tampering, Elevation of Privilege via supply-chain).

## Files reviewed

| File | LOC delta | Type |
|---|---|---|
| `.github/workflows/ci.yml` | +120 | NEW |
| `.gitlab-ci.yml` | +71/-3 | MODIFIED |
| `scripts/ci-local.sh` | +163 | NEW |
| `scripts/verify-ci-config.sh` | +795 | NEW |
| `_bmad-output/planning-artifacts/badges-spec.md` | +59 | NEW (docs) |
| `_bmad-output/implementation-artifacts/8-6-...md` | +496 | story doc |
| `_bmad-output/implementation-artifacts/sprint-status-epic-8.yaml` | +3/-1 | tracking |

## Classification

**Overall: HIGH** — one HIGH supply-chain finding (gitleaks tarball without integrity verification) that affects both CI platforms identically. The remaining footprint is clean: action pinning is correctly enforced and SHAs were verified against upstream, default workflow permissions are read-only, no command-injection sinks were found, and no secret-leak patterns (`set -x`, `env` dumps) are present.

| Severity | Count |
|---|---|
| CRITICAL | 0 |
| HIGH | 1 |
| MEDIUM | 3 |
| LOW | 2 |
| INFO | 3 |

## Recommendation

**Proceed with conditions.** The HIGH finding (H-1) should be fixed before the public push (Story 8.10) because it is an active supply-chain weakness that runs on every CI invocation. The MEDIUMs are quality-of-defense items that should be addressed as follow-ups (not blockers for Story 8.6 merge).

`blocking_severity: HIGH` is policy. **H-1 blocks merge to main.** Either remediate now or accept-as-risk with explicit written justification recorded in the story.

---

## Findings

### H-1 — Gitleaks tarball downloaded without integrity verification (HIGH)

**Files & lines:**
- `.github/workflows/ci.yml:96–99`
- `.gitlab-ci.yml:118–121`

**What:**
```yaml
- name: Install gitleaks
  run: |
    curl -sSfL \
      "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz" \
      | tar -xz -C /usr/local/bin gitleaks
```
The same pattern is mirrored in `.gitlab-ci.yml`. The tarball is fetched over HTTPS and piped directly into `tar -xz`. There is no `sha256sum -c`, no `cosign verify-blob`, and no GPG verification step. The extracted binary is then executed against the entire repository.

**Why it matters:**
- CWE-494 (Download of Code Without Integrity Check) and CWE-829 (Inclusion of Functionality from Untrusted Source).
- HTTPS protects the channel, not the content. Compromise of the gitleaks GitHub release artifact, a malicious release published under a stolen maintainer token, or a TUF/CDN cache-poisoning event would all yield arbitrary code execution inside CI on every push to `main` or `feature/**` branches.
- The blast radius includes whatever the runner can reach: source tree, env vars from any subsequent step (currently zero secrets injected into this job, which limits impact today), and the upload-artifact step.
- This is exactly the threat model that motivated the SHA pinning of `actions/*` elsewhere in the same workflow — and is left wide open here.

**Why this is HIGH and not CRITICAL:**
- The `secret-scan` job currently receives no secrets and has only `contents: read`. A trojaned gitleaks would not directly exfiltrate signing keys or cloud credentials in this workflow today.
- Compromising the upstream gitleaks release is non-trivial; this is a latent risk, not an active exploit path.
- It would, however, be a clear security finding in any external audit of a sovereign-OSS project that ships its own CI templates.

**Remediation (in order of robustness):**
1. **Pin checksum in CI YAML:** download the tarball + `gitleaks_<v>_checksums.txt` from the same release, verify with `sha256sum --check` before extracting. The release page already publishes signed checksums.
2. **Use the official gitleaks GitHub Action pinned to a SHA** (e.g. `gitleaks/gitleaks-action@<40-hex>`) — same supply-chain assumptions as the rest of the workflow and removes the bespoke install step.
3. **Bake gitleaks into the GitLab `ci-go` image** (`docker/Dockerfile.ci.go`) and reference the pinned image. The story's AC4 already prescribes this for GitLab. Today the GitLab job re-downloads gitleaks at runtime instead, defeating that intent.

CWE: 494, 829. OWASP: A08 (Integrity Failures).

---

### M-1 — `ci-local.sh` mounts the repo writable into a privileged user container (MEDIUM)

**File & line:** `scripts/ci-local.sh:54–59`

```sh
docker run --rm \
    -v "${REPO_ROOT}:/workspace" \
    -w /workspace \
    "$@"
```

**What:** The host repo is bind-mounted into the container without `:ro`, the container default-user is root (golang/elixir alpine images run as root), and no capability dropping (`--cap-drop=ALL`) or `--security-opt=no-new-privileges` is applied.

**Why it matters:**
- Any malicious dependency pulled in `mix deps.get` or `go test -race` (which can compile arbitrary Go code via `//go:generate`-style hooks if a test is hostile) executes as root in the container with full write access to the developer's repo and any sibling files reachable via `/workspace/..` traversal (none here, since the bind is `${REPO_ROOT}` exactly, but new flags or arg additions could regress this).
- Container escapes via Linux capabilities are mitigated by Docker's default seccomp/AppArmor profile, but defense-in-depth dictates `--cap-drop=ALL` for a build-only container that needs no special capabilities.
- This is a developer-machine concern, not a CI runner concern — `ci-local.sh` does not run on GitHub or GitLab.

**Why MEDIUM, not HIGH:** Compromise requires a hostile dependency already trusted by the developer. The script is also not the production CI path. Still, it is the file developers will copy, edit, and reuse; hardening the template now is cheap.

**Remediation:**
- Add `--cap-drop=ALL --security-opt=no-new-privileges`.
- Consider `--user "$(id -u):$(id -g)"` so files written by the container land with the developer's UID/GID instead of root-owned (already a usability papercut).
- Document that mounted repos are writable by design (so changes to vendored deps survive the container exit).

CWE: 250 (Execution with Unnecessary Privileges).

---

### M-2 — Cache key on `feature/**` branches can poison `main` cache restore (MEDIUM)

**Files & lines:**
- `.github/workflows/ci.yml:82–84`
- `.gitlab-ci.yml:71–73`, `87–90`, `101–107`

**What:**
- GitHub: `key: mix-${{ hashFiles('core/mix.lock') }}` with `restore-keys: mix-` — any branch with the same `mix.lock` writes a cache entry that `main` can restore from. More dangerously, the `restore-keys: mix-` prefix match means a stale or malicious feature-branch cache wins when an exact key match fails.
- GitLab: `key: "go-${CI_COMMIT_REF_SLUG}"` is per-branch (good), but `paths: /root/go/pkg/mod` writes there from any feature branch. Cross-branch poisoning is not directly possible with branch-scoped keys, so this is GitHub-specific.

**Why it matters:** A feature branch can drop a tampered compiled artifact (e.g. corrupted `_build/` ELF, or modified vendored Go binary) into the cache, and a later `main` build that gets a `restore-keys: mix-` partial match will restore and execute it. This is the classic GitHub Actions cache-poisoning vector documented by GitHub Security Lab and TrailOfBits.

**Why MEDIUM, not HIGH:** Exploiting this requires a branch with write access to the repo (i.e. an insider or a compromised maintainer account), which is the same blast radius as committing malicious code directly. But it bypasses code review because cache writes are invisible to reviewers.

**Remediation:**
- For `main` builds, drop `restore-keys` entirely and require an exact-key match — accept the cold-cache penalty for canonical builds.
- Or: scope `restore-keys` to the branch, e.g. `restore-keys: mix-${{ github.ref }}-`.
- See GitHub's own guidance: <https://github.blog/security/supply-chain-security/four-tips-to-keep-your-github-actions-workflows-secure/>.

CWE: 349 (Acceptance of Extraneous Untrusted Data).

---

### M-3 — `.github/workflows/ci.yml` has no `concurrency:` block; no timeout on jobs (MEDIUM)

**File:** `.github/workflows/ci.yml` (whole file)

**What:** No `concurrency:` group is defined; rapid pushes to the same branch run in parallel rather than cancelling the older run. No `timeout-minutes:` is set on jobs (default is 360 minutes / 6 hours).

**Why it matters:**
- A hostile or accidentally-runaway test (`go test` with an infinite loop) can burn a runner's full quota. For public OSS this is a denial-of-budget vector.
- Parallel runs on the same branch waste runner minutes and increase cache-poisoning surface (M-2).

**Why MEDIUM:** Operational hygiene rather than a clean exploit path. Still industry-standard for any public CI.

**Remediation:**
```yaml
concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true
```
And add `timeout-minutes: 15` (or per-job appropriate values).

CWE: 400 (Uncontrolled Resource Consumption).

---

### L-1 — `pr_target` event not included in triggers; no explicit guard against `pull_request_target` (LOW)

**File:** `.github/workflows/ci.yml:19–25`

**What:** Triggers are `push` (main, feature/**) and `pull_request`. `pull_request_target` is **not** present — which is correct. The note here is documentary: keep it that way. `pull_request_target` runs the workflow from the base branch with secrets and would be exploitable by hostile PR contents.

**Why LOW:** Nothing is wrong today. Recording the decision so future edits don't introduce `pull_request_target` for "convenience" without security review.

**Remediation:** Add a `# DO NOT use pull_request_target — runs with secrets against PR-controlled tree. See https://securitylab.github.com/research/github-actions-preventing-pwn-requests/` comment near the `on:` block.

---

### L-2 — `verify-ci-config.sh` test for SHA pinning accepts a 40-char SHA in any `uses:` line; does not verify the SHA matches the comment (LOW)

**File:** `scripts/verify-ci-config.sh:330–378` (`test_github_actions_pinned_to_sha`)

**What:** The test enforces that every `uses:` reference ends in `@<40-hex>`. It does **not** verify that the SHA actually corresponds to the version in the trailing `# v4.1.1` comment. A future PR could change `actions/checkout@b4ffde65...` to `actions/checkout@<SHA-of-malicious-fork>` while keeping the `# v4.1.1` comment, and this test would still pass.

**Why LOW:** SHA pinning is the primary defense; the comment-version drift is a secondary concern. Mitigated by code review (the diff would show a SHA change). The Kassandra audit explicitly verified all five SHAs against upstream GitHub APIs (see Verifications section below).

**Remediation (optional):** Add a test that calls `gh api repos/{owner}/{repo}/git/refs/tags/{version} --jq .object.sha` for each `uses:` reference and compares against the inline SHA. Run only in a job that has `gh` available; skip otherwise.

---

### INFO-1 — Action SHA verification (positive finding)

All five GitHub Actions in `.github/workflows/ci.yml` were verified against `api.github.com` upstream:

| Action | Comment version | Inline SHA | Upstream resolution | Verdict |
|---|---|---|---|---|
| `actions/checkout` | v4.1.1 | `b4ffde65f46336ab88eb53be808477a3936bae11` | tag v4.1.1 → commit `b4ffde65...` | **MATCH** |
| `actions/setup-go` | v5.0.1 | `cdcb36043654635271a94b9a6d1392de5bb323a7` | tag v5.0.1 → commit `cdcb3604...` | **MATCH** |
| `erlef/setup-beam` | v1.16.0 | `61e01a43a562a89bfc54c7f9a378ff67b03e4a21` | annotated tag v1.16.0 → commit `61e01a43...`, GPG-signed by paulo-ferraz-oliveira | **MATCH** |
| `actions/cache` | v4.0.2 | `0c45773b623bea8c8e75f6c82b208c3cf94ea4f9` | tag v4.0.2 → commit `0c45773b...` | **MATCH** |
| `actions/upload-artifact` | v4.3.1 | `5d5d22a31266ced268874388b861e4b58bb5c2f3` | tag v4.3.1 → commit `5d5d22a3...` | **MATCH** |

No fork-substitution or comment-drift attack present.

### INFO-2 — `permissions:` is correctly read-only (positive finding)

`.github/workflows/ci.yml:29–30` sets a global `permissions: contents: read`. No job overrides it with elevated scope. `GITHUB_TOKEN` reaches the runner with the minimum scope required to clone the repo. This eliminates the most common GitHub Actions privilege-escalation vectors.

### INFO-3 — No code-injection sinks via user-controlled GitHub context

Searched for `${{ github.head_ref }}`, `${{ github.event.pull_request.title }}`, `${{ github.event.head_commit.message }}` and similar patterns in `run:` blocks (CWE-78 / GitHub Actions script injection per Synacktiv 2021). **No matches.** GitLab `$CI_COMMIT_BRANCH`, `$CI_COMMIT_REF_SLUG` are used only inside YAML conditional `if:` rules (string equality) and as cache key suffixes (interpolated by GitLab itself, not by shell). No shell expansion of attacker-controlled context values exists.

---

## Nebu invariants check

| Invariant | Status | Note |
|---|---|---|
| OIDC validation | N/A | CI config does not touch auth code paths. |
| Compliance / DSGVO audit immutability | N/A | gitleaks-report.json artifact is per-run output, not an audit log. |
| Crypto primitives | N/A | No use of `:crypto` or new SQL migrations in this diff. |
| Matrix power-level enforcement | N/A | No room/permission code touched. |
| Secrets in logs | **OK** | No `set -x`, `env`, `cat .env`, or `echo $SECRET` patterns found. `$CI_REGISTRY_PASSWORD` is used only as a positional arg to `docker login -p` (line 35), which GitLab's job log scrubber does redact for masked variables. Confirm `CI_REGISTRY_PASSWORD` is set as masked + protected in the GitLab project settings before the GitLab pipeline runs against `gitlab.opencode.de/nebu/nebu-server` — this is a runtime configuration concern outside the diff. |
| Public-repo safety (`.github/workflows/` will be world-readable on push) | **OK** | No inline secrets in workflow files. Only references are to `${{ env.GO_VERSION }}` (public), `GITLEAKS_VERSION` (public). |

---

## Drift / dual-CI bypass risk

The two CI files are independently editable. AC2 (job topology parity) is enforced today by `verify-ci-config.sh` checking that both files contain the same five job names, but **not** that the jobs do equivalent work. A malicious or sloppy edit could, e.g., disable secret-scan in GitHub while leaving GitLab green — the verify script would still pass since the job *exists* under that name.

This is **out of scope for an 8.6 fix** (would require deeper semantic equivalence checks) but worth recording. Story 8.10 (branch protection) should make both `secret-scan` jobs Required Status Checks on `main`, and a future story should add a parity-of-`script:`-content check.

Severity: this is INFO for now (no exploit path until both CIs are public) but it should be a tracked risk for Epic 8 retrospective.

---

## Verification log

- Action SHAs: cross-referenced against `api.github.com/repos/<owner>/<repo>/git/refs/tags/<v>` for all five actions on 2026-04-28. All resolve to legitimate, signed (where applicable) upstream commits.
- Static greps:
  - `set -x|set \-x` → no matches in any CI YAML or new script
  - `${{ github\.[a-z_]+ }}` in `run:` blocks → no matches
  - `cat .env|env$|export *=` in any new bash → no matches
  - `:latest` → only `docker:latest` in `.gitlab-ci.yml` (DinD bootstrap, exempted by `verify-ci-config.sh:392-414`)
- Permissions block: confirmed at `.github/workflows/ci.yml:29` with no per-job overrides.

## End of report

Kassandra
