# Security Review — Epic 7: Full Admin UI — 2026-04-30

**Agent:** Kassandra
**Diff base:** `b80c3e0..HEAD`
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL` (default), `model=claude-sonnet-4-6`

---

## Executive Summary

Epic 7 adds 81 files (12 283 insertions) implementing the Full Admin UI in Go SSR templates with Tailwind/DaisyUI. The CSRF middleware exists and is correctly implemented — the `CSRFMiddleware()` function performs constant-time double-submit verification. The architectural problem is that the 10 new state-changing POST routes are wired *without* the `csrf()` wrapper, so the verification step never executes. This is a fully formed HIGH finding: an authenticated admin session cookie is sufficient to forge a state-changing request. No CRITICAL findings were identified — the Admin UI surface is admin-only, impact is bounded, and all data is currently stub-only. Two MEDIUM hygiene gaps are noted. The migration (000027) is correctly constructed and preserves the `audit_log` immutability invariant.

---

## Findings

### [HIGH] CSRF protection not enforced on all state-changing POST routes

- **CWE / OWASP:** CWE-352 / A01:2021 – Broken Access Control (CSRF bypass)
- **File:** `gateway/cmd/gateway/main.go:313–344`
- **Description:** The `CSRFMiddleware()` function exists, is correctly implemented (constant-time `subtle.ConstantTimeCompare`, double-submit-cookie), and is applied to GET routes (where it sets and injects the token) and to `POST /admin/logout`. However, the 10 new Epic 7 POST routes are registered without the `csrf()` wrapper. The middleware never runs on these routes, which means the `_csrf` form field embedded in every template is never verified server-side. An attacker who can trick an authenticated admin into visiting a crafted page can forge any of these state changes:
  - `POST /admin/users/{userId}/display-name`
  - `POST /admin/users/{userId}/role`
  - `POST /admin/users/{userId}/deactivate`
  - `POST /admin/users/{userId}/reactivate`
  - `POST /admin/rooms/{roomId}/name`
  - `POST /admin/rooms/{roomId}/archive`
  - `POST /admin/rooms/{roomId}/unarchive`
  - `POST /admin/config`
  - `POST /admin/config/role-mapping`
  - `POST /admin/compliance/{id}/approve`
  - `POST /admin/compliance/{id}/reject`
  - The comment `// intentionally NO csrf() wrapper (stub phase; see TODO in handler)` acknowledges the gap, but "stub phase" does not reduce the runtime risk — the middleware is present, the routes are live.
- **Impact:** A forged POST (via malicious `<img>`, `<form>`, or `fetch()` from any origin) by a logged-in admin can deactivate users, change roles, archive rooms, approve or reject compliance requests, or overwrite server configuration. Impact is bounded to the admin surface — no end-user data exfiltration is possible in the current stub implementation, and exploitability requires an authenticated admin session.
- **Empfehlung:** Add `csrf()` to each of the 10 POST routes in `main.go`, exactly as `POST /admin/logout` is wired: `mux.Handle("POST /admin/users/{userId}/role", csrf(sessionGuard(...)))`. The templates already embed `_csrf` hidden fields and `confirm_dialog` passes `CSRFToken` — no template changes are needed.
- **Referenz:** OWASP ASVS V4.10.1, NIST AC-3, CWE-352

---

### [MEDIUM] Missing request body size limit on new POST handlers

- **CWE / OWASP:** CWE-400 – Uncontrolled Resource Consumption
- **File:** `gateway/cmd/gateway/main.go:313–344`
- **Description:** The existing `POST /admin/logout` and bootstrap POST routes are wrapped with `bodyLimit64KiB(...)`. The 10 new Epic 7 POST routes have no `bodyLimit` wrapper. `r.ParseForm()` internally calls `http.MaxBytesReader` only if one is already set; without it, Go's `net/http` reads up to 10 MiB (the `defaultMaxMemory` constant) into memory per form submission. An authenticated admin can send a multi-megabyte request body and cause excessive memory allocation in the gateway process.
- **Impact:** Denial of service against the gateway process. Requires a valid admin session cookie — attack surface limited to admins but does not require social engineering.
- **Empfehlung:** Wrap each new POST route with `bodyLimit64KiB(...)`, matching the pattern already used for `POST /admin/logout`:
  ```
  mux.Handle("POST /admin/users/{userId}/role",
      bodyLimit64KiB(csrf(sessionGuard(...))))
  ```
