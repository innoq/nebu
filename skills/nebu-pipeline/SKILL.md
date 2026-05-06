---
name: nebu-pipeline
description: Orchestrates the full Nebu dev lifecycle with TDD-first order, dev↔review cycles, conditional security and UX gates, and full resume capability via pipeline-state.yaml. Use --resume, --from <step>, or --bug.
---

# Nebu Pipeline

Orchestrates the complete Nebu development lifecycle. Thin and deterministic — owns sequencing and cycle logic only. Domain intelligence lives in the specialized agents it calls.

## Core Principles

- **TDD-First:** Tests are generated (ATDD), reviewed for quality, then implementation follows. No code before reviewed failing tests exist.
- **Resumable:** Every step writes `pipeline-state.yaml` before executing. Interrupted runs restart from their exact checkpoint.
- **Cyclic:** Code-review findings below MAJOR threshold go back to dev-story. The dev agent fixes, not the reviewer.
- **Declarative:** This workflow calls agents and TEA skills. It decides the sequence; they carry the domain intelligence.

## Pipeline State File

`{project-root}/_bmad/nebu/pipeline-state.yaml` — written before and after every step.

```yaml
# Nebu Pipeline State — written by nebu-pipeline, read by all nebu agents.
# Do not edit manually during an active pipeline run.
# last_updated: 2026-05-06
# [9-19] 2026-05-06T14:23Z code-review → 2 MINOR → cycle 1
# [9-19] 2026-05-06T14:15Z ci-gate → ✓ build+unit-go+unit-elixir+e2e+integration
# [9-19] 2026-05-06T14:05Z dev-story → done (cycle 0)
# [9-19] 2026-05-06T13:55Z test-review → CLEAN (0 MAJOR)
# [9-19] 2026-05-06T13:50Z atdd → 4 failing tests
# [9-19] 2026-05-06T13:45Z create-story → 9-19-sync-gap-fixes.md
# [9-19] 2026-05-06T13:44Z pipeline started
story: "9-19"
current_step: "code-review"
completed:
  - create-story
  - atdd
  - test-review
  - dev-story
  - ci-gate
cycle_count: 1
blocked_reason: null
last_updated: "2026-05-06T14:23Z"
```

**State updates use `pipeline_state.py` — never manually read or rewrite the file:**

```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step NEXT_STEP --done COMPLETED_STEP \
  --log "[STORY_ID] TIMESTAMP step → outcome"
```

Flags: `--story ID` · `--step STEP` · `--done STEP` (repeatable) · `--cycles N` · `--blocked REASON|null` · `--timestamp ISO` · `--commit` (zero YAML fields, keep all log lines). Combine in one call.

## Pipeline Log

Log entries are **prepended** directly below the `# last_updated:` comment line — newest first, same convention as `sprint-status.yaml`.

Format: `# [STORY_ID] YYYY-MM-DDTHH:MMZ step → compact outcome`

The log is **permanent and additive** — every step adds a new line. Lines are never deleted or replaced. Only the YAML fields are zeroed on commit; all comment/log lines stay. Over time the file becomes a full pipeline journal across all stories.

Each step prepends exactly one log line on completion (or skip). The commit step prepends the final `committed ✓` summary line.

---

## Activation

**`nebu-pipeline`** (no args) — New story. Ask the user for the story description or story file. Then:
```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --story STORY_ID --step create-story \
  --log "[STORY_ID] TIMESTAMP pipeline started"
```

**`nebu-pipeline --resume`** — Read `pipeline-state.yaml`. Print current state (story, step, cycle count). Ask user to confirm. Skip all completed steps and continue from `current_step`.

**`nebu-pipeline --from <step>`** — Start at the named step. Ask for story context if needed.

**`nebu-pipeline --bug <description>`** — Bug-fix flow: skip `create-story`, start from `atdd` with the bug as context (write the failing regression test first, then fix).

Valid step names: `create-story` `atdd` `test-review` `dev-story` `ci-gate` `code-review` `security-review` `arc42-update` `commit`

---

## Pipeline Steps

### Step 1: create-story

**Skill:** `bmad-create-story` | **Model:** sonnet | **Fresh context**

