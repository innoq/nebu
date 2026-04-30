---
id: 7-16a
type: bugfix
security_review: not-needed
created: 2026-04-30
---

# Story 7.16a: Bugfix — Bootstrap-Testfixture TRUNCATE schlägt mit permission denied fehl

Status: ready-for-dev

## Story

As a developer running integration tests,
I want `theServerHasNoBootstrapCompleted` to use the `nebu_migrate` DB role for TRUNCATE,
so that the Godog bootstrap/OIDC/compliance/GDPR scenarios don't fail at the very first setup step.

## Context / Background

**Root cause:** `theServerHasNoBootstrapCompleted()` in `admin_bootstrap_steps_test.go:89` ruft `openTestDB()` auf. `openTestDB` verwendet `dbURL` = `NEBU_TEST_DB_URL` = `nebu_app`. Nach Story 5.29a besitzt `nebu_app` kein TRUNCATE-Recht auf `server_config` (nur SELECT/INSERT/UPDATE per RLS-Policy). Der Kommentar in Zeile 87 sagt „Runs as the nebu table owner" — aber der Code verwendet den falschen User.

**Symptom (Integration-Test-Lauf 2026-04-30):**
```
truncate server_config: ERROR: permission denied for table server_config (SQLSTATE 42501)
```

**Betroffene Szenarien (7 Stück — alle scheitern am ersten Schritt):**
- Bootstrap Wizard step 1 renders correctly
- Bootstrap Wizard step 2 redirects to OIDC login after valid OIDC config
- Bootstrap completes and dashboard is accessible
- Unauthenticated dashboard request is redirected
- Unauthenticated request to root is redirected to login when bootstrap complete
- OIDC login and logout via Dex
- Full Four-Eyes Compliance Export / GDPR Deletion and Anonymization

**Regression eingeführt durch:** Story 5.29a (Role Separation — `nebu_app` als non-superuser, `nebu_migrate` als Table-Owner). Der Bootstrap-Test-Step wurde dabei nicht angepasst.

**Fix-Umfang:** Ausschließlich `gateway/test/integration/` — keine Produktionsänderung, kein Schema-Change.

## Acceptance Criteria

1. `main_test.go` deklariert `var migrationDBURL string` und setzt es aus `os.Getenv("NEBU_TEST_MIGRATION_DB_URL")` (Fallback: `postgresql://nebu_migrate:nebu_migrate_dev_pw@postgres:5432/nebu`).

2. `admin_bootstrap_steps_test.go:theServerHasNoBootstrapCompleted` öffnet eine separate Verbindung über `migrationDBURL` (nicht `dbURL`) und führt `TRUNCATE TABLE server_config` + `TRUNCATE TABLE bootstrap_draft` darüber aus. Der INSERT danach (Re-Seed `bootstrap_active`) darf weiter über `migrationDBURL` laufen, da `nebu_migrate` auch INSERT darf.

3. `openTestDB()` (Zeile 58) bleibt unverändert — nur der Bootstrap-Reset-Step bekommt eine eigene Migration-Verbindung.

4. Nach dem Fix laufen alle 7 oben genannten Godog-Szenarien durch (sie scheitern dann ggf. an funktionalen Fehlern, aber nicht mehr an `permission denied`).

5. Kein anderer Test-Step darf durch die Änderung brechen (unit- und integration-Lauf bleibt grün bis auf bereits bekannte Fehler).

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [Integration: Bootstrap Wizard step 1 renders correctly] — Godog
   - Given: `theServerHasNoBootstrapCompleted` läuft ohne Fehler durch
   - When: GET /admin/bootstrap ohne Session-Cookie
   - Then: HTTP 200 mit Bootstrap-Formular

2. [Integration: OIDC login and logout via Dex] — Godog
   - Given: Clean-State via `theServerHasNoBootstrapCompleted` + abgeschlossenem Bootstrap
   - When: OIDC-Login-Flow via Dex
   - Then: Session-Cookie gesetzt, Dashboard erreichbar

*(Die existierenden Godog-Szenarien in `gateway/features/admin_bootstrap.feature` und den zugehörigen Step-Definitionen sind die ATDD-Specs — kein neuer Test-Code erforderlich. Der Fix ist rein in den Test-Infrastruktur-Dateien.)*

## Implementation Notes

**Dateien:**
- `gateway/test/integration/main_test.go` — `var migrationDBURL string` + Setzen aus Env
- `gateway/test/integration/admin_bootstrap_steps_test.go` — `theServerHasNoBootstrapCompleted` nutzt `migrationDBURL` statt `dbURL` für die TRUNCATE/INSERT-Verbindung

**Kein Makefile-Change nötig** — `NEBU_TEST_MIGRATION_DB_URL` wird bereits in `test-integration` übergeben.
