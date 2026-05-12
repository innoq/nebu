# Security Review — Story 9-2: Admin UI Users → gRPC Core Integration

**Reviewer:** Kassandra (BMAD Security Reviewer)
**Date:** 2026-05-04
**Branch:** `feature/phase-2-epic-9`
**Diff under review:** `git diff --staged` (10 files; gateway focus: `gateway/internal/admin/users.go`, `gateway/cmd/gateway/main.go`)

---

## Classification

**HIGH** — One HIGH (audit-log gap on state-changing admin actions) and one MEDIUM (defense-in-depth role gate inside handlers absent). No CRITICAL.

The HIGH is blocking under SEC Gate 1 policy: state-changing admin actions are part of the audit-log invariant set (Compliance RSP, ADR-008-adjacent), and the new code path silently bypasses an audit pathway that the parallel JSON Admin API has implemented since Story 6.5.

---

## Severity Counts

- **CRITICAL:** 0
- **HIGH:** 1
- **MEDIUM:** 2
- **LOW:** 2
- **INFO:** 2

---

## Scope of Review

This story wires `gateway/internal/admin/users.go` (the Admin **HTML** UI) to the real Elixir gRPC RPCs introduced in Story 9-1: `ListAdminUsers`, `GetAdminUser`, `DeactivateUser`, `ReactivateUser`, `UpdateUserRole`. Routes (registered in `gateway/cmd/gateway/main.go`):

| Route | Middleware chain |
|---|---|
| `GET  /admin/users` | `csrf → sessionGuard` |
| `GET  /admin/users/{userId}` | `csrf → sessionGuard` |
| `POST /admin/users/{userId}/role` | `bodyLimit64KiB → csrf → sessionGuard` |
| `POST /admin/users/{userId}/deactivate` | `bodyLimit64KiB → csrf → sessionGuard` |
| `POST /admin/users/{userId}/reactivate` | `bodyLimit64KiB → csrf → sessionGuard` |
| `POST /admin/users/{userId}/display-name` | `bodyLimit64KiB → csrf → sessionGuard` |

Outer wrapper (in `main.go` line 1159): `admin.SecurityHeadersMiddleware`.

---

## Findings

### HIGH-1 — State-changing admin actions are not audited