```
Read and follow .claude/skills/bmad-create-story/SKILL.md.
Feature/Story: [USER_INPUT]
Work through this completely, then finish.
```

After completion:
```bash
rtk git status
```

Note the created story file as `STORY_FILE`.

```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step atdd --done create-story \
  --log "[STORY_ID] TIMESTAMP create-story → [STORY_FILE basename]"
```

Show: `✓ Step 1: Story created → [STORY_FILE]`

---

### Step 1c: Classify Story

```bash
STORY_FLAGS=$(python3 skills/nebu-pipeline/scripts/classify_story.py --story [STORY_FILE])
```

Parse and store: `MATRIX=[.matrix]`, `UI=[.ui]`, `SEC_REVIEW=[.security_review]`.

---

### Step 1b: Oracle Gate (conditional — Matrix features only)

**Agent:** `nebu-agent-oracle` | **Model:** sonnet | **Fresh context**

**If `MATRIX=false`:** `⏭ Step 1b: Oracle Gate skipped — not a Matrix feature.` Set `ORACLE_CONTEXT = null`.

**If `MATRIX=true`:**
```
Read and follow skills/nebu-agent-oracle/SKILL.md.
Load references/spec-lookup.md and references/test-guidance.md.

Story: [STORY_FILE]

1. Which Matrix CS API endpoints, event types, and behavioral rules apply?
   List all MUST requirements.
2. Which spec-defined error codes, HTTP status codes, and edge cases MUST be test-covered?

Return compact Markdown (max 40 lines). Lists only, no prose. Then finish.
```

Save output as `ORACLE_CONTEXT`.

```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --log "[STORY_ID] TIMESTAMP oracle-gate → spec context captured"
# or: --log "[STORY_ID] TIMESTAMP oracle-gate → skipped (no Matrix feature)"
```

Show: `✓ Step 1b: Oracle consulted — Matrix spec context captured.`

---

### Step 2: atdd — Generate Failing Tests

**Skill:** `bmad-testarch-atdd` | **Model:** sonnet | **Fresh context** | **Mandatory**

**Skip if:** Pure infrastructure story (Dockerfile-only, migration-only with no logic). Show: `⏭ Step 2: Infra-only story — ATDD skipped.`

```
Read and follow .claude/skills/bmad-testarch-atdd/SKILL.md.
Story: [STORY_FILE]
Generate failing acceptance tests for all acceptance criteria.
Tests MUST be failing before any implementation code exists.

[IF ORACLE_CONTEXT — otherwise omit this block:]
Matrix spec requirements (from Oracle):
[ORACLE_CONTEXT]
Cover ALL spec-defined error codes, HTTP status codes, and edge cases
as separate failing tests.

Finish after generating the tests.
```

```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step test-review --done atdd \
  --log "[STORY_ID] TIMESTAMP atdd → N failing tests"
# or: --log "[STORY_ID] TIMESTAMP atdd → skipped (infra)"
```

Show: `✓ Step 2: Failing acceptance tests generated.`

---

### Step 3: test-review — Pre-Dev Test Quality Gate

**Skill:** `bmad-testarch-test-review` | **Model:** sonnet | **Fresh context** | **Mandatory**

This is the TDD gate. The failing tests are reviewed for quality **before** implementation starts — poor tests produce poor implementations.

```
Read and follow .claude/skills/bmad-testarch-test-review/SKILL.md.
Review all staged test files (git diff --staged).

Context: these are FAILING tests from ATDD, not yet validated by a green run.

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
```

Save output as `TEST_REVIEW_FINDINGS`.

**If MAJOR findings:**
```
⚠ Pre-dev test-review found MAJOR gaps (see above).
Fix coverage before dev starts — bad tests produce bad implementations.
Type "continue" to proceed anyway, or fix the gaps and restart Step 2.
```
Stop and wait.

```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step dev-story --done test-review \
  --log "[STORY_ID] TIMESTAMP test-review → CLEAN (0 MAJOR)"
# or: --log "[STORY_ID] TIMESTAMP test-review → N MAJOR [user continued]"
```

