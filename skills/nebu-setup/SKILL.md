---
name: nebu-setup
description: Sets up Nebu Dev module in a project. Use when the user requests to 'install nebu module', 'configure Nebu Dev', or 'setup nebu pipeline'.
---

# Module Setup

## Overview

Installs and configures the Nebu Dev module into a project. Module identity comes from `assets/module.yaml`. Writes shared config and registers capabilities, then performs Nebu-specific initialization: agent sanctum scaffolding, pipeline state stub, and dependency verification.

- **`{project-root}/_bmad/config.yaml`** — shared project config: core settings at root plus a `nebu` section with module metadata. User-only keys are never written here.
- **`{project-root}/_bmad/config.user.yaml`** — personal settings: `user_name`, `communication_language`. Gitignored.
- **`{project-root}/_bmad/module-help.csv`** — registers all nebu capabilities for the help system.
- **`{project-root}/_bmad/nebu/`** — operational directory for the nebu module (pipeline state, runtime files).

Both config scripts use an anti-zombie pattern — existing nebu entries are removed before writing fresh ones.

`{project-root}` is a **literal token** in config values — never substitute it with an actual path.

## On Activation

1. Read `assets/module.yaml` for module metadata.
2. Check if `{project-root}/_bmad/config.yaml` exists and has a `nebu` section — if present, this is an update.
3. Check for legacy config at `{project-root}/_bmad/nebu/config.yaml` or `{project-root}/_bmad/core/config.yaml` — if found, treat as fresh install and consolidate.

If the user provides arguments (`accept all defaults`, `--headless`, or inline values), use them and skip interactive prompting. Still display the confirmation summary.

## Collect Configuration

No nebu-specific config variables beyond core BMad settings. Ask the user only for core values if they don't exist yet.

Show defaults in brackets. Present all values so the user can respond once:

**Core config** (only if no core keys exist yet): `user_name`, `communication_language` and `document_output_language` (ask as a single language question), `output_folder` (default: `{project-root}/_bmad-output`).

`user_name` and `communication_language` go exclusively to `config.user.yaml`. The rest go to `config.yaml`.

## Write Files

Write a temp JSON file: `{"core": {...}}` (omit core if it already exists). Then run both scripts in parallel:

```bash
python3 ./scripts/merge_config.py --config-path "{project-root}/_bmad/config.yaml" --user-config-path "{project-root}/_bmad/config.user.yaml" --module-yaml assets/module.yaml --answers {temp-file} --legacy-dir "{project-root}/_bmad"
python3 ./scripts/merge_help_csv.py --target "{project-root}/_bmad/module-help.csv" --source assets/module-help.csv --legacy-dir "{project-root}/_bmad" --module-code nebu
```

If either exits non-zero, surface the error and stop.

## Create Output Directories

After writing config, create any path-type values from config that don't yet exist. Use `mkdir -p`. Resolve `{project-root}` to the actual project root for filesystem operations only — stored config values keep the literal token.

## Cleanup Legacy Directories

```bash
python3 ./scripts/cleanup_legacy.py --bmad-dir "{project-root}/_bmad" --module-code nebu --also-remove _config --skills-dir "{project-root}/.claude/skills"
```

If the script exits non-zero, surface the error and stop.

---

## Nebu-Specific Initialization

Run these steps after the standard config/cleanup steps above.

### 1. Check Dependencies

Run these checks. Warn on failure (do not block) except bmad-tea (required — error if missing).

**rtk (token optimizer):**
```bash
rtk --version 2>/dev/null || echo "missing"
```
If missing: `⚠ rtk not found. Install from https://github.com/anthropics/rtk for token savings. Pipeline will still work without it.`

**docker (CI execution):**
```bash
docker info --format '{{.ServerVersion}}' 2>/dev/null || echo "missing"
```
If missing: `⚠ Docker not found. nebu-agent-testing requires Docker to run the CI gate. Install Docker Desktop to enable local CI runs.`

