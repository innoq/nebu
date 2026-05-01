# Kassandra — SEC Gate 2 Security Review: Epic 9

**Report ID:** epic-9-security-review-2026-05-01  
**Review type:** SEC Gate 2 — Mandatory epic-end security review  
**Diff range:** `git diff 43a5eeb..HEAD`  
**Date:** 2026-05-01  
**Reviewer:** Kassandra (Security Review Agent)  
**Epic classification:** Pure documentation epic (Story 9-1 only)

---

## Executive Summary

Epic 9 introduced arc42 architecture documentation, ADR files, getting-started and API scope guides, two bash CI check scripts, CI pipeline additions, and a BMAD skill for future doc regeneration. No runtime application code, database migrations, or authentication logic was modified. The attack surface change is minimal.

**Overall classification: CLEAN**

No CRITICAL findings. No HIGH findings. Three MEDIUM/LOW observations documented below.

---

## Diff Scope Classification

| File bucket | Files in diff | Weight |
|---|---|---|
| Documentation (`docs/`) | 29 files | Low — no runtime code |
| Shell scripts (`scripts/`) | 2 files | Low — CI-only, no user-controlled input |
| CI config (`.github/`, `.gitlab-ci.yml`) | 2 files | Low — infrastructure |
| BMAD skill (`.claude/skills/`) | 2 files | Negligible — LLM prompting only |
| BMAD artifacts (`_bmad-output/`) | 2 files | Negligible |

---

## Findings

### MEDIUM-1 — Dev credentials documented in public getting-started guide

**File:** `docs/getting-started.md` lines 89, 97–99, 116  
**CWE:** CWE-312 (Cleartext Storage of Sensitive Information)  
**OWASP:** A02:2021 – Cryptographic Failures (credential exposure in documentation)

**Description:**  
`docs/getting-started.md` documents the following development credentials verbatim:
- OIDC Client Secret: `nebu-admin-secret`
- User `kai@example.com` / `changeme`
- User `compliance@example.com` / `changeme`
- User `alex@example.com` / `changeme`

These are Dex-seeded dev fixture credentials intended solely for local Docker Compose stacks.

**Why not HIGH:**  
These credentials have no attacker-reachable production path. They are explicitly scoped to the local dev stack (Dex is `localhost:5556`, not exposed). The documentation itself states these are development-only. The `changeme` password pattern signals dev fixture, not production credential. The same values already appeared in pre-Epic-9 story files in `docs/stories/`. No new credentials are introduced; the getting-started guide simply consolidates what was already scattered across story files.

**Impact:**  
If a developer copies these credentials into a production configuration by mistake, the resulting exposure is a compromised development OIDC client, not a Nebu instance itself (OIDC provider is operator-supplied in production). The risk is confusion, not direct compromise.

**Recommendation:**  
Add an explicit warning box at the top of the development credential table:

```markdown
> **Warning:** These are local development credentials for the Dex fixture only.
> They have no meaning in production deployments. Never reuse these values.
```

This is a defense-in-depth measure. The current state is acceptable for a public getting-started guide.

---

### MEDIUM-2 — Architecture docs expose bootstrap auto-admin mechanism

**File:** `docs/architecture/08-concepts.md` line 23  
**CWE:** CWE-200 (Exposure of Sensitive Information to an Unauthorized Actor)

**Description:**  
Section 08 documents: _"Bootstrap mode: First OIDC login automatically receives `instance_admin`. Bootstrap mode is permanently disabled after the first admin setup."_

This is the correct behavior and is already documented in `CLAUDE.md` and other internal artifacts. However, publishing it in the public architecture docs makes the bootstrap privilege-escalation window explicit and discoverable by an attacker who wants to target a Nebu installation before first setup is complete.

**Why not HIGH:**  
The bootstrap window requires a race condition: the attacker must be able to authenticate via the configured OIDC provider before the legitimate first admin completes setup. In practice this requires either: (a) the OIDC provider issues tokens to the attacker, or (b) the server has no OIDC provider configured yet (at which point no login is possible). Neither scenario is trivially reachable.  
The existing `docs/architecture/11-risks.md` already documents an accepted risk table and this mechanism is not a new finding.

**Recommendation:**  
Consider whether the explicit statement of "first OIDC login automatically receives `instance_admin`" needs to appear in the public-facing arc42 docs. A softer phrasing such as "Bootstrap mode grants elevated access to the first authenticated operator" is equally informative for architecture purposes without handing attackers a precise targeting guide.

This is a documentation hygiene suggestion, not a blocking security issue.

---

### LOW-1 — CI actions/checkout and actions/setup-go use floating major-version tags

**File:** `.github/workflows/ci.yml` line 136 (new `verify-docs` job)  
**CWE:** CWE-494 (Download of Code Without Integrity Check)  
**OWASP:** A06:2021 – Vulnerable and Outdated Components (supply chain)