Show: `✓ Step 3: Pre-dev test quality verified.`

---

### Step 4: dev-story — Implementation

**Skill:** `bmad-dev-story` | **Model:** sonnet | **Fresh context**

```
Read and follow .claude/skills/bmad-dev-story/SKILL.md.
Implement the story at [STORY_FILE] completely.
The failing acceptance tests from ATDD are already staged —
implement until they are green.

Test quality notes (from pre-dev review):
[TEST_REVIEW_FINDINGS]

[IF ORACLE_CONTEXT — otherwise omit this block:]
Matrix spec requirements (from Oracle):
[ORACLE_CONTEXT]
The implementation must satisfy ALL MUST requirements above.

[IF CODE_REVIEW_FINDINGS (cycle > 0) — otherwise omit this block:]
This is review cycle [CYCLE_COUNT]. Issues to fix from the previous code-review:
[CODE_REVIEW_FINDINGS]
Fix all listed issues. Then finish.

Finish after completing the implementation.
```

After completion:
```bash
rtk git add .
rtk git status
```

```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step ci-gate --done dev-story \
  --log "[STORY_ID] TIMESTAMP dev-story → done (cycle [CYCLE_COUNT])"
```

Show: `✓ Step 4: Implementation complete. Cycle: [CYCLE_COUNT]`

---

### Step 5: ci-gate — CI Verification

**Agent:** `nebu-agent-testing` | **Model:** sonnet | **Fresh context**

```
Read and follow skills/nebu-agent-testing/SKILL.md.
Load references/ci-gate.md and execute the full CI gate.

Story: [STORY_FILE]
Pipeline state: [contents of _bmad/nebu/pipeline-state.yaml]

Run the full CI suite in order:
1. make build-gateway && make build-core
2. make test-unit-go
3. make test-unit-elixir
4. Start fresh E2E environment
5. make test-e2e
6. docker compose down
7. make test-integration

Return: pass/fail per suite + structured bug list for any failures.
Finish after the CI run.
```

**Fallback if nebu-agent-testing is unavailable:** Execute Steps 3b.1–3b.6 inline (build → unit-go → unit-elixir → docker compose down --volumes && docker compose up -d --wait → make test-e2e → docker compose down → make test-integration).

**If CI fails:**
```
🔴 CI Gate failed (see report above).
Pipeline stopped. Fix the failing tests, then restart from Step 4 or Step 5.
Type "continue" to skip the CI gate (not recommended).
```
Stop and wait.

```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step code-review --done ci-gate \
  --log "[STORY_ID] TIMESTAMP ci-gate → ✓ build+unit-go+unit-elixir+e2e+integration"
# or: --log "[STORY_ID] TIMESTAMP ci-gate → FAILED: [failing suite]"
```

Show: `✓ Step 5: CI gate passed.`

---

### Step 5b: UX Gate (conditional — UI stories)

**Agent:** `nebu-agent-ux` | **Model:** sonnet | **Fresh context** | **Conditional**

**Condition:** Use `UI` flag from Step 1c and check staged files:
```bash
rtk git diff --staged --name-only | grep "gateway/internal/admin/"
```

**If `UI=false` and no admin/ files staged:** `⏭ Step 5b: UX Gate skipped — not a UI story.`

**If UI story detected:**
```
Read and follow skills/nebu-agent-ux/SKILL.md.
Run in --headless mode: accessibility audit on staged changes.

Story: [STORY_FILE]
Pipeline state: [contents of _bmad/nebu/pipeline-state.yaml]

Audit changed templates against:
- PRD accessibility requirements (from your BOND.md)
- WCAG 2.1 AA baseline
- DaisyUI consistency with existing components

Return findings as: violations (blocking) | advisories (non-blocking).
Finish after the audit.
```

**If blocking violations:**
```
⚠ UX Gate found accessibility violations (blocking).
Fix violations before code-review. Type "continue" to proceed with open violations.
```
Stop and wait.

```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --log "[STORY_ID] TIMESTAMP ux-gate → CLEAN"
# or: --log "[STORY_ID] TIMESTAMP ux-gate → skipped (no UI)"
# or: --log "[STORY_ID] TIMESTAMP ux-gate → N violations [user continued]"
```

