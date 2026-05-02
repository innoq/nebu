# Security Review — Story 6.3 (Admin API Router + RequireRole Middleware) — 2026-05-01

**Agent:** Kassandra
**Diff base:** `git diff --staged`
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default — `.claude/security-agent.yaml` not present), `model=claude-sonnet-4-6`

## Executive Summary

The new `RequireRole` middleware and `RegisterAdminRoutes` router implement a single, narrow access gate for the
Admin API stub routes. Middleware order is correct (JWT outermost), the empty-role branch fails closed with 401,
cross-role checks use strict equality with no fallback, and no PII is leaked in the error responses. One
defense-in-depth gap (no rate limiter on `/api/v1/admin/*` GETs) is documented as a TODO and only relevant once
the 501 stubs are replaced — recorded here as MEDIUM so it is not lost.

## Findings

### [MEDIUM] No rate limiter on `/api/v1/admin/*` Admin API stub routes

- **CWE / OWASP:** CWE-770 (Allocation of Resources Without Limits or Throttling) / A04:2021 (Insecure Design)
- **Datei:** `gateway/internal/api/router.go:35-38`
- **Beschreibung:** Each `/api/v1/admin/{config,metrics,rooms,users}` route is wrapped only with
  `jwtMW(RequireRole("instance_admin")(handler))`. Compared with the existing pattern in
  `gateway/cmd/gateway/main.go` (e.g. line 949: `complianceRL(jwtMiddleware(...))` for the live
  `GET /api/v1/compliance/access-requests`), no per-route rate limiter is attached. The handlers currently
  return 501 stubs, so the practical DoS risk is small (no DB hit, no allocation, response is small JSON).
  Once Stories 6.4+ wire real handlers (DB queries on users/rooms, metric aggregation), the missing limiter
  will widen to a real DoS vector that an authenticated `instance_admin` could abuse, and the absence of an
  IP-tier limiter also means `RequireRole`'s 401/403 path can be hammered cheaply by any unauthenticated
  caller. The author already left a `TODO(Stories 6.4+)` comment in `router.go:25-26` acknowledging this.
- **Impact:** No exploit today (501 stubs). Becomes HIGH once real handlers land if the TODO is forgotten.
- **Empfehlung:** Track this explicitly: either (a) add `adminAPIRL` (e.g. 60 req/min, burst 20 — analogous to
  `adminRL` already declared at `main.go:219`) inside `RegisterAdminRoutes` now, accepting that it currently
  protects only 501 stubs; or (b) add a guard test in 6.4+ that asserts a rate limiter is present in the
  middleware chain before each handler graduates from 501 to live. Option (a) is simpler and removes the
  reliance on a TODO surviving into the next story.
- **Referenz:** OWASP ASVS V11.1.4, NIST SP 800-53 SC-5

### [INFO] Middleware order is correct and explicitly tested

- **Datei:** `gateway/internal/api/router.go:35-38`, `gateway/internal/api/router_test.go:176-198`
- **Beschreibung:** `mux.Handle(..., jwtMW(RequireRole(role)(http.HandlerFunc(sh.Method))))` — Go function
  composition makes `jwtMW` the outermost layer at runtime, so `JWTMiddleware` populates
  `ContextKeySystemRole` before `RequireRole` reads it. `TestRegisterAdminRoutes_JWTRunsBeforeRole` is a
  regression guard against accidental order inversion. Order matters because reversing it would let
  `RequireRole` fall through to its empty-role 401 path even on otherwise valid JWTs — which fails closed but
  would render the entire gate non-functional.
- **Impact:** Positive observation worth preserving for the audit trail.

### [INFO] Fail-closed empty-role branch is well-defined

