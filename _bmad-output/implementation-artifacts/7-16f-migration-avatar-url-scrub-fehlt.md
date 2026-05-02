---
id: 7-16f
type: bugfix
security_review: optional
created: 2026-04-30
---

# Story 7.16f: Bugfix — Migration 000026 avatar_url_scrub fehlt (Story 5.29b AC7 deferred)

Status: ready-for-dev

## Story

As a developer,
I want migration 000026 (`000026_avatar_url_scrub.up.sql`) to exist and be applied,
so that `TestProfilesAvatarURLScrub_MigrationApplied` grün wird und die bereits existierende Scrub-Logik auch durch eine Migration abgesichert ist.

## Context / Background

**Failure:**
```
avatar_url_scrub_migration_test.go:75: migration 000026 (avatar_url_scrub) has not been applied —
  000026_avatar_url_scrub.up.sql must be created and run (Story 5.29b AC7 not implemented)
```

**Kontext:** Story 5.29b hat AC7 (die Migration) als deferred markiert. Die Scrub-Logik selbst (`TestProfilesAvatarURLScrub_RemovesUnsafeURIs`) ist bereits getestet und PASS. Es fehlt nur die SQL-Migration, die:
1. Bestehende `avatar_url`-Werte in `profiles` bereinigt (unsichere URIs auf NULL/default setzen)
2. Optional: CHECK-Constraint hinzufügen der künftige unsichere URIs blockiert

**Migration-Inhalt (basierend auf existierendem Scrub-Logic-Test):**
```sql
-- Remove avatar_url values that contain javascript:, data:, vbscript: or file: schemes
UPDATE profiles
SET avatar_url = NULL
WHERE avatar_url IS NOT NULL
  AND (
    lower(avatar_url) LIKE 'javascript:%'
    OR lower(avatar_url) LIKE 'data:%'
    OR lower(avatar_url) LIKE 'vbscript:%'
    OR lower(avatar_url) LIKE 'file:%'
  );
```

## Acceptance Criteria

1. `gateway/migrations/000026_avatar_url_scrub.up.sql` existiert mit einem UPDATE-Statement, das unsichere Avatar-URLs (javascript:, data:, vbscript:, file: Schemes) auf NULL setzt.

2. `gateway/migrations/000026_avatar_url_scrub.down.sql` existiert (no-op oder leeres Statement — Daten-Verlust ist irreversibel, aber die Migration selbst kann registriert bleiben).

3. Die Migration ist in `schema_migrations`-Tabelle als angewendet registriert wenn der Stack gestartet wird (golang-migrate läuft automatisch beim Gateway-Start).

4. `TestProfilesAvatarURLScrub_MigrationApplied` grün:
   - Prüft: `SELECT COUNT(*) FROM schema_migrations WHERE version='000026'` → 1

5. `TestProfilesAvatarURLScrub_RemovesUnsafeURIs` bleibt grün (war bereits PASS).

6. Keine anderen Migrations-Tests brechen (Reihenfolge-Konsistenz).

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [Integration: TestProfilesAvatarURLScrub_MigrationApplied] — Go integration (existierender Test)
   - Given: Stack gestartet mit neuer Migration
   - When: SELECT COUNT(*) FROM schema_migrations WHERE version='000026'
   - Then: count = 1

2. [Integration: TestProfilesAvatarURLScrub_RemovesUnsafeURIs] — Go integration (existierend, Regression-Guard)
   - Given: profiles-Row mit `avatar_url = 'javascript:alert(1)'`
   - When: Migration angewendet
   - Then: `avatar_url IS NULL`

## Implementation Notes

**Dateien:**
- `gateway/migrations/000026_avatar_url_scrub.up.sql` — neu erstellen
- `gateway/migrations/000026_avatar_url_scrub.down.sql` — neu erstellen (no-op: `SELECT 1;`)

**Migrations-Nummerierung prüfen:** Sicherstellen, dass 000026 tatsächlich die nächste freie Nummer ist (aktuelle letzte Migration in `gateway/migrations/` prüfen).

**Security-Gate 1 (per-story):** Optional. XSS-Prävention via Datenmigration ist sicherheitsrelevant aber kein neuer Angriffsvektor — Scrub-Logik existiert bereits in der Anwendung.