- **Referenz:** OWASP ASVS V13.2.6, CWE-400

---

### [MEDIUM] Flash message content is attacker-influenced (no allowlist enforcement)

- **CWE / OWASP:** CWE-79 – Improper Neutralization of Input During Web Page Generation
- **File:** `gateway/internal/admin/users.go:94–95`, `rooms.go:92–93`, `config.go:24–25`, `role_mapping.go:31–32`, `compliance_handler.go:28–29`
- **Description:** The GET handlers read `r.URL.Query().Get("flash")` verbatim and assign the string to `AlertBannerData.Message`, which is then rendered via Go's `html/template`. Go's auto-escaping does prevent classic HTML injection in `{{ .Message }}` — the rendered output is HTML-entity-escaped. There is no CRITICAL or HIGH XSS risk here with `html/template`. However, the flash parameter is completely open: any string can be injected by constructing a URL (e.g. `GET /admin/users/usr-001?flash=You+have+been+hacked`). This is a social-engineering / phishing vector — a crafted link produces an official-looking success banner with arbitrary text rendered on the Admin UI page.
  - All POST redirect targets are hardcoded (`"/admin/users/"+userID+"?flash=Role+updated"`), so the flash text itself is not attacker-influenced in normal use. But a direct GET to the detail page with `?flash=<arbitrary>` is fully open.
- **Impact:** An attacker with a valid admin account (or who can trick an admin into visiting a crafted URL) can display arbitrary text in a success banner. Not RCE or data exfiltration. The risk is bounded and does not trigger the Rufschädigungstest, but it is a defense-in-depth gap.
- **Empfehlung:** Apply an allowlist to the flash parameter in GET handlers — accept only known safe values (`"Role+updated"`, `"Display+name+updated"`, etc.) or use a server-side flash mechanism (session-keyed, single-read) to eliminate the open parameter entirely. Minimum fix: reject `flash` values longer than 80 characters or not matching a known-good set.
- **Referenz:** OWASP ASVS V5.2.1, CWE-79 (low severity variant)

---

### [INFO] Migration 000027 correctly preserves audit_log immutability

- **CWE / OWASP:** N/A
- **File:** `gateway/migrations/000027_grant_delete_to_nebu_app.up.sql`
- **Description:** The migration grants DELETE to `nebu_app` on all public tables except `audit_log` and `schema_migrations`. The explicit `REVOKE DELETE ON audit_log FROM nebu_app` in step 3 is belt-and-suspenders over the loop exclusion. The `ALTER DEFAULT PRIVILEGES` future-proofs new tables. The dynamic PL/pgSQL loop uses `format('GRANT DELETE ON public.%I TO nebu_app', r.tablename)` with `%I` identifier quoting — not `%s` — which prevents SQL injection via table names. The down migration performs the inverse `REVOKE` correctly.
- **Referenz:** Nebu Audit-Log Immutability Invariant

---

### [INFO] `html/template` auto-escaping verified across all new templates

- **CWE / OWASP:** CWE-79
- **File:** `gateway/internal/admin/templates/`, `gateway/internal/admin/handler.go`
- **Description:** No usage of `template.HTML(...)`, `template.JS(...)`, or `template.URL(...)` type conversions was found in the Epic 7 diff. All user-controlled values (display names, room names, config values, error messages) are rendered via `{{ .Field }}` in standard `html/template` templates, which HTML-entity-encodes all values. The `TemplateHandler.render()` function uses `template.ParseFS` and `tmpl.ExecuteTemplate` — the standard safe path.

---

### [INFO] Security headers applied globally including new routes

