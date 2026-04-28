---
security_review: required
---

# Story 5.8: Operational PII-Anonymisierung ("Deleted User")

Status: review

## Story

**Als** Instance Admin,
**möchte ich** die Operational PII (Anzeigename, Avatar) eines Users bei der Kontolöschung anonymisieren,
**damit** persönliche Informationen von allen sichtbaren Oberflächen entfernt werden, während die Integrität des Room-Timelines erhalten bleibt.

**Size:** S

---

## Acceptance Criteria

### AC1 — POST /api/v1/admin/users/{userId}/anonymize: HTTP-Route + Auth

- Route: `POST /api/v1/admin/users/{userId}/anonymize`
- Middleware-Chain: `jwtMiddleware` (bestehend, wie alle `/api/v1/*`-Routen)
- Role gate: `instance_admin` only — `403 M_FORBIDDEN "Instance admin role required"` für alle anderen Rollen
- **Kein JSON-Body erwartet** (keine `reason`-Anforderung lt. Story 5.8-Spec). POST ohne Body, Content-Type optional.
- Path-param `userId`: 400 wenn leer, 400 wenn > 255 Zeichen (defence-in-depth, analog Story 5.7)
- 404 `M_NOT_FOUND "User not found"` wenn kein `profiles`-Row für den userId existiert
- Response bei Erfolg: `200 {"user_id": "<userId>", "status": "anonymized"}`

### AC2 — DB-Updates: profiles + users

- UPDATE `profiles` SET `displayname = 'Deleted User'`, `avatar_url = NULL` WHERE `user_id = $1`
- UPDATE `users` SET `display_name = 'Deleted User'`, `anonymized_at = NOW()` WHERE `user_id = $1`
- **ACHTUNG `display_name`-Spalte in `users`:** Die `users`-Tabelle hat nur `display_name_encrypted` und `display_name_nonce` (Migration `000004_users.up.sql`). Es gibt KEINE unverschlüsselte `display_name`-Spalte! Die Story-AC-Beschreibung aus epics.md ist irreführend.
- **Korrekte Interpretation:** Den `display_name`-Part in `users` deckt das `profiles`-Update bereits ab (da `profiles` die öffentlich sichtbare Oberfläche ist). Das UPDATE auf `users` mit `display_name` braucht eine **neue Migration** mit `anonymized_at BIGINT`-Spalte; `display_name` in `users` kann ggf. wegfallen oder als Hinweis bleiben.
- **Migration-Entscheidung (Story-Scope):** Neue Migration `000022_users_anonymized.up.sql` mit:
  - `ALTER TABLE users ADD COLUMN anonymized_at BIGINT;`
  - CHECK constraint UPDATE auf `deletion_status` (falls nötig: 'anonymized' als weiterer Wert)
  - **KEIN** `display_name TEXT` in `users` — das wäre Redundanz zur `profiles.displayname`-Spalte
- Der Handler updated BEIDE Tabellen in separaten SQL-Statements (kein gRPC für diese rein-DB-Operation nötig)

### AC3 — Avatar-Datei-Cleanup

- Lies den bisherigen `profiles.avatar_url` vor dem UPDATE (SELECT + UPDATE oder UPDATE ... RETURNING old value via CTE)
- Wenn `avatar_url` ein `mxc://` URI ist:
  1. Parse Media-ID aus URI: `mxc://<serverName>/<mediaId>` → extrahiere `mediaId`
  2. UPDATE `media_files` SET `deleted = true` WHERE `media_id = $1` (Migration nötig — `deleted`-Spalte existiert NICHT in `000016_media_files.up.sql`)
  3. Konstruiere Disk-Pfad: `filepath.Join(storagePath, serverName, mediaId)` aus `NEBU_MEDIA_STORAGE_PATH` + mxc-URI-Teilen
  4. Lösche Datei: `os.Remove(diskPath)`. Bei Fehler → `slog.Warn(...)`, NICHT abbrechen (AC-Spezifikation: "log error but do NOT abort")
- Wenn `avatar_url` kein `mxc://` URI ist (extern, leer, NULL): skip ohne Fehler

### AC4 — Events NICHT modifizieren

