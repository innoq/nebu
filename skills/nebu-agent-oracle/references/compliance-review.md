---
name: compliance-review
code: compliance-review
description: Review code, stories, PRs, or feature implementations for Matrix Client-Server API spec compliance. Produces structured findings with spec citations.
---

# Compliance Review

## What Success Looks Like

Every deviation from the Matrix Client-Server API spec is identified, classified by severity, cited to the exact spec section, and accompanied by a concrete fix. Zero false positives. Zero false negatives for MUST violations.

## Context7 First

Use context7 to verify spec behavior before producing findings — do not rely on training data for compliance decisions.

## Input

Accept any of: code diff/snippet, story or acceptance criteria, PR description, Gherkin feature file, API handler implementation, or description of a Matrix feature's behavior.

Check MEMORY.md first: are there known accepted deviations for this codebase? Do not re-flag known exceptions as new findings.

## Review Dimensions

**1. HTTP method and path correctness**
- Correct HTTP method (GET/PUT/POST per spec)
- Correct path structure and API version prefix

**2. Request body fields**
- All MUST fields present; types match spec; enums correct; format constraints satisfied

**3. Response structure**
- HTTP status code correct; MUST response fields present; error format `{"errcode": "M_*", "error": "..."}`; 429 includes `retry_after_ms`

**4. Event content**
- Event `type` matches spec exactly; `content` has all required fields; `state_key` present on state events

**5. Transaction IDs**
- PUT send uses `txnId` path param; no POST for message sending

**6. Authentication**
- `Authorization: Bearer` header; correct error codes for auth failures

**7. Sync response handling**
- `since` token used for incremental; `next_batch` stored; `limited` flag + `prev_batch` handled

**8. Pagination**
- `from`/`to` params (not offset/limit); correct `dir` values

**9. Power level enforcement**
- Correct PL checked; defaults applied when no `m.room.power_levels`

**10. RFC 2119 compliance**
- Every applicable MUST satisfied; SHOULD violations noted separately

## Output Format

```
## Matrix Spec Compliance Review

**Spec version:** Matrix Client-Server API [version]
**Input:** [brief description]

### Findings

| # | Severity | Location | Finding | Spec Reference |
|---|----------|----------|---------|----------------|
| 1 | MUST violation | [file:line or section] | [what is wrong] | §[section] |
| 2 | SHOULD violation | ... | ... | §[section] |
| 3 | Spec gap | ... | [untested behavior] | §[section] |

### Detail

**Finding #1 — [short title]**
[What the implementation does] vs [what the spec requires]. Fix: [concrete change].

### Summary

[N] MUST violations, [N] SHOULD violations, [N] spec gaps.
[Blocking / Non-blocking] — MUST violations block; SHOULD violations are advisory.
```

If no findings: state "No spec violations found" with dimensions reviewed.

## Memory Integration

After the review, note in the session log:
- Any accepted deviations that should be added to MEMORY.md as known exceptions
- Any new spec quirks discovered for the Nebu codebase
