---
name: nebu-pipeline
description: Orchestrates the full Nebu dev lifecycle with TDD-first order, dev↔review cycles, conditional security and UX gates, and full resume capability via pipeline-state.yaml. Use --resume, --from <step>, or --bug.
---

# Nebu Pipeline

Orchestrates the complete Nebu development lifecycle. Thin and deterministic — owns sequencing and cycle logic only. Step agent prompts live in `pipeline-steps-spec.md`; select them with `select_pipeline_step.py`.

## Core Principles

- **TDD-First:** Tests are generated (ATDD), reviewed for quality, then implementation follows. No code before reviewed failing tests exist.
- **Resumable:** Every step writes `pipeline-state.yaml` before executing. Interrupted runs restart from their exact checkpoint.
- **Cyclic:** Code-review findings below MAJOR threshold go back to dev-story. The dev agent fixes, not the reviewer.
- **Declarative:** This workflow calls agents and TEA skills. It decides the sequence; they carry the domain intelligence.

## Pipeline State File

`{project-root}/_bmad-output/nebu/pipeline-state.yaml` — written before and after every step.

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

Log entries are **prepended** directly below the `# last_updated:` comment line — newest first.

Format: `# [STORY_ID] YYYY-MM-DDTHH:MMZ step → compact outcome`

The log is **permanent and additive** — every step adds one line. Lines are never deleted or replaced. Only the YAML fields are zeroed on commit; all comment/log lines stay.

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

## Story File Location

```
docs/stories/
  mvp/
    epic-1/   ← epics 1–9
    ...
    epic-9/
  phase2/
    epic-10/  ← epics 10+
    ...
```

**When creating a story** (Step 1): verify the file landed in `docs/stories/mvp/epic-{N}/` (or `phase2/` for epic 10+). If it was written elsewhere, move it with `git mv` before continuing.

**`STORY_FILE`** throughout the pipeline: `docs/stories/mvp/epic-{N}/[STORY_ID]-slug.md` (or `phase2/`).

---

## How Steps Are Invoked

Each step follows the same pattern:

1. Extract story context with `extract_story_section.py`
2. Fetch the agent prompt with `select_pipeline_step.py --step <name>`
3. Inject runtime values into the prompt (substitute `[PLACEHOLDERS]`; drop `[IF X — omit if null]` blocks when the variable is null/empty)
4. Launch the subagent with the assembled prompt
5. Save the output as a named variable (carries to subsequent steps)
6. Update `pipeline-state.yaml`

---

## Pipeline Steps

### Step 1: create-story

**Skill:** `bmad-create-story` | **Model:** sonnet | **Fresh context**

```bash
STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step create-story)
```

Inject `[STEP_DESC]` into subagent, substituting `[USER_INPUT]`.

After completion:
```bash
rtk git status
```

Note the created story file as `STORY_FILE`.

```bash
# Create step-results directory for this story — all step outputs persist here
mkdir -p _bmad-output/nebu/step-results/[STORY_ID]/

python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step atdd --done create-story \
  --log "[STORY_ID] TIMESTAMP create-story → [STORY_FILE basename]"
```

Show: `✓ Step 1: Story created → [STORY_FILE]`

---

### Step 1c: Classify Story

```bash
# Ensure step-results directory exists (creation point when story already exists)
mkdir -p _bmad-output/nebu/step-results/[STORY_ID]/

STORY_FLAGS=$(python3 skills/nebu-pipeline/scripts/classify_story.py --story [STORY_FILE])
```

Parse and store: `MATRIX=[.matrix]`, `UI=[.ui]`, `SEC_REVIEW=[.security_review]`.

---

### Step 1b: Oracle Gate (conditional — Matrix features only)

**Agent:** `nebu-agent-oracle` | **Model:** sonnet | **Fresh context**

**If `MATRIX=false`:** `⏭ Step 1b: Oracle Gate skipped — not a Matrix feature.` Set `ORACLE_CONTEXT = null`.

**If `MATRIX=true`:**

```bash
ORACLE_INPUT=$(python3 skills/nebu-pipeline/scripts/extract_story_section.py \
  --story [STORY_FILE] --sections "Story" "Acceptance Criteria")

STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step oracle-gate)
```

Inject `[STEP_DESC]` into subagent, substituting `[ORACLE_INPUT]`.

Write output to disk (authoritative source for subsequent steps):
```bash
# [paste oracle-gate output here — the sub-agent's full response]
tee _bmad-output/nebu/step-results/[STORY_ID]/oracle-context.md
```

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