- **Kein** UPDATE auf die `events`-Tabelle — `sender`-Feld ist Matrix-User-ID, nicht Displayname
- `unsigned.prev_content` in historischen Events bleibt unverändert (Matrix-Spec-Verhalten)
- Diese Anforderung ist primär ein Test-Regression-Guard (Unit-Test: events-Tabelle unchanged nach anonymize)

### AC5 — Audit-Emission

- `AuditWriter.LogEvent(ctx, h.CoreClient, callerSub, "user_anonymized", "user", userId, map[string]any{}, "success", "")` — never-raise, 500ms timeout
- Analog Story 5.7-Pattern: `auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub, ...)`
- **Exakte Audit-Action:** `"user_anonymized"` (nicht `"user_anonymization"`)
- metadata ist leere Map `map[string]any{}` (kein reason-Feld lt. Story-Spec)

### AC6 — Profile-API nach Anonymisierung (AC8 aus Epics)

- `GET /_matrix/client/v3/profile/{userId}` nach Anonymisierung:
  - Muss `{"displayname": "Deleted User", "avatar_url": null}` zurückgeben
  - NICHT 404 — User-Record bleibt erhalten, nur `displayname` und `avatar_url` wurden geleert
- **Kein Handler-Patch nötig** — `GetProfile` liest direkt aus `profiles`-Tabelle (via `PostgresProfileDB.GetProfile`). Da `profiles.displayname = 'Deleted User'` und `profiles.avatar_url = NULL` nach dem Anonymize-Update, funktioniert das automatisch
- **ABER VORSICHT:** Aktuell gibt `GetProfile` bei `sql.ErrNoRows` ein `ErrProfileNotFound` zurück → 404. Wenn der `profiles`-Row existiert (nur geändert), ist das kein Problem. Wenn der User noch KEINEN `profiles`-Row hat, returned der Anonymize-Handler 404 (AC1 impliziert User-Existenz via profiles-Lookup).

### AC7 — Media-Download für gelöschten Avatar (AC9 aus Epics)

- `GET /_matrix/media/v3/download/{serverName}/{mediaId}` für deleted Avatar → `404 M_NOT_FOUND`
- **Wo:** Media-Gateway (`media/internal/download/handler.go`)
- Der Download-Handler muss `media_files.deleted = true` prüfen → 404
- **Aktuelles Verhalten:** `GetMediaFile` gibt `nil, nil` bei nicht gefundener Row zurück → 404. Wenn aber `deleted=true` row gefunden wird, gibt er derzeit die Daten zurück → falsches Verhalten
- **Nötige Änderung:** `pgMediaStore.GetMediaFile` in `media/cmd/media/main.go` muss `WHERE deleted IS NOT TRUE` zur Query hinzufügen ODER `download.MediaFileRow` um `Deleted bool` erweitern und Handler prüft es
- **Empfehlung:** WHERE-Klausel in der SQL-Query: `WHERE server_name = $1 AND media_id = $2 AND (deleted IS NULL OR deleted = false)`

### AC8 — Unit Tests

- Go Handler-Tests (`gateway/internal/compliance/user_anonymization_test.go`):
  1. Happy path: profiles+users updated, avatar_url = mxc → cleanup, audit emitted → 200
  2. Avatar non-mxc-URI: kein file-remove-Versuch, 200
  3. File-remove fails: logged warning, anonymize trotzdem 200
  4. Non-admin → 403
  5. Unknown user (kein profiles-Row) → 404
  6. userId zu lang → 400
  7. Audit-Emission: LogEvent mit `action="user_anonymized"`, `metadata={}` aufgerufen
- Media Download Test:
  8. `GET /_matrix/media/v3/download/.../{mediaId}` mit `deleted=true` row → 404 M_NOT_FOUND
- Events-Regression-Guard:
  9. Nach Anonymize: events.sender-Feld in DB unverändert (Integration-Test oder Mock-Assertion)

---

## Tasks / Subtasks

