---
name: frameworks
description: Security frameworks as weighted analysis lenses — OWASP Top 10, ASVS L2, CWE Top 25, STRIDE, NIST SP 800-53.
---

# Frameworks as Lenses

These are not checklists. Apply them weighted by the components that changed in the diff. A migration-only diff does not need OWASP Top 10 categories; an admin-UI diff does not need STRIDE boundary analysis.

## OWASP Top 10 (2021) — Web-Layer Lens

Apply to Go API gateway handlers, middleware, HTTP routes.

| Cat | Focus |
|---|---|
| A01 Broken Access Control | IDOR, missing authz middleware, path traversal, CORS misconfiguration |
| A02 Cryptographic Failures | Weak primitives (MD5, SHA1, DES), plaintext secrets, missing TLS |
| A03 Injection | SQL (via `database/sql`), command (via `exec.Command`), LDAP, XSS in templates |
| A04 Insecure Design | Missing rate limits, missing body size limits, open redirects |
| A05 Security Misconfiguration | Default credentials, verbose errors, missing security headers |
| A06 Vulnerable Components | See `./dependency-scan.md` |
| A07 Auth Failures | Weak session handling, missing MFA paths, credential stuffing exposure |
| A08 Data Integrity | Unsigned updates, deserialization, missing integrity checks |
| A09 Logging Failures | Secrets in logs, missing audit entries, tamperable logs |
| A10 SSRF | Unvalidated redirect URLs, internal IP access from handlers |

## OWASP ASVS L2 — Auth / Session / Crypto Lens

Apply to anything under `gateway/internal/auth/` or `core/apps/signature/`.

Key controls:
- **V2.1 / V2.5** Password, credential recovery, session binding
- **V3.2** Session rotation
- **V6.2** Algorithm correctness — no custom crypto
- **V7.1** Error handling — no stack traces to clients, no info disclosure
- **V9.1** TLS enforcement — 1.3, no downgrade

## CWE Top 25 — Code-Level Pattern Lens

The ones most likely in Nebu's stack:

- **CWE-20** Input validation — missing struct tag validation
- **CWE-22** Path traversal — `filepath.Join(base, userInput)` without containment check
- **CWE-78** OS command injection
- **CWE-79** XSS — Go templates with `template.HTML(userInput)` bypass
- **CWE-89** SQL injection — string-concatenated queries
- **CWE-94 / CWE-95** Code injection — `Code.eval_string`, dynamic `apply/3`
- **CWE-200** Information exposure — verbose errors, stack traces
- **CWE-209** Error message info leak
- **CWE-269 / CWE-285** Privilege management — `SECURITY DEFINER` without `search_path`
- **CWE-287 / CWE-295** Auth / certificate validation — `InsecureSkipVerify: true`
- **CWE-307** Brute-force — missing rate limits on auth
- **CWE-327** Weak crypto primitive — MD5 / SHA1 / DES / ECB
- **CWE-347** Signature verification skipped or swallowed
- **CWE-352** CSRF — state-changing endpoints without token
- **CWE-400 / CWE-1333** Resource exhaustion — unbounded body, goroutines, ReDoS
- **CWE-502** Deserialization of untrusted data
- **CWE-601** Open redirect — `http.Redirect` to attacker-controlled URL
- **CWE-611** XXE — XML parser with external entities enabled
- **CWE-798** Hardcoded credentials in source
- **CWE-918** SSRF — `http.Get(userURL)` without host allowlist

## STRIDE — Architecture Threat Modeling Lens

Relevant when the diff adds a new component boundary, data flow, or trust boundary. Walk through:

- **S**poofing — Can an attacker impersonate a principal at this boundary?
- **T**ampering — Can messages / data be modified in transit or at rest?
- **R**epudiation — Is the action logged immutably?
- **I**nformation disclosure — Does an error response or log leak internal state?
- **D**enial of service — Are resources bounded?
- **E**levation of privilege — Does this create a path to a more privileged context?

Especially relevant for Nebu's gRPC boundary (Gateway ↔ Core) and SQL migrations (new RLS surfaces).

## NIST SP 800-53 — Compliance Controls Lens

Nebu's compliance posture benefits from NIST language in findings where the gap is compliance-relevant:

- **AU (Audit & Accountability)** — Does this change generate appropriate audit records? Are they tamper-evident?
- **AC (Access Control)** — Is least-privilege honored? Is RBAC enforced at the correct layer?
- **SC (System & Communications Protection)** — Is inter-service traffic authenticated and encrypted?
- **SI (System & Information Integrity)** — Is input validated and are integrity checks in place?

Using the control ID in a finding helps downstream audit conversations.

## Weighting Heuristic

| Diff touches | Apply primarily |
|---|---|
| `gateway/internal/{admin,matrix,middleware}/` | OWASP Top 10, CWE-79 / 89 / 352 / 601 / 918 |
| `gateway/internal/auth/` or `core/apps/signature/` | OWASP ASVS V2/V3/V6, CWE-287 / 295 / 327 / 347 |
| `core/apps/permissions/` | NIST AC family, CWE-285 / 269 |
| `gateway/migrations/` or Ecto changesets | CWE-89, RSP / audit invariants, STRIDE-T / E |
| Dockerfile / Compose / CI only | Skip application-layer lenses |
| Dependencies only (`go.sum`, `mix.lock`) | `./dependency-scan.md` |

If nothing in the diff matches the lens, do not force findings through it.