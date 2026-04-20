# Security Review — Story 5.10: Bootstrap Mode Guard — Replay Prevention — 2026-04-20

**Agent:** Kassandra
**Diff base:** `git diff --staged` (7 files, +284 / -12)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4-6`

## Executive Summary

This diff closes the two primary entry points of the admin-takeover chain identified in the security audit: `LoginStartHandler` now rejects `mode=bootstrap` after completion (403), and `POST /admin/bootstrap/select-claim` is wired behind `BootstrapGuard`. The cookie `Path` narrowing from `/admin` to `/admin/callback` is a correct defense-in-depth measure. One residual TOCTOU window exists in `CallbackHandler` (bootstrap branch does not re-check `IsBootstrapActive`), but the downstream guard on `select-claim` prevents exploitation — the remaining risk is limited to inert draft writes, explicitly scoped for Story 5.11.

## Findings

### [MEDIUM] TOCTOU window in CallbackHandler bootstrap branch

- **CWE / OWASP:** CWE-367 (Time-of-Check Time-of-Use) / A01:2021 (Broken Access Control)
- **File:** `gateway/internal/admin/auth.go:493`
- **Description:** `CallbackHandler` branches into the bootstrap flow when `sc.Mode == "bootstrap"` (read from the signed state cookie set during `LoginStartHandler`). It does not call `IsBootstrapActive()` again. An attacker who starts `GET /admin/login/start?mode=bootstrap` while bootstrap is still active, then delays the OIDC callback until after another admin completes bootstrap, will land in the bootstrap branch of `CallbackHandler`. This writes `bootstrap_sub` and `bootstrap_email` to `bootstrap_draft` (lines 497-505) and renders the claim-selection page.
- **Impact:** The attacker sees a claim-selection form and can attempt `POST /admin/bootstrap/select-claim`. However, the new `BootstrapGuard` on that route (added in this diff) blocks the POST with a 302 redirect. The net impact is limited to: (a) two inert rows in `bootstrap_draft`, and (b) a rendered HTML page with no actionable submit path. No OIDC configuration or `admin_group_claim` can be overwritten.
- **Recommendation:** Add a re-check of `IsBootstrapActive()` at the top of the `sc.Mode == "bootstrap"` branch in `CallbackHandler` (or in Story 5.11's transactional wrapper, which is the declared scope for this fix). Return 403 if bootstrap is no longer active, to close the window completely and prevent unnecessary draft writes.
- **Reference:** OWASP ASVS V1.4.5 (access control applied at every layer), NIST AC-3

### [INFO] select-claim route now behind BootstrapGuard

- **File:** `gateway/cmd/gateway/main.go:224`
- **Description:** `POST /admin/bootstrap/select-claim` changed from bare `mux.HandleFunc` to `mux.Handle(..., guard(http.HandlerFunc(...)))`. This is the primary fix for the audit finding — an unauthenticated POST to `select-claim` after bootstrap completion is now blocked by `BootstrapGuard` with a 302 redirect to `/admin/dashboard`. The inner handler (`ClaimSelectionHandler`) is never reached and no DB writes occur.
- **Impact:** Positive — closes the most direct attack vector in the admin-takeover chain.

### [INFO] Cookie Path narrowed to /admin/callback

- **File:** `gateway/internal/admin/auth.go:307` (set) and `auth.go:486` (delete)
- **Description:** The `admin_oidc_state` cookie `Path` is changed from `/admin` to `/admin/callback`. Both the Set-Cookie (in `LoginStartHandler`) and the delete-cookie (in `CallbackHandler`) use the same path, ensuring correct deletion. This prevents the state cookie from being transmitted on unrelated `/admin/*` requests.
- **Impact:** Positive — reduces cookie exposure surface. Defense-in-depth.

### [INFO] BootstrapGuard redirect target changed to /admin/dashboard

- **File:** `gateway/internal/admin/middleware.go:111`
- **Description:** When bootstrap is complete and the request path starts with `/admin/bootstrap`, the redirect target is now `/admin/dashboard` instead of `/admin/login`. This is semantically correct — a completed bootstrap means the instance is set up, and `/admin/dashboard` (behind `SessionGuard`) will handle the auth redirect if needed.
- **Impact:** Positive — prevents a redirect loop scenario where `/admin/login` could redirect back to bootstrap paths.

### [INFO] Pre-existing: error info disclosure in LoginStartHandler

- **File:** `gateway/internal/admin/auth.go:244`
- **Description:** `http.Error(w, "Failed to load OIDC configuration: "+err.Error(), ...)` exposes the raw `err.Error()` to the client. This is not introduced by this diff (the line is unchanged) but is worth noting as a pre-existing CWE-209 pattern for a future hardening pass. The error may contain DB connection details or internal paths depending on the failure mode.
- **Impact:** Observation only — no change in this diff. Recommend addressing in a future hardening story.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ N/A — no compliance table access in diff |
| `reason` field on compliance access         | ✅ N/A — no compliance data access in diff |
| Audit-log immutability                      | ✅ N/A — no audit table changes |
| `instance_admin` notification (if in-scope) | ✅ N/A — no scope escalation in diff |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ — `CallbackHandler` still verifies via `provider.Verifier(&oidc.Config{ClientID: clientID}).Verify()` (line 444). The new guard in `LoginStartHandler` operates before the OIDC flow starts, so no token validation is bypassed. |
| Matrix Power Level checks                   | ✅ N/A — no room-scoped operations |
| No hardcoded secrets                        | ✅ — no string literals for keys, tokens, or passwords in the diff. Test files use `"test-secret"` which is appropriate for unit tests. |
| TLS 1.3 enforcement                         | ✅ N/A — no `tls.Config` changes |
| AES-256-GCM correctness                     | ✅ N/A — no encryption changes |
| Ed25519 verify-before-accept                | ✅ N/A — no signature verification in scope |
| No secrets in logs / error messages         | ⚠️ — Pre-existing: `auth.go:244` returns `err.Error()` to the HTTP client (CWE-209). Not introduced by this diff. New error responses in this diff (`auth.go:282` "Internal Server Error", `auth.go:286` "Bootstrap already completed") are safe — no internal state leaked. |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 1 |
| LOW       | 0 |
| INFO      | 4 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed. The single MEDIUM finding (TOCTOU in CallbackHandler) is mitigated by the downstream BootstrapGuard and explicitly scoped for Story 5.11 (transactional bootstrap completion).

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
