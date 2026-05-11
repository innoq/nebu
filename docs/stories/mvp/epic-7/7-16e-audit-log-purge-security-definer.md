---
id: 7-16e
type: bugfix
security_review: required
created: 2026-04-30
---

# Story 7.16e: Bugfix — audit_log_purge SECURITY DEFINER Elevation greift nicht

Status: ready-for-dev

## Story

As a compliance officer,
I want the `audit_log_purge` function to successfully delete expired rows even when called by `nebu_app`,
so that `TestAuditLogPurge_AppRoleCanCallSecurityDefiner` grün wird und abgelaufene Audit-Logs automatisch bereinigt werden.

## Context / Background

**Failure:**
```
role_separation_test.go:337: AC6 FAIL: audit_log_purge returned deleted=0, want >= 1 —
  the expired row was not deleted (SECURITY DEFINER elevation may be missing or function owner cannot bypass RLS)
role_separation_test.go:350: AC6 FAIL: seeded row still present after audit_log_purge (count=1) —
  SECURITY DEFINER elevation not effective
```

**Root Cause:**

`audit_log_purge` ist als `SECURITY DEFINER`-Funktion definiert, damit `nebu_app` (ohne BYPASSRLS) die Funktion aufruft, die Funktion aber mit den Rechten des Funktions-Owners ausgeführt wird. Allerdings:

1. **Falscher Funktions-Owner:** Wenn die Funktion `nebu_app` gehört (statt `nebu_migrate` oder einem User mit `BYPASSRLS`), hat sie keine Rechte, Rows durch FORCE RLS zu löschen. `SECURITY DEFINER` bedeutet: die Funktion läuft mit den Rechten des **Owners** — und wenn der Owner keine BYPASSRLS-Berechtigung hat, scheitert auch die Funktion.

2. **Fehlende `BYPASSRLS`-Berechtigung für den Funktions-Owner:** Story 5.29a setzte `BYPASSRLS` auf `nebu_migrate`. Die `audit_log_purge`-Funktion muss entweder `nebu_migrate` (mit BYPASSRLS) als Owner haben, oder der Owner braucht explizit `SET ROLE nebu_migrate` im Funktionskörper.

**Diagnose aus dem Fehler:**
- `deleted=0` → die DELETE-Statement in `audit_log_purge` wird ausgeführt, findet aber keine Rows (RLS blockiert)
- `seeded row still present` → RLS-Policy `FORCE ROW LEVEL SECURITY` auf `audit_log` verhindert DELETE auch im SECURITY DEFINER-Kontext wenn Owner kein BYPASSRLS hat

**Fix:** Migration um `ALTER FUNCTION audit_log_purge OWNER TO nebu_migrate` erweitern (oder in der Funktion `SET LOCAL ROLE nebu_migrate` aufrufen). Der Funktions-Owner muss identisch sein mit dem User der `BYPASSRLS` trägt.

## Acceptance Criteria

1. `audit_log_purge(interval)` kann von `nebu_app` aufgerufen werden und löscht abgelaufene Rows.

2. Der Funktions-Owner von `audit_log_purge` ist `nebu_migrate` (prüfbar via `SELECT proowner FROM pg_proc WHERE proname='audit_log_purge'` → muss `nebu_migrate` ergeben).

3. `SECURITY DEFINER`-Flag bleibt erhalten (Funktion läuft als `nebu_migrate`, nicht als Aufrufer).

4. `TestAuditLogPurge_AppRoleCanCallSecurityDefiner` grün:
   - Seed: 1 Audit-Log-Row mit `expires_at = NOW() - 1 second`
   - Call: `SELECT audit_log_purge('1 second')` als `nebu_app`
   - Then: `deleted >= 1`, Row nicht mehr vorhanden

5. `TestAppRole_CannotDeleteAuditLog` bleibt grün (direktes DELETE als `nebu_app` ist weiterhin verboten).

6. Kein anderer RLS-Test bricht.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [Integration: TestAuditLogPurge_AppRoleCanCallSecurityDefiner] — Go integration (existierender Test)
   - Given: 1 abgelaufene Row in audit_log (expires_at in der Vergangenheit)
   - When: `SELECT audit_log_purge(...)` als nebu_app
   - Then: deleted >= 1, Row gelöscht

2. [Integration: TestAppRole_CannotDeleteAuditLog] — Go integration (existierend, Regression-Guard)
   - Given: audit_log hat Rows
   - When: direktes `DELETE FROM audit_log` als nebu_app
   - Then: SQLSTATE 42501 permission denied (unverändert)

## Implementation Notes

**Dateien:**
- `gateway/migrations/` — neue Migration (nächste Nummer nach aktuellem Stand) mit:
  ```sql
  ALTER FUNCTION audit_log_purge(interval) OWNER TO nebu_migrate;
  ```
  Oder in der bestehenden Migration wo `audit_log_purge` definiert ist nachbessern.

- Prüfen: In welcher Migration ist `audit_log_purge` definiert? Ggf. dort `OWNER TO nebu_migrate` hinzufügen oder als neue Migration.

**Wichtig:** `nebu_migrate` muss `BYPASSRLS` haben (wurde in Story 5.29a via `ALTER ROLE nebu_migrate BYPASSRLS` gesetzt). Die Funktion erbt diese Berechtigung wenn `nebu_migrate` der Owner ist.

**Security-Gate 1 (per-story):** Pflicht. SECURITY DEFINER + RLS-Bypass ist sicherheitskritisch.