```bash
ATDD_INPUT=$(python3 skills/nebu-pipeline/scripts/extract_story_section.py \
  --story [STORY_FILE] --sections "Story" "Acceptance Criteria" "Acceptance Tests")

# Read oracle context from file (written by Step 1b — empty string if gate was skipped)
ORACLE_CONTEXT=$(cat _bmad-output/nebu/step-results/[STORY_ID]/oracle-context.md 2>/dev/null || echo "")

STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step atdd)
```

Inject `[STEP_DESC]` into subagent, substituting `[ATDD_INPUT]`; `[ORACLE_CONTEXT]` → value or drop block if empty.

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

This is the TDD gate. Poor tests produce poor implementations — review before dev starts.

```bash
AC_SECTION=$(python3 skills/nebu-pipeline/scripts/extract_story_section.py \
  --story [STORY_FILE] --sections "Story" "Acceptance Criteria")

STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step test-review)
```

Inject `[STEP_DESC]` into subagent, substituting `[AC_SECTION]`.

Write output to disk:
```bash
tee _bmad-output/nebu/step-results/[STORY_ID]/test-review-findings.md
```

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

```bash
# Read step results from disk — authoritative, survives context compression
TEST_REVIEW_FINDINGS=$(cat _bmad-output/nebu/step-results/[STORY_ID]/test-review-findings.md 2>/dev/null || echo "")
ORACLE_CONTEXT=$(cat _bmad-output/nebu/step-results/[STORY_ID]/oracle-context.md 2>/dev/null || echo "")
CODE_REVIEW_FINDINGS=$(cat _bmad-output/nebu/step-results/[STORY_ID]/code-review-findings.md 2>/dev/null || echo "")

STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step dev-story)
```

Inject `[STEP_DESC]` into subagent, substituting:
- `[STORY_FILE]` → path
- `[TEST_REVIEW_FINDINGS]` → value or drop block if empty
- `[ORACLE_CONTEXT]` → value or drop block if empty
- `[CODE_REVIEW_FINDINGS]` → value or drop block if empty (cycle 0)
- `[CYCLE_COUNT]` → current cycle number

**Context fallback (cycle > 0):** If the previous cycle's issues suggest the agent misunderstood story intent, replace `[STORY_FILE]` with full story content — see **Context Fallback Rule**.

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

**Agent:** `nebu-agent-testing` | **Model:** haiku | **Fresh context**

Primary path — run the CI script directly:
```bash
python3 skills/nebu-agent-testing/scripts/ci_gate.py --story [STORY_ID]
```

Parse the JSON output. **Fallback if script unavailable:**
```bash
STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step ci-gate)
```
Inject `[STEP_DESC]` into subagent (no substitutions needed).

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

```bash
STORY_SECTIONS=$(python3 skills/nebu-pipeline/scripts/extract_story_section.py \
  --story [STORY_FILE] --sections "Story" "Acceptance Criteria")

STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step ux-gate)
```

Inject `[STEP_DESC]` into subagent, substituting `[STORY_SECTIONS]`.

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

```bash
AC_SECTION=$(python3 skills/nebu-pipeline/scripts/extract_story_section.py \
  --story [STORY_FILE] --sections "Story" "Acceptance Criteria")

# Read findings from disk
TEST_REVIEW_FINDINGS=$(cat _bmad-output/nebu/step-results/[STORY_ID]/test-review-findings.md 2>/dev/null || echo "")
ORACLE_CONTEXT=$(cat _bmad-output/nebu/step-results/[STORY_ID]/oracle-context.md 2>/dev/null || echo "")

STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step code-review)
```

Inject `[STEP_DESC]` into subagent, substituting:
- `[AC_SECTION]` → value
- `[TEST_REVIEW_FINDINGS]` → value or drop block if empty
- `[ORACLE_CONTEXT]` → value or drop block if empty
- `[CYCLE_COUNT]` → current cycle number

Write output to disk (overwritten each cycle — always the latest review):
```bash
tee _bmad-output/nebu/step-results/[STORY_ID]/code-review-findings.md
```

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
- `fix` → increment `CYCLE_COUNT`, update state, return to Step 4
- `accept [reason]` → record justification, continue to Step 7

**If only MINOR/INFO findings and `CYCLE_COUNT < 3`:**
```
Code-review found [N] MINOR findings. Sending back to dev agent for fixing.
Cycle [CYCLE_COUNT + 1] starting...
```
Increment `CYCLE_COUNT`. Update state. Return to Step 4. After dev completes, return here.

**If `CYCLE_COUNT >= 3` with same findings still present:**
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

# MINOR path (cycle back to dev-story):
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --step dev-story --cycles [CYCLE_COUNT+1] \
  --log "[STORY_ID] TIMESTAMP code-review → N MINOR → cycle [CYCLE_COUNT+1]"