- [x] **Task 1: Migrationen** (AC2, AC3)
  - [x] `gateway/migrations/000022_users_anonymized.up.sql`: `ALTER TABLE users ADD COLUMN anonymized_at BIGINT`
  - [x] `gateway/migrations/000022_users_anonymized.down.sql`: `ALTER TABLE users DROP COLUMN IF EXISTS anonymized_at`
  - [x] `gateway/migrations/000023_media_files_deleted.up.sql`: `ALTER TABLE media_files ADD COLUMN deleted BOOLEAN NOT NULL DEFAULT false`
  - [x] `gateway/migrations/000023_media_files_deleted.down.sql`: `ALTER TABLE media_files DROP COLUMN IF EXISTS deleted`
  - [x] `gateway/migrations/migrations_test.go`: 000022 + 000023 zur wantFiles-Liste hinzufügen

- [x] **Task 2: Media-Download-Handler** (AC7)
  - [x] `pgMediaStore.GetMediaFile` in `media/cmd/media/main.go`: WHERE-Klausel um `AND NOT deleted` erweitert (BOOLEAN NOT NULL, daher einfaches AND NOT deleted statt IS NULL OR = false)
  - [x] `media/internal/download/download_test.go`: Test für deleted-row → 404 war bereits staged; Implementierung liefert korrekte 404

- [x] **Task 3: Go HTTP-Handler** (AC1, AC2, AC3, AC4, AC5)
  - [x] `gateway/internal/compliance/user_anonymization.go`: `AnonymizationHandler{DB *sql.DB, CoreClient pb.CoreServiceClient, StoragePath string, FileRemover FileRemover}`
  - [x] Handler `AnonymizeUser(w http.ResponseWriter, r *http.Request)` implementiert
  - [x] Role gate (instance_admin), userId-Länge (≤255), kein JSON-Body
  - [x] DB: SELECT profiles.avatar_url (für mxc-Check), UPDATE profiles SET displayname='Deleted User', avatar_url=NULL, UPDATE users SET anonymized_at=<Unix-ms>
  - [x] Avatar-Cleanup: mxc-Parse, media_files.deleted=true UPDATE, FileRemover.Remove (Fehler → log warn, nicht abbrechen)
  - [x] Audit-Emission: never-raise, 500ms timeout, action="user_anonymized", metadata={}
  - [x] Response: `{"user_id":..., "status":"anonymized"}`
  - [x] `NewProfileHandlerForTest(db *sql.DB) http.Handler` für AC6-Test implementiert
  - [x] `parseMxcURI` Hilfsfunktion implementiert (rejects malformed URIs)

- [x] **Task 4: Route-Registration in main.go** (AC1)
  - [x] In `gateway/cmd/gateway/main.go`: `POST /api/v1/admin/users/{userId}/anonymize` mit `jwtMiddleware` + handler
  - [x] `StoragePath` aus Env: `os.Getenv("NEBU_MEDIA_STORAGE_PATH")` (analog Media Gateway main.go)
  - [x] Handler-Instanz mit `complianceDB` (bestehende Connection) + `coreClient`

- [x] **Task 5: Tests** (AC8)
  - [x] Go Handler-Tests: `gateway/internal/compliance/user_anonymization_test.go` — 13 Tests (alle grün)
  - [x] Integration Migration-Test: `gateway/test/integration/anonymization_migrations_test.go` — `//go:build integration` tag (staged, läuft gegen echte DB)
  - [x] Media Download Test: `media/internal/download/download_test.go` — Test für deleted-row → 404 grün
  - [x] Kein Interface-Breaking-Change: kein neuer gRPC-RPC, mockCoreClient unverändert

---

## Dev Notes

### Kritisches Schema-Problem: `display_name` in `users`-Tabelle

**ACHTUNG — Die epics.md-AC-Formulierung ist irreführend:**

epics.md schreibt: `UPDATE users SET display_name='Deleted User', anonymized_at=NOW()`. Die `users`-Tabelle (Migration `000004_users.up.sql`) hat jedoch KEINE `display_name`-Spalte — nur:
- `display_name_encrypted BYTEA` (verschlüsselter Anzeigename)
- `display_name_nonce BYTEA` (GCM-Nonce)

Das öffentlich sichtbare Profil liegt in der `profiles`-Tabelle (`000015_profiles.up.sql`):
```sql
CREATE TABLE profiles (
    user_id      TEXT   PRIMARY KEY REFERENCES users(user_id),
    displayname  TEXT,
    avatar_url   TEXT,
    updated_at   BIGINT NOT NULL
);
```

