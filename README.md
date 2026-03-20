# Nebu – Enterprise Chat Server

> Ein hochskalierbarer, Matrix-kompatibler Chat-Server für Enterprise-Umgebungen.  
> Open Source (Apache 2.0) · Kein Federation-Overhead · Standard-Clients (Element, Cinny, etc.)

---

## Vision

Die bestehenden Matrix-Server-Implementierungen haben ein gemeinsames Problem: Sie sind entweder
lizenzrechtlich für Enterprises ungeeignet (AGPLv3), noch nicht production-ready, oder skalieren
nicht horizontal. Nebu schließt diese Lücke:

- **Apache 2.0** – keine Copyleft-Pflicht, Enterprise-freundlich
- **Matrix Client-Server API** kompatibel – alle Standard-Clients funktionieren ohne Änderungen
- **Kein Federation** – reduzierte Komplexität, maximale Performance im eigenen Cluster
- **Minimale externe Abhängigkeiten** – Go + Elixir/OTP + PostgreSQL, keine weiteren Dienste nötig
- **Souveräner Betrieb** – kein proprietäres SaaS, keine Lizenzkosten, vollständig self-hosted
- **Nachrichtenintegrität** – Ed25519-Signaturen auf jeder Nachricht, Non-Repudiation by design
- **OIDC-first Auth** – jeder OIDC-Provider funktioniert out-of-the-box (Keycloak, Azure AD, Google)
- **Slack-orientiertes Berechtigungsmodell** – selbstverwaltend, flach, wenig Bürokratie
- **Enterprise-Features** ab Tag 1 geplant: Audit Logging, Compliance-Zugriff, DSGVO
- **Horizontal skalierbar** – Elixir-Cluster (libcluster + Horde) + stateless Go-Gateway

---

## Zielgruppe

- Unternehmen, die Slack/Teams ersetzen wollen und volle Datenkontrolle benötigen
- Öffentliche Verwaltung / Behörden (DSGVO, On-Premise-Pflicht)
- Organisationen mit hohen Compliance-Anforderungen (BSI, ISO 27001)
- DevOps-Teams, die einen selbst gehosteten, skalierbaren Messenger brauchen

---

## Architektur-Überblick

```
┌─────────────────────────────────────────────────────┐
│            Matrix Clients                            │
│    (Element, Cinny, FluffyChat, Hydrogen, ...)       │
│         TLS 1.3 für alle Verbindungen                │
└───────────┬───────────────────────┬─────────────────┘
            │ Matrix Client API     │ Media Upload/Download
            │ HTTPS + WSS           │ HTTPS
┌───────────▼───────────┐  ┌────────▼────────────────┐
│    Go API Gateway     │  │   Go Media Gateway      │
│                       │  │                         │
│ • Matrix HTTP-Routing │  │ • Upload Handler        │
│ • TLS 1.3 Termination │  │ • Download / Streaming  │
│ • Signatur-Prüfung    │  │ • Thumbnail Generierung │
│ • SSO/OIDC/SAML       │  │ • Auth-Check per MXC    │
│ • Rate Limiting       │  │ • AES-256-GCM Crypto    │
└───────────┬───────────┘  └────────┬────────────────┘
            │ gRPC (mTLS)           │ gRPC (mTLS)
┌───────────▼───────────────────────▼────────────────┐
│                 Elixir/OTP Core                     │
│                                                     │
│  ETS            → Sessions, Token Cache             │
│  GenServer      → Room, Presence, Typing            │
│  Horde          → Distributed Room Registry         │
│  pg Groups      → Room Broadcast (Pub/Sub)          │
│  libcluster     → Automatic Node Discovery          │
│  Supervisor     → Fault Tolerance, Auto-Restart     │
│  :crypto        → Ed25519 + X25519 (OTP built-in)   │
│                                                     │
│  Node 1 ◄──── Elixir Distribution TLS ───► Node N  │
└───────────┬───────────────────────┬────────────────┘
            │ TLS                   │ TLS
┌───────────▼───────────┐  ┌────────▼────────────────┐
│      PostgreSQL       │  │   Dateisystem /          │
│                       │  │   Object Storage         │
│ • Event Log           │  │                         │
│ • Signaturen          │  │ • Media-Blobs           │
│ • Rooms, User, State  │  │ • AES-256-GCM at rest   │
│ • Audit Log           │  │ • lokal oder            │
│ • Media Keys          │  │   S3-kompatibel         │
│ • FTS (tsvector)      │  │                         │
│ • pgcrypto at rest    │  │                         │
└───────────────────────┘  └─────────────────────────┘
```

### Warum nur Go + Elixir/OTP?

| Schicht | Lösung | Begründung |
|---|---|---|
| API Gateway | Go | Bestes HTTP/Middleware-Ökosystem, TLS, schnelles Deployment |
| Media Gateway | Go | Streaming, Multipart-Upload, AES-256-GCM |
| Core / Messaging | Elixir/OTP | Actor-Model, Supervisor Trees, Mix-Tooling, libcluster |
| Signatur-Crypto | `:crypto` (OTP built-in) | Ed25519 + X25519 nativ ab OTP 24, kein externes Lib |
| Sessions / Cache | ETS (OTP built-in) | Ersetzt Redis vollständig, kein Netzwerk-Hop |
| Pub/Sub | `pg` Process Groups (built-in) | Ersetzt NATS/Redis Pub/Sub, direkte Prozess-Kommunikation |
| Node Discovery | libcluster (Hex) | Automatische Cluster-Bildung, kein manuelles Konfigurieren |
| Distributed Registry | Horde (Hex) | CRDT-basiert, netsplit-sicher, Single→Cluster ohne Refactoring |

**Resultat: Drei Laufzeit-Komponenten – Go-Binary, Elixir-Release, PostgreSQL.**
Kein Redis. Kein NATS. Kein Kafka. Keine weiteren Lizenzen.

### Warum kein Federation?

Matrix Federation ist der komplexeste Teil des Protokolls (State Resolution v2, Server-to-Server
Auth, Key Exchange). Ohne Federation entfällt ~40% der Implementierungskomplexität.
Enterprise-Deployments laufen ohnehin auf einer eigenen Instanz.

