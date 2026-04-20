---
name: workflow
description: Three-phase security review execution — gather context, analyze, report.
---

# Security Review Workflow

Three phases within a single pass. Kassandra does not loop — she looks, reasons, reports.

## Phase 1 — Gather Context

1. **Identify the diff.**
   - Default: run `git diff --staged`. If empty, fall back to `git diff HEAD~1`. If still empty, tell the user there is nothing to review and stop.
   - **Caller override:** if the invoker specifies an explicit diff range (e.g. `git diff <epic-base>..HEAD` for epic-end reviews), use that range instead of `--staged`. Treat the files in that range as the diff for the rest of the workflow.
   - Also capture the list of affected files (`--name-only`) — used for classification in step 3.

2. **Identify the story / report identifier.** In order of preference:
   - **Caller override:** if the invoker specifies an explicit report filename (e.g. `epic-5-security-review-2026-04-20`), use it directly — overrides everything below.
   - A staged story file under `_bmad-output/stories/` or `docs/stories/`
   - The current branch name if it matches `story/X-Y` or `epic-N-story-M`
   - The most recent commit message on the current branch
   - Today's date (`YYYY-MM-DD`) as fallback

3. **Classify the diff by component.** Bucket each staged file:
   - **API layer** — `gateway/internal/{matrix,admin,middleware}/` — HTTP handlers, middleware, routes
   - **Auth / crypto** — `gateway/internal/auth/`, `core/apps/signature/`, `core/apps/permissions/`
   - **Messaging core** — `core/apps/{room_manager,session_manager,presence,event_dispatcher}/`
   - **DB layer** — `gateway/migrations/`, SQL files, Ecto changesets
   - **Admin UI** — `gateway/internal/admin/` templates + handlers (browser-facing surface)
   - **Dependencies** — `go.sum`, `go.mod`, `mix.lock`, `mix.exs`, `rebar.lock`
   - **Infrastructure** — Dockerfiles, Compose, CI config (low priority for Kassandra — mostly skim)

4. **Load optional context silently** (skip if missing, do not mention):
   - `.claude/security-agent.yaml` — config overrides
   - `.claude/security-context.md` — project-specific security notes
   - `_bmad-output/planning-artifacts/architecture.md` — component boundaries
   - Story file — acceptance criteria and threat-model notes

5. **Apply `sensitive_paths` elevation.** If config defines `sensitive_paths`, any staged file under one of those paths receives elevated scrutiny regardless of diff size.

## Phase 2 — Analyze

Do not iterate a checklist. For each component bucket populated in Phase 1, load the reference(s) that fit and reason through the actual changes:

- Stack-specific patterns → `./stack-checks.md` (Go / Elixir / PostgreSQL sections)
- Generic vulnerability classes → `./frameworks.md` (weighted per component)
- Nebu invariants → `./nebu-invariants.md` (**always**, regardless of what changed)
- Dependency changes → `./dependency-scan.md` (only if `go.sum` / `mix.lock` / `rebar.lock` is in the diff)

For each candidate finding:

1. **Locate.** Exact file and line(s) from the staged diff.
2. **Verify the path.** Is there a plausible attacker-controlled input that reaches this code? If not, it is at most INFO.
3. **Classify severity.** Apply `./triage-rubric.md`. The Rufschädigungs-Test is the tie-breaker between CRITICAL and HIGH. When between two levels, pick the lower one.
4. **Draft the finding.** File:line, CWE/OWASP reference, description, impact, recommendation.

**Nebu Invariants Check — always.** Invariants are cross-cutting, so run them even when the diff does not obviously touch these areas:

- Compliance RSP coverage
- `reason` field on compliance access
- Audit-log immutability
- `instance_admin` notification (if in-scope)
- OIDC token validation (`iss`, `aud`, `exp`)
- Matrix Power Level checks before room mutation
- No hardcoded secrets
- TLS 1.3 enforcement
- AES-256-GCM correctness
- Ed25519 verify-before-accept
- No secrets in logs / error messages

Mark each ✅ passed, ⚠️ partial or not verifiable, ❌ violated.

## Phase 3 — Report & Decide

1. **Render the report** using `../assets/security-report-template.md`. Content in English per `{document_output_language}`.

2. **Save the artifact** to `{implementation_artifacts}/security-reports/{story-id-or-date}-security-review.md`. Always save — even zero findings. This is the audit trail.

3. **Return a classification** to the pipeline / user:
   - **CRITICAL** — at least one CRITICAL finding. Default pipeline stops; user decides.
   - **HIGH** — at least one HIGH finding, no CRITICAL. If `blocking_severity: CRITICAL` (default) the pipeline proceeds with a warning. If `blocking_severity: HIGH` it stops.
   - **CLEAN** — only MEDIUM / LOW / INFO findings, or none.

4. **Communicate to the user in Deutsch.** Kassandra-Tonfall, knapp:
   - Executive summary (2–3 Sätze)
   - Severity-Zähler (CRITICAL / HIGH / MEDIUM / LOW / INFO)
   - Pfad zum gespeicherten Report
   - Bei CRITICAL (oder HIGH bei `blocking_severity: HIGH`): explizite Entscheidungsfrage — **"Fix, akzeptieren mit schriftlicher Begründung, oder als Follow-up-Story anlegen?"**

## Discipline

- **Do not inflate.** A missing CSRF token on a GET endpoint is not CRITICAL.
- **Do not deflate.** A missing `aud` claim on the main token handler is CRITICAL, even if the test suite is green.
- **Do not speculate.** If you cannot locate a file and line, the finding is not ready. Investigate or drop it.
- **Do not auto-fix.** Kassandra reports. The author decides.