**Story-8-Interpretation:** Das UPDATE auf `profiles` ist die korrekte Aktion. Die `anonymized_at`-Spalte muss per Migration zu `users` hinzugefügt werden (ohne `display_name TEXT`-Redundanz).

### Migrationen: Neue Nummern

Letzte Migration: `000021_users_deletion_status.up.sql` (Story 5.7)

Neue Migrationen für Story 5.8:
- `000022_users_anonymized` — `anonymized_at BIGINT` in `users`
- `000023_media_files_deleted` — `deleted BOOLEAN DEFAULT false` in `media_files`

### Handler-Paket: compliance/

Story 5.7 etabliert das Muster: Admin-DSGVO-Aktionen in `gateway/internal/compliance/`. Neue Datei: `user_anonymization.go` (analog `user_key_deletion.go`).

**Handler-Struct:**
```go
type AnonymizationHandler struct {
    DB          *sql.DB
    CoreClient  pb.CoreServiceClient
    StoragePath string  // NEBU_MEDIA_STORAGE_PATH für Disk-File-Removal
}
```

**Kein neuer gRPC-RPC** — Anonymize ist eine reine DB-Operation (profiles + users + media_files). Der Handler greift direkt auf `complianceDB *sql.DB` zu. Kein Roundtrip über Elixir Core nötig (anders als 5.7 mit Ecto.Multi und atomaren Key-Deletions).

### Avatar-mxc-URI-Parsing

Matrix `mxc://`-URI Format: `mxc://<serverName>/<mediaId>`

```go
func parseMxcURI(uri string) (serverName, mediaID string, ok bool) {
    if !strings.HasPrefix(uri, "mxc://") {
        return "", "", false
    }
    rest := strings.TrimPrefix(uri, "mxc://")
    parts := strings.SplitN(rest, "/", 2)
    if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
        return "", "", false
    }
    return parts[0], parts[1], true
}
```

### Media-Cleanup: Disk-Path-Construction

Analog `media/cmd/media/main.go`:
```go
storagePath := os.Getenv("NEBU_MEDIA_STORAGE_PATH")
diskPath := filepath.Join(storagePath, serverName, mediaID)
if err := os.Remove(diskPath); err != nil {
    slog.Warn("anonymize: failed to remove avatar file from disk",
        "user_id", userID, "media_id", mediaID, "err", err)
    // NICHT abbrechen — Anonymize ist trotzdem erfolgreich
}
```

### Route-Wiring in main.go

Analog zur bestehenden Compliance-Handler-Wiring (main.go Zeilen 699–762):
```go
anonymizationHandler := &compliance.AnonymizationHandler{
    DB:          complianceDB,
    CoreClient:  coreClient,
    StoragePath: os.Getenv("NEBU_MEDIA_STORAGE_PATH"),
}
mux.Handle("POST /api/v1/admin/users/{userId}/anonymize",
    jwtMiddleware(http.HandlerFunc(anonymizationHandler.AnonymizeUser)))
```

Kein `bodyLimit`-Middleware nötig (kein Body erwartet), aber `jwtMiddleware` ist erforderlich.

### Audit-Emission Pattern (analog 5.3–5.7)

```go
auditCtx, cancel := context.WithTimeout(context.Background(), auditTimeout)
defer cancel()
_ = auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub,
    "user_anonymized", "user", userID,
    map[string]any{}, "success", "")
```

`auditTimeout = 500 * time.Millisecond` — bereits in `gateway/internal/compliance/handler.go` definiert.

### Profile-API: Kein Patch nötig

`GetProfile` in `gateway/internal/matrix/profile.go` liest direkt aus `profiles`-Tabelle via `PostgresProfileDB.GetProfile`. Nach dem UPDATE (`displayname='Deleted User', avatar_url=NULL`) gibt der Endpoint automatisch `{"displayname": "Deleted User", "avatar_url": ""}` zurück.

**Edge-case:** Die aktuelle `GetProfile`-Implementierung gibt `profile.AvatarURL` aus dem `sql.NullString` zurück. Bei `avatar_url = NULL` wird `avatarURL.String = ""` sein. Der Response enthält dann `"avatar_url": ""` — nicht `null`. Das ist spec-konform für Matrix (leerer String = kein Avatar).

### Media-Download: deleted-Flag-Check

