---
security_review: not-needed
---

# Story 5.28: Security Review Pipeline Gate (Story + Epic Level)

Status: ready-for-dev

## Story

As a BMAD pipeline operator,
I want the `/bmad-pipeline` skill to decide per-story whether a security review is needed and to enforce a security review at epic-end,
so that security-sensitive changes cannot slip through without review and the epic-end gate catches anything that fell through the per-story check.

---

## Background / Motivation

Security audit (2026-04-20) surfaced CRITICAL/HIGH findings that went unnoticed through all four prior epics despite each story passing `bmad-code-review`. The code reviewer focuses on correctness and test coverage — not OWASP Top 10, not CSRF, not timing attacks.

User decision (2026-04-20): add a security-review step to `/bmad-pipeline`, evaluated per story (skip if not security-sensitive), mandatory at epic-end.

---

## Acceptance Criteria

1. **Per-story frontmatter flag.** Every story file may declare:
   ```
   ---
   security_review: required | optional | not-needed
   ---
   ```
   - `required` — pipeline MUST run a scoped security review (post-code-review, pre-commit)
   - `optional` — pipeline asks the user once
   - `not-needed` — pipeline skips (process stories, pure docs, etc.)
   - Missing flag — pipeline auto-classifies via heuristics (see AC 3)

2. **Auto-classification heuristic.** When the frontmatter flag is absent, the pipeline inspects the staged diff and marks the story as `required` if **any** of:
   - Files touched under `gateway/internal/auth/`, `middleware/`, `admin/`, `crypto/`, `db/`
   - New HTTP route registered in `cmd/gateway/main.go` (`mux.Handle` / `mux.HandleFunc` lines added)
   - Files touched under `core/apps/signature/`, `core/apps/permissions/`, Elixir `*.ex` file imports `:crypto` or handles external input
   - New SQL migration
   Otherwise: `not-needed`. The pipeline logs the decision + reasoning.

3. **Security review sub-step.** After `bmad-code-review` (Schritt 5 in `bmad-pipeline`), if the story is `required` or the user confirmed `optional`:
   - Run a security-focused adversarial review using `bmad-code-review` with a **security scope prompt** (see AC 4) in a fresh subagent
   - Findings classified CRITICAL/HIGH/MEDIUM/LOW
   - CRITICAL/HIGH block the commit (same semantics as MAJOR in code review)
   - MEDIUM/LOW are surfaced and can be accepted by the user

4. **Security scope prompt** (reusable in the pipeline + ad-hoc):
   > "You are a security auditor. For the provided diff, hunt for: SQL injection, XSS in templates, CSRF on state-changing endpoints, auth bypass (missing middleware, IDOR), timing attacks on secret comparison, open redirects, missing body-size limits, missing rate limits, weak crypto primitives (md5, sha1, DES), plaintext secret logging, missing security headers, path traversal, JWT validation flaws (alg confusion, missing exp/aud/nonce). Classify findings CRITICAL/HIGH/MEDIUM/LOW. Output title, file:line, evidence, why, fix."

5. **Epic-end gate.** `Schritt 7` ("Epic-Status prüfen") is extended: when the epic is detected as complete, before the retrospective pause, the pipeline:
   - Computes the full diff of the epic (base = epic-start commit, HEAD = current)
   - Runs the same security scope prompt against that diff
   - Any CRITICAL or HIGH → blocks with a clear message; the user must create follow-up stories (or accept with documented justification)
   - Produces a one-page `epic-{N}-security-review.md` in `_bmad-output/implementation-artifacts/` even if zero findings — as the auditable artifact

6. **Documentation.** `CLAUDE.md` is updated with:
   - The `security_review` frontmatter convention
   - The epic-end security gate
   - Reference to this story

7. **Regression test.** None needed — this is a pipeline skill change, not runtime code. Manual smoke via running `/bmad-pipeline` on a small security-sensitive change and confirming the gate triggers.

---

## Acceptance Tests

### Manual acceptance (no ATDD — pipeline skill, not Go code):

1. Run `/bmad-pipeline` on a dummy story with `security_review: required` — verify security review step executes after code review.

2. Run `/bmad-pipeline` on a dummy story with `security_review: not-needed` — verify security review step is skipped with a clear log message.

3. Complete a dummy single-story epic — verify epic-end security review runs and the `epic-{N}-security-review.md` artifact is created.

4. Introduce a synthetic SQL-injection vulnerability in the dummy story — verify the gate blocks the commit with CRITICAL severity.

---

## Implementation Notes

- Edit `.claude/skills/bmad-pipeline/SKILL.md` — add Schritt 5b (per-story gate) and extend Schritt 7 (epic-end gate)
- Update `CLAUDE.md` under the "BMAD Workflow" section
- The security scope prompt is identical between per-story and epic-end — factor it out into a constant in the SKILL.md text so there's only one source of truth
- This story itself runs through the pipeline as `security_review: not-needed` (it IS the gate, so nothing to review)
