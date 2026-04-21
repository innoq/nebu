# Security Review — Story 5.14: Security Headers Middleware — 2026-04-20

**Agent:** Kassandra
**Diff base:** `git diff --staged` (8 files, +257 / -6)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4-6`

## Executive Summary

Story 5.14 adds a `SecurityHeadersMiddleware` for all `/admin/*` responses (CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy, conditional HSTS), extracts the last remaining inline JavaScript handler from `bootstrap-claims.html` into an external file, and registers a new `ServeJSFile` handler with proper path-traversal defense. The CSP policy is strict — no `unsafe-inline`, no `unsafe-eval`, `frame-ancestors 'none'`, `object-src 'none'`. No remaining inline event handlers were found in any template. One LOW observation regarding `X-Forwarded-Proto` trust and one INFO observation on the new attack surface are noted; no actionable vulnerability was found.

## Findings

### [LOW] X-Forwarded-Proto trusted without origin validation

- **CWE / OWASP:** CWE-346 / A05:2021 Security Misconfiguration
- **Datei:** `gateway/internal/admin/middleware.go:191`
- **Beschreibung:** The HSTS condition trusts `X-Forwarded-Proto: https` as sent by the client without verifying that the request actually arrived through a trusted reverse proxy. An attacker sending `X-Forwarded-Proto: https` directly to the gateway on a plain-HTTP deployment would receive an HSTS header, which by itself has no security impact (the browser would only upgrade future requests — which is a no-op if the origin has no TLS listener). However, this pattern can become problematic if future middleware decisions (e.g., Secure cookie flag, redirect logic) also key off this header without proxy trust validation.
- **Impact:** No exploitable path today. The HSTS header on a plain-HTTP response is ignored by conforming browsers (RFC 6797 Section 7.2: UAs must ignore HSTS on non-secure transport). Defensive hygiene concern only.
- **Empfehlung:** Consider stripping or ignoring `X-Forwarded-Proto` unless a trusted-proxy configuration is in place (e.g., check `r.RemoteAddr` against a known proxy CIDR). This is a Phase 2 concern — document and revisit when the deployment architecture is finalized.
- **Referenz:** OWASP ASVS V14.5.3; RFC 6797 Section 7.2

### [INFO] New static file serving endpoint — attack surface note

- **CWE / OWASP:** CWE-22 / A01:2021 Broken Access Control (mitigated)
- **Datei:** `gateway/internal/admin/static.go:67`
- **Beschreibung:** `ServeJSFile` serves embedded JS files from `static/js/`. Path traversal is mitigated via `path.Base()` (strips directory components) and `.js` suffix enforcement. The file is read from `embed.FS`, which is immutable at compile time — no runtime file-system access. This is the same proven pattern used by `ServeFontFile` and `ServeVendorFile` in the same file. No vulnerability — this note records the new surface for traceability.
- **Impact:** None. `path.Base` + suffix check + `embed.FS` make traversal impossible.
- **Empfehlung:** No action required.
- **Referenz:** CWE-22 (mitigated)

### [INFO] Inline `style` attribute removed from base template

- **Datei:** `gateway/internal/admin/templates/layouts/base.html:70`
- **Beschreibung:** The logout form previously used `style="margin:0;padding:0;"` which would have violated the strict `style-src 'self'` CSP. This was replaced with Tailwind utility classes `class="m-0 p-0"`. No inline `style` attributes remain in any template. This is a positive finding — the CSP can be enforced without `unsafe-inline` in `style-src`.
- **Impact:** Positive — CSP integrity maintained.
- **Empfehlung:** No action required.

### [INFO] CSP policy analysis — strict and complete

- **Datei:** `gateway/internal/admin/middleware.go:187`
- **Beschreibung:** The CSP policy `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'` is well-constructed:
  - No `unsafe-inline` or `unsafe-eval` in any directive
  - `frame-ancestors 'none'` provides clickjacking protection (supersedes X-Frame-Options in modern browsers)
  - `base-uri 'self'` prevents `<base>` tag injection
  - `form-action 'self'` prevents form hijacking
  - `object-src 'none'` blocks plugin-based attacks
  - `connect-src 'self'` permits the SSE endpoint while blocking external exfiltration
  - `img-src 'self' data:` allows data-URI images (DaisyUI icons) without opening to external sources
- **Impact:** Positive — defense-in-depth well-implemented.
- **Empfehlung:** No action required.

### [INFO] Test coverage explicitly validates security properties

- **Datei:** `gateway/internal/admin/security_headers_test.go`
- **Beschreibung:** Three test functions cover: (1) all four non-HSTS headers present on every admin path and HSTS absent on plain HTTP, (2) HSTS present when `r.TLS != nil`, (3) HSTS present when `X-Forwarded-Proto: https` is set. The CSP value is asserted character-for-character against the expected constant. This is a positive testing pattern — security headers are regression-protected.
- **Impact:** Positive — prevents silent header removal or weakening in future changes.
- **Empfehlung:** No action required.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ — not affected (no DB / compliance changes) |
| `reason` field on compliance access         | ✅ — not affected |
| Audit-log immutability                      | ✅ — not affected |
| `instance_admin` notification (if in-scope) | ✅ — not in scope |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ — not affected (no auth handler changes) |
| Matrix Power Level checks                   | ✅ — not affected |
| No hardcoded secrets                        | ✅ — no secrets in diff |
| TLS 1.3 enforcement                         | ✅ — not affected (no `tls.Config` changes) |
| AES-256-GCM correctness                     | ✅ — not affected |
| Ed25519 verify-before-accept                | ✅ — not affected |
| No secrets in logs / error messages         | ✅ — no log statements added; error responses are generic "Not Found" |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 0 |
| LOW       | 1 |
| INFO      | 4 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