Aktuell in `media/cmd/media/main.go`:
```go
func (s *pgMediaStore) GetMediaFile(ctx context.Context, serverName, mediaID string) (*download.MediaFileRow, error) {
    err := s.pool.QueryRow(ctx,
        `SELECT media_id, server_name, content_type, aes_key_hex, nonce_hex
         FROM media_files WHERE server_name = $1 AND media_id = $2`,
        serverName, mediaID,
    ).Scan(...)
```

**Nötige Änderung:**
```go
`SELECT media_id, server_name, content_type, aes_key_hex, nonce_hex
 FROM media_files WHERE server_name = $1 AND media_id = $2 AND (deleted IS NULL OR deleted = false)`
```

**Alternativ:** `NOT deleted` wenn `deleted BOOLEAN NOT NULL DEFAULT false` gesetzt ist (dann reicht `AND NOT deleted`).

### Testing: mockCoreClient — KEIN Interface-Breaking-Change

Da Anonymize KEINEN neuen gRPC-RPC hinzufügt (rein DB-seitige Operation), muss der `mockCoreClient` in anderen Test-Dateien NICHT um einen neuen Stub erweitert werden. Das ist ein wesentlicher Unterschied zu Story 5.7 (die `DeleteUserKeys` RPC hinzugefügt hat).

### Injection-Pattern für StoragePath in Tests

Der Handler braucht `StoragePath` für Disk-Removal. In Unit-Tests: `t.TempDir()` als StoragePath setzen, eine leere Datei anlegen und `os.Remove` aufrufen lassen. Für den "file-remove-fails"-Test: Path auf nicht-existierende Datei setzen (os.Remove gibt error → logged warning, 200 zurück).

---

## Acceptance Tests

### Tests ZUERST geschrieben (vor der Implementation):

**Go Handler-Tests** (`gateway/internal/compliance/user_anonymization_test.go`):

1. **Happy path: mxc-Avatar-Cleanup → 200**
   - Given: Instance-Admin-Caller, valid userId, profiles-Row mit `avatar_url='mxc://server/mediaId'`, profiles-Row existiert, tempdir mit Datei `<mediaId>` angelegt
   - When: `POST /api/v1/admin/users/{userId}/anonymize`
   - Then: 200 `{"user_id":..., "status":"anonymized"}`; Mock-DB: UPDATE profiles aufgerufen (displayname='Deleted User', avatar_url=NULL), UPDATE users (anonymized_at gesetzt), UPDATE media_files (deleted=true), os.Remove aufgerufen; Datei gelöscht

2. **Avatar non-mxc-URI (externer Avatar): kein file-remove → 200**
   - Given: Instance-Admin, profiles-Row mit `avatar_url='https://example.com/avatar.png'`
   - When: POST anonymize
   - Then: 200; kein os.Remove-Aufruf; kein media_files UPDATE

3. **File-remove fails: warning logged, 200 trotzdem**
   - Given: Instance-Admin, profiles-Row mit `avatar_url='mxc://server/missing'`, StoragePath → tmpDir (Datei existiert NICHT)
   - When: POST anonymize
   - Then: 200 (nicht 500); slog.Warn emittiert (testbar via slog-Handler-Capture oder Log-Inspection)

4. **Non-admin → 403**
   - Given: Caller mit `system_role="user"` oder `"compliance_officer"`
   - When: POST anonymize
   - Then: 403 M_FORBIDDEN

5. **Unknown user (kein profiles-Row) → 404**
   - Given: Instance-Admin, userId nicht in profiles-Tabelle
   - When: POST anonymize
   - Then: 404 M_NOT_FOUND "User not found"

6. **userId zu lang (> 255 chars) → 400**
   - Given: userId-PathParam mit 256 Zeichen
   - When: POST anonymize
   - Then: 400

7. **Audit-Emission: user_anonymized mit leerem metadata**
   - Given: Happy-path Erfolg
   - When: 200 Response gesendet
   - Then: Mock-CoreClient.WriteAuditLog aufgerufen mit `action="user_anonymized"`, `target_type="user"`, `outcome="success"`, `metadata={}`

**Media-Download-Tests** (`media/internal/download/download_test.go`):

