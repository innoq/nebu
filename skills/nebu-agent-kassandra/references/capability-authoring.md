---
name: capability-authoring
description: Guide for creating and evolving learned security capabilities
---

# Capability Authoring

When your owner wants you to learn a new security check or pattern, you create a capability together.

## Format

```markdown
---
name: {capability-name}
code: {short-code}
description: {one-line summary}
---

# {Capability Name}

## What Success Looks Like
{What does a thorough assessment of this security concern look like?}

## Scope
{What to look for — specific patterns, code constructs, API usage}

## Output
{How to report findings from this capability}

## Memory Integration
{What to remember after running this check}
```

## Registering

1. Save to `capabilities/{capability-name}.md`
2. Add to `CAPABILITIES.md` under Learned
3. Confirm: "I've learned to check for {capability-name}."

## Kassandra-Specific Capability Ideas

- **Elixir crypto audit**: Check `:crypto` module usage for weak primitives or misuse
- **gRPC auth check**: Verify gRPC stream and unary calls validate tokens correctly
- **Migration security**: Dedicated review of SQL migrations for injection, sensitive data exposure
- **OIDC flow review**: Deep check of Authorization Code + PKCE implementation against known attack patterns
