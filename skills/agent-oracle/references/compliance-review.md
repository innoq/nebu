---
name: compliance-review
description: Review code, stories, PRs, or feature implementations for Matrix Client-Server API v1.18 spec compliance. Produces structured findings with spec citations.
---

# Compliance Review

## What Success Looks Like

Every deviation from the Matrix Client-Server API v1.18 spec is identified, classified by severity, cited to the exact spec section, and accompanied by a concrete fix. Zero false positives — findings that say "this is wrong" must be wrong per the spec, not per opinion. Zero false negatives — every MUST violation in the reviewed material is found.

## Input

Accept any of: a code diff/snippet, a story or acceptance criteria, a PR description, a Gherkin feature file, an API handler implementation, or a description of a Matrix feature's behavior. If the input is unclear, ask for clarification before reviewing.

## Review Dimensions

For each piece of input, check:

**1. HTTP method and path correctness**
- Correct HTTP method (GET for reads, PUT for idempotent sends, POST for non-idempotent operations)
- Correct path structure and path parameter names
- Correct API version prefix (`/_matrix/client/v3/` or version-specific variants)

**2. Request body fields**
- All MUST fields present
- No extra fields that the spec does not define (may indicate misunderstanding)
- Field types match spec (string, integer, boolean, array, object)
- Enums use the correct string values
- Format constraints satisfied (MXC URIs, user ID format, room ID format, event ID format)

**3. Response structure**
- HTTP status code correct for the operation and each outcome
- Response body contains all MUST fields
- Error responses use the correct `{"errcode": "M_*", "error": "..."}` format with the spec-defined errcode for each failure condition
- 429 responses include rate limiting fields

**4. Event content**
- Event `type` string matches the spec-defined value exactly
- `content` object has all required fields for that event type and msgtype
- `msgtype` is present for `m.room.message` and uses a spec-defined value or a custom namespace (`x.` prefix)
- State events include `state_key` (empty string or user ID as spec requires)

**5. Transaction IDs**
- PUT send requests use `txnId` path parameter for idempotency
- No POST for message sending (must be PUT with txnId)

**6. Authentication**
- `Authorization: Bearer <token>` header used (not query param except where spec allows)
- Correct error codes for auth failures (M_UNKNOWN_TOKEN, M_MISSING_TOKEN)

**7. Sync response handling**
- `since` token used for incremental sync
- `next_batch` stored and returned on next request
- Timeline `limited` flag handled
- `prev_batch` available for backfill when `limited: true`

**8. Pagination**
- Correct `from`/`to` parameters (not `offset`/`limit`)
- `dir=b` for backward, `dir=f` for forward
- `start`/`end` in response when applicable

**9. Power level enforcement**
- Correct power level checked for the operation (send event, set state, invite, kick, ban, redact, etc.)
- Default power levels applied when `m.room.power_levels` not set

**10. RFC 2119 compliance**
- Every MUST in the spec that applies to the reviewed code is satisfied
- SHOULD violations are noted but classified separately

## Output Format

```
## Matrix Spec Compliance Review

**Spec version:** Matrix Client-Server API v1.18
**Input:** [brief description]

### Findings

| # | Severity | Location | Finding | Spec Reference |
|---|----------|----------|---------|----------------|
| 1 | MUST violation | [file:line or story section] | [what is wrong] | §[section] |
| 2 | SHOULD violation | ... | ... | §[section] |
| 3 | Spec gap | ... | [behavior not covered by tests/story] | §[section] |

### Detail

**Finding #1 — [short title]**
[What the implementation does] vs [what the spec requires]. Fix: [concrete change].

...

### Summary

[N] MUST violations, [N] SHOULD violations, [N] spec gaps.
[Blocking / Non-blocking for merge] — MUST violations block; SHOULD violations are advisory.
```

If no findings: state "No spec violations found" with the dimensions reviewed and any edge cases that were not reviewable from the provided input.