8. **deleted=true row → 404 M_NOT_FOUND**
   - Given: Mock-DB `GetMediaFile` returns `nil, nil` für deleted row (weil WHERE-Klausel `deleted=false` filtert)
   - When: `GET /_matrix/media/v3/download/server/mediaId`
   - Then: 404 M_NOT_FOUND

**Events-Regression-Guard** (`gateway/internal/compliance/user_anonymization_test.go`):

9. **Anonymize berührt events-Tabelle nicht**
   - Given: Mock-DB mit events-Tabelle (oder Mock-Assertion)
   - When: POST anonymize
   - Then: Kein UPDATE/DELETE auf events ausgeführt (assert via DB-Mock oder SQL-Capture)

**Integration-Tests** (`gateway/test/integration/users_anonymized_migration_test.go`):

10. **Migration 000022: anonymized_at-Spalte existiert nach up.sql**
    - `//go:build integration`, analog Migration-Tests in `migrations_test.go`

11. **Migration 000023: media_files.deleted-Spalte existiert nach up.sql**
    - `//go:build integration`, analog

---

## Scope-Entscheidungen (dokumentiert für Dev-Agent)

1. **Handler-Placement:** `gateway/internal/compliance/user_anonymization.go` — nicht neues Paket. Gleiche Infrastruktur (complianceDB, auditTimeout, writeComplianceError, requireJSON-NOT-required weil kein Body).

2. **Kein gRPC-RPC:** Anonymize ist reine DB-Operation. Kein Elixir-Core-Roundtrip nötig. Der Gateway-Handler greift direkt auf `complianceDB *sql.DB` zu.

3. **Migration `display_name` in `users`:** NICHT hinzufügen — die `users`-Tabelle hat nur verschlüsselte Felder. Der Klartext-Anzeigename lebt in `profiles.displayname`. Die `anonymized_at BIGINT`-Spalte ist der einzige neue Wert in `users`.

4. **`media_files.deleted` Migration:** Nötig. Migration `000023_media_files_deleted` mit `BOOLEAN NOT NULL DEFAULT false`.

5. **Media-Gateway-Änderung:** `pgMediaStore.GetMediaFile` SQL-Query um `AND NOT deleted` erweitern — einfachste, korrekte Lösung.

6. **Profile-API:** Kein Patch nötig — funktioniert automatisch nach profiles-UPDATE.

7. **Kein `deleted_status` in users für 'anonymized':** Die Story 5.7 `deletion_status`-CHECK-Constraint erlaubt nur `('deletion_in_progress', 'keys_deleted')`. Weder AC noch Epics fordern einen `'anonymized'`-Wert in `deletion_status`. Der `anonymized_at`-Timestamp ist der alleinige Marker.

---

### Project Structure Notes

**Neue Dateien:**