**Description:**  
The new `verify-docs` CI job (added in Epic 9) uses `actions/checkout@v6` — a floating major-version tag, not a pinned SHA. The CI file header comment states _"All uses: steps are pinned to full 40-char commit SHAs (supply-chain hardening)"_ but this is contradicted by the actual `checkout@v6` and `setup-go@v6` references throughout the file.

**Why LOW and not MEDIUM:**  
This is a pre-existing condition: `actions/checkout@v6` was already used in all other jobs before Epic 9. The new `verify-docs` job simply inherits the same pattern. The risk is a supply-chain compromise of a `github.com/actions/checkout` major version tag — which is a real but low-probability risk for a first-party GitHub-owned action. The Epic 9 diff does not worsen the existing posture.

**Recommendation:**  
Pin `actions/checkout` and `actions/setup-go` to full SHAs in all jobs, consistent with the stated header policy. The `actions/upload-artifact` action is already pinned correctly (`@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a`). Apply the same pattern to `checkout` and `setup-go`. This is an existing debt; track as follow-up rather than blocking Epic 9.

---

## Nebu Invariants Check

| Invariant | Result | Notes |
|---|---|---|
| No hardcoded secrets in committed files | ✅ | Dev fixture credentials in getting-started are documented intentionally as dev-only (see MEDIUM-1). No API keys, JWTs, or production tokens found. |
| No path traversal in shell scripts | ✅ | `REPO_ROOT` is derived from `$(dirname "${BASH_SOURCE[0]}")` — controlled by the script's own location, not user input. All file paths are constructed from fixed array literals. |
| No command injection in bash scripts | ✅ | `run_test` dispatches only hard-coded function names (literal strings at call sites, never from env or user input). Python scripts are passed via heredoc literals; only the file path argument is variable, and it is the already-validated `${manifest}` path. No `eval`, no `bash -c` with user input. |
| No secrets in CI environment variable passing | ✅ | New `verify-docs` job uses no `${{ secrets.* }}` references. It only runs `scripts/verify-docs.sh` which reads local files. |
| No secret exfiltration in CI steps | ✅ | `verify-docs` job: `continue-on-error: true` (warning mode), no upload steps, no curl to external endpoints. |
| No sensitive PII or runtime secrets in docs | ✅ | Internal path references (`NEBU_INTERNAL_SECRET_FILE`, `.secrets/`) point to gitignored runtime secrets. The documents correctly instruct users not to commit these. |
| OIDC / Auth bypass via docs | ⚠️ | Bootstrap mechanism is publicly documented (see MEDIUM-2). Not exploitable without OIDC provider access, but worth reviewing phrasing. |
| Compliance RSP / Audit immutability | ✅ | No migrations, no DB code in this epic. Invariants not in scope. |
| Crypto primitives | ✅ | ADR-007 documents Ed25519 + X25519 correctly. ADR-011 correctly defers E2EE decision. No new crypto code introduced. |
| TLS / gRPC security | ✅ | Not in scope for this epic (documentation only). |
| Matrix Power Level checks | ✅ | Not in scope (no Matrix handlers modified). |

---

## CI Pipeline Analysis

The `verify-docs` job addition is minimal and correct:
- `continue-on-error: true` is intentional and appropriately commented (docs are not yet authoritative)
- No secrets are passed to the job
- The job runs only `scripts/verify-docs.sh` — a read-only file-presence check
- GitLab `allow_failure: true` mirrors the GitHub behavior

No supply chain risk was introduced beyond the pre-existing floating-tag pattern (see LOW-1).

---

## BMAD Skill Security Assessment

`.claude/skills/bmad-generate-arc42/SKILL.md` and `customize.toml` define an LLM prompting workflow. No shell commands are executed by the skill itself — it is invoked as a Claude Code skill and produces file writes. No injection vectors exist: the skill reads BMAD artifact files and writes markdown. No `eval`, subprocess, or external network calls are defined.

---

## Summary

| Severity | Count |
|---|---|
| CRITICAL | 0 |
| HIGH | 0 |
| MEDIUM | 2 |
| LOW | 1 |
| INFO | 0 |

**Epic 9 classification: CLEAN**

The two MEDIUM findings are documentation hygiene issues with no direct exploit path. The LOW finding is pre-existing CI debt. None require blocking action before the epic is marked done. Both MEDIUM items are suitable as follow-up story acceptance criteria (documentation quality pass) rather than security hotfixes.

---

_Report generated by Kassandra — Security Review Agent (SEC Gate 2)_  
_Artifact path: `_bmad-output/implementation-artifacts/security-reports/epic-9-security-review-2026-05-01.md`_
