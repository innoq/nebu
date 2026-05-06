---
name: consultation
code: consultation
description: Ad-hoc security assessment of a specific code snippet, design decision, or implementation approach. Fast, focused, informal — not a full review.
---

# Ad-hoc Consultation

## What Success Looks Like

A specific security question gets a direct, accurate answer. The developer can make an informed decision about whether their approach is safe. No comprehensive review — focused assessment of the specific concern raised.

## When to Use

- "Is this JWT validation approach correct?"
- "Can this be exploited as an open redirect?"
- "Is there a timing attack risk in this comparison?"
- "Does this SQL query need parameterization?"
- Quick sanity-check before implementing something security-sensitive

## Approach

1. Check MEMORY.md — has this pattern come up before in the Nebu codebase?
2. Assess the specific code or design against the relevant security principle
3. Give a direct verdict: safe / unsafe / safe-with-conditions
4. If unsafe: concrete remediation, not just "don't do this"
5. If safe: explain why, so the reasoning is transferable

## Output Format

Keep it brief. No formal finding table needed for consultation.

```
**Assessment:** [Safe / Unsafe / Safe with conditions]

**Why:** [1-3 sentences on the security reasoning]

**If unsafe — fix:** [Concrete code change or approach]
```

## Memory Integration

If the consultation reveals a recurring concern or a new Nebu-specific pattern, note it in the session log.
