---
name: security-review
code: security-review
description: Per-story adversarial security review of staged changes. CRITICAL/HIGH findings block the commit. Produces structured findings report.
---

# Security Review

## What Success Looks Like

Every exploitable vulnerability in the staged diff is found, classified correctly, and accompanied by a concrete remediation. Zero false comfort — if it's a real finding, it gets reported regardless of how "unlikely" the attack path seems. CRITICAL and HIGH findings block the commit.

## Preparation

Before reviewing, check MEMORY.md for:
- Known accepted risks (do not re-flag these as new findings)
- Recurring patterns in this codebase (heightened attention to known weak spots)
- Architecture context (Go Gateway auth layer, Elixir Core event handling)

## Security Scope

Review staged diff against all of the following dimensions:

**Injection**
- SQL injection — parameterized queries vs. string concatenation in Go (`database/sql`) and Elixir (Ecto)
- Path traversal — user-controlled file paths without sanitization
- Command injection — user input in shell commands

**Web Application**
- XSS — user-controlled content rendered in HTML templates (Admin UI via go:embed)
- CSRF — state-changing endpoints without anti-CSRF protection (check for SameSite cookies or CSRF tokens)
- Open redirects — `redirect_uri` or `return_to` parameters not validated against allowlist

**Authentication & Authorization**
- Auth bypass — missing middleware on new routes; check `cmd/gateway/main.go` route registration
- IDOR — missing ownership check before returning or modifying resources
- JWT validation flaws — algorithm confusion (`alg: none`, RS256→HS256), missing `exp`/`aud`/`nonce` validation
- Soft auth — endpoints that return data before completing auth checks

**Secret Handling**
- Timing attacks — `==` comparison for secrets/tokens instead of `crypto/subtle.ConstantTimeCompare` (Go) or `:crypto.hash_equals` (Elixir)
- Plaintext secrets in logs — `fmt.Printf`/`Logger.info` with access tokens, internal secrets, passwords
- Secrets in error messages — stack traces or error details leaking sensitive values

**Input Validation**
- Missing body-size limits — no `http.MaxBytesReader` or equivalent on request body
- Missing rate limits — new endpoints not covered by rate limiting middleware
- Unvalidated content types — accepting arbitrary content-type without validation

**Cryptography**
- Weak primitives — MD5/SHA1 for security purposes, DES/3DES, RC4
- Weak key sizes — RSA <2048 bits, EC <256 bits
- Hardcoded secrets — API keys, passwords, or internal secrets in source code

**Infrastructure**
- Missing security headers — new HTML endpoints without `X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`
- New SQL migrations — check for injection vectors, missing constraints, sensitive data stored plaintext

## Output Format

```markdown
## Security Review — [story/epic ID]

**Diff scope:** [brief description]
**Date:** [YYYY-MM-DD]

### Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | CRITICAL | [file:line] | [type + description] | [concrete fix] |
| 2 | HIGH | ... | ... | ... |
| 3 | MEDIUM | ... | ... | ... |
| 4 | LOW | ... | ... | ... |

### Detail

**Finding #1 — [short title]** [CRITICAL/HIGH/MEDIUM/LOW]
[What the code does that creates the vulnerability. Why it's exploitable. Concrete remediation step.]

### Summary

CRITICAL: [N] — block commit
HIGH: [N] — block commit
MEDIUM: [N] — advisory, address before epic end
LOW: [N] — advisory

**Verdict:** [BLOCKED / APPROVED]
```

If zero findings: state "No security issues found" with scope reviewed.

## Severity Definitions

- **CRITICAL**: Exploitable without authentication or with low-privilege credentials. Direct data breach, RCE, or complete auth bypass.
- **HIGH**: Exploitable with standard user credentials or with reasonable effort. IDOR, privilege escalation, significant data exposure.
- **MEDIUM**: Requires specific conditions or chaining with other issues. Defense-in-depth weakness.
- **LOW**: Best-practice deviation without direct exploit path. Hardening opportunity.

## Memory Integration

After review:
- Record any new recurring pattern or codebase-specific weakness in session log
- Update MEMORY.md with any accepted risks formally acknowledged by the owner
- Note finding types that appear repeatedly — these warrant architectural attention