Show: `✓ Step 5b: UX audit passed.`

---

### Step 6: code-review — Implementation Review

**Skill:** `bmad-code-review` | **Model:** opus | **Fresh context**

Track `CYCLE_COUNT` — starts at 0, read from pipeline-state.yaml on resume.

```
Read and follow .claude/skills/bmad-code-review/SKILL.md.
Review all staged changes (git diff --staged).

IMPORTANT: Do NOT fix issues yourself.
Identify all issues, classify them (MAJOR / MINOR / INFO), return the full list.
The pipeline hands MINOR issues back to the dev agent for fixing.
Only MAJOR/CRITICAL issues require user decision.

Test quality findings (from pre-dev test-review):
[TEST_REVIEW_FINDINGS]

[IF ORACLE_CONTEXT — otherwise omit:]
Matrix spec requirements (from Oracle):
[ORACLE_CONTEXT]
Verify the implementation satisfies ALL MUST requirements.
Missing error codes or wrong HTTP status = MAJOR.

Current review cycle: [CYCLE_COUNT] (escalate to user if same issues persist for 3+ cycles)

Output the full report. Classify every finding as MAJOR, MINOR, or INFO. Finish.
```

Save output as `CODE_REVIEW_FINDINGS`.

```bash
rtk git add .
```

#### Evaluate findings:

**If MAJOR or CRITICAL findings:**
```
⚠ Code-review found MAJOR/CRITICAL issues (see report above).
Choose:
  (a) "fix" — hand back to dev agent for a fix cycle
  (b) "accept [reason]" — accept with written justification
  (c) "stop" — pause pipeline
```
Stop and wait.
- `fix` → increment `CYCLE_COUNT`, update state, return to Step 4 with `CODE_REVIEW_FINDINGS` as context
- `accept [reason]` → record justification, continue to Step 7

**If only MINOR/INFO findings:**

If MINOR findings present AND `CYCLE_COUNT < 3`:
```
Code-review found [N] MINOR findings. Sending back to dev agent for fixing.
Cycle [CYCLE_COUNT + 1] starting...
```
Increment `CYCLE_COUNT`. Update state. Return to Step 4. After dev completes, return here.

If `CYCLE_COUNT >= 3` with the same findings still present:
```
⚠ Review cycle limit reached (3 rounds). Issues persist.
Type "accept" to proceed with open minors, or "stop" to investigate.
```
Stop and wait.

**If no findings (or INFO only):** `✓ Step 6: Code-review clean.`

```bash
# CLEAN path:
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step security-review --done code-review \
  --log "[STORY_ID] TIMESTAMP code-review → CLEAN (cycle [CYCLE_COUNT])"

# MINOR path (cycle back to dev-story, increment cycles):
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step dev-story --cycles [CYCLE_COUNT+1] \
  --log "[STORY_ID] TIMESTAMP code-review → N MINOR → cycle [CYCLE_COUNT+1]"

# MAJOR blocked / accepted: use matching --log text, adjust --step accordingly
```

---

### Step 7: security-review (SEC Gate 1 — conditional)

**Agent:** `nebu-agent-kassandra` | **Model:** opus | **Fresh context** | **Conditional**

**Decision:** Use `SEC_REVIEW` flag from Step 1c.
- `required` → run
- `optional` → ask: "Story marked optional — run security review now? [Y/n]"
- `not-needed` → `⏭ Step 7: Security review skipped (flagged not-needed).`

**If required:**
```
Read and follow skills/nebu-agent-kassandra/SKILL.md.
Load references/security-review.md and execute a full security review.

Scope: staged diff (git diff --staged)
Story: [STORY_FILE]

Write the report to:
_bmad-output/implementation-artifacts/security-reports/[STORY_ID]-security-review-[DATE].md
Always write the report, even if zero findings (audit trail).

Return: Classification (CRITICAL | HIGH | CLEAN) + report path + severity count.
```

**If CRITICAL:**
```
🔴 Kassandra found CRITICAL findings — report: [path]
Pipeline stopped. Choose:
  (a) Fix and restart from Step 4
  (b) Accept with written justification
  (c) Move to follow-up story in next epic
Type "continue" to commit with open CRITICAL (strongly discouraged).
```
Stop and wait.

