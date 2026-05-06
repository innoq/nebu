---
name: capability-authoring
description: Guide for creating and evolving learned capabilities
---

# Capability Authoring

When your owner wants you to learn a new ability, you create a capability together. This guide tells you how to write, format, and register it.

## Capability Types

### Prompt (default)
A markdown file with guidance on what to achieve. Best for judgment-based tasks.

```
capabilities/
└── {example-capability}.md
```

### Script
A Python or bash script for deterministic tasks. Create the script alongside a short markdown file.

```
capabilities/
├── {example-capability}.md    # When to run, what to do with results
└── {example-capability}.py    # The actual computation
```

## Format for Prompt Capabilities

Every capability file needs frontmatter so I can register it:

```markdown
---
name: {capability-name}
code: {short-code}
description: {one-line summary}
---

# {Capability Name}

## What Success Looks Like
{Outcome-focused description — what does a great result look like?}

## Approach
{Only include if genuinely non-obvious. Skip mechanical steps I'd figure out from the outcome.}

## Memory Integration
{What to remember after running this capability.}
```

## Registering a New Capability

After creating the file:
1. Save it to `capabilities/{capability-name}.md`
2. Add it to `CAPABILITIES.md` under the Learned section
3. Confirm: "I've learned how to {capability-name}. You can ask me to do it anytime."

## Oracle-Specific Capability Ideas

Things you might want to teach me:
- **ADR review**: check that architecture decisions don't create Matrix spec conflicts
- **Spec changelog**: summarize what changed between Matrix spec versions for Nebu's scope
- **Room version guide**: explain event auth rule differences for specific room versions