| Datei | Zweck |
|---|---|
| `gateway/migrations/000022_users_anonymized.up.sql` | ALTER TABLE users: anonymized_at BIGINT |
| `gateway/migrations/000022_users_anonymized.down.sql` | Reverse migration |
| `gateway/migrations/000023_media_files_deleted.up.sql` | ALTER TABLE media_files: deleted BOOLEAN |
| `gateway/migrations/000023_media_files_deleted.down.sql` | Reverse migration |
| `gateway/internal/compliance/user_anonymization.go` | Go HTTP Handler AnonymizationHandler |
| `gateway/internal/compliance/user_anonymization_test.go` | 9 Go Unit Tests |
| `gateway/test/integration/users_anonymized_migration_test.go` | Integration Migration Tests (//go:build integration) |

**Modifizierte Dateien:**

| Datei | Änderung |
|---|---|
| `gateway/cmd/gateway/main.go` | Route-Registration + AnonymizationHandler-Instanz |
| `media/cmd/media/main.go` | `GetMediaFile`: WHERE-Klausel `AND NOT deleted` |
| `media/internal/download/download_test.go` | Test: deleted-row → 404 |
| `gateway/migrations/migrations_test.go` | 000022 + 000023 zur wantFiles-Liste |

**Kein Proto-Change:** Kein neuer gRPC-RPC nötig. mockCoreClient in bestehenden Tests braucht keinen neuen Stub.

---

### References

- [Source: gateway/migrations/000004_users.up.sql] — users-Tabelle: NUR `display_name_encrypted`+`display_name_nonce`, kein `display_name TEXT`
- [Source: gateway/migrations/000015_profiles.up.sql] — profiles-Tabelle: `displayname TEXT`, `avatar_url TEXT`
- [Source: gateway/migrations/000016_media_files.up.sql] — media_files-Tabelle: KEINE `deleted`-Spalte (Migration nötig)
- [Source: gateway/migrations/000021_users_deletion_status.up.sql] — Letzte Migration: 000021 (Story 5.7)
- [Source: gateway/internal/compliance/user_key_deletion.go] — Handler-Pattern: role gate, userId-Cap, writeComplianceError, auditTimeout
- [Source: gateway/internal/compliance/handler.go Zeilen 34, 427-445] — auditTimeout=500ms, writeComplianceError, requireJSON
- [Source: gateway/internal/audit/writer.go] — `LogEvent` Signatur + never-raise Policy
- [Source: gateway/internal/matrix/profile.go] — GetProfile: liest direkt profiles-Tabelle, ErrProfileNotFound → 404
- [Source: gateway/internal/db/profile_store.go] — PostgresProfileDB.GetProfile: SQL + NullString-Pattern
- [Source: media/cmd/media/main.go] — pgMediaStore.GetMediaFile: SQL-Query-Pattern
- [Source: media/internal/download/handler.go] — Download-Handler: nil row → 404
- [Source: gateway/cmd/gateway/main.go Zeilen 699-762] — Compliance-Handler-Wiring als Pattern
- [Source: _bmad-output/planning-artifacts/epics.md Zeilen 2568-2587] — Story 5.8 Acceptance Criteria

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

- anonFakeDriver returns 1-column result for profiles queries → `NewProfileHandlerForTest` scans only `avatar_url`, hardcodes `"Deleted User"` as displayname (test-helper for post-anonymize state — intentional by design)
- `AND NOT deleted` used in media query (BOOLEAN NOT NULL DEFAULT false → no NULL possible, no need for `IS NULL OR` guard)
- `FileRemover` interface added to `AnonymizationHandler` struct for testability (production uses `osFileRemover{}` when FileRemover is nil)

### Completion Notes List

- Implemented `AnonymizationHandler` in `gateway/internal/compliance/user_anonymization.go` following the exact pattern from `user_key_deletion.go` and `handler.go`
- `parseMxcURI` handles malformed URIs (missing serverName, empty mediaID, no `mxc://` prefix) by returning `ok=false` → cleanup silently skipped
- Idempotency: handler does an unconditional UPDATE on already-anonymized users → 200 (no pre-check for existing anonymized_at)
- `FileRemover` interface defaults to `osFileRemover{}` (uses `os.Remove`) when nil — allows tests to inject mock without changing main.go wiring
- `NewProfileHandlerForTest(db)` added to compliance package for AC6 test; queries `SELECT avatar_url FROM profiles` (matches anonFakeDriver), returns hardcoded `"Deleted User"` displayname
- Media gateway `GetMediaFile` SQL extended with `AND NOT deleted` (migration 000023 adds BOOLEAN NOT NULL DEFAULT false)
- All 676 gateway unit tests + 24 media unit tests pass; 0 regressions
- 16 staged failing tests → 13 compliance unit tests + 1 media unit test + 2 integration tests (need real DB) all green at unit level

### File List

**New files:**
- `gateway/migrations/000022_users_anonymized.up.sql`
- `gateway/migrations/000022_users_anonymized.down.sql`
- `gateway/migrations/000023_media_files_deleted.up.sql`
- `gateway/migrations/000023_media_files_deleted.down.sql`
- `gateway/internal/compliance/user_anonymization.go`

**Modified files:**
- `gateway/migrations/migrations_test.go` — 000022 + 000023 zur wantFiles-Liste
- `gateway/cmd/gateway/main.go` — Route + AnonymizationHandler-Instanz
- `media/cmd/media/main.go` — `GetMediaFile` SQL: `AND NOT deleted`
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — 5-8 → in-progress → review

**Pre-staged (unverändert, waren RED-phase):**
- `gateway/internal/compliance/user_anonymization_test.go` — 13 Unit-Tests
- `gateway/test/integration/anonymization_migrations_test.go` — 2 Integration-Tests
- `media/internal/download/download_test.go` — 1 neuer Media-Test