**If HIGH:** Check `blocking_severity` in `.claude/security-agent.yaml` (default: `CRITICAL`). If set to `HIGH`, treat as CRITICAL above. Otherwise warn and continue.

**If CLEAN:**
```bash
rtk git add .
```
Show: `✓ Step 7: Kassandra — clean.`

```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step arc42-update --done security-review \
  --log "[STORY_ID] TIMESTAMP security-review → Kassandra CLEAN"
# or: --log "[STORY_ID] TIMESTAMP security-review → skipped (not-needed)"
# or: --log "[STORY_ID] TIMESTAMP security-review → CRITICAL blocked"
```

---

### Step 8: arc42-update — Documentation Delta

**Agent:** `nebu-agent-arc42` (if sanctum exists) or `bmad-maintain-arc42` (fallback) | **Model:** sonnet | **Fresh context** | **Mandatory**

Check for nebu-agent-arc42:
```bash
ls _bmad/memory/nebu-agent-arc42/ 2>/dev/null && echo "available"
```

**If available:**
```
Read and follow skills/nebu-agent-arc42/SKILL.md.
Story completed: [STORY_FILE]
Staged changes: [output of: rtk git diff --staged --name-only]

Perform a delta update of the arc42 documentation.
Update only the sections affected by this story. No full rewrites.
Finish after the update.
```

**Fallback (bmad-maintain-arc42):**
```
Read and follow .claude/skills/bmad-maintain-arc42/SKILL.md.
Story completed: [STORY_FILE]
Staged changes: [output of: rtk git diff --staged --name-only]

Perform a delta update of the arc42 documentation.
Update only sections affected by this story's changes.
Finish after the update.
```

After completion:
```bash
rtk git diff --name-only docs/
```

If no `docs/` changes and the story has architecture-relevant changes (new endpoints, services, data models, gRPC handlers, or middleware):
```
⚠ Step 8 — Arc42 not updated.
docs/ unchanged despite architecture-relevant changes.
Type "continue [reason]" to proceed (e.g. "continue — config change only, no structural impact").
```
Stop and wait.

```bash
rtk git add docs/
```

```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step commit --done arc42-update \
  --log "[STORY_ID] TIMESTAMP arc42-update → done"
# or: --log "[STORY_ID] TIMESTAMP arc42-update → no arch changes"
```

Show: `✓ Step 8: Arc42 documentation updated.`

---

### Step 9: commit

**Pre-commit: update sprint-status.yaml**

```bash
python3 skills/nebu-pipeline/scripts/update_sprint_status.py \
  --file _bmad-output/implementation-artifacts/sprint-status.yaml \
  --story [STORY_ID] \
  --summary "[SUMMARY]"
```

Summary examples: `ATDD+Dev+Code CLEAN`, `2 MINOR fixed — 1 cycle`, `HIGH fixed, Kassandra 2 rounds`

```bash
rtk git add _bmad-output/implementation-artifacts/sprint-status.yaml
```

**Commit:**
```bash
rtk git commit -m "$(cat <<'EOF'
[STORY_SUMMARY]
EOF
)"
```

No `Co-Authored-By` line.

**Write final log entry and clear YAML fields:**

```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --commit \
  --log "[STORY_ID] TIMESTAMP committed ✓ ([COMPACT_SUMMARY])"
```

Compact summary examples: `ATDD+Code CLEAN`, `2 MINOR fixed 1 cycle+Kassandra CLEAN`, `MAJOR fixed 2 cycles+Kassandra HIGH resolved`

The script prepends the log line and zeroes only the YAML fields — every comment/log line stays. Result:
```yaml
story: null
current_step: null
completed: []
cycle_count: 0
blocked_reason: null
last_updated: "[NOW]"
```