- **CWE / OWASP:** CWE-693
- **File:** `gateway/internal/admin/middleware.go:190–207`
- **Description:** `SecurityHeadersMiddleware` sets `Content-Security-Policy` (with `script-src 'self'`, `frame-ancestors 'none'`, `object-src 'none'`), `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and HSTS. As a global middleware applied to the admin mux, all new Epic 7 routes inherit these headers without any per-route action required.

---

### [INFO] No new SQL queries in Epic 7 handlers

- **CWE / OWASP:** CWE-89
- **File:** N/A
- **Description:** All new Epic 7 handlers operate exclusively on in-memory stub data. No `db.Query`, `db.Exec`, or `db.QueryRow` calls appear in the Epic 7 diff. SQL injection risk is not introduced in this epic.

---

### [INFO] Role-mapping regex scope is intentional and appropriately narrow

- **CWE / OWASP:** N/A
- **File:** `gateway/internal/admin/role_mapping.go:14`, `role_mapping.go:67–77`
- **Description:** The `oidcGroupClaimRe` (`^[a-zA-Z0-9:_-]+$`) is applied only to `oidc_group_claim` (the OIDC claim key name). `instance_admin_group` and `compliance_user_group` receive only length validation (max 100 runes), not the regex. This asymmetry is intentional and correct: OIDC claim names have a well-defined character set per RFC 7519, but group values are arbitrary strings set by the IdP operator and may contain spaces, dots, or Unicode. Length-only validation for group values is appropriate.

---

## Nebu Invariants Check

| Invariant                                   | Status | Notes |
|---------------------------------------------|:------:|-------|
| Compliance RSP coverage                     | ✅     | All new handlers operate on in-memory stubs — no DB queries against compliance tables. No RSP bypass possible. |
| `reason` field on compliance access         | ✅     | Stub-only compliance handlers; no real compliance data accessed. |
| Audit-log immutability                      | ✅     | Migration 000027 explicitly REVOKEs DELETE on `audit_log` after the dynamic GRANT loop. No migration touches the audit table structure. |
| `instance_admin` notification (if in-scope) | ⚠️     | No notification hook for bulk compliance approvals via the UI (stub phase). Not verifiable from this diff whether Epic 6 will wire this — needs follow-up. |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅     | New routes are protected by `sessionGuard`, which verifies the HMAC-signed `admin_session` cookie. OIDC token validation happens at login time (pre-existing). No new token-reading code in Epic 7. |
| Matrix Power Level checks                   | ✅     | Not in scope — Epic 7 is Admin UI only, no Matrix room mutations. |
| No hardcoded secrets                        | ✅     | No API keys, tokens, or credentials found in the diff. Stub data contains fictional emails only. |
| TLS 1.3 enforcement                         | ⚠️     | No new `tls.Config` in the diff. Existing TLS config is unchanged. Not verifiable from this diff alone. |
| AES-256-GCM correctness                     | ✅     | No new crypto in Epic 7. |
| Ed25519 verify-before-accept                | ✅     | Not in scope for Admin UI. |
| No secrets in logs / error messages         | ✅     | `slog.Error` calls in `handler.go:122–123` log template name and error only — no session tokens, cookies, or credentials. `http.Error` responses return generic messages ("bad request", "Internal Server Error"). |

---

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0     |
| HIGH      | 1     |
| MEDIUM    | 2     |
| LOW       | 0     |
| INFO      | 5     |

---

## Pipeline Decision

**HIGH findings present, `blocking_severity: CRITICAL` (default)** — Pipeline proceeds with a warning. The HIGH finding (missing CSRF enforcement on 10 POST routes) and the MEDIUM body-size-limit gap should be converted to a tracked follow-up story before Epic 7 routes serve real data (Epic 6 integration). The CSRF TODO comments (`// TODO(story-7-csrf)`) are already present; they need a concrete story attached.

Required action before Epic 6 integration: add `csrf()` and `bodyLimit64KiB()` to all 10 new POST routes. The fix is mechanical — no architectural change required.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
