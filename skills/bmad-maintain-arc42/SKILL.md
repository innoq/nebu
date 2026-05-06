---
name: bmad-maintain-arc42
description: >
  Delta-Update for arc42 docs — reads docs/.arc42-manifest.json to determine
  which planning artifacts changed since the last generation, then regenerates
  only the affected arc42 sections and updates the manifest per-section.
  Use when you want a fast, targeted refresh instead of a full /bmad-generate-arc42 run.
---

# bmad-maintain-arc42 — Delta Arc42 Update Skill

This skill performs a targeted, delta-based refresh of the arc42 documentation.
Instead of regenerating all sections unconditionally, it checks which source
artifacts changed since `docs/.arc42-manifest.json` was last written and only
regenerates the affected sections.

## When to Use

- After merging a story that touched a specific planning artifact (e.g., only `architecture.md` changed)
- When `/bmad-generate-arc42` would be overkill (most sections are still current)
- As part of a CI pre-check to keep docs fresh incrementally

## Workflow

### Step 1 — Read the Manifest

Read `docs/.arc42-manifest.json` and extract the top-level `generated_at` timestamp.
This is the baseline: anything committed after this timestamp is considered "changed".

```bash
python3 -c "
import json
with open('docs/.arc42-manifest.json') as f:
    m = json.load(f)
print(m['generated_at'])
"
```

### Step 2 — Detect Changed Source Artifacts

Use `git diff` to compare the manifest's `generated_at` commit baseline against HEAD,
scoped to `_bmad-output/planning-artifacts/`:

```bash
# Find the commit closest to the generated_at timestamp
BASE_COMMIT=$(git log --before="<generated_at>" --format="%H" -1)

# List files that changed since that commit
git diff "${BASE_COMMIT}..HEAD" --name-only -- _bmad-output/planning-artifacts/ \
  | sort -u
```

Alternatively, use `git log` with `--since` to list files committed after `generated_at`:

```bash
git log --since="<generated_at>" --name-only --pretty=format: -- _bmad-output/planning-artifacts/ \
  | sort -u \
  | grep -v '^$'
```

This produces a list of changed files (relative to the repo root), e.g.:

```
_bmad-output/planning-artifacts/architecture.md
_bmad-output/planning-artifacts/prd.md
```

If no files are listed, all arc42 sections are up-to-date — output a summary and exit 0.

### Step 3 — Map Changed Artifacts to arc42 Sections

Use the `arc42_section_map` from `customize.toml` (or the built-in map below) to
identify which arc42 sections are affected by the changed source files.

**Built-in section map** (mirrors `customize.toml`):

| arc42 Section          | Source Artifact              |
|------------------------|------------------------------|
| 01-intro               | prd.md                       |
| 02-constraints         | architecture.md              |
| 03-context             | architecture.md              |
| 04-solution-strategy   | architecture.md              |
| 05-building-blocks     | architecture.md              |
| 06-runtime             | architecture.md              |
| 07-deployment          | architecture.md              |
| 08-concepts            | architecture.md              |
| 09-decisions           | architecture.md              |
| 10-quality             | prd.md                       |
| 11-risks               | sprint-status.yaml           |
| 12-glossary            | architecture.md              |

Example: if only `architecture.md` changed, sections 02–09 and 12 are regenerated.
If `prd.md` changed, sections 01 and 10 are regenerated.
If `sprint-status.yaml` changed, section 11 is regenerated.

### Step 4 — Regenerate Affected Sections

For each affected section, read the corresponding source artifact from
`_bmad-output/planning-artifacts/<source>` and rewrite the matching file under
`docs/architecture/`:

- Synthesize the section content from the source artifact's relevant paragraphs
  (guided by the `section` keyword hints in `customize.toml`)
- Preserve the existing file structure and heading levels
- Do not modify `editable: true` files (check `docs/.arc42-manifest.json` `files` map)

### Step 5 — Update arc42-manifest.json

After regenerating each section, update `docs/.arc42-manifest.json`:

1. Set the per-file `generated_at` timestamp for each regenerated section to `now` (UTC, ISO 8601 with Z suffix)
2. Set the top-level `generated_at` to `now` as well (reflects latest run)

```python
import json
from datetime import datetime, timezone

now_ts = datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')

with open('docs/.arc42-manifest.json') as f:
    manifest = json.load(f)

manifest['generated_at'] = now_ts

for section_file in regenerated_sections:
    if section_file in manifest.get('files', {}):
        manifest['files'][section_file]['generated_at'] = now_ts

with open('docs/.arc42-manifest.json', 'w') as f:
    json.dump(manifest, f, indent=2)
    f.write('\n')
```

### Step 6 — Verify with verify-docs.sh

Run `scripts/verify-docs.sh` to confirm the updated docs pass CI checks:

```bash
bash scripts/verify-docs.sh
```

If it fails, investigate and fix before exiting.

## Output

Print a summary at the end:

```
bmad-maintain-arc42 complete.
  Changed source artifacts: <N>
  Regenerated arc42 sections: <list>
  Manifest updated: docs/.arc42-manifest.json (generated_at: <now_ts>)
  verify-docs.sh: PASS
```

## Error Handling

- **Manifest not found:** Exit with error — run `/bmad-generate-arc42` first to create the manifest
- **git log returns nothing (no history):** Treat all sections as needing regeneration (fall back to full run)
- **Source artifact missing:** Warn and skip the affected section; do not fail the whole run
- **verify-docs.sh fails:** Report the failure and exit non-zero
