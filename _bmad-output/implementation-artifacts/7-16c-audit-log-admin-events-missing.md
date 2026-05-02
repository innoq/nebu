---
id: 7-16c
type: bugfix
security_review: required
created: 2026-04-30
---

# Story 7.16c: Bugfix — Audit-Log: admin_login / logout / login_failed / bootstrap_completed nicht emittiert

Status: ready-for-dev

## Story

As a compliance officer,
I want admin login, logout, login failure, and bootstrap completion to be recorded in the audit log,
so that the audit trail is complete and `TestAdmin*_EmitsAuditEntry` Integration-Tests grün werden.

## Context / Background

**Fehlende Audit-Events (4 Test-Failures):**
```
audit_integration_test.go:172: timed out waiting for audit action="admin_login"; received: []
audit_integration_test.go:196: timed out waiting for audit action="bootstrap_completed"; received: []
audit_integration_test.go:219: timed out waiting for audit action="admin_logout"; received: []
audit_integration_test.go:243: timed out waiting for audit action="admin_login_failed"; received: []
```

**Root cause:** Die betroffenen Handler rufen `audit.LogEvent` nicht auf:
- `CallbackHandler` (OIDC-Callback, `gateway/internal/auth/`) — `admin_login` bei Erfolg, `admin_login_failed` bei Fehler
- `LogoutHandler` (Admin-Logout) — `admin_logout`
- `ClaimSelectionHandler` (Bootstrap-Claim-Auswahl, abschließt Bootstrap) — `bootstrap_completed`

Die Audit-Log-Infrastruktur (`gateway/internal/audit/`) existiert und ist funktionsfähig (andere Audit-Events funktionieren). Nur die o.g. Handler fehlen.

**Audit-Event-Schema (gemäß bestehenden Tests und `audit` Package):**
```
action: "admin_login" | "admin_logout" | "admin_login_failed" | "bootstrap_completed"
actor_user_id: OIDC-Subject oder "system" für bootstrap
outcome: "success" | "failure"
target_id: optional (Session-ID oder leer)
```

## Acceptance Criteria

1. `CallbackHandler.ServeHTTP` (Erfolgsfall — Session-Cookie gesetzt) ruft `audit.LogEvent` mit `action="admin_login"`, `actor_user_id=<OIDC-Subject>`, `outcome="success"` auf, **bevor** der Redirect ausgeführt wird.

2. `CallbackHandler.ServeHTTP` (Fehlerfall — OIDC-Fehler oder fehlende Claims) ruft `audit.LogEvent` mit `action="admin_login_failed"`, `actor_user_id="unknown"` (kein Subject bekannt), `outcome="failure"` auf.

3. `LogoutHandler.ServeHTTP` ruft `audit.LogEvent` mit `action="admin_logout"`, `actor_user_id=<Subject aus Session>`, `outcome="success"` auf.

4. `ClaimSelectionHandler.ServeHTTP` (POST, Bootstrap abgeschlossen) ruft `audit.LogEvent` mit `action="bootstrap_completed"`, `actor_user_id=<Subject aus Session>`, `outcome="success"` auf.

5. Wenn `audit.LogEvent` fehlschlägt (z.B. gRPC-Core nicht erreichbar), wird der Fehler geloggt (`slog.Error`) aber der User-Flow wird **nicht** unterbrochen — der Redirect geht weiter. (Fail-open für Audit-Logging — Verfügbarkeit > Auditierbarkeit im Admin-Flow.)

6. `TestAdminLogin_EmitsAuditEntry`, `TestAdminLogout_EmitsAuditEntry`, `TestAdminLoginFailure_EmitsAuditEntry`, `TestBootstrap_EmitsAuditEntry` alle grün.

7. Unit-Tests für die geänderten Handler (`callback_test.go`, `logout_test.go`, `claim_selection_test.go` o.ä.) prüfen: Audit-Event wird mit korrektem `action`-Wert emittiert. Test-Double oder Interface-Stub für `audit.Logger`.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [Unit: CallbackHandler — admin_login emittiert bei Erfolg] — Go httptest
   - Given: Gültiger OIDC-Callback mit Subject "admin@example.com"
   - When: POST /admin/auth/callback
   - Then: `audit.Logger.LogEvent(action="admin_login", actor="admin@example.com")` wurde aufgerufen

2. [Unit: CallbackHandler — admin_login_failed emittiert bei OIDC-Fehler] — Go httptest
   - Given: Ungültiger State-Parameter im Callback
   - When: POST /admin/auth/callback
   - Then: `audit.Logger.LogEvent(action="admin_login_failed", actor="unknown")` wurde aufgerufen

3. [Unit: LogoutHandler — admin_logout emittiert] — Go httptest
   - Given: Gültige Admin-Session mit Subject "admin@example.com"
   - When: POST /admin/logout
   - Then: `audit.Logger.LogEvent(action="admin_logout", actor="admin@example.com")` wurde aufgerufen

4. [Unit: ClaimSelectionHandler — bootstrap_completed emittiert] — Go httptest
   - Given: Bootstrap noch nicht abgeschlossen, gültige Session
   - When: POST /admin/bootstrap/claims
   - Then: `audit.Logger.LogEvent(action="bootstrap_completed")` wurde aufgerufen

5. [Integration: TestAdminLogin_EmitsAuditEntry] — Go integration (existierender Test)
6. [Integration: TestAdminLogout_EmitsAuditEntry] — Go integration (existierender Test)
7. [Integration: TestAdminLoginFailure_EmitsAuditEntry] — Go integration (existierender Test)
8. [Integration: TestBootstrap_EmitsAuditEntry] — Go integration (existierender Test)

## Implementation Notes

**Relevante Dateien:**
- `gateway/internal/auth/callback.go` — `CallbackHandler`
- `gateway/internal/admin/` — `LogoutHandler`, `ClaimSelectionHandler` (genaue Dateinamen aus Codebase entnehmen)
- `gateway/internal/audit/` — bestehende Audit-Infrastruktur; Interface für Test-Double definieren falls noch nicht vorhanden

**Security-Gate 1 (per-story):** Pflicht. Audit-Log ist sicherheitsrelevantes Kontrollsystem.
