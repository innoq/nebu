---
name: nebu-pipeline-epic
description: Runs a full epic by iterating through stories, each in a fully isolated story pipeline sub-agent. Use when you want to implement all pending stories of an epic without manual step-by-step control.
---

# Nebu Pipeline — Epic Mode

Thin coordinator. For each pending story it spawns an isolated `nebu-pipeline` sub-agent with a fresh context. The sub-agent owns the full story lifecycle (create-story through commit). This coordinator only decides: next story, wait for user, or epic-complete gates.

**Why isolation matters:** each story sub-agent starts with zero accumulated context from previous stories. It works only with what its own steps produce — no stale findings, no cross-story bleed.

---

## Activation

**`nebu-pipeline-epic`** — ask user for epic number, then start.

**`nebu-pipeline-epic --epic N`** — start or resume epic N.

**`nebu-pipeline-epic --epic N --from STORY_ID`** — skip all stories before STORY_ID.

---

## Step 0: Discover Pending Stories

```bash
# Find all story files for the epic (mvp or phase2)
find docs/stories/mvp/epic-[N]/ docs/stories/phase2/epic-[N]/ \
  -name "*.md" 2>/dev/null | sort
```

For each file, check its status:
```bash
grep "^Status:" [STORY_FILE]
```

- `Status: done` → skip (already committed)
- `Status: ready-for-dev` or `Status: in-progress` or no status → pending
- `Status: review` → treat as in-progress (pipeline will resume)

Show the pending list with index and title (H1 line of each file):
```
Pending stories for epic [N]:
  1. [STORY_FILE] — [H1 title]
  2. ...

Total: [N] stories pending. Start? [Y/n]
```

If `--from STORY_ID` was given: skip all stories whose ID comes before STORY_ID in the sorted list.

---

## Step 1–N: Story Sub-Agent Loop

For each pending story in order:

### Launch

Print: `▶ [[INDEX]/[TOTAL]] Story [STORY_ID] starting — isolated sub-agent`

Spawn a sub-agent with this prompt (no other context from previous stories):

```
Read and follow skills/nebu-pipeline/SKILL.md.

Story file: [STORY_FILE]
Story ID: [STORY_ID]

The story file already exists. Start from Step 1c (classify).
Skip Step 1 (create-story) — the story is already written.

Run all pipeline steps: 1c → 1b → 2 → 3 → 4 → 5 → 5b → 6 → 7 → 8 → 9.
For each step, use select_pipeline_step.py and extract_story_section.py as defined in the pipeline.

Cycle behavior (code-review MINOR findings): auto-cycle back to dev-story without asking.
Stop and surface to user only for:
  - MAJOR or CRITICAL code-review findings (user chooses fix/accept/stop)
  - CRITICAL security-review findings
  - CI gate failure
  - Pre-dev test-review MAJOR gaps

Step results are written to:
  _bmad-output/nebu/step-results/[STORY_ID]/
Use these files — not in-memory variables — as the authoritative source when
passing findings between steps. This ensures no stale context bleeds between steps.

On completion return exactly:
  DONE:[STORY_ID]:[compact pipeline summary]
  BLOCKED:[reason]
  FAILED:[reason]
```

### Handle Result

**`DONE`:**
```bash
python3 skills/nebu-pipeline/scripts/pipeline_state.py \
  --file _bmad-output/nebu/pipeline-state.yaml \
  --log "[STORY_ID] TIMESTAMP epic-loop → story done"
```
Print: `✓ [[INDEX]/[TOTAL]] Story [STORY_ID] done — [summary]`
Continue to next story.

**`BLOCKED`:**
```
⏸ Story [STORY_ID] blocked: [reason]
Resolve the issue, then type "continue" to resume from this story,
or "skip" to move to the next story.
```
Stop and wait. On "continue": re-spawn the sub-agent with `nebu-pipeline --resume` context.
On "skip": log the skip and move on.

**`FAILED`:**
```
🔴 Story [STORY_ID] failed: [reason]
Epic paused. Investigate, then type "retry" to re-run this story
or "skip" to move to the next story.
```
Stop and wait.

---

## Step N+1: Epic Complete

After all stories are done:

### SEC Gate 2 — Mandatory Epic Security Review

Determine the epic base commit:
```bash
rtk git log --grep="epic-[N-1]-retrospective\|retrospektive\|epic-[N-1]" --oneline | head -3
```

If unclear: ask "Epic base commit SHA or tag for `git diff [BASE]..HEAD`?"

```bash
STEP_DESC=$(python3 skills/nebu-pipeline/scripts/select_pipeline_step.py --step epic-security-review)
```

Spawn Kassandra sub-agent (model: opus, fresh context):
```
[STEP_DESC]
```
Substituting `[EPIC_BASE]` → base commit SHA, `[DATE]` → today, `{N}` → epic number.

**If CRITICAL or HIGH:**
```
🔴 Epic SEC Gate 2 found issues — report: [path]
Choose:
  (a) Create follow-up stories in next epic
  (b) Accept as risk with written justification
  (c) Fix now and re-run SEC Gate 2
```
Stop and wait.

**If CLEAN:**
Print: `✓ Epic [N] SEC Gate 2 — Kassandra clean.`

### Epic Wrap-Up

```
🏁 Epic [N] complete!

Next steps (run these yourself):
  /bmad-testarch-trace   — requirements-to-tests traceability matrix
  /bmad-retrospective    — epic retrospective

Security review artifact: _bmad-output/implementation-artifacts/epic-[N]-security-review-[DATE].md
```

Stop and wait for user.

---

## Error Handling

| Situation | Response |
|---|---|
| Story file not found | Warn, ask user to confirm path, stop if unresolved |
| Sub-agent times out | Treat as BLOCKED, surface to user |
| pipeline-state.yaml conflict | Read current state, show to user, ask how to proceed |
| `find` returns no files | "No story files found for epic [N] — check docs/stories/ structure" |
| SEC Gate 2 sub-agent fails | Stop, surface error, user decides |

---

## Resuming an Interrupted Epic Run

If the epic was interrupted (e.g., after story 3 of 8):

1. Re-run `nebu-pipeline-epic --epic N`
2. The coordinator reads story statuses from `grep "^Status:"` per file
3. Done stories (Status: done) are automatically skipped
4. The first non-done story starts fresh as a new isolated sub-agent

No manual state tracking needed — the story files themselves are the source of truth.