- **Datei:** `gateway/internal/api/middleware.go:28-33`
- **Beschreibung:** `r.Context().Value(middleware.ContextKeySystemRole).(string)` — the comma-ok-style
  `_` discards the type-assertion failure, and an absent / non-string value resolves to `""`, which the
  immediately following `if systemRole == ""` branch maps to 401. This means a request that reaches
  `RequireRole` without first traversing `JWTMiddleware` is rejected, even if the route registration is later
  edited incorrectly. Note that `auth.MapSystemRole` (`gateway/internal/auth/roles.go:6-13`) never returns the
  empty string for a JWT-authenticated request — it returns `"user"` for any non-privileged claim — so this
  401 path only fires on the absence of `JWTMiddleware`, not on the absence of a role claim. The unit test
  `TestRequireRole_EmptyStringRole_Returns401` covers an explicit empty value as a defense-in-depth check.
- **Impact:** Positive observation. The middleware is a strict allow-list against two specific role strings.

### [INFO] No PII in error responses

- **Datei:** `gateway/internal/api/middleware.go:31, 36`
- **Beschreibung:** 401 body is the static `"Missing access token"`. 403 body is
  `<role> role required` where `<role>` is a developer-supplied constant (`"instance_admin"` or
  `"compliance_officer"`) at `RegisterAdminRoutes`-call time, not a user-controlled string from the request.
  No subject ID, email, claim contents, or token bytes are reflected in the response.
- **Impact:** No information disclosure.

### [INFO] No timing-attack surface

- **Datei:** `gateway/internal/api/middleware.go:35`
- **Beschreibung:** `systemRole != role` compares two short well-known role strings. Both ends of the
  comparison are public values (the developer's required role constant and the JWT claim). No secret material
  is involved, so a constant-time compare (`crypto/subtle.ConstantTimeCompare`) is not required and would only
  be cargo-cult.
- **Impact:** No exploit path.

### [INFO] Compliance route intentionally not migrated — guarded against duplicate registration

- **Datei:** `gateway/internal/api/router.go:17-20`, `gateway/cmd/gateway/main.go:944-949`,
  `gateway/internal/api/router_test.go:152-167`
- **Beschreibung:** `GET /api/v1/compliance/access-requests` continues to be owned by `main.go` because the
  live Story 5.4 handler (`accessRequestHandler.GetAccessRequests`) carries the `complianceRL` rate limiter
  and an in-handler role check at `gateway/internal/compliance/handler.go:322`. Re-registering the same
  pattern from `RegisterAdminRoutes` would trigger Go 1.22's ServeMux duplicate-pattern panic at startup
  (denial-of-service on boot). A regression test
  (`TestRegisterAdminRoutes_ComplianceRoute_NotRegisteredByRouter`) and an explicit comment in `main.go:943`
  guard against accidental re-registration. Story 6.11 will perform the migration.
- **Impact:** No issue — this is a deliberate transitional state with appropriate guard rails.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ |
| `reason` field on compliance access         | ✅ |
| Audit-log immutability                      | ✅ |
| `instance_admin` notification (if in-scope) | ✅ |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ |
| Matrix Power Level checks                   | ✅ |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ✅ |
| AES-256-GCM correctness                     | ✅ |
| Ed25519 verify-before-accept                | ✅ |
| No secrets in logs / error messages         | ✅ |

Notes on ✅ status: the diff does not touch DB layer, crypto, audit tables, compliance handlers, signed
events, TLS configuration, or secret material. Wiring relies on `JWTMiddleware`'s already-audited token
validation (signature, `aud`, `exp` per `gateway/internal/middleware/auth.go:96-106`) — which is visibly
applied to every `/api/v1/admin/*` route via `jwtMW(RequireRole(...)(...))`. Compliance access continues to
flow through the unchanged `accessRequestHandler` path in `main.go:949` with `complianceRL` and the
in-handler role check at `compliance/handler.go:322`. Audit-log table grants and migration files are not in
the diff.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 1 |
| LOW       | 0 |
| INFO      | 5 |

## Pipeline Decision

- **CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed. The MEDIUM finding (missing rate limiter on
  `/api/v1/admin/*`) should be tracked: either fixed in 6.3 by adding `adminAPIRL` now, or covered by an
  explicit guard test that blocks Stories 6.4+ from graduating a stub to a live handler without a rate
  limiter in the chain.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