# MAJOR blocked / accepted: adjust --step and --log accordingly
```

---

### Step 7: security-review (SEC Gate 1 — conditional)

**Agent:** `nebu-agent-kassandra` | **Model:** opus | **Fresh context** | **Conditional**

**Decision:** Use `SEC_REVIEW` flag from Step 1c.
- `required` → run
- `optional` → ask: "Story marked optional — run security review now? [Y/n]"
- `not-needed` → `⏭ Step 7: Security review skipped (flagged not-needed).`

**If running:**

```bash
SEC_CONTEXT=$(python3 skills/nebu-pipeline/scripts/extract_story_section.py \
  --story [STORY_FILE] --sections "Story" "Acceptance Criteria")

STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step security-review)
```

Inject `[STEP_DESC]` into subagent, substituting `[SEC_CONTEXT]`, `[STORY_ID]`, `[DATE]`.

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

**Agent:** `nebu-agent-arc42` (if available) or `bmad-maintain-arc42` (fallback) | **Model:** haiku | **Fresh context** | **Mandatory**

```bash
ls _bmad/memory/nebu-agent-arc42/ 2>/dev/null && echo "available"
# Sets ARC42_SKILL_PATH:
#   available → "skills/nebu-agent-arc42/SKILL.md"
#   fallback  → ".claude/skills/bmad-maintain-arc42/SKILL.md"

ARC42_CONTEXT=$(python3 skills/nebu-pipeline/scripts/extract_story_section.py \
  --story [STORY_FILE] --sections "Story" "Dev Notes")

STAGED_FILES=$(rtk git diff --staged --name-only)

STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step arc42-update)
```

Inject `[STEP_DESC]` into subagent, substituting `[ARC42_SKILL_PATH]`, `[ARC42_CONTEXT]`, `[STAGED_FILES]`.

After completion:
```bash
rtk git diff --name-only docs/
```

If no `docs/` changes and story has architecture-relevant changes (new endpoints, services, data models, gRPC handlers, or middleware):
```
⚠ Step 8 — Arc42 not updated.
docs/ unchanged despite architecture-relevant changes.
Type "continue [reason]" to proceed.
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

Show: `✓ Step 9: Committed. Pipeline state cleared.`

---

### Step 10: epic-check

```bash
rtk read _bmad-output/implementation-artifacts/sprint-status.yaml
```

Check if the story just committed is the last story of the epic (entry immediately before `epic-{N}-retrospective` in the YAML).

**If epic complete → SEC Gate 2 (mandatory):**

Determine the epic base commit:
```bash
rtk git log --grep="epic-{N-1}-retrospective\|retrospektive" --oneline
```

If unclear, ask: "Epic diff base? (commit SHA or tag)"

```bash
STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step epic-security-review)
```

Inject `[STEP_DESC]` into subagent, substituting `[EPIC_BASE]`, `[DATE]`, `{N}`.

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

## Context Fallback Rule

**Never reduce story context between retries. Only ever expand it.**

Each step extracts a targeted subset of the story. If a step produces output suggesting the agent lacked sufficient story knowledge — wrong implementation direction, bug from misunderstood requirements, reviewer noting divergence from story intent, or agent explicitly asking for more context — expand before retrying:

```bash
FULL_STORY=$(python3 skills/nebu-pipeline/scripts/extract_story_section.py \
  --story [STORY_FILE] --all)
```

Pass `[FULL_STORY]` in place of the narrower variable. The `--all` flag extracts all `##` sections in document order.

**Signals that trigger a context fallback (any step):**
- Agent output contradicts the story's stated user goal
- Bug-fix cycle where the root cause is requirements-related
- Reviewer: "implementation doesn't match the story intent"
- Agent asks for more context or story background
- Two consecutive review cycles with the same misunderstanding

**Rule:** a retry with expanded context is always valid. A retry with reduced context is never valid.

---

## Error Handling

| Situation | Response |
|---|---|
| Agent fails | Show full error. Stop, user decides. |
| git command fails | Show git error. Stop, wait for user. |
| `sprint-status.yaml` missing | `ℹ sprint-status.yaml not found — epic check skipped.` |
| `select_pipeline_step.py` returns error | Report missing step name and stop. |
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
| ci-gate | nebu-agent-testing | haiku |
| UX Gate (5b, conditional) | nebu-agent-ux | sonnet |
| code-review | bmad-code-review | opus |
| security-review (conditional) | nebu-agent-kassandra | opus |
| epic-review (SEC Gate 2) | nebu-agent-kassandra | opus |
| arc42-update | nebu-agent-arc42 / bmad-maintain-arc42 | haiku |

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
- Story context extraction: see **Context Fallback Rule**. If any step seems to have too little context, use `--all` for that step's retry.