---

## Sicherheitskonzept

### Übersicht

Im Enterprise-Kontext sind Nachrichten Firmeneigentum. Daraus folgen klare Prioritäten:

```
Priorität 1 – Transport Security:     TLS 1.3 auf allen Verbindungen
Priorität 2 – Nachrichtenintegrität:  Ed25519-Signatur auf jeder Nachricht
Priorität 3 – Encryption at Rest:     pgcrypto + AES-256-GCM für Media
Priorität 4 – Compliance-Zugriff:     Server kann Klartext lesen (by design)

Bewusste Entscheidung: Kein E2EE
  → Nachrichten sind Firmeneigentum, voller Admin-Zugriff ist Pflicht
  → Compliance, eDiscovery, Strafverfolgung ohne Schlüssel-Escrow-Komplexität
  → Serverseitige Volltextsuche funktioniert direkt
  → Entspricht dem Modell von Slack, Teams, und allen Enterprise-Messengern
```

---

### Transport Encryption (TLS 1.3)

Alle Verbindungen sind verschlüsselt. Es gibt keinen unverschlüsselten Pfad.

```
Client        → Go Gateway       TLS 1.3  (HTTPS / WSS)
Go Gateway    → Elixir Core      gRPC (PSK-gesichert via Compose secrets)
Go Media GW   → Elixir Core      gRPC (PSK-gesichert via Compose secrets)
Elixir Core   → PostgreSQL       TLS (PostgreSQL native ssl=on)
Elixir Node   → Elixir Node      Elixir Distribution mit TLS
```

Die Elixir Distribution ist standardmäßig nur durch einen Cookie geschützt – das reicht
für produktive Umgebungen nicht aus. TLS auf der Distribution ist Pflicht und wird via
OTP Release-Konfiguration aktiviert. Certs werden bei `make setup` generiert.

---

### Nachrichtenintegrität & Signaturen (Ed25519)

Jede Nachricht wird vom sendenden Client mit seinem privaten Schlüssel signiert.
Der Server verifiziert die Signatur vor dem Speichern – unsignierte oder ungültig signierte
Nachrichten werden abgelehnt.

#### Ziele

```
✅ Authentizität:      Beweisbar WER eine Nachricht gesendet hat
✅ Integrität:         Nachricht kann nicht unbemerkt verändert werden
✅ Non-Repudiation:    Sender kann das Senden nicht abstreiten
✅ Compliance-Nachweis: Signatur + Timestamp = rechtssicherer Beweis
```

#### Warum Ed25519?

```
Ed25519 vs. RSA-2048:
  Signatur-Größe:   64 Byte     vs.  256 Byte
  Public-Key-Größe: 32 Byte     vs.  294 Byte
  Signier-Speed:    ~70.000/s   vs.  ~3.000/s
  Verify-Speed:     ~25.000/s   vs.  ~90.000/s
  In Elixir/OTP:    built-in    vs.  built-in
  In Go:            built-in    vs.  built-in
```

#### Flow

```
Client (beim Senden):
  1. Canonical JSON des Events erstellen
     (sortierte Keys, keine Whitespace-Varianz)
  2. SHA-256 Hash des Canonical JSON
  3. Hash mit User-Private-Key (Ed25519) signieren
  4. Event + Signatur an Server senden

Server (Elixir Core beim Empfang):
  1. Canonical JSON rekonstruieren
  2. SHA-256 Hash berechnen
  3. Signatur mit User-Public-Key (Ed25519) verifizieren
     (Public Key liegt in users-Tabelle, gespeichert vom Client via /keys/upload)
  4. Bei Fehler: Event ablehnen (HTTP 403)
  5. Bei Erfolg: Event + Signatur atomar in PostgreSQL speichern

Compliance / Audit:
  1. Event + Signatur aus PostgreSQL laden
  2. Signatur mit Public Key des Users verifizieren
  3. Beweis: User X hat exakt diesen Inhalt zu Zeitpunkt Y gesendet
```

#### Elixir Implementierung

```elixir
# core/apps/signature/lib/nebu/signature.ex

defmodule Nebu.Signature do
  @moduledoc "Ed25519 event signature verification"

  @spec verify(map(), binary()) :: :ok | {:error, :invalid_signature}
  def verify(event, public_key) do
    signature = Map.get(event, "signature")
    canonical = event |> Map.delete("signature") |> Nebu.CanonicalJSON.encode()
    hash      = :crypto.hash(:sha256, canonical)
    case :crypto.verify(:eddsa, :none, hash, signature, [public_key, :ed25519]) do
      true  -> :ok
      false -> {:error, :invalid_signature}
    end
  end
end
```

#### Datenbankschema

```sql
CREATE TABLE events (
    event_id      TEXT PRIMARY KEY,
    room_id       TEXT NOT NULL,
    sender        TEXT NOT NULL,       -- User-ID
    type          TEXT NOT NULL,       -- m.room.message etc.
    content       BYTEA NOT NULL,      -- AES-256 verschlüsselt (pgcrypto)
    content_hash  TEXT NOT NULL,       -- SHA-256 des Klartext-Content
    signature     BYTEA NOT NULL,      -- Ed25519 Signatur (64 Byte)
    origin_ts     BIGINT NOT NULL,     -- Client-Timestamp (ms)
    server_ts     BIGINT NOT NULL,     -- Server-Timestamp (ms), vom Server gesetzt
    depth         BIGINT NOT NULL
);

CREATE TABLE users (
    user_id       TEXT PRIMARY KEY,
    display_name  TEXT,
    public_key    BYTEA NOT NULL,      -- Ed25519 Public Key (32 Byte)
    created_at    BIGINT NOT NULL
);

-- Index für Signatur-Verifikation im Audit-Fall
CREATE INDEX events_sender_ts_idx ON events (sender, server_ts);
```

#### Key Management für User-Schlüssel

