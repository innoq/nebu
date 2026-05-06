---
name: epic-review
code: epic-review
description: Mandatory epic-end security review against the full epic diff. Produces an HTML report saved to _bmad-output/implementation-artifacts/. Always runs — even at zero findings (audit trail).
---

# Epic-End Security Review

## What Success Looks Like

The full epic diff is reviewed against the complete security scope. A report is produced and saved — always, even when there are zero findings. CRITICAL/HIGH findings become follow-up stories or are explicitly accepted as risk with written justification before the epic is marked done.

## Scope

Full git diff from epic base to HEAD: `git diff <epic-base>..HEAD`

Apply the complete security scope from the per-story review, plus:
- **Cross-story patterns**: vulnerabilities that span multiple stories (e.g., auth middleware added in story 1 bypassed in story 5)
- **Architectural weaknesses**: patterns that indicate systemic risk, not just point failures
- **Migration chain**: all SQL migrations reviewed together — data integrity, missing indexes on foreign keys, sensitive data handling across schema evolution

## Process

1. Run: `git diff <epic-base>..HEAD` to get the full epic diff
2. Check MEMORY.md for known accepted risks from the epic
3. Review against full security scope
4. Produce HTML report

## Output

Save as: `{project-root}/_bmad-output/implementation-artifacts/epic-{N}-security-review-{YYYY-MM-DD}.md`

```markdown
# Epic {N} Security Review — {YYYY-MM-DD}

## Scope
- Epic: {epic name/number}
- Base: {git ref}
- HEAD: {git ref}
- Stories covered: {list}

## Findings

| # | Severity | Story | Location | Vulnerability | Status |
|---|----------|-------|----------|---------------|--------|
| 1 | HIGH | 9-12 | gateway/internal/auth/... | ... | New / Previously Accepted |

## Detail
[Per finding detail as per security-review format]

## Cross-Story Patterns
[Any patterns that span multiple stories]

## Accepted Risks
[CRITICAL/HIGH findings formally accepted with written justification and owner sign-off]

## Follow-up Stories Required
[CRITICAL/HIGH that must become stories before epic is marked done]

## Summary
CRITICAL: [N] | HIGH: [N] | MEDIUM: [N] | LOW: [N]
Follow-up stories required: [N]
Accepted risks: [N]

**Epic security gate:** [PASS / BLOCKED — requires follow-up stories]
```

## Memory Integration

After epic review:
- Add systemic patterns discovered to MEMORY.md
- Record all formally accepted risks with justification
- Note the epic number and date for audit trail continuity
