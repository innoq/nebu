# Pipeline Steps Specification

Agent prompts for each nebu-pipeline step. Selected by `select_pipeline_step.py --step <name>`.
The coordinator (SKILL.md) injects runtime values for `[BRACKETED]` placeholders before
passing the prompt to the subagent. Conditional blocks (`[IF X — omit if null]`) are
evaluated by the coordinator: if the variable is null/empty, the entire block is dropped.

---

## create-story

Read and follow .claude/skills/bmad-create-story/SKILL.md.
Feature/Story: [USER_INPUT]
Work through this completely, then finish.

---

## oracle-gate

Read and follow skills/nebu-agent-oracle/SKILL.md.
Load references/spec-lookup.md and references/test-guidance.md.

Story context:
[ORACLE_INPUT]

1. Which Matrix CS API endpoints, event types, and behavioral rules apply?
   List all MUST requirements.
2. Which spec-defined error codes, HTTP status codes, and edge cases MUST be test-covered?

Return compact Markdown (max 40 lines). Lists only, no prose. Then finish.

---

## atdd

Read and follow .claude/skills/bmad-testarch-atdd/SKILL.md.
Acceptance Criteria and existing test stubs:
[ATDD_INPUT]
Generate failing acceptance tests for all acceptance criteria.
Tests MUST be failing before any implementation code exists.

[IF ORACLE_CONTEXT — omit this block if null:]
Matrix spec requirements (from Oracle):
[ORACLE_CONTEXT]
Cover ALL spec-defined error codes, HTTP status codes, and edge cases
as separate failing tests.

Finish after generating the tests.

---

## test-review

Read and follow .claude/skills/bmad-testarch-test-review/SKILL.md.
Review all staged test files (git diff --staged).

Context: these are FAILING tests from ATDD, not yet validated by a green run.

Story and Acceptance Criteria (verify each has at least one test):
[AC_SECTION]

Check:
- Does each acceptance criterion have at least one test?
- Any hard waits, non-deterministic assertions, or brittle selectors?
- GenServer state stories: is there a crash/restart test?
- UI stories (ui: true in frontmatter): are Playwright tests present in e2e/tests/ or e2e/features/?
- Are Matrix spec error codes and HTTP status codes tested (not just the happy path)?

Classify each finding: MAJOR / MINOR / INFO.
MAJOR = missing AC coverage, missing crash/restart test for GenServer state,
        missing Playwright tests for a UI story.

Output the full report and finish.

---

## dev-story

Read and follow .claude/skills/bmad-dev-story/SKILL.md.
Implement the story at [STORY_FILE] completely.
The failing acceptance tests from ATDD are already staged —
implement until they are green.

Test quality notes (from pre-dev review):
[TEST_REVIEW_FINDINGS]

[IF ORACLE_CONTEXT — omit this block if null:]
Matrix spec requirements (from Oracle):
[ORACLE_CONTEXT]
The implementation must satisfy ALL MUST requirements above.

[IF CODE_REVIEW_FINDINGS — omit this block if null (cycle 0):]
This is review cycle [CYCLE_COUNT]. Issues to fix from the previous code-review:
[CODE_REVIEW_FINDINGS]
Fix all listed issues. Then finish.

Finish after completing the implementation.

---

## ci-gate

Read and follow skills/nebu-agent-testing/SKILL.md.
Load references/ci-gate.md and run the full CI gate verification.
Report pass/fail per suite and finish.

---

## ux-gate

Read and follow skills/nebu-agent-ux/SKILL.md.
Run in --headless mode: accessibility audit on staged changes.

Story context:
[STORY_SECTIONS]

Audit changed templates against:
- PRD accessibility requirements (from your BOND.md)
- WCAG 2.1 AA baseline
- DaisyUI consistency with existing components

Return findings as: violations (blocking) | advisories (non-blocking).
Finish after the audit.

---

## code-review

Read and follow .claude/skills/bmad-code-review/SKILL.md.
Review all staged changes (git diff --staged).

IMPORTANT: Do NOT fix issues yourself.
Identify all issues, classify them (MAJOR / MINOR / INFO), return the full list.
The pipeline hands MINOR issues back to the dev agent for fixing.
Only MAJOR/CRITICAL issues require user decision.

Story and Acceptance Criteria (verify implementation covers each):
[AC_SECTION]

Test quality findings (from pre-dev test-review):
[TEST_REVIEW_FINDINGS]

[IF ORACLE_CONTEXT — omit this block if null:]
Matrix spec requirements (from Oracle):
[ORACLE_CONTEXT]
Verify the implementation satisfies ALL MUST requirements.
Missing error codes or wrong HTTP status = MAJOR.

Current review cycle: [CYCLE_COUNT] (escalate to user if same issues persist for 3+ cycles)

Output the full report. Classify every finding as MAJOR, MINOR, or INFO. Finish.

---

## security-review

Read and follow skills/nebu-agent-kassandra/SKILL.md.
Load references/security-review.md and execute a full security review.

Scope: staged diff (git diff --staged)
Story context:
[SEC_CONTEXT]

Write the report to:
docs/stories/mvp/epic-{N}/security-reports/[STORY_ID]-security-review-[DATE].md
Always write the report, even if zero findings (audit trail).

Return: Classification (CRITICAL | HIGH | CLEAN) + report path + severity count.

---

## epic-security-review

Read and follow skills/nebu-agent-kassandra/SKILL.md.
Load references/epic-review.md.

Diff range: git diff [EPIC_BASE]..HEAD
Report path: _bmad-output/implementation-artifacts/epic-{N}-security-review-[DATE].md
Always write the report — this is a mandatory audit artifact.

Focus beyond per-story findings:
- Attack surfaces that emerge across multiple stories combined
- New endpoints + partial auth that together enable bypass
- Migrations that break RLS or existing policies
- gRPC handlers that bypass middleware
- Cumulative crypto or secrets drift

Return: Classification (CRITICAL | HIGH | CLEAN) + report path.

---

## arc42-update

Read and follow [ARC42_SKILL_PATH].
Story context:
[ARC42_CONTEXT]
Staged changes: [STAGED_FILES]

Perform a delta update of the arc42 documentation.
Update only the sections affected by this story. No full rewrites.
Finish after the update.