**File:** `gateway/internal/admin/users.go` lines 303–404 (UpdateRoleHandler, DeactivateUserHandler, ReactivateUserHandler)
**Companion file:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` lines 1785–1868 (deactivate_user, reactivate_user, update_user_role)
**CWE:** CWE-778 (Insufficient Logging), CWE-223 (Omission of Security-Relevant Information)
**Frameworks:** OWASP A09:2021 (Security Logging and Monitoring Failures), ASVS v4.0 §7.1, NIST AU-2

**Evidence.** Three state-changing admin handlers (`UpdateRoleHandler`, `DeactivateUserHandler`, `ReactivateUserHandler`) call `coreClient.UpdateUserRole / DeactivateUser / ReactivateUser` directly and emit no audit-log entry. The Elixir Core RPCs `deactivate_user/2`, `reactivate_user/2`, and `update_user_role/2` (server.ex lines 1785, 1818, 1844) likewise do **not** call `audit_writer_module().log(...)` — neither side of the wire produces a row in `audit_log`.

By contrast, the parallel JSON Admin API (`gateway/internal/api/server.go` lines 680–685, 728–732) explicitly emits `audit.LogEvent(ctx, s.CoreClient, actorID, "user_deactivated", "user", userID, …)` and `"user_reactivated"` after every successful RPC — the Admin UI route now silently bypasses this control.

**Impact.** Admin actions on user state (deactivate, reactivate, role change — including escalation to `instance_admin`) leave **no forensic trail**. A compromised or malicious admin account can deactivate users, change roles, or reactivate previously-disabled accounts and the operator has no record. Compliance RSP requires an immutable audit log for all admin user-management actions; this gap breaks that invariant for the Admin UI surface.

**Reproduction.** Issue `POST /admin/users/usr-001/role` with form body `role=instance_admin` from a valid admin session, then `SELECT * FROM audit_log WHERE target_id='usr-001' ORDER BY ts DESC LIMIT 5;` — no `role_changed` / `user_role_updated` row appears. The same query against a deactivation via `POST /api/v1/admin/users/usr-001/deactivate` does produce a row (`user_deactivated`).

**Fix direction.** Either (a) call `audit.LogEvent` from the Admin UI handlers after each successful gRPC call (mirror the JSON API pattern), or (b) move audit emission into the Elixir `deactivate_user / reactivate_user / update_user_role` server handlers via `Compliance.AuditWriter.log/6`. Option (b) is preferable because it covers both call sites uniformly; option (a) is acceptable as a near-term fix. Required actions per AuditWriter allowlist: `user_deactivated`, `user_reactivated`, `user_role_updated` (or equivalent — confirm against `@known_actions` in `compliance/audit_writer.ex`).

---

### MEDIUM-1 — No in-handler role check; entire admin UI security depends on a single upstream OIDC group claim

**File:** `gateway/internal/admin/middleware.go` lines 90–145 (`SessionGuardWithStore`)
**Files relying on it:** `gateway/cmd/gateway/main.go` lines 316–321 (the four state-changing admin/users routes)
**CWE:** CWE-862 (Missing Authorization), CWE-1390 (Weak Authentication)
**Frameworks:** OWASP A01:2021 (Broken Access Control), STRIDE Elevation-of-Privilege, ASVS v4.0 §4.1

**Evidence.** `SessionGuardWithStore` validates the `admin_session` cookie against `admin_sessions` (sid, user_id, expires_at, revoked_at) and stores `user_id` in context — it does **not** read or enforce a role. The `AdminSession` struct (`gateway/internal/admin/session_store.go` line 9) has no `Role` field. None of the four POST handlers in `users.go` (`UpdateRoleHandler` line 303, `DeactivateUserHandler` line 347, `ReactivateUserHandler` line 379, `UpdateDisplayNameHandler` line 413) re-checks the actor's system role.

The full authorization gate is therefore the OIDC callback's `auth.MatchesAdminGroupClaim(claims, adminGroupClaim)` test in `gateway/internal/admin/auth.go` line 741. **For Story 9-2 this is sufficient** — only users matching the `admin_group_claim` (typically `instance_admin`) ever get an `admin_session` cookie, so a `compliance_officer`-only user cannot reach these routes today.

**Impact / future risk.** This design is fragile under foreseeable Epic 7+ changes:
- If a future story grants `compliance_officer` users read-only access to `/admin/*` (a likely Phase 2 path — they already have a separate admin namespace under `/admin/compliance/*`), the deactivate/reactivate/role-update routes will instantly inherit that access with no in-handler check. Privilege escalation by default.
- Operators who configure a permissive `admin_group_claim` (e.g. mapping multiple groups to admin) cannot tighten user-management mutations independently.
- The JSON Admin API path (`/api/v1/admin/users/*`) does enforce `RequireRole("instance_admin", rolesRepo)` — see `gateway/cmd/gateway/main.go` line 1149 — so the two surfaces (HTML UI + JSON API) have **inconsistent** authorization models for the same logical operation.

**Fix direction.** Add an `instance_admin`-required check at the start of each state-changing handler in `users.go`, sourced from the session row's role (requires extending `AdminSession` + `admin_sessions` schema with a `role` column populated from the OIDC claim at session creation). Alternatively, wrap the four POST routes in `main.go` with a new `RequireAdminRole(sessionStore, rolesRepo)` middleware analogous to the JSON API's `RequireRole`. Document the resulting invariant in an ADR.

---

### MEDIUM-2 — Admin UI deactivation skips guards present in the JSON API (state-machine, reason field, idempotency)

**File:** `gateway/internal/admin/users.go` lines 347–373 (`DeactivateUserHandler`), lines 379–405 (`ReactivateUserHandler`)
**CWE:** CWE-840 (Business Logic Errors), CWE-754 (Improper Check for Unusual or Exceptional Conditions)
**Frameworks:** OWASP A04:2021 (Insecure Design)

**Evidence.** The JSON API's `DeactivateAdminUser` (`gateway/internal/api/server.go` lines 634–688) enforces:

1. A minimum 10-character `reason` field (audit traceability).
2. A 409 conflict response when the user is already deactivated.
3. A 409 conflict on reactivation when the target is `anonymized` or in `keys_deleted` state.
4. A 404 when the user does not exist.

The Admin UI handler enforces none of these — it issues the gRPC call unconditionally, relies on the Elixir core to return `NotFound` for non-existent users (handled), but accepts an empty `reason` (in fact does not even read one) and silently re-activates a previously-anonymized account if the underlying RPC permits it. The Elixir core's `reactivate_user/2` (server.ex line 1818) only checks DB `set_is_active(user_id, true)` — there is no anonymized-state guard there either.

**Impact.** Inconsistent behaviour between Admin UI and JSON API for the same logical operation; a deactivation performed via the UI is unrecoverable for forensic purposes (no reason captured); reactivation via UI may bypass GDPR-driven invariants ("anonymized users must not be reactivated"). Severity is MEDIUM rather than HIGH only because the operations remain restricted to `instance_admin` (see MEDIUM-1) and no privilege escalation is involved — the risk is operational integrity and compliance scope.

**Fix direction.** Extend the deactivation form template with a required `reason` textarea (≥10 chars), validate on the server side, propagate to the gRPC request (extend `DeactivateUserRequest` with a `reason` field — Story 9-1 follow-up), and add anonymized/keys_deleted guards to either the Admin UI handler or, preferably, the Elixir `reactivate_user` RPC.

---

### LOW-1 — Flash messages emitted by error paths are dropped silently; user sees no feedback after a server-side failure

**File:** `gateway/internal/admin/users.go` lines 324–328, 355–359, 387–391
**File:** `gateway/internal/admin/flash.go` lines 5–17 (`allowedFlashMessages` whitelist)

**Evidence.** Error redirects emit query strings such as `?flash=User+not+found`, `?flash=Error+updating+role`, `?flash=Error+deactivating+user`, `?flash=Error+reactivating+user`. None of these are present in the `allowedFlashMessages` whitelist (`flash.go` line 5). `sanitizeFlash` (line 21) drops any message not in the whitelist, so the redirect renders the page with **no error banner**. The user perceives the action as a no-op.

**Security-adjacent angle.** This is primarily a UX defect, but it does have a security tail: an operator who deactivates a user from the UI and sees no confirmation banner may retry, generating duplicate gRPC calls, increasing log noise during an incident. The whitelist itself is correct — preventing flash-injection-as-XSS — so the security control is intact.

**Fix direction.** Add the four error strings to `allowedFlashMessages` (`User not found`, `Error updating role`, `Error deactivating user`, `Error reactivating user`) and add unit-test coverage so future regressions are caught.

---

### LOW-2 — gRPC errors are logged with `slog.Warn` without redacting potentially sensitive details

**File:** `gateway/internal/admin/users.go` lines 87, 203, 213, 327, 358, 390
**CWE:** CWE-209 (Generation of Error Message Containing Sensitive Information)

**Evidence.** Each gRPC error path logs the raw `err` from the Elixir core via `slog.Warn(..., "err", err)`. The Elixir handlers raise `GRPC.RPCError` messages that include `inspect(reason)` from internal DB errors (see `server.ex` lines 1808, 1832, 1865 — `"deactivate_user DB update failed: #{inspect(reason)}"`). These messages can contain Postgres error strings, schema names, or the user_id involved.

**Impact.** No content is sent to the browser (the user-facing redirect uses generic flash strings — see LOW-1; the 500 response on `DetailHandler` line 204 emits only `"internal server error"`). The risk is operational log exposure: anyone with `slog` access can see arbitrary internal-server text. This is acceptable for an internal log destination but worth flagging as a sanitisation opportunity for log shippers exporting to a less-trusted SIEM tier.

**Fix direction.** Use `status.Code(err)` to extract the gRPC code and log only the code + a short error category, not the full message. Or sanitize at the Elixir side: replace `inspect(reason)` with a fixed string for non-NotFound errors.

---

### INFO-1 — Nil-core fallback path is well-isolated and does not create a production security gap

**File:** `gateway/internal/admin/users.go` lines 79, 111, 195, 220, 316, 331, 350, 362, 382, 394
**Outcome:** No vulnerability.

The `if h.core != nil { gRPC } else { stub }` pattern is invoked from production with a non-nil `coreClient` (`main.go` line 315: `admin.NewUsersHandler(tmplHandler, coreClient)`). The stub fallback only triggers in unit tests. The stub path mutates package-level `stubUsers` slice in-memory — a state-leakage hazard for tests (parallel tests would race) but not a production issue. Verified by the new unit-test contract `gateway/internal/admin/users_todo_test.go` which asserts no `TODO(epic-6)` markers remain in `users.go`.

**Note for future:** mark `coreClient` as a required (not variadic) constructor argument in a follow-up refactor; the variadic signature was a transitional concession (see Dev Notes in the story file). A required argument removes the entire nil-fallback class of accidents.

---

### INFO-2 — PII in responses is correctly masked; no leakage of unmasked email/display_name to the browser

**File:** `gateway/internal/admin/users.go` line 56 (`protoToStubUser`)
**Outcome:** No vulnerability.

`protoToStubUser` reads `u.GetEmailMasked()` from the gRPC response — the masking happens server-side in the Elixir core (Story 9-1 invariant). The Admin UI never sees the unmasked email and therefore cannot leak it to the browser. `DisplayName` is by design user-facing and is rendered through Go `html/template` auto-escaping (`tmpl.render` → standard `html/template`), preventing XSS. Confirmed by inspecting the rendered fields: `ConfirmDialog.Message` (line 275) interpolates `user.DisplayName` into a string but the string is then passed to the template engine, which escapes `<`, `>`, `&`, `"`, `'` in HTML context.

---

## CSRF Posture

All four state-changing routes are wrapped in `csrf` (`main.go` lines 318–321) — verified. Double-submit-cookie pattern from Story 5.13. No CSRF gap.

## Body-size Limits

All state-changing routes are wrapped in `bodyLimit64KiB` (64 KiB) — verified. Sized correctly for form bodies; matches the existing admin-POST pattern.

## Input Validation on userId

`r.PathValue("userId")` — Go 1.22+ mux path-segment matching cannot contain `/` and is URL-decoded by the standard library. No direct injection vector to gRPC: the value is passed to a typed protobuf field, not concatenated into a query string. The `userID` is concatenated into redirect Location headers (`/admin/users/<userID>?flash=...`) — Go's `http.Redirect` enforces CRLF-stripping on the Location header so no response splitting is possible. No open-redirect risk because the Location is a relative path inside `/admin/users/`. **PASS.**

## Role-value Validation

`UpdateRoleHandler` validates `role` against a hard-coded allowlist (`{instance_admin, compliance_officer, user}`) on line 310 — defence in depth even though the Elixir core also validates (server.ex line 1842). **PASS.**

## CSP / Security Headers

`SecurityHeadersMiddleware` wraps the entire `/admin` mux (`main.go` line 1159). No regression introduced by this story.

---

## Out-of-Scope Observations (not findings)

- The story file `_bmad-output/implementation-artifacts/9-2-admin-ui-users-api-integration.md` correctly tags `security_review: required` (verified via the auto-classification rule — diff touches `gateway/internal/admin/`).
- The Playwright spec `e2e/tests/features/admin/users-api-integration.spec.ts` exercises real browser flows against the running stack — no cookie forging, consistent with the Nebu TDD standard.
- The gRPC client interface `AdminUsersClient` is consumer-defined and minimal — good. *Variadic* constructor (`core ...AdminUsersClient`) is an antipattern that swallows misconfiguration; flagged as INFO-1 follow-up.

---

## Recommended Next Steps

1. **Block on HIGH-1.** Either wire `audit.LogEvent` into the three Admin UI handlers (Go side, immediate fix) or add `Compliance.AuditWriter.log/6` calls inside `deactivate_user / reactivate_user / update_user_role` in `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (preferred — covers JSON API + UI uniformly). Ship before Story 9-2 is marked `done`.
2. **Schedule a follow-up story** for MEDIUM-1 (in-handler role gate + `role` column on `admin_sessions`) and MEDIUM-2 (reason field + anonymized-state guard for UI deactivate/reactivate) — both are required before any future story extends `/admin` access beyond `instance_admin`.
3. **Triage LOW-1** in the same sprint as HIGH-1 — adding four entries to `allowedFlashMessages` is a one-line fix and removes a reproducible UX defect that masks server-side errors.
4. **Track LOW-2 and INFO-1** for the Epic 9 retrospective; neither blocks the story.

---

**End of report.**
