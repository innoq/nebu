---
name: triage-rubric
description: Severity triage rubric — how Kassandra decides between CRITICAL, HIGH, MEDIUM, LOW, INFO.
---

# Triage Rubric

Severity is decided by **impact**, not by cleverness or exploit complexity. When a finding sits between two levels, pick the **lower** one — over-triage erodes trust and the next real CRITICAL gets ignored.

## The Rufschädigungs-Test (tie-breaker between CRITICAL and HIGH)

For every candidate CRITICAL, ask:

> **If this is exploited and surfaces in a security advisory, CVE, or press article about Nebu — does it cause reputational damage to the product or its operators?**

- **Yes** → CRITICAL. Pipeline must stop.
- **Real exploit path exists, but the impact is narrow (affects only admins, requires a non-default configuration, affects the actor themselves, specialized niche)** → HIGH.

This test prevents both over-inflation (every bug becomes CRITICAL) and under-playing (a real breach class gets labeled HIGH because "it's complex").

## Levels

### CRITICAL

Reputationally damaging. Direct exploit path. No special precondition.

Examples:
- Auth bypass on the main Matrix API token handler
- SQL injection reachable from any authenticated user
- Compliance data leak to an unprivileged user
- Audit-log tampering (UPDATE / DELETE allowed on audit tables)
- Hardcoded production secret in source
- Ed25519 verification skipped before accepting a signed event
- RSP missing on a compliance table with user-scoped data
- ECB mode used to encrypt any non-trivial payload
- `InsecureSkipVerify: true` on an inter-service TLS path
- Missing Matrix Power Level check on a state-changing room operation

**Pipeline effect:** Default `blocking_severity: CRITICAL` stops the pipeline. User decision required — fix, accept with written justification, or convert to follow-up story.

### HIGH

Real exploit path, but bounded impact. Narrow user segment, admin-only, specific non-default configuration, or requires chaining with another issue. Still must be fixed before the next release.

Examples:
- IDOR on an admin-only endpoint
- Missing rate limit on a non-auth endpoint (DoS risk, but not catastrophic)
- Weak password policy in a local-dev-only code path
- Verbose error response that leaks schema information to authenticated users
- Missing CSRF token on a state-changing endpoint that is also authenticated via bearer token
- Power-level check missing on a seldom-used admin-only room operation
- `govulncheck` reports a real CVE in a runtime dependency

**Pipeline effect:** With default config, HIGH warns but does not block. If config sets `blocking_severity: HIGH`, HIGH stops the pipeline.

### MEDIUM

Weakness without a direct exploit path today, but a future code change could elevate it. Defense-in-depth gaps go here.

Examples:
- Missing explicit `MinVersion` on a `tls.Config` (current Go default is 1.2, acceptable but implicit)
- Request body size limit set generously (10 MB where 1 MB would suffice)
- Cookie forging in E2E tests — test integrity, not runtime security
- Missing security header (`X-Content-Type-Options`) on a non-sensitive page
- `govulncheck` finding in a test-only dependency

**Pipeline effect:** Logged in report. No pipeline action.

### LOW

Hygiene issue with no practical exploit path.

Examples:
- `// TODO: review auth` comment in a handler whose auth is actually correct
- Unused crypto import
- Log line that includes a truncated request ID that could be misread as a token
- Inconsistent error format across handlers

**Pipeline effect:** Logged in report.

### INFO

Observation, not a vulnerability. Included for transparency and future reference.

Examples:
- Dependency update to a newer version — scan is clean
- New handler added that is correctly secured — note the new attack surface explicitly
- Positive finding worth preserving ("New migration adds `ENABLE ROW LEVEL SECURITY` — aligned with Nebu invariant")
- A test was added that specifically covers a security property

**Pipeline effect:** Logged in report.

## Anti-patterns in triage

- **Inflating a MEDIUM to HIGH** because "there's a chance someone could". Without a concrete path, it is MEDIUM.
- **Deflating a CRITICAL to HIGH** because "compensating controls exist elsewhere". If the control is elsewhere, state that — but the local issue is still measured locally. A locked front door does not justify leaving the back door open.
- **Bulk severity classification.** Every finding gets its own severity based on its own impact. No rule like "all CWE-79 is HIGH".
- **Severity from the CWE alone.** CWE is a category, not a severity. SQL injection in a local-dev-only tool is not CRITICAL; in a login handler it is.
- **Padding with LOWs.** If there are no real LOW findings, do not invent them for symmetry. A CLEAN report with zero LOWs is allowed.