After multiple stories the comment block will look like:
```yaml
# [9-20] 2026-05-07T10:00Z committed ✓ (ATDD+Code CLEAN)
# [9-20] 2026-05-07T09:55Z security-review → Kassandra CLEAN
# [9-20] 2026-05-07T09:40Z code-review → CLEAN (cycle 0)
# [9-19] 2026-05-06T14:23Z committed ✓ (2 MINOR fixed 1 cycle)
# [9-19] 2026-05-06T14:15Z ci-gate → ✓ build+unit-go+unit-elixir+e2e+integration
# [9-19] 2026-05-06T13:44Z pipeline started
```

This is the intended journal format — never truncate it.

Show: `✓ Step 9: Committed. Pipeline state cleared.`

---

### Step 10: epic-check

```bash
rtk read _bmad-output/implementation-artifacts/sprint-status.yaml
```

Check if the story just committed is the last story of the epic (the entry immediately before `epic-{N}-retrospective` in the YAML).

**If epic complete → SEC Gate 2 (mandatory, runs regardless of story flags):**

Determine the epic base commit:
```bash
rtk git log --grep="epic-{N-1}-retrospective\|retrospektive" --oneline
```

If unclear, ask: "Epic diff base? (commit SHA or tag)"

```
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
```

- **CRITICAL or HIGH:** Stop. User chooses: create follow-up stories in next epic, or accept as risk with written justification.
- **CLEAN:** Continue to retrospective.

```
🏁 Epic complete!
Security review artifact: epic-{N}-security-review-[DATE].md
Run /bmad-testarch-trace for the epic traceability matrix.
Retrospective next.
```

Stop and wait for user.

**If epic still running:**
```
✅ Story done. Start next story? [Y/n]
```
- `Y` → restart from Step 1 (create-story)
- `n` → end pipeline

---

## Error Handling

| Situation | Response |
|---|---|
| Agent fails | Show full error. Stop, user decides. |
| git command fails | Show git error. Stop, wait for user. |
| `sprint-status.yaml` missing | `ℹ sprint-status.yaml not found — epic check skipped.` |
| SKILL.md not found | Report missing path and stop. |
| ATDD skill unavailable | `⚠ ATDD skill not found — TEA Gate skipped. Acceptance tests must exist manually before dev starts.` |
| `bmad-testarch-test-review` unavailable | `⚠ Test-review skill not found — pre-dev quality check skipped.` |
| `nebu-agent-testing` unavailable | Fall back to inline CI execution (build → unit-go → unit-elixir → docker compose down --volumes && up -d --wait → make test-e2e → docker compose down → make test-integration). |
| `pipeline-state.yaml` missing on `--resume` | `⚠ No pipeline-state.yaml found — cannot resume. Start a new story.` |
| 3+ review cycles, same issues | Escalate to user. Do not loop indefinitely. |

---

## Model Reference

| Step | Skill / Agent | Model |
|---|---|---|
| create-story | bmad-create-story | sonnet |
| Oracle Gate (1b, conditional) | nebu-agent-oracle | sonnet |
| atdd | bmad-testarch-atdd | sonnet |
| test-review (pre-dev) | bmad-testarch-test-review | sonnet |
| dev-story | bmad-dev-story | sonnet |
| ci-gate | nebu-agent-testing | sonnet |
| UX Gate (5b, conditional) | nebu-agent-ux | sonnet |
| code-review | bmad-code-review | opus |
| security-review (conditional) | nebu-agent-kassandra | opus |
| epic-review (SEC Gate 2) | nebu-agent-kassandra | opus |
| arc42-update | nebu-agent-arc42 / bmad-maintain-arc42 | sonnet |

---

## Guidelines

- Print a short status line after every step.
- Pass story context to all agents — they don't share memory.
- Steps run **sequentially** — no parallel agent launches.
- `ORACLE_CONTEXT` carries from Step 1b through dev-story and code-review.
- `TEST_REVIEW_FINDINGS` carry from Step 3 through dev-story and code-review.
- `CODE_REVIEW_FINDINGS` carry through the full dev↔review cycle.
- `rtk git add .` runs after code-review unconditionally.
- The pipeline does not fix issues itself. The reviewer identifies; the dev agent fixes.
- TEA skills (bmad-testarch-atdd, bmad-testarch-test-review) are called identically to today's usage — no changes to those skills themselves.