```
Key-Generierung:  Client-seitig (Browser / App)
                  Private Key verlässt das Gerät nie
                  → bleibt beim Nutzer, nicht beim Server

Key-Registrierung: Public Key beim Login / Registrierung an Server übermitteln
                   Server speichert nur Public Key in users-Tabelle

Key-Rotation:     Neuer Key-Pair → neuer Public Key an Server
                  Alte Signaturen bleiben mit altem Public Key verifizierbar
                  → Audit-Trail bleibt vollständig

Geräteverlust:    Admin kann Key-Reset erzwingen (mit Audit-Log-Eintrag)
```

---

### Encryption at Rest – Datenbank

Event-Content wird verschlüsselt gespeichert. Der Server kennt den Klartext beim Empfang
(kein E2EE), speichert ihn aber nie unverschlüsselt auf Disk.

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Insert: Verschlüsselung im Elixir Core vor dem DB-Write
INSERT INTO events
    (event_id, room_id, sender, type, content, content_hash, signature, origin_ts, server_ts, depth)
VALUES
    ($1, $2, $3, $4,
     pgp_sym_encrypt($5, $6, 'cipher-algo=aes256'),  -- $6 = Server-Secret
     $7,   -- SHA-256 Hash des Klartext
     $8,   -- Ed25519 Signatur
     $9, $10, $11);

-- Select: Entschlüsselung im Elixir Core nach dem DB-Read
SELECT pgp_sym_decrypt(content, $1)::jsonb,
       content_hash,
       signature,
       sender,
       server_ts
FROM events WHERE event_id = $2;
```

### Encryption at Rest – Media

```
Upload:
  Plaintext → [Go Media GW] → AES-256-GCM verschlüsseln (pro-Datei-Key)
                            → Verschlüsselter Blob → Storage
                            → Key + MXC-URI + SHA-256 → PostgreSQL

Download:
  Auth-Check (Room-Membership) → Key aus PostgreSQL
  → Blob entschlüsseln → als Stream an Client
```

```go
// media/internal/crypto/aes.go
func Encrypt(plaintext []byte) (ciphertext, key, nonce []byte, err error) {
    key = make([]byte, 32)
    if _, err = io.ReadFull(rand.Reader, key); err != nil {
        return
    }
    block, _ := aes.NewCipher(key)
    gcm, _   := cipher.NewGCM(block)
    nonce     = make([]byte, gcm.NonceSize())
    io.ReadFull(rand.Reader, nonce)
    ciphertext = gcm.Seal(nonce, nonce, plaintext, nil)
    return
}
```

---

## Berechtigungsmodell & Rollen

### Grundprinzip: OIDC-first

Identität kommt ausschließlich vom OIDC-Provider. Nebu verwaltet Berechtigungen
selbst – es findet kein Rollen-Sync aus dem OIDC-Provider statt.

```
OIDC-Provider (Keycloak, Azure AD, Google Workspace, ...)
    ↓
    liefert:  sub (User-ID) + email + name
    ↓
Go Gateway: Token validieren → User-ID extrahieren
    ↓
Elixir Core: "Wer bist du?" → OIDC
             "Was darfst du?" → Nebu intern
```

SAML und LDAP sind kein eigenständiges Feature – sie werden als OIDC-Provider
vorgeschaltet (z.B. Keycloak als SAML/LDAP-Proxy). Nebu selbst spricht
nur OIDC.

### OIDC Login Flow

```
1. Client öffnet /_matrix/client/v3/login
   → Server gibt OIDC-Provider-URL zurück

2. Client → OIDC-Provider (Authorization Code Flow)
   → OIDC-Provider gibt Authorization Code zurück

3. Go Gateway tauscht Code gegen Access Token + ID Token
   → ID Token: sub, email, name extrahieren
   → User in PostgreSQL anlegen falls neu (auto-provisioning)
   → Nebu Session-Token generieren (ETS)

4. Session-Token → Client
   → Alle weiteren Requests mit diesem Token authentifiziert
```

```go
// gateway/internal/auth/oidc.go
type OIDCConfig struct {
    Issuer       string // z.B. https://keycloak.example.com/realms/corp
    ClientID     string
    ClientSecret string
    RedirectURL  string
}

// Token validieren + User-Identity extrahieren
func (o *OIDCConfig) ValidateToken(ctx context.Context, rawToken string) (*UserIdentity, error) {
    provider, _ := oidc.NewProvider(ctx, o.Issuer)
    verifier    := provider.Verifier(&oidc.Config{ClientID: o.ClientID})
    token, err  := verifier.Verify(ctx, rawToken)
    if err != nil {
        return nil, err
    }
    var claims struct {
        Sub   string `json:"sub"`
        Email string `json:"email"`
        Name  string `json:"name"`
    }
    token.Claims(&claims)
    return &UserIdentity{ID: claims.Sub, Email: claims.Email, Name: claims.Name}, nil
}
```

---

### System-Rollen (server-weit)

Es gibt genau drei System-Rollen. Alles andere ist Room-selbstverwaltend.

```
┌──────────────────────────────────────────────────────────────┐
│  instance_admin                                              │
│                                                              │
│  Technischer Betreiber – so wenig Macht wie möglich          │
│  ✅ Admin-API: User sperren, deaktivieren                    │
│  ✅ Server-Konfiguration                                     │
│  ✅ Deployment, Updates                                      │
│  ❌ Kann Nachrichten NICHT lesen (kein Compliance-Zugriff)   │
│  ❌ Ist NICHT automatisch Mitglied in Rooms                  │
├──────────────────────────────────────────────────────────────┤
│  compliance_officer                                          │
│                                                              │
│  Datenschutz / Legal / HR – streng auditiert                 │
│  ✅ Volltextsuche über alle Rooms                            │
│  ✅ Nachrichten-Export (mit Signaturen als Beweis)           │
│  ✅ Audit-Log lesen                                          │
│  ❌ Kann keine Rooms verwalten                               │
│  ❌ Kann keine User ändern oder sperren                      │
│  ⚠️  Jeder Zugriff → Audit-Log-Eintrag (Pflichtfeld: Grund) │
│  ⚠️  Notification an instance_admin bei Zugriff             │
├──────────────────────────────────────────────────────────────┤
│  user  (Standard – jeder OIDC-Login)                         │
│                                                              │
│  ✅ Rooms erstellen                                          │
│  ✅ Eigene Rooms vollständig selbst verwalten                │
│  ✅ Andere User einladen                                     │
│  ✅ Direct Messages mit jedem User                           │
│  ❌ Kein Zugriff auf fremde Rooms ohne Einladung             │
└──────────────────────────────────────────────────────────────┘
```

System-Rollen werden beim User-Anlegen gesetzt und in PostgreSQL gespeichert.
Sie werden **nicht** aus dem OIDC-Provider synchronisiert – Änderungen nur über Admin-API.

---

### Room-Berechtigungen (Power-Level-Modell)

Nebu nutzt das in Matrix eingebaute Power-Level-System direkt.
Kein eigenes Rollen-Framework nötig.

```
Power Level   Bezeichnung     Rechte
──────────────────────────────────────────────────────────────
100           room_admin      Alles: Room löschen/archivieren,
                              Power-Level anderer Mitglieder
                              ändern, alle Moderator-Rechte
 50           moderator       User einladen und kicken,
                              Nachrichten anderer löschen,
                              Room-Name/-Topic ändern,
                              Room-Einstellungen verwalten
  0           member          Nachrichten senden und lesen,
  (default)                   andere User einladen
                              (konfigurierbar per Room)
 -1           read_only       Nur lesen, kein Schreiben,
                              kein Einladen