**context7 MCP (framework docs):**
Check the project `.mcp.json` for a `context7` entry:
```bash
grep "context7" .mcp.json 2>/dev/null | head -1 || echo "not configured"
```
If not configured: `⚠ context7 MCP not configured. nebu-agent-oracle uses context7 for live Matrix spec lookups. Configure it in your Claude MCP settings.`

**playwright MCP (browser testing):**
```bash
grep "playwright" .mcp.json 2>/dev/null | head -1 || echo "not configured"
```
If not configured: `⚠ playwright MCP not configured. nebu-agent-ux and nebu-agent-testing use it for browser-level E2E validation. Configure at: https://github.com/microsoft/playwright-mcp`

**bmad-tea module (required):**
```bash
ls .claude/skills/bmad-testarch-atdd/SKILL.md \
   .claude/skills/bmad-testarch-test-review/SKILL.md \
   .claude/skills/bmad-tea/SKILL.md 2>/dev/null | wc -l
```
If count < 3: `🔴 bmad-tea module not found. nebu-pipeline requires bmad-testarch-atdd, bmad-testarch-test-review, and bmad-tea. Install the bmad-tea module first, then re-run /nebu-setup.` Stop.

### 2. Create Nebu Operational Directory

```bash
mkdir -p {project-root}/_bmad/nebu
```

Write `{project-root}/_bmad/nebu/pipeline-state.yaml` if it does not yet exist:

```yaml
# Nebu Pipeline State — written by nebu-pipeline, read by all nebu agents
# Do not edit manually during a pipeline run.
story: null
current_step: null
completed: []
cycle_count: 0
blocked_reason: null
last_updated: null
```

Show: `✓ Pipeline state file initialized.`

### 3. Scaffold Agent Sanctums

Run `init_sanctum.py` for each memory agent. The scripts are idempotent — if a sanctum already exists, they exit with `status: already-exists`.

Resolve `{project-root}` to the actual project root path. Run sequentially (each agent may ask a First Breath question):

```bash
uv run skills/nebu-agent-oracle/scripts/init_sanctum.py {project-root} skills/nebu-agent-oracle/
```
```bash
uv run skills/nebu-agent-kassandra/scripts/init_sanctum.py {project-root} skills/nebu-agent-kassandra/
```
```bash
uv run skills/nebu-agent-testing/scripts/init_sanctum.py {project-root} skills/nebu-agent-testing/
```
```bash
uv run skills/nebu-agent-ux/scripts/init_sanctum.py {project-root} skills/nebu-agent-ux/
```

For each script, parse the JSON output:
- `status: ok` → show `✓ [agent-name] sanctum created.`
- `status: already-exists` → show `✓ [agent-name] sanctum already exists — skipped.`
- Non-zero exit → show error output and warn (continue with next agent).

### 4. Report

Show a summary:

```
Nebu Dev module installed.

Dependencies:
  rtk:         ✓ / ⚠ not found
  docker:      ✓ / ⚠ not found
  context7:    ✓ / ⚠ not configured
  playwright:  ✓ / ⚠ not configured
  bmad-tea:    ✓ (required)

Agent sanctums:
  nebu-agent-oracle:     ✓ created / ✓ already exists
  nebu-agent-kassandra:  ✓ created / ✓ already exists
  nebu-agent-testing:    ✓ created / ✓ already exists
  nebu-agent-ux:         ✓ created / ✓ already exists

Pipeline state: {project-root}/_bmad/nebu/pipeline-state.yaml

Next steps:
  • Invoke each agent once to complete First Breath (run /nebu-agent-oracle, etc.)
  • Run /nebu-pipeline to start your first story
```

Then display the `module_greeting` from `assets/module.yaml`.

---

## Confirm

Use the merge script JSON output to report what was written: config values set, user settings in `config.user.yaml`, help entries added, fresh install vs update. If legacy files were deleted, mention the migration.

## Outcome

Once `user_name` and `communication_language` are known (from input, arguments, or existing config), use them for the rest of the session.