```

```
Automatische Vergabe:
  Room-Ersteller     → Power Level 100 (room_admin)
  Eingeladener User  → Power Level 0   (member)
  Upgrade/Downgrade  → nur durch room_admin möglich
```

```
Selbstverwaltung – kein instance_admin nötig:
  Alice erstellt #projekt-alpha  → Alice ist room_admin (100)
  Alice lädt Bob ein             → Bob ist member (0)
  Alice setzt Bob auf 50         → Bob ist moderator
  Bob lädt Carol ein             → Carol ist member (0)
  Alice archiviert den Room      → nur Alice kann das
```

---

### Channel-Typen

```
public_room    Jeder User kann joinen ohne Einladung
               Im Room-Verzeichnis sichtbar
               Durchsuchbar (für Mitglieder)

private_room   Nur per Einladung
               Nicht im Room-Verzeichnis
               Compliance-Officer kann durchsuchen

direct_message Automatisch erstellt beim ersten DM
               Immer privat, kein Dritter einladbar
               Beide Teilnehmer sind gleichwertig (Power 100)

group_dm       Kleine Gruppe (max. konfigurierbar, default 20)
               Kein formaler Room, kein Moderator-Konzept
               Ersteller kann weitere User hinzufügen
```

---

### Datenbankschema Berechtigungen

```sql
-- System-Rollen
CREATE TYPE system_role AS ENUM ('user', 'instance_admin', 'compliance_officer');

CREATE TABLE users (
    user_id       TEXT PRIMARY KEY,         -- OIDC sub
    email         TEXT NOT NULL UNIQUE,
    display_name  TEXT,
    public_key    BYTEA NOT NULL,           -- Ed25519 Public Key
    system_role   system_role NOT NULL DEFAULT 'user',
    is_active     BOOLEAN NOT NULL DEFAULT true,
    created_at    BIGINT NOT NULL,
    last_seen_ts  BIGINT
);

-- Room-Mitgliedschaften inkl. Power Level
CREATE TABLE room_members (
    room_id       TEXT NOT NULL,
    user_id       TEXT NOT NULL REFERENCES users(user_id),
    power_level   SMALLINT NOT NULL DEFAULT 0,  -- 100, 50, 0, -1
    joined_at     BIGINT NOT NULL,
    invited_by    TEXT REFERENCES users(user_id),
    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX room_members_user_idx ON room_members (user_id);
CREATE INDEX room_members_room_idx ON room_members (room_id, power_level DESC);

-- Rooms
CREATE TYPE room_type AS ENUM ('public_room', 'private_room', 'direct_message', 'group_dm');

CREATE TABLE rooms (
    room_id       TEXT PRIMARY KEY,
    room_type     room_type NOT NULL,
    name          TEXT,
    topic         TEXT,
    created_by    TEXT NOT NULL REFERENCES users(user_id),
    created_at    BIGINT NOT NULL,
    is_archived   BOOLEAN NOT NULL DEFAULT false
);
```

---

### Compliance-Zugriff – Vier-Augen-Prinzip

```
compliance_officer will Raum oder User durchsuchen:

  1. Request: POST /admin/compliance/access
     Body: { room_id, user_id, reason, time_range }
     → Pflichtfeld: reason (Freitext, min. 20 Zeichen)

  2. Audit-Log-Eintrag (unveränderlich):
     { officer_id, target, reason, timestamp, granted_until }

  3. Notification an alle instance_admins
     (intern via Room-Event in #audit-notifications)

  4. Zugriff aktiv für konfigurierten Zeitraum (default: 24h)
     → Danach automatisch entzogen

  5. Jede Suchanfrage + jeder Export innerhalb des Zeitraums
     → eigener Audit-Log-Eintrag
```

```sql
-- Audit Log – append-only, niemals UPDATE/DELETE
CREATE TABLE audit_log (
    id            BIGSERIAL PRIMARY KEY,
    actor_id      TEXT NOT NULL,              -- wer hat gehandelt
    action        TEXT NOT NULL,              -- z.B. 'compliance_access_granted'
    target_type   TEXT,                       -- 'room', 'user', 'message'
    target_id     TEXT,
    reason        TEXT,                       -- Pflichtfeld bei Compliance
    metadata      JSONB,                      -- zusätzliche Infos
    created_at    BIGINT NOT NULL
);

-- Kein DELETE, kein UPDATE erlaubt – nur INSERT
-- PostgreSQL Row Security Policy sichert das ab:
ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY audit_insert_only ON audit_log FOR INSERT WITH CHECK (true);
-- SELECT nur für compliance_officer und instance_admin (via App-Layer)
```

---

### OIDC-Provider Konfiguration (Beispiele)

```yaml
# Keycloak (selbst gehostet – empfohlen für maximale Souveränität)
oidc:
  issuer: https://keycloak.example.com/realms/corp
  client_id: nebu
  client_secret: ${OIDC_CLIENT_SECRET}
  scopes: [openid, email, profile]

# Azure AD
oidc:
  issuer: https://login.microsoftonline.com/{tenant-id}/v2.0
  client_id: ${AZURE_CLIENT_ID}
  client_secret: ${AZURE_CLIENT_SECRET}
  scopes: [openid, email, profile]

# Google Workspace
oidc:
  issuer: https://accounts.google.com
  client_id: ${GOOGLE_CLIENT_ID}
  client_secret: ${GOOGLE_CLIENT_SECRET}
  scopes: [openid, email, profile]
```

Keycloak ist die empfohlene Option für souveränen Betrieb – es läuft lokal,
unterstützt LDAP/AD als User-Federation und kann als SAML-Proxy fungieren.
Damit ist LDAP/AD indirekt über Keycloak unterstützt, ohne dass Nebu
eigene LDAP-Implementierung braucht.

---

## Volltextsuche

Nachrichten sind serverseitig lesbar – Volltextsuche funktioniert direkt ohne
zusätzliche Komponenten. PostgreSQL FTS ist der MVP-Ansatz.

### PostgreSQL Full Text Search (MVP)

```sql
-- FTS-Spalte direkt auf der events-Tabelle
-- content_plain wird beim Lesen/Entschlüsseln befüllt (nicht persistent)
-- Stattdessen: separate search-Tabelle mit Klartext für FTS

CREATE TABLE event_search (
    event_id      TEXT PRIMARY KEY REFERENCES events(event_id) ON DELETE CASCADE,
    room_id       TEXT NOT NULL,
    sender        TEXT NOT NULL,
    content_plain TEXT NOT NULL,       -- Klartext NUR für Suche
    search_vector TSVECTOR
        GENERATED ALWAYS AS (
            to_tsvector('german', content_plain)
        ) STORED,
    origin_ts     BIGINT NOT NULL
);

CREATE INDEX event_search_fts_idx  ON event_search USING GIN(search_vector);
CREATE INDEX event_search_room_idx ON event_search (room_id, origin_ts DESC);
```

```sql
-- Suche mit Ranking + Snippet
SELECT
    e.event_id,
    e.sender,
    e.origin_ts,
    ts_headline('german', s.content_plain, query,
                'MaxWords=15, MinWords=5, StartSel=**, StopSel=**') AS snippet,
    ts_rank(s.search_vector, query) AS rank
FROM event_search s
JOIN events e USING (event_id),
     plainto_tsquery('german', $1) query
WHERE s.room_id = ANY($2)          -- nur Rooms wo User Mitglied ist
  AND s.search_vector @@ query
ORDER BY rank DESC
LIMIT 20;
```

```
Features gratis:
  ✅ Mehrsprachig (german, english, etc. konfigurierbar)
  ✅ Stemming  ("gesendet" findet "senden", "sendet")
  ✅ Ranking nach Relevanz
  ✅ Snippet mit Highlighting
  ✅ Keine externe Komponente
  ✅ Compliance: Suche durch Admins über alle Rooms möglich
```

### Semantische Suche mit pgvector (Phase 5, optional)

pgvector erweitert die FTS um semantisches Verständnis – "Urlaubsantrag" findet auch
"Urlaubsplanung" oder "Abwesenheit". Dies erfordert ein lokales Embedding-Modell und
wird als optionale spätere Phase betrachtet.

```
Voraussetzungen Phase 5:
  → pgvector Extension (PostgreSQL contrib, Apache 2.0)
  → Lokales Embedding-Modell (z.B. nomic-embed-text via Ollama, MIT-Lizenz)
  → Kein Cloud-API, vollständig souverän
  → Hybrid-Suche: FTS + pgvector kombiniert via Reciprocal Rank Fusion (RRF)

ADR-009: pgvector-Einführung ist zu diesem Zeitpunkt offen.
```

---

## Media Server

Der Media Server implementiert die Matrix Content Repository API.

### Matrix Media Endpoints

```
POST /_matrix/media/v3/upload
GET  /_matrix/media/v3/download/{serverName}/{mediaId}
GET  /_matrix/media/v3/thumbnail/{serverName}/{mediaId}
GET  /_matrix/media/v3/preview_url                       (Phase 5, optional)
```

### Upload Flow

```
1. Client: POST /upload + Auth-Token + Content-Type + Binary Body
2. Go Media Gateway:
   a. Token via Elixir Core verifizieren (gRPC)
   b. Dateigrößen-Limit prüfen (default: 50MB, konfigurierbar)
   c. MIME-Type gegen Whitelist validieren
   d. SHA-256 Hash berechnen (Deduplizierung)
   e. AES-256-GCM verschlüsseln (pro-Datei-Key)
   f. Verschlüsselten Blob → Storage
   g. Key + Hash + Metadata → PostgreSQL
   h. MXC-URI generieren: mxc://server/randomId
3. MXC-URI zurück an Client
```

### Download Flow

```
1. Client: GET /download/{server}/{mediaId} + Auth-Token
2. Auth-Check: User ist Mitglied eines Rooms mit diesem Media-Event?
3. Key aus PostgreSQL (media_keys)
4. Blob von Storage laden + entschlüsseln
5. Als Stream an Client (kein vollständiges In-Memory-Laden)
```

### Storage Backend

```
Lokal (default):
  /var/nebu/media/{shard}/{mediaId}.enc
  Shard = erste 2 Zeichen der MediaId
  → Kein externer Dienst, einfachster Betrieb

S3-kompatibel (Phase 4):
  MinIO, Ceph, AWS S3, Hetzner Object Storage
  → Go AWS SDK v2 (Apache 2.0)
  → Verschlüsselung immer im Go-Prozess – Storage sieht nur Ciphertext
```

---

## Matrix API – Implementierter Scope

### Core Messaging

```
POST /_matrix/client/v3/login
POST /_matrix/client/v3/logout
GET  /_matrix/client/v3/sync
PUT  /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}
GET  /_matrix/client/v3/rooms/{roomId}/messages
POST /_matrix/client/v3/createRoom
POST /_matrix/client/v3/join/{roomIdOrAlias}
PUT  /_matrix/client/v3/rooms/{roomId}/typing/{userId}
POST /_matrix/client/v3/rooms/{roomId}/receipt/{receiptType}/{eventId}
GET  /_matrix/client/v3/profile/{userId}
PUT  /_matrix/client/v3/profile/{userId}/displayname
GET  /_matrix/client/v3/presence/{userId}/status
```

### Public Key Management (Signaturen)

```
POST /_matrix/client/v3/keys/upload    ← Client registriert Ed25519 Public Key
GET  /_matrix/client/v3/keys/query     ← Public Key eines Users abfragen
```

### Media Content Repository

```
POST /_matrix/media/v3/upload
GET  /_matrix/media/v3/download/{serverName}/{mediaId}
GET  /_matrix/media/v3/thumbnail/{serverName}/{mediaId}
```

### Bewusst weggelassen

```
/_matrix/federation/*          ← kein Server-to-Server
/_matrix/identity/*            ← kein Identity Server
/_matrix/client/v3/keys/claim  ← kein E2EE, One-Time-Keys nicht nötig
```

---

## Tech Stack

```
API Gateway:        Go 1.26+
Media Gateway:      Go 1.26+
Core:               Elixir/OTP 1.19+
Signaturen:         Ed25519 via :crypto (OTP built-in)
PII Verschlüsslung: X25519 + AES-256-GCM via :crypto (OTP built-in)
Sessions/Cache:     ETS (OTP built-in)              ← kein Redis
Pub/Sub:            pg Process Groups (built-in)    ← kein NATS
Node Discovery:     libcluster (Hex)
Distributed Reg.:   Horde (Hex)
Gateway ↔ Core:     gRPC (protobuf, PSK-gesichert)
Datenbank:          PostgreSQL 16+
Migrations:         golang-migrate (Go Gateway owned)
Volltextsuche:      PostgreSQL FTS (tsvector, built-in)
Media Storage:      Lokal (default) oder S3-kompatibel
Transport:          TLS 1.3 überall, Elixir Distribution TLS
At Rest:            AES-256-GCM (Media), BYTEA encrypted (DB)
Container:          Docker + Docker Compose
CI/CD:              GitHub Actions
```

---

## Lizenz-Check aller Komponenten

| Komponente | Lizenz | Enterprise-OK? |
|---|---|---|
| Elixir/OTP (inkl. ETS, pg, :crypto, ssl) | Apache 2.0 | ✅ |
| libcluster + Horde (Hex packages) | Apache 2.0 | ✅ |
| Go | BSD 3-Clause | ✅ |
| PostgreSQL + pgcrypto | PostgreSQL License | ✅ |
| Jason (Elixir JSON) | MIT | ✅ |
| disintegration/imaging (Thumbnails) | MIT | ✅ |
| Go AWS SDK v2 | Apache 2.0 | ✅ (nur S3-Backend) |

Kein AGPLv3. Kein BSL. Kein SSPL. Keine Lizenzkosten. Keine Konzern-Abhängigkeit.

---

## Projektstruktur (geplant)

```
nebu/
├── README.md
├── docker-compose.yml
├── docker-compose.prod.yml
│
├── gateway/                        ← Go API Gateway
│   ├── cmd/gateway/
│   ├── internal/
│   │   ├── auth/                   ← OIDC, SAML, LDAP
│   │   ├── matrix/                 ← Matrix API Handler
│   │   ├── middleware/             ← Rate Limiting, TLS, Logging
│   │   └── grpc/                   ← Core-Kommunikation (mTLS)
│   └── Dockerfile
│
├── media/                          ← Go Media Gateway
│   ├── cmd/media/
│   ├── internal/
│   │   ├── upload/                 ← Upload Handler, Validierung
│   │   ├── download/               ← Download, Streaming
│   │   ├── thumbnail/              ← Thumbnail Generierung
│   │   ├── crypto/                 ← AES-256-GCM en/decrypt
│   │   └── storage/                ← Local + S3 Backend
│   └── Dockerfile
│
├── core/                           ← Elixir/OTP Umbrella
│   ├── apps/
│   │   ├── room_manager/           ← Horde + Room GenServer + Power-Level-Checks
│   │   ├── session_manager/        ← ETS + PostgreSQL hybrid since-token
│   │   ├── presence/               ← Presence Manager
│   │   ├── event_dispatcher/       ← EventBus gRPC Stream + pg Process Groups
│   │   ├── signature/              ← Ed25519 signing + X25519 encryption + Nebu.EventId
│   │   ├── permissions/            ← Power-Level-Enforcement, System-Rollen
│   │   └── nebu_db/                ← Ecto Repo (shared DB infrastructure)
│   ├── config/
│   │   ├── config.exs
│   │   ├── dev.exs
│   │   ├── prod.exs
│   │   └── runtime.exs             ← NEBU_* Env-Vars zur Laufzeit
│   ├── mix.exs                     ← Umbrella Root
│   ├── mix.lock
│   └── Dockerfile
│
├── proto/
│   └── core.proto                  ← gRPC Definitionen
│
├── certs/
│   ├── generate-dev-certs.sh       ← Self-signed für lokale Entwicklung
│   └── .gitignore
│
└── docs/
    ├── PRD.md
    ├── architecture/
    │   ├── SAD.md
    │   ├── data-model.md
    │   ├── security.md             ← TLS, Signaturen, at-rest Konzept
    │   ├── permissions.md          ← Rollen, Power-Level, OIDC-Flow
    │   ├── media-server.md
    │   ├── search.md               ← FTS MVP + pgvector Ausblick
    │   └── adr/
    │       ├── 001-go-gateway.md
    │       ├── 002-erlang-core.md
    │       ├── 003-no-federation.md
    │       ├── 004-no-redis-ets-instead.md
    │       ├── 005-no-nats-erlang-dist.md
    │       ├── 006-tls-everywhere.md
    │       ├── 007-no-e2ee-server-readable.md
    │       ├── 008-ed25519-signatures.md
    │       ├── 009-fts-before-pgvector.md
    │       ├── 010-media-local-vs-s3.md
    │       ├── 011-oidc-first-no-ldap.md
    │       └── 012-power-level-model.md
    └── stories/
        ├── epic-01-oidc-auth.md
        ├── epic-02-rooms-permissions.md
        ├── epic-03-messaging-signatures.md
        ├── epic-04-media.md
        ├── epic-05-compliance-audit.md
        ├── epic-06-search.md
        └── epic-07-clustering.md
```

---

## Roadmap

### Phase 1 – Core MVP (Ziel: Login via OIDC, erste Nachricht)

- [ ] TLS-Setup + Zertifikat-Generierung (Dev: self-signed)
- [ ] Go Gateway: HTTP-Routing, TLS 1.3
- [ ] OIDC Authorization Code Flow (Keycloak als Dev-Provider via Docker)
- [ ] Auto-Provisioning: User beim ersten Login anlegen
- [ ] Elixir Core: Room GenServer (Horde), Session Manager (ETS)
- [ ] gRPC zwischen Gateway und Core (PSK-gesichert)
- [ ] Elixir Distribution TLS konfiguriert (OTP Release config)
- [ ] `/login` (OIDC-Redirect), `/sync`, `/send` implementiert
- [ ] PostgreSQL Schema: users, rooms, room_members, events (pgcrypto)
- [ ] Docker Compose Dev-Setup inkl. Keycloak
- [ ] Element Web: Login via OIDC, erste Textnachricht senden

### Phase 2 – Berechtigungen & Rollen

- [ ] System-Rollen in PostgreSQL: user, instance_admin, compliance_officer
- [ ] Power-Level-Enforcement im Elixir Core (permissions-App)
- [ ] Room-Typen: public_room, private_room, direct_message, group_dm
- [ ] Room-Verzeichnis (`/publicRooms`) für public_rooms
- [ ] Admin-API: User-Rolle setzen, User sperren/aktivieren
- [ ] Audit-Log Tabelle (append-only, Row Security Policy)
- [ ] Compliance-Zugriffs-Flow: Request → Audit-Eintrag → Notification → Zugriff
- [ ] Power-Level-Änderungen über Room-State-Events

### Phase 3 – Nachrichtenintegrität (Signaturen)

- [ ] Ed25519 Key-Generierung client-seitig dokumentiert
- [ ] `POST /keys/upload` – Client registriert Public Key
- [ ] `GET /keys/query` – Public Key eines Users abfragen
- [ ] Elixir `signature`-App: Canonical JSON + Ed25519 Verifikation
- [ ] Server lehnt unsignierte / ungültig signierte Events ab
- [ ] Signatur wird atomar mit Event in PostgreSQL gespeichert
- [ ] Signatur-Verifikation im Audit-Fall dokumentiert und testbar

### Phase 4 – Media

- [ ] Go Media Gateway: Upload, Download, Streaming
- [ ] AES-256-GCM Verschlüsselung at rest (pro-Datei-Key)
- [ ] Auth-Check für Downloads (Room-Membership via permissions-App)
- [ ] Lokales Storage Backend mit Shard-Struktur
- [ ] Thumbnail Generierung (JPEG, PNG, WebP)
- [ ] Media Quota pro User (konfigurierbar)
- [ ] Deduplizierung via SHA-256

### Phase 5 – Suche & Compliance

- [ ] PostgreSQL FTS: `event_search`-Tabelle + tsvector Index
- [ ] Such-Endpoint: `GET /_matrix/client/v3/search` (eigene Rooms)
- [ ] Compliance-Suche: alle Rooms (compliance_officer only, auditiert)
- [ ] Nachrichten-Export mit Signatur-Nachweis (JSON + PDF)
- [ ] Mehrsprachige Suche (german + english Konfiguration)
- [ ] Message Retention Policies

### Phase 6 – Clustering & Scale

- [ ] Elixir Cluster (mehrere Core-Nodes via libcluster, Elixir Distribution TLS)
- [ ] Horde Cluster-Modus aktivieren (members: :auto, kein Refactoring nötig)
- [ ] Stateless Go Gateway (horizontal skalierbar)
- [ ] PostgreSQL Read Replicas
- [ ] S3-kompatibles Media Storage Backend
- [ ] Health Checks & Readiness Probes

### Phase 7 – Enterprise Hardening

- [ ] DSGVO-Export (vollständiger Nutzerdaten-Export)
- [ ] Right-to-be-forgotten (kryptografische Löschung)
- [ ] Push Notifications (Matrix Push Gateway)
- [ ] Rate Limiting (per User, per Room, global)
- [ ] Performance-Benchmarks (Ziel: 100k concurrent users / Node)
- [ ] Load Tests (k6)

### Phase 8 – Semantische Suche (optional)

- [ ] ADR-009 finalisieren (pgvector Entscheidung)
- [ ] pgvector Extension einführen
- [ ] Lokales Embedding-Modell (Ollama + nomic-embed-text, MIT)
- [ ] Hybrid-Suche: FTS + pgvector via Reciprocal Rank Fusion
- [ ] Embedding-Job als async Elixir Worker (GenServer)

---

## Docker Setup

### Entwicklung (lokal)

```bash
git clone https://github.com/your-org/nebu
cd nebu
./certs/generate-dev-certs.sh
docker compose up
```

```yaml
# docker-compose.yml
services:
  gateway:
    build:
      context: ./gateway
      target: gateway
    ports:
      - "8008:8008"
      - "8448:8448"
    environment:
      CORE_GRPC_ADDR: core:9000
      DB_URL: postgres://nebu:secret@postgres:5432/nebu
      TLS_CERT: /certs/gateway.crt
      TLS_KEY:  /certs/gateway.key
      TLS_CA:   /certs/ca.crt
      OIDC_ISSUER: ${OIDC_ISSUER:-}
    volumes:
      - ./certs:/certs:ro
    depends_on: [core, postgres]

  media:
    build:
      context: ./media
      target: media
    ports:
      - "8009:8009"
    environment:
      CORE_GRPC_ADDR: core:9000
      DB_URL: postgres://nebu:secret@postgres:5432/nebu
      MEDIA_PATH: /var/nebu/media
      TLS_CERT: /certs/media.crt
      TLS_KEY:  /certs/media.key
      TLS_CA:   /certs/ca.crt
    volumes:
      - ./certs:/certs:ro
      - media_data:/var/nebu/media
    depends_on: [core, postgres]

  core:
    build:
      context: ./core
      target: core
    environment:
      RELEASE_COOKIE: ${RELEASE_COOKIE:-devcookie}
      NEBU_DB_URL: postgres://nebu:secret@postgres:5432/nebu
      NEBU_INTERNAL_SECRET_FILE: /run/secrets/internal_secret
    volumes:
      - ./certs:/certs:ro
    depends_on: [postgres]

  keycloak:
    image: quay.io/keycloak/keycloak:24-alpine
    command: start-dev
    ports:
      - "8080:8080"
    environment:
      KEYCLOAK_ADMIN: admin
      KEYCLOAK_ADMIN_PASSWORD: admin
      KC_DB: dev-mem
    # Im Dev-Betrieb: Realm + Client manuell anlegen oder via Import

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: nebu
      POSTGRES_USER: nebu
      POSTGRES_PASSWORD: secret
    command: >
      postgres
        -c ssl=on
        -c ssl_cert_file=/var/lib/postgresql/certs/postgres.crt
        -c ssl_key_file=/var/lib/postgresql/certs/postgres.key
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./certs:/var/lib/postgresql/certs:ro

volumes:
  postgres_data:
  media_data:
```

### Multi-Stage Dockerfiles

```dockerfile
# gateway/Dockerfile  (identisch für media/)
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o gateway ./cmd/gateway

FROM alpine:3.19 AS gateway
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/gateway /usr/local/bin/
EXPOSE 8008 8448
ENTRYPOINT ["gateway"]
```

```dockerfile
# core/Dockerfile
FROM elixir:1.19-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY . .
RUN rebar3 release

FROM alpine:3.19 AS core
RUN apk add --no-cache libstdc++ ncurses openssl
COPY --from=builder /app/_build/prod/rel/core /opt/core
EXPOSE 9000 4369
ENTRYPOINT ["/opt/core/bin/core", "foreground"]
```

---

## Lizenz

Apache License 2.0 – vollständig Enterprise-kompatibel.  
Keine Copyleft-Pflicht. Proprietäre Erweiterungen und kommerzielle Nutzung erlaubt.

---

## Architektur-Entscheidungen

Alle Entscheidungen sind finalisiert. Details in `_bmad-output/planning-artifacts/architecture.md` und `docs/architecture/adr/`.

| ADR | Entscheidung |
|---|---|
| 001 | Elixir/OTP (nicht Erlang) — libcluster, Mix-Tooling |
| 002 | Kein Redis, kein NATS — ETS + pg Process Groups |
| 003 | Content-Hash Event-ID (Matrix Room Version 6+) |
| 004 | Horde Registry + DynamicSupervisor |
| 005 | gRPC Server-Streaming EventBus + Unary Fallback |
| 006 | message_buffer Drain-Strategie (Linear MVP → AIMD Phase 2) |
| 007 | Ed25519 (Signing) + X25519 (Encryption) — zwei Schlüsselpaare |
| 008 | Node-Registration: PSK MVP → Ephemeral mTLS Phase 2 |
| 009 | OpenAPI Spec-First mit oapi-codegen |

---

## BMAD Workflow – Empfohlener Einstieg

```
1. Architect Agent
   → PRD.md: Non-Functional Requirements, Scale-Ziele, Security
   → ADRs 001–012 ausarbeiten (Tabelle oben als Ausgangspunkt)
   → docs/architecture/security.md: TLS + Signaturen + at-rest
   → docs/architecture/permissions.md: OIDC-Flow, Rollen, Power-Level
   → docs/architecture/search.md: FTS MVP + pgvector Ausblick

2. Design Agent
   → Datenbankschema: users, rooms, room_members, events,
                      event_search, media_keys, audit_log
   → gRPC Proto: Gateway ↔ Core (Auth, Permissions, Signatur, Media)
   → Canonical JSON Spezifikation (deterministisch, testbar)
   → OIDC-Flow Sequenzdiagramm
   → Power-Level-Enforcement Regeln (vollständige Matrix)

3. Story Breakdown
   → Epic 1: "Login via OIDC (Keycloak), erste Textnachricht"
   → Epic 2: "Room erstellen, User einladen, Power-Level verwalten"
   → Epic 3: "compliance_officer durchsucht Room mit Audit-Trail"
   → Epic 4: "Signatur auf jeder Nachricht, Verifikation serverseitig"
   → Epic 5: "Bild hochladen, verschlüsselt speichern, herunterladen"
   → Epic 6: "Volltextsuche über eigene Rooms"
   → Pro Story: Akzeptanzkriterien + Berechtigungs-Kriterien explizit

4. Developer Agent
   → Story 1.1: TLS-Setup + generate-dev-certs.sh + Keycloak Dev-Container
   → Story 1.2: Go Gateway OIDC Authorization Code Flow
   → Story 1.3: Auto-Provisioning neuer User beim ersten Login
   → Story 1.4: Elixir Core – Session GenServer + ETS
   → Story 1.5: /sync Long-Poll + /send
   → Story 2.1: permissions-App – Power-Level-Checks
   → Story 2.2: Room-Typen + Room-Verzeichnis
   → Story 2.3: Admin-API: Rollen setzen, User sperren
   → Story 3.1: Audit-Log Tabelle + Row Security Policy
   → Story 3.2: Compliance-Zugriffs-Flow + Notification
   → Story 4.1: Elixir signature-App + Canonical JSON
   → Story 4.2: /keys/upload + /keys/query
   → Story 5.1: Media Upload + AES-256-GCM
   → Story 6.1: event_search Tabelle + FTS Index
   → ...
```

---

*Dieses README dient als Ausgangspunkt für den BMAD-Workflow.*  
*Alle ADRs sind vor Beginn der Implementierung zu finalisieren.*  
*ADR-011 (OIDC Dev-Provider) und ADR-012 (Power-Level-Persistenz) blockieren das DB-Schema – höchste Priorität.*