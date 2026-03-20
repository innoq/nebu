---
stepsCompleted: [1, 2, 3, 4, 5, 6, 7, 8]
inputDocuments:
  - '_bmad-output/planning-artifacts/prd.md'
  - '_bmad-output/planning-artifacts/ux-design-specification.md'
  - 'README.md'
workflowType: 'architecture'
lastStep: 8
status: 'complete'
completedAt: '2026-03-18'
project_name: 'open-chat'
user_name: 'Phil'
date: '2026-03-18'
---

# Architecture Decision Document

_This document builds collaboratively through step-by-step discovery. Sections are appended as we work through each architectural decision together._

## Project Context Analysis

### Requirements Overview

**Functional Requirements:**
52 FRs in 8 Kategorien: Identity & Authentication (FR1–6), Messaging & Rooms (FR7–16),
Room-Konfiguration (FR17–24), Cryptographic Identity & PII (FR25–29), Compliance & Audit
(FR30–35), User & Room Administration (FR36–40), Notifications (FR41–42),
Server Operations & Observability (FR43–52).

Architektonisch kritisch: FR25–29 (Ed25519 Doppelrolle Signing + PII-Verschlüsselung),
FR30–35 (Compliance Four-Eyes mit append-only Audit Log), FR43–47 (Observability + TLS).

**Non-Functional Requirements:**
- Performance: ≤500ms Message-Latenz, Silber >500/Node als MVP-Gate (NFR-P1–P4)
- Security: TLS 1.3, at-rest Verschlüsselung, append-only signierter Audit Log (NFR-S1–S6)
- Scalability: Stateless Gateway, Elixir Multi-Node via libcluster Phase 2 (NFR-SC1–SC3)
- Reliability: OTP Process-Isolation, PostgreSQL als Single Source of Truth (NFR-R1–R3)
- Operability: `docker-compose up` in ≤10 Minuten, Admin UI im Gateway-Binary (NFR-O1–O4)
- Compliance: DSGVO Right-to-be-Forgotten via kryptografischer Schlüssellöschung (NFR-C1–C3)
- Matrix-Konformität: Inkompatibilitäten mit Element/FluffyChat gelten als Bugs (NFR-M1–M2)
- Accessibility: WCAG 2.1 Level AA für Admin UI (NFR-A1–A3)
- Crypto-Agilität: Kryptografische Primitive modular austauschbar (NFR-CR1)

**Scale & Complexity:**
- Primary domain: API Backend / Distributed Systems / Matrix Protocol
- Complexity level: Enterprise/Hoch
- Estimated architectural components: ~12

### Resolved Technology Decisions

**Elixir/OTP (nicht Erlang/OTP):** Entschieden wegen libcluster (automatische
Node-Discovery), Mix-Tooling, Elixir-Ökosystem. README und CLAUDE.md sind veraltet —
Elixir/OTP ist die kanonische Entscheidung. Alle zukünftigen Artefakte verwenden Elixir.

### Technical Constraints & Dependencies

- Drei Laufzeit-Komponenten: Go-Binary (Gateway + Media GW), Elixir-Release (Core), PostgreSQL
- Kein Redis, kein NATS — ETS + pg Process Groups + Mnesia ersetzen diese vollständig
- OIDC-only Authentication — kein lokaler Auth-Pfad, kein Passwort-Hashing
- Docker Compose als einziges supported Deployment-Target (Kubernetes: possible, unsupported)
- Matrix Client-Server API Kompatibilität: Inkompatibilitäten sind Bugs
- Apache 2.0 Lizenz: alle Abhängigkeiten müssen kompatibel sein
- Interne TLS-Anforderung: MVP auto-negotiiert im Container-Netz; Growth: konfigurierbar

### Cross-Cutting Concerns Identified

| Concern | Betroffene Komponenten |
|---|---|
| Ed25519 Key Management | Gateway, Core (signature), PostgreSQL, Admin UI |
| OIDC Auth + Bootstrap Mode | Gateway (auth), Core (session), Admin UI |
| TLS everywhere (extern + intern) | Gateway, Media-GW, Core, Elixir Distribution |
| Append-Only Audit Log + Ed25519-Signatur | Core, PostgreSQL (RLS), Compliance API |
| gRPC Gateway↔Core Boundary | Gateway/grpc, Core/proto, alle Features |
| Observability (/health, /ready, /metrics) | Gateway, Core, Docker Compose |
| URL-State Constraint (Admin UI) | Admin UI (alle Views, alle Workflows) |
| Compliance Four-Eyes Flow | Core, Admin UI, Notification System |
| Three PII Tiers + Cryptographic Deletion | Core, PostgreSQL, Admin API |

### Identified Gaps & Open Questions

Folgende Lücken wurden in der Analyse identifiziert und müssen als ADRs oder Constraints
adressiert werden:

| # | Lücke | Typ | Priorität |
|---|---|---|---|
| G1 | Sync-API-Strategie: ETS Session-State, since-Token-Format, Cold vs. Incremental Sync | ADR-Kandidat | Hoch |
| G2 | gRPC Interface-Scope: welche Operationen sync (Request/Response) vs. streaming | ADR-Kandidat | Hoch |
| G3 | Single-Node vs. Cluster — Room-Autoritätsstrategie muss ab MVP designt werden, sonst bricht Clustering | Constraint | Hoch |
| G4 | Lastprofil für Silber-Tier-Test: Traffic-Mix (sync + send + presence + typing gleichzeitig) | NFR-Ergänzung | Mittel |
| G5 | Kryptografische Deletion — Failure Mode & Atomarität bei Prozess-Crash mid-delete | FR-Ergänzung | Hoch |
| G6 | CI/CD Integration-Test-Strategie: Go + Elixir Stack im Test (Docker Compose im CI?) | ADR-Kandidat | Mittel |
| G7 | Event-ID-Generierung in Non-Federation: UUID, Content-Hash oder eigenes Schema? | ADR-Kandidat | Hoch |
| G8 | Server Name Konfiguration & Matrix-Identität: `@user:server.name` — Konfiguration, Migration-Konsequenzen | Constraint | Hoch |
| G9 | Media Gateway MVP-Scope: README beschreibt vollständigen Media-GW, PRD setzt Media als Growth — Klärung nötig | Scope-Gap | Hoch |

---

## Starter Template & Project Scaffolding

### Primary Technology Domain

API Backend — Multi-Process Distributed System. Kein klassischer Starter-Template-Ansatz,
da Go + Elixir keine npm/create-X Bootstrapper haben. Manuelles Scaffolding.

### Initialization

```bash
# Go Gateway + Media Gateway
go mod init github.com/nebu/nebu
mkdir -p gateway/cmd/gateway gateway/internal/{auth,matrix,middleware,grpc,registry,buffer}
mkdir -p media/cmd/media media/internal/{upload,download,thumbnail,crypto,storage}

# Elixir/OTP Core (Umbrella)
mix new core --umbrella
cd core && mix new apps/room_manager --sup
mix new apps/session_manager --sup
mix new apps/presence --sup
mix new apps/event_dispatcher --sup
mix new apps/signature --sup
mix new apps/permissions --sup

# Shared
mkdir proto
mkdir -p gateway/migrations  # golang-migrate SQL-Dateien
```

### Resolved: Migrations (G10)

**Entscheidung: Go Gateway übernimmt alle PostgreSQL-Migrations.**

- Tooling: `github.com/golang-migrate/migrate/v4` (Apache 2.0, aktiv maintained)
- SQL-Dateien in `gateway/migrations/*.sql` — keine DSL, kein ORM
- Migrations laufen **synchron beim Go-Start**, bevor der HTTP-Listener hochkommt
- Elixir hat **keinen Schema-Schreibzugriff** — Go ist alleiniger Schema-Owner
- Kein separater Init-Container nötig

### Resolved: Resilienz & Selbst-Heilung (G11–G13)

**Selbst-Heilungs-Hierarchie:**

```
Level 1 — OTP Supervisor Trees:   crasht ein GenServer → sofortiger Neustart intern
Level 2 — Docker restart: always: crasht der Container → Docker startet neu
Level 3 — heart (optional):       nur für Bare-Metal-Deployments außerhalb Container
```

Go Gateway ist **kein Process-Supervisor** — es ist Health-Poller, Status-Tracker und
Durable-Buffer-Manager.

**Go Gateway Status-Modell (G12 — abgeleitet aus gRPC-Stream-Status):**

| Status | Bedingung | Gateway-Verhalten |
|---|---|---|
| GRÜN | gRPC EventBus-Stream steht | Normaler Betrieb, direktes Streaming |
| GELB | Stream unterbrochen, Fallback Unary-Polling erfolgreich | Vorsorglich in `message_buffer` schreiben, Unary-Polling weiter |
| ROT | Stream UND Unary-Polling scheitern | Alle Writes in `message_buffer` halten, 200 OK + event_id (Matrix-konform), Docker heilt Elixir |
| GRÜN nach ROT | Stream re-established | Drain-Worker startet, Buffer abarbeiten |

Kein konfigurierbarer Threshold — Status ist direkte Funktion des gRPC-Verbindungsstatus.

**Container Health-Check:**
```yaml
core:
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:4000/health"]
    interval: 10s
    timeout: 5s
    retries: 3
    start_period: 30s
  restart: always
```

`/health`-Response enthält von Anfang an `load_factor: 0.0–1.0` (MVP: immer `1.0`) —
für adaptive Drain-Steuerung in Phase 2 vorbereitet.

**`message_buffer` Tabelle:**

```sql
CREATE TABLE message_buffer (
    id           BIGSERIAL PRIMARY KEY,
    txn_id       TEXT NOT NULL,
    room_id      TEXT NOT NULL,
    sender       TEXT NOT NULL,
    payload      JSONB NOT NULL,
    received_at  BIGINT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending', -- pending | held
    retry_count  SMALLINT NOT NULL DEFAULT 0,
    processed_at BIGINT
);

CREATE TABLE message_dead_letter (
    id           BIGSERIAL PRIMARY KEY,
    buffer_id    BIGINT NOT NULL,
    txn_id       TEXT NOT NULL,
    payload      JSONB NOT NULL,
    failed_at    BIGINT NOT NULL,
    last_error   TEXT
);
```

**Buffer-Drain-Strategie (G13) — Pluggbares Strategy Pattern:**

Drain-Verhalten ist eine austauschbare Strategie, konfigurierbar zur Laufzeit.
MVP: Linear. Phase 2: Adaptiv (AIMD basierend auf Elixir `load_factor`).

```
DrainStrategy Interface:
  rate(load_factor float64, buffer_size int64) → msg/s

MVP-Implementierung (Linear):
  rate = BASE_RATE                          -- konfigurierbar, Default 100 msg/s

Phase 2-Implementierung (AIMD-Adaptiv):
  slope     → konfigurierbar (Steigungskoeffizient)
  intercept → konfigurierbar (Parallelverschiebung)
  rate = max(MIN_RATE, BASE_RATE * (1 - load_factor) * slope + intercept)
```

Admin-UI (Phase 2): Drain-Funktion als interaktiver Graph visualisiert.
Admin kann Slope + Intercept einstellen und sieht live die resultierende Rate-Kurve.

Weitere Drain-Parameter (beide Phasen):
- Reihenfolge: FIFO nach `received_at`
- Retry-Limit: Default 3 (konfigurierbar)
- Dead-Letter nach N Retries → `message_dead_letter` + Prometheus-Metrik + Admin-UI-Anzeige

**V3 — Elixir Node-Registrierung: Security-Modell**

Zwei-Phasen-Ansatz für die Sicherung des `/internal/*` Endpunkts:

**MVP — Pre-Shared Secret via Docker Compose Secrets:**

```bash
# make setup — generiert einmalig, .secrets/ in .gitignore
mkdir -p .secrets && openssl rand -hex 32 > .secrets/internal_secret
```

```yaml
# docker-compose.yml
secrets:
  internal_secret:
    file: .secrets/internal_secret
services:
  gateway:
    secrets: [internal_secret]
    environment:
      NEBU_INTERNAL_SECRET_FILE: /run/secrets/internal_secret
  core:
    secrets: [internal_secret]
    environment:
      NEBU_INTERNAL_SECRET_FILE: /run/secrets/internal_secret
```

Go validiert `Authorization: Bearer <secret>` Header auf allen `/internal/*` Requests.
Secret wird aus File geladen — nie als Env-Var direkt (kein `docker inspect` Leak).
Nach `docker compose down && up`: `make setup` neu ausführen → neues Secret.

**Phase 2 — Ephemeral mTLS (Consul-Connect-Muster):**

`generate-dev-certs.sh` erzeugt bei `make setup` eine kurzlebige CA + Gateway-Cert + Core-Client-Cert.
Go Gateway validiert eingehende Connections von Elixir anhand des Client-Certs (mTLS).
Certs werden als Docker Secrets geteilt — ephemer, bei jedem `make setup` neu generiert.
Vorteil: Automatische Key-Rotation bei Neustart, kein zentraler Secret-Store nötig.

```bash
# generate-dev-certs.sh (Erweiterung Phase 2)
openssl genrsa -out .secrets/ca.key 4096
openssl req -x509 -new -key .secrets/ca.key -out .secrets/ca.crt -days 1
openssl genrsa -out .secrets/gateway.key 2048
openssl req -new -key .secrets/gateway.key | openssl x509 -req -CA .secrets/ca.crt ...
```

**Elixir Node-Registrierung:**
- Elixir registriert sich beim Start via `POST /internal/nodes/register`
- Go pollt `GET :4000/health` alle 5s (konfigurierbar)
- heart: deaktiviert im Container, optional für Bare-Metal

---

## Core Architectural Decisions

### Daten-Architektur

**G7 — Event-ID-Generierung: Content-Hash**

- Format: `$<base64url(SHA-256(canonical_json(event \ {signatures, unsigned})))>`
- Spec: Matrix Room Version 6+ (Federation-kompatibel ab Tag 1)
- Canonical JSON: Keys alphabetisch sortiert, keine `signatures`- und `unsigned`-Felder
- Implementierung: Elixir `Nebu.EventId` Modul, wiederverwendet `canonical_json/1` aus Signature-App
- Vorteil: tamper-evident (ID ≠ neu berechneter Hash → Manipulation erkannt), reproduzierbar bei Recovery
- Cascading: Event-ID-Berechnung vor DB-Write, Verifikation beim Audit-Export

**G8 — Server Name: Immutable**

- `server_name` (z.B. `chat.example.com`) wird beim ersten Start aus Config gelesen und in `server_config` Tabelle geschrieben
- PostgreSQL Row Security Policy verhindert UPDATE — technisch erzwungene Immutabilität
- Admin-UI: schreibgeschützte Anzeige mit expliziter Warnung "Änderung erfordert vollständige Datenmigration"
- Phase 3: Hostname-Migration-Konzept als eigenständiges Feature
- Cascading: alle User-IDs (`@user:server_name`), Room-IDs (`!id:server_name`), Event-IDs

```sql
CREATE TABLE server_config (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    set_at     BIGINT NOT NULL
);
-- RLS: nur INSERT erlaubt, kein UPDATE/DELETE
ALTER TABLE server_config ENABLE ROW LEVEL SECURITY;
CREATE POLICY config_insert_only ON server_config FOR INSERT WITH CHECK (true);
```

**V1 — Kryptografische Schlüsselarchitektur: Zwei Schlüsselpaare pro User**

Ed25519 ist ein Signatur-Algorithmus und kann nicht direkt für Verschlüsselung verwendet werden.
Jeder User erhält zwei separate Schlüsselpaare:

| Schlüsselpaar | Algorithmus | Zweck | OTP-Modul |
|---|---|---|---|
| Signing Key | Ed25519 | Nachrichtensignatur, non-repudiation | `:crypto.sign/4` mit `eddsa` |
| Encryption Key | X25519 (ECDH) | PII-Verschlüsselung, DSGVO-Deletion | `:crypto.generate_key(:ecdh, :x25519)` |

- Beide Keys: native in OTP 24+ via `:crypto` — kein externes Hex-Package nötig
- Verschlüsselung von Sensitive PII: ECDH-Schlüsselaustausch (X25519) → AES-256-GCM (symmetrisch)
- DSGVO Right-to-be-Forgotten: `DELETE` beide private Keys → PII irrecoverably verschlüsselt
- PostgreSQL: `signing_key_id` und `encryption_key_id` als separate Spalten in `users`-Tabelle
- Referenz-Modell: Signal Protocol, Age Encryption, WireGuard — alle nutzen separate Ed25519/X25519

```elixir
# Schlüsselgenerierung bei User-Registrierung
{signing_pub, signing_priv}    = :crypto.generate_key(:eddsa, :ed25519)
{encrypt_pub, encrypt_priv}    = :crypto.generate_key(:ecdh, :x25519)
```

**G5 — Kryptografische Deletion: Transaction + Retry**

- PostgreSQL-Transaktion: `DELETE signing_private_key` + `DELETE encryption_private_key` + `UPDATE sensitive_pii_marker` atomar
- Bei Fehler: Retry möglich (idempotent), UI zeigt Status
- Audit-Log-Eintrag `deletion_failed` auch bei gescheitertem Versuch (DSGVO-Nachweis)
- Audit-Log-Eintrag `deletion_succeeded` nach Erfolg

### API & Kommunikation

**G1 — Sync-API: Hybrid ETS + PostgreSQL**

- ETS: aktive Session-State, since-Token-Cursor (Hot-Path, in-memory)
- PostgreSQL: since-Token Persistenz — bei Elixir-Restart: Recovery ohne Cold-Sync-Zwang
- Since-Token-Format: `v1_<base64url(server_ts_ms + cursor_map)>` — versioniert, opak für Clients
- Cold-Sync: bei fehlendem oder unbekanntem since-Token
- Cascading: Session-Manager GenServer hält ETS-State, schreibt Checkpoints in PostgreSQL

**G2 — gRPC Interface: Server-Streaming + Unary Fallback**

Zwei Kommunikationspfade Go→Elixir:

| Pfad | Typ | Wann |
|---|---|---|
| Primary | gRPC Server-Streaming `EventBus` | GRÜN-Status |
| Fallback | gRPC Unary `GetPendingEvents` | GELB-Status |

Proto-Services:
```protobuf
service CoreService {
  // Matrix-Operationen (Go Client → Elixir Server)
  rpc SendEvent(SendEventRequest) returns (SendEventResponse);
  rpc CreateRoom(CreateRoomRequest) returns (CreateRoomResponse);
  rpc JoinRoom(JoinRoomRequest) returns (JoinRoomResponse);
  rpc GetMessages(GetMessagesRequest) returns (GetMessagesResponse);
  rpc SetPresence(SetPresenceRequest) returns (SetPresenceResponse);
  rpc SetTyping(SetTypingRequest) returns (SetTypingResponse);
  rpc ValidateToken(ValidateTokenRequest) returns (ValidateTokenResponse);
  rpc GetPendingEvents(GetPendingRequest) returns (GetPendingResponse); // Fallback

  // Event-Bus (Streaming)
  rpc EventBus(EventBusRequest) returns (stream Event);
}
```

Ein EventBus-Stream pro Go-Instanz (nicht pro Client). Go verteilt intern an wartende
Client-Long-Poll-Verbindungen.

Bei Stream-Verlust: Matrix-Clients bekommen leeres Sync-Response + `retry_after_ms` —
Matrix-konform, Clients retrien automatisch.

**G12 — GELB/ROT-Definition (aus G2 abgeleitet):**

Kein arbiträrer Threshold — Status ist direkte Funktion des gRPC-Verbindungsstatus:
- GRÜN: EventBus-Stream healthy
- GELB: Stream unterbrochen, Unary-Fallback erfolgreich
- ROT: Stream UND Unary scheitern

**G3 — Room-Autorität: Horde**

- `Horde.Registry` + `Horde.DynamicSupervisor` ab MVP
- `members: :auto` via libcluster — Single-Node und Cluster-Betrieb ohne Code-Änderung
- CRDT-basiert: netsplit-sicher, kein Split-Brain bei Room-Prozessen
- Phase 2 Clustering: Konfigurationsswitch, kein Refactoring

### Infrastructure & Deployment

**G9 — Media Gateway: Minimal MVP**

- Scope MVP: `POST /_matrix/media/v3/upload` + `GET /_matrix/media/v3/download/{server}/{id}`
- Verschlüsselung: AES-256-GCM per File (32-Byte Key + 12-Byte Nonce, je File einmalig generiert)
- Storage: Lokales Filesystem (`/var/nebu/media/{shard}/{id}.enc`)
- Key-Storage: `media_keys` Tabelle in PostgreSQL (s.u.)
- Kein Thumbnail, kein S3 im MVP
- Auth-Check: Room-Membership via Elixir permissions (gRPC)
- Growth: Thumbnails, S3-Backend, Quota

**V2 — `media_keys` Tabelle:**

```sql
CREATE TABLE media_keys (
    media_id     TEXT PRIMARY KEY,  -- "{shard}/{id}" — identisch mit Filesystem-Pfad
    aes_key      BYTEA NOT NULL,    -- 32 Bytes AES-256
    aes_nonce    BYTEA NOT NULL,    -- 12 Bytes GCM Nonce
    uploader_id  TEXT NOT NULL,     -- user_id — für DSGVO Right-to-be-Forgotten
    room_id      TEXT,              -- nullable: DM-Uploads haben keinen room_id
    uploaded_at  BIGINT NOT NULL
);
CREATE INDEX media_keys_uploader_idx ON media_keys (uploader_id);
```

DSGVO-Deletion: `DELETE FROM media_keys WHERE uploader_id = $1` — File bleibt auf Disk,
ist aber permanent undecryptable (kryptografische Löschung ohne Disk-Wipe).

**G6 — CI/CD Integration-Tests: Hybrid**

- Unit-Tests: Go (`go test`), Elixir (`mix test`) — schnelles Feedback, immer
- Integration-Tests: Docker Compose Stack + Gherkin-Szenarien — separater CI-Job auf main/PR
- Gherkin: primäres Quality Gate (alle Must-Have-Features müssen grün sein)
- Unit-Tests: Pflicht für Crypto-Operationen, Canonical-JSON, Ed25519, PII-Verschlüsselung

**G4 — Lastprofil Silber-Tier-Test (>500 concurrent users)**

```
Traffic-Mix:
  60% GET /sync (Long-Poll, immer aktive Verbindungen)
  20% PUT /rooms/{id}/send (Nachrichtenversand)
  10% Presence + Typing Indicators
   5% CreateRoom / JoinRoom
   5% Profile + sonstige

Referenz-Topologie:
  10 aktive Rooms × 50 Mitglieder = 500 concurrent users
  (realistisches Stress-Profil für Horde + Room-GenServer-Concurrency)

Infrastruktur: 2× AWS EC2 m5.large (oder vergleichbar)
Ohne Redis, NATS, Kafka — Elixir/OTP + PostgreSQL only
```

### Decision Impact Analysis

**Implementierungs-Reihenfolge (kritischer Pfad):**

1. `server_config` + `server_name` (Fundament aller IDs)
2. PostgreSQL-Schema + golang-migrate Setup
3. gRPC Proto (`core.proto`) + Code-Generation Go + Elixir
4. Elixir Session-Manager + ETS + Horde-Registry
5. OIDC Auth + Bootstrap-Mode
6. `/sync` mit Hybrid ETS+PostgreSQL since-Token
7. `SendEvent` + Ed25519-Signatur + Content-Hash Event-ID
8. Compliance Four-Eyes + Audit-Log
9. message_buffer + Drain-Strategy (linear)
10. Minimal Media (Upload/Download)

**Cross-Component-Dependencies:**

| Entscheidung | Abhängig von | Beeinflusst |
|---|---|---|
| Content-Hash Event-ID | Canonical JSON (Signature-App) | Alle Event-Writes, Audit-Export |
| Horde Registry | libcluster Konfiguration | alle Room-GenServer, Presence |
| gRPC EventBus Stream | Proto-Definition | Sync-API, GELB/ROT-Status |
| message_buffer | GRÜN/GELB/ROT-Status | Alle schreibenden Matrix-Ops |
| Drain-Strategy Pattern | message_buffer Schema | Admin-UI Phase 2 Graph |
| server_name RLS | PostgreSQL Schema | User-IDs, Room-IDs, Event-IDs |

---

## Implementation Patterns & Consistency Rules

### Naming Conventions

**PostgreSQL:**
- Tabellen: `snake_case`, **plural** — `users`, `room_members`, `audit_logs`, `events`
- Spalten: `snake_case` — `user_id`, `created_at`, `server_ts`
- Indizes: `{table}_{columns}_idx` — `events_room_id_ts_idx`
- Foreign Keys: `{referenced_table_singular}_id` — `room_id`, `sender_id`

**Go:**
- Packages: lowercase, singular — `package matrix`, `package auth`, `package buffer`
- Exported Types/Functions: PascalCase — `RoomManager`, `SendEvent`
- JSON Tags: `snake_case` — `` `json:"room_id"` ``, `` `json:"server_ts"` ``
- Env-Variablen: `NEBU_{COMPONENT}_{KEY}` — `NEBU_DB_URL`, `NEBU_OIDC_ISSUER`, `NEBU_CORE_GRPC_ADDR`

**Elixir:**
- Module: `Nebu.{Domain}.{Name}` — `Nebu.Room.Manager`, `Nebu.Auth.OIDC`, `Nebu.Signature`
- Funktionen/Variablen: `snake_case` — `send_event/2`, `validate_token/1`

**Proto/gRPC:**
- Messages: PascalCase — `SendEventRequest`, `EventBusResponse`
- Fields: `snake_case` — `room_id`, `sender_id`, `origin_ts`
- Services: PascalCase — `CoreService`

**Matrix + Admin API JSON:** beide `snake_case` — konsistent mit Matrix-Spec.

---

### Timestamps

Boundary-spezifische Formate — Phils Prinzip "String nur wo nur Transport":

| Kontext | Format | Begründung |
|---|---|---|
| PostgreSQL | `BIGINT` ms | Range-Queries, Index-Effizienz, Arithmetic — nicht verhandelbar |
| Proto/gRPC | `int64` ms | Proto native |
| Matrix API JSON | `int64` ms | Matrix-Spec verpflichtend |
| Admin API JSON | ISO 8601 String `"2026-03-18T14:32:00Z"` | Human-readable für Kai im Browser |
| Interner Transport (keine Query/Arithmetic) | String OK | wenn Wert nur durchgereicht wird |

Go: `time.UnixMilli()` / `time.UnixMilli(ts).UTC()`
Elixir: `System.system_time(:millisecond)` / `DateTime.from_unix!(ts, :millisecond)`

---

### API Response Formate

**Matrix API** — Exakt nach Spec, kein Wrapper:
```json
{ "event_id": "$abc123", "room_id": "!xyz:server.name" }
```

**Matrix Error Format** (nur für `/_matrix/*` Endpunkte):
```json
{ "errcode": "M_NOT_FOUND", "error": "Room not found" }
```

**Admin API** (`/api/v1/*`) — Standard-Wrapper, immer:
```json
// Erfolg
{ "data": { ... }, "meta": { "cursor": "v1_abc", "limit": 50, "total": 200 } }

// Fehler
{ "error": { "code": "USER_NOT_FOUND", "message": "User with id X not found" } }
```

**Strikte Trennung:** Matrix-Endpunkte geben niemals Admin-Format zurück. Admin-Endpunkte geben niemals Matrix-Format zurück. Keine Überschneidung.

**Admin API Pagination:** Cursor-based — `?cursor=<opaque>&limit=50`. Kein Offset/Page.

---

### Auth-Token-Flow

```
Matrix-Client → Go Gateway:
  Authorization: Bearer <oidc_token>
  → Go validiert Token via OIDC-Provider
  → extrahiert: user_id, system_role

Go → Elixir (gRPC Metadata):
  "x-user-id": "@user:server.name"
  "x-system-role": "user" | "instance_admin" | "compliance_officer"

Elixir: vertraut Go vollständig — keine eigene Token-Validierung
Kein Token wird an Elixir weitergeleitet — nur user_id + system_role
```

**V4 — Keycloak OIDC Claims Mapping:**

OIDC-Claims → Nebu-Interna, orientiert an den User-Szenarien (Kai/Admin, Compliance Officer, Alex/User):

| OIDC Claim | Nebu-Nutzung | Typ | Wert-Beispiele |
|---|---|---|---|
| `sub` | `user_id` → `@{sub}:server.name` | String (UUID) | Stable, eindeutig |
| `preferred_username` | Display-Name | String | `kai.mueller` |
| `email` | Sensitive PII (Tier 2, verschlüsselt via X25519) | String | Niemals im Log |
| `nebu_role` | `system_role` | String | `instance_admin` \| `compliance_officer` \| `user` |

Das `nebu_role` Custom Claim muss im Keycloak-Realm via **Client Mapper** konfiguriert werden:
- Mapper Typ: "Hardcoded claim" (für Test-User) oder "User Attribute" (für Produktion)
- Claim Name: `nebu_role`
- `dev/keycloak/realm-export.json` enthält drei vorkonfigurierte Test-User:
  - `kai@example.com` → `nebu_role: instance_admin`
  - `compliance@example.com` → `nebu_role: compliance_officer`
  - `alex@example.com` → `nebu_role: user`

Bootstrap Mode: erster OIDC-Login erhält automatisch `instance_admin`, unabhängig vom `nebu_role` Claim — danach permanent deaktiviert.

---

### Health & Readiness Endpoints

Angelehnt an Spring Boot Actuator: strikte Trennung von Liveness (lebt der Prozess?) und
Readiness (kann er Traffic annehmen?). Docker Compose nutzt `/health` (Liveness).
Go's GRÜN/GELB/ROT leitet sich aus dem `/ready`-Status des Core ab.

**Go Gateway:**

```
GET :8080/health   → Liveness
GET :8080/ready    → Readiness
GET :8080/metrics  → Prometheus
```

```json
// GET :8080/health — minimal, immer erreichbar wenn Prozess lebt
{ "status": "UP", "version": "0.1.0" }

// GET :8080/ready — alle Abhängigkeiten geprüft
{
  "status": "READY",
  "checks": {
    "database":   { "status": "UP" },
    "core_grpc":  { "status": "UP", "nebu_status": "GRÜN" },
    "migrations": { "status": "UP", "version": 7 }
  }
}
// status: "READY" | "NOT_READY"
// nebu_status: "GRÜN" | "GELB" | "ROT"
```

**Elixir Core:**

```
GET :4000/health   → Liveness + Komponenten-Status
```

```json
// GET :4000/health
{
  "status": "UP",
  "load_factor": 0.42,
  "version": "0.1.0",
  "node": "nebu@core-1",
  "components": {
    "database":      { "status": "UP" },
    "room_registry": { "status": "UP", "room_count": 142 },
    "event_bus":     { "status": "UP", "connected_gateways": 2 }
  }
}
// status: "UP" | "DEGRADED" | "DOWN"
// load_factor: 0.0-1.0 (MVP: immer 1.0, Phase 2: real berechnet)
```

HTTP Status Codes: `200 OK` für UP/DEGRADED, `503 Service Unavailable` für DOWN.
DEGRADED bedeutet: Core läuft, aber Komponente hat Probleme — Go wechselt auf GELB.

---

### API Spec-First Workflow (V6)

`gateway/api/openapi.yaml` ist die **alleinige Source of Truth** für die Admin API.
Implementierung folgt immer dem Spec — kein freestyle.

**Workflow:**
```
1. openapi.yaml bearbeiten          ← PR-Review hier
2. make gen-api                     ← codegen via DOCKER_GO
3. generiertes Interface implementieren
4. openapi.json (konvertiert) via go:embed served
```

**Makefile:**
```makefile
gen-api:
    $(DOCKER_GO) sh -c "go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
        -generate types,server \
        -package admin \
        -o gateway/internal/admin/api_gen.go \
        gateway/api/openapi.yaml"
```

Generierte Datei `api_gen.go` enthält:
- Alle Request/Response-Typen (Go structs)
- `ServerInterface` — das Interface das die Implementierung erfüllen muss
- Validation Middleware (Request-Body-Validation gegen Schema)

`openapi.yaml` → (konvertiert bei gen-api) → `openapi.json` → via `go:embed` serviert an `/api/v1/openapi.json`.

---

### Error Handling

**Go** — Return-basiert, kein panic außer bei Programmierfehlern:
```go
func (s *Server) SendEvent(ctx context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error) {
    if err := s.validate(req); err != nil {
        return nil, status.Errorf(codes.InvalidArgument, "invalid: %v", err)
    }
    result, err := s.core.SendEvent(ctx, req)
    if err != nil {
        return nil, status.Errorf(codes.Internal, "core error: %v", err)
    }
    return result, nil
}
```

**Elixir** — Tagged Tuples, kein throw/raise außer für Programmierfehler:
```elixir
def send_event(attrs) do
  with {:ok, validated} <- validate(attrs),
       {:ok, event}     <- persist(validated) do
    {:ok, event}
  end
end
```

**gRPC Status Codes:**

| Situation | Code |
|---|---|
| Nicht gefunden | `NOT_FOUND` |
| Ungültige Eingabe | `INVALID_ARGUMENT` |
| Nicht authentifiziert | `UNAUTHENTICATED` |
| Verboten | `PERMISSION_DENIED` |
| Interner Fehler | `INTERNAL` |
| Elixir nicht erreichbar | `UNAVAILABLE` |

---

### Logging

**Go** — `log/slog` (stdlib, Go 1.21+), strukturiert:
```go
slog.Info("event sent", "room_id", roomID, "sender", sender, "event_id", eventID)
slog.Warn("grpc stream lost, switching to polling", "node_id", nodeID)
slog.Error("db write failed", "op", "SendEvent", "err", err)
```

**Elixir** — `Logger` mit Keyword-Metadata:
```elixir
Logger.info("event sent", room_id: room_id, sender: sender, event_id: event_id)
Logger.warning("elixir node degraded", node: node_id)
Logger.error("db write failed", error: inspect(err))
```

**Log Level Policy:**
- `DEBUG`: nur Entwicklung, niemals Credentials oder PII
- `INFO`: normale Operationen — Login, Event sent, Room created
- `WARN` / `WARNING`: degraded state, Retry, Fallback aktiv, GELB-Status
- `ERROR`: unerwartete Fehler, DB-Failures, gRPC-Errors, ROT-Status

---

### Testing Patterns

**Go** — Table-driven Tests als Standard:
```go
func TestCanonicalJSON(t *testing.T) {
    tests := []struct{ name, input, want string }{
        {"sorted keys", `{"b":1,"a":2}`, `{"a":2,"b":1}`},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.want, canonicalJSON(tt.input))
        })
    }
}
```

**Elixir** — `describe`-Blöcke, ExMachina für Factories:
```elixir
describe "EventId.generate/1" do
  test "deterministic for same content" do
    event = build(:event)
    assert EventId.generate(event) == EventId.generate(event)
  end
end
```

**Fixture-Orte:**
- Go: `gateway/testdata/*.json`
- Elixir Factories: `test/support/factories.ex` (ExMachina)
- Gherkin Feature Files: `test/features/*.feature`
- Test-Schlüssel (Ed25519 Testkeys): `test/fixtures/test_keys/` — in `.gitignore`, generiert via `mix test.setup`

**Gherkin:** Feature-Files auf Englisch, Scenarios beschreiben User-Behavior:
```gherkin
Feature: Message Integrity
  Scenario: Valid signed event is accepted
    Given a user with a registered Ed25519 public key
    When they send a correctly signed event
    Then the event is stored with status 200 OK
```

---

### gRPC / Proto Patterns

- Jede Operation: eigener `{Operation}Request` + `{Operation}Response` Message-Typ
- Pagination in Responses: `next_page_token` (string, leer wenn letzte Seite)
- Leere Responses: `google.protobuf.Empty`
- Optionale Felder: `optional` Keyword (proto3)
- Stream-Reconnect: exponentielles Backoff, max 30s, `jitter` zur Vermeidung von Thundering Herd

---

### Enforcement

**Alle AI-Agenten MÜSSEN:**
1. Timestamps als `BIGINT` in PostgreSQL speichern — kein `TIMESTAMPTZ`, kein `TEXT`
2. Auth-Token nie an Elixir weitergeben — nur `user_id` + `system_role` via gRPC-Metadata
3. Matrix-Endpunkte geben Matrix-Format zurück, Admin-Endpunkte geben Admin-Format zurück — kein Mischen
4. Env-Variablen nach `NEBU_{COMPONENT}_{KEY}` Schema benennen
5. Go: table-driven Tests für alle Funktionen mit mehreren Inputs
6. Elixir: `{:ok, result}` / `{:error, reason}` — kein throw/raise für Business-Logic-Fehler
7. Event-IDs immer via `Nebu.EventId.generate/1` erzeugen — nie manuell konstruieren
8. `canonical_json/1` aus der Signature-App verwenden — keine eigene Implementierung
9. **Admin UI ausschließlich via `embed.FS` ausliefern** — kein Filesystem-Zugriff zur Laufzeit:
   ```go
   //go:embed templates/* static/*
   var adminFS embed.FS
   ```
10. **PII-Verschlüsselung ausschließlich via X25519 (Encryption Key)** — nie via Ed25519 (Signing Key)
11. **Admin API Implementation muss `ServerInterface` aus `api_gen.go` erfüllen** — kein freestyle routing
12. **Secrets nie als Env-Var direkt** — immer via `NEBU_*_FILE` auf gemountete Secret-Datei

**Anti-Patterns (verboten):**
```go
// ❌ Offset-Pagination
GET /api/v1/users?page=2&per_page=50

// ✅ Cursor-Pagination
GET /api/v1/users?cursor=v1_abc&limit=50
```
```elixir
# ❌ Direkte Event-ID-Konstruktion
event_id = "$" <> UUID.generate()

# ✅ Canonical Event-ID
event_id = Nebu.EventId.generate(event)
```
```go
// ❌ ISO 8601 in Matrix API Response
{"server_ts": "2026-03-18T14:32:00Z"}

// ✅ Integer ms in Matrix API Response
{"server_ts": 1742305920000}
```
```go
// ❌ Filesystem-Zugriff für Admin UI Templates
http.ServeFile(w, r, "internal/ui/templates/dashboard.html")

// ✅ go:embed
//go:embed templates/* static/*
var adminFS embed.FS
tmpl := template.Must(template.ParseFS(adminFS, "templates/dashboard.html"))
```
```elixir
# ❌ Ed25519 Key für PII-Verschlüsselung verwenden
encrypted = :crypto.private_encrypt(ed25519_priv, pii_data, :rsa_pkcs1_padding)  # falsch + unmöglich

# ✅ X25519 Key für ECDH Key Agreement → AES-256-GCM Encryption
{shared_secret, _} = :crypto.compute_key(:ecdh, recipient_x25519_pub, sender_x25519_priv, :x25519)
# dann: AES-256-GCM mit derived key
```
```go
// ❌ Secret direkt als Env-Var
os.Getenv("NEBU_INTERNAL_SECRET")

// ✅ Secret aus gemounteter Datei
os.ReadFile(os.Getenv("NEBU_INTERNAL_SECRET_FILE"))
```

---

## Project Structure & Boundaries

### Complete Project Directory Structure

```
nebu/
│
├── .tool-versions                  ← Version-Pinning (asdf/mise): go, elixir, erlang, buf
├── .env.example                    ← alle NEBU_* Variablen dokumentiert
├── .gitignore
├── Makefile                        ← alle Dev-Kommandos, Build via Container
├── buf.yaml                        ← Proto Code-Generation Config
├── buf.gen.yaml                    ← Generator-Targets: Go + Elixir
├── README.md
├── CHANGELOG.md                    ← Pflicht bei jedem Release
│
├── .github/
│   └── workflows/
│       ├── ci-unit.yml             ← Go + Elixir Unit-Tests (schnell, immer)
│       └── ci-integration.yml      ← Docker Compose Stack + Godog (main/PR)
│
├── docker-compose.yml              ← Dev-Stack (Gateway, Core, Postgres, Keycloak)
├── docker-compose.prod.yml         ← Production-Stack
├── docker-compose.build.yml        ← Build-Container-Stack (kein lokales Go/Elixir nötig)
│
├── dev/
│   └── keycloak/
│       └── realm-export.json       ← Nebu Dev-Realm, auto-importiert beim Stack-Start
│
├── proto/
│   ├── core.proto                  ← CoreService: alle gRPC Definitionen
│   └── gen/
│       ├── go/                     ← generierte Go-Stubs (via buf in Build-Container)
│       │   ├── go.mod              ← separates Go-Modul: beide Binaries importieren es
│       │   ├── core.pb.go
│       │   └── core_grpc.pb.go
│       └── elixir/                 ← generierte Elixir-Stubs
│           └── core.pb.ex
│
├── gateway/                        ← Go API Gateway
│   ├── cmd/
│   │   └── gateway/
│   │       └── main.go             ← Startup: migrate → registry → HTTP
│   ├── internal/
│   │   ├── auth/
│   │   │   ├── oidc.go             ← FR1-6: Token-Validierung, user_id+role extraction
│   │   │   └── bootstrap.go        ← FR5-6: Bootstrap-Mode (erster Admin)
│   │   ├── matrix/
│   │   │   ├── login.go            ← POST /_matrix/client/v3/login
│   │   │   ├── logout.go           ← POST /_matrix/client/v3/logout
│   │   │   ├── sync.go             ← GET  /_matrix/client/v3/sync
│   │   │   ├── send.go             ← PUT  /rooms/{id}/send/...
│   │   │   ├── messages.go         ← GET  /rooms/{id}/messages
│   │   │   ├── rooms.go            ← POST /createRoom, POST /join/{id}
│   │   │   ├── typing.go           ← PUT  /rooms/{id}/typing/{userId}
│   │   │   ├── receipts.go         ← POST /rooms/{id}/receipt/...
│   │   │   ├── profile.go          ← GET/PUT /profile/{userId}
│   │   │   ├── presence.go         ← GET  /presence/{userId}/status
│   │   │   └── keys.go             ← POST /keys/upload, GET /keys/query
│   │   ├── admin/
│   │   │   ├── api.go              ← /api/v1/* Router
│   │   │   ├── users.go            ← FR36-38: User CRUD
│   │   │   ├── rooms.go            ← FR39-40: Room Management
│   │   │   ├── compliance.go       ← FR30-35: Four-Eyes, Audit-Log
│   │   │   └── metrics.go          ← FR44-45: /health, /ready, /metrics
│   │   ├── ui/
│   │   │   ├── templates/          ← Go-Templates für Admin UI (SSR)
│   │   │   │   ├── layout.html
│   │   │   │   ├── dashboard.html
│   │   │   │   ├── users.html
│   │   │   │   ├── rooms.html
│   │   │   │   ├── compliance.html
│   │   │   │   └── bootstrap.html  ← First-Run-Experience
│   │   │   └── static/
│   │   │       ├── app.js          ← Vue.js minimal (SSE Live-Metriken)
│   │   │       └── style.css
│   │   ├── grpc/
│   │   │   ├── client.go           ← gRPC CoreService Client
│   │   │   ├── stream.go           ← EventBus Stream + exponentielles Backoff
│   │   │   └── fallback.go         ← Unary-Polling Fallback (GELB-Status)
│   │   ├── registry/
│   │   │   └── registry.go         ← Elixir Node-Registry (HTTP /internal/nodes/*)
│   │   ├── buffer/
│   │   │   ├── buffer.go           ← message_buffer Write/Read
│   │   │   ├── drain.go            ← Drain-Worker + Strategy Interface
│   │   │   └── strategy/
│   │   │       ├── linear.go       ← MVP: konstante Rate
│   │   │       └── aimd.go         ← Phase 2: AIMD adaptiv
│   │   ├── middleware/
│   │   │   ├── auth.go             ← Token-Validierung Middleware
│   │   │   ├── logging.go          ← slog Request-Logging
│   │   │   └── cors.go
│   │   └── config/
│   │       └── config.go           ← alle NEBU_* Env-Vars strukturiert
│   ├── api/
│   │   └── openapi.json            ← Admin API Spec (go:embed, served at /api/v1/openapi.json)
│   ├── migrations/                 ← golang-migrate SQL-Dateien
│   │   ├── 001_initial_schema.up.sql
│   │   ├── 001_initial_schema.down.sql
│   │   ├── 002_server_config.up.sql    ← server_name + RLS
│   │   ├── 002_server_config.down.sql
│   │   ├── 003_message_buffer.up.sql
│   │   └── ...
│   ├── testdata/
│   │   ├── events/                 ← Fixture JSON für Unit-Tests
│   │   └── oidc/                   ← Mock OIDC Responses
│   ├── go.mod                      ← importiert proto/gen/go via replace
│   ├── go.sum
│   └── Dockerfile                  ← Multi-Stage: builder (Go) + runtime (distroless)
│
├── media/                          ← Go Media Gateway (Minimal MVP)
│   ├── cmd/
│   │   └── media/
│   │       └── main.go
│   ├── internal/
│   │   ├── upload/
│   │   │   └── handler.go          ← POST /_matrix/media/v3/upload
│   │   ├── download/
│   │   │   └── handler.go          ← GET  /_matrix/media/v3/download/{server}/{id}
│   │   ├── crypto/
│   │   │   └── aes.go              ← AES-256-GCM encrypt/decrypt
│   │   └── storage/
│   │       └── local.go            ← /var/nebu/media/{shard}/{id}.enc
│   ├── go.mod                      ← importiert proto/gen/go via replace
│   └── Dockerfile                  ← Multi-Stage: builder (Go) + runtime (distroless)
│
├── core/                           ← Elixir/OTP Umbrella
│   ├── apps/
│   │   ├── nebu_db/                ← geteilte DB-Infrastruktur (Ecto Repo)
│   │   │   ├── lib/nebu/
│   │   │   │   ├── repo.ex         ← Nebu.Repo (Ecto.Repo)
│   │   │   │   └── db_helpers.ex
│   │   │   └── mix.exs
│   │   ├── room_manager/           ← FR7-24: Horde + Room GenServer + Power-Level
│   │   │   ├── lib/nebu/room/
│   │   │   │   ├── manager.ex      ← Horde.DynamicSupervisor
│   │   │   │   ├── server.ex       ← Room GenServer
│   │   │   │   └── power_level.ex
│   │   │   ├── test/
│   │   │   └── mix.exs
│   │   ├── session_manager/        ← ETS + PostgreSQL Hybrid since-Token
│   │   │   ├── lib/nebu/session/
│   │   │   │   ├── manager.ex
│   │   │   │   └── token.ex        ← v1_<...> Format
│   │   │   ├── test/
│   │   │   └── mix.exs
│   │   ├── presence/               ← FR15: Presence-Status
│   │   │   ├── lib/nebu/presence/
│   │   │   ├── test/
│   │   │   └── mix.exs
│   │   ├── event_dispatcher/       ← EventBus gRPC Stream + pg Process Groups
│   │   │   ├── lib/nebu/event/
│   │   │   │   ├── dispatcher.ex
│   │   │   │   └── bus.ex
│   │   │   ├── test/
│   │   │   └── mix.exs
│   │   ├── signature/              ← FR25-29: Ed25519 + Canonical JSON + Event-ID
│   │   │   ├── lib/nebu/
│   │   │   │   ├── signature.ex
│   │   │   │   ├── event_id.ex     ← Nebu.EventId.generate/1
│   │   │   │   └── canonical_json.ex
│   │   │   ├── test/
│   │   │   └── mix.exs
│   │   └── permissions/            ← System-Rollen + Room-Policy
│   │       ├── lib/nebu/permissions/
│   │       │   ├── system_role.ex
│   │       │   └── room_policy.ex
│   │       ├── test/
│   │       └── mix.exs
│   ├── config/
│   │   ├── config.exs
│   │   ├── dev.exs
│   │   ├── prod.exs
│   │   └── runtime.exs             ← NEBU_* Env-Vars zur Laufzeit
│   ├── test/
│   │   ├── support/
│   │   │   └── factories.ex        ← ExMachina Factories
│   │   └── fixtures/
│   │       └── test_keys/          ← Ed25519 Testkeys (.gitignore)
│   ├── scripts/
│   │   └── gen_test_keys.sh        ← Test-Key-Generierung (via mix test.setup)
│   ├── mix.exs                     ← Umbrella Root
│   ├── mix.lock
│   └── Dockerfile                  ← Multi-Stage: builder (Elixir) + runtime (OTP release)
│
├── test/
│   ├── features/                   ← Gherkin Feature-Files
│   │   ├── auth.feature            ← FR1-6
│   │   ├── messaging.feature       ← FR7-16
│   │   ├── compliance.feature      ← FR30-35
│   │   ├── resilience.feature      ← message_buffer, GELB/ROT
│   │   └── performance.feature     ← Silber-Tier Lasttest-Szenarien
│   └── integration/                ← Godog Test-Runner
│       ├── go.mod
│       ├── main_test.go            ← Godog Bootstrap + Docker Compose Setup
│       └── steps/
│           ├── auth_steps.go
│           ├── messaging_steps.go
│           ├── compliance_steps.go
│           └── resilience_steps.go
│
├── certs/
│   ├── generate-dev-certs.sh
│   └── .gitignore
│
└── docs/
    ├── architecture/
    │   ├── SAD.md
    │   ├── data-model.md
    │   ├── grpc-contract.md
    │   └── adr/
    │       ├── 001-elixir-otp.md
    │       ├── 002-no-redis-nats.md
    │       ├── 003-content-hash-event-id.md
    │       ├── 004-horde-registry.md
    │       ├── 005-grpc-streaming.md
    │       ├── 006-message-buffer-drain.md
    │       ├── 007-ed25519-x25519-dual-keypair.md   ← V1: zwei Schlüsselpaare pro User
    │       ├── 008-node-registration-psk-mtls.md    ← V3: PSK MVP → Ephemeral mTLS Phase 2
    │       └── 009-openapi-spec-first.md             ← V6: oapi-codegen Spec-First
    └── stories/
        ├── epic-01-foundation.md
        ├── epic-02-matrix-core.md
        ├── epic-03-auth-oidc.md
        ├── epic-04-compliance.md
        ├── epic-05-resilience.md
        └── epic-06-media.md
```

---

### Build-Container-Strategie

**Kein lokales Go, Elixir oder buf nötig** — alles läuft in Docker:

```makefile
# Makefile — alle Kommandos via Build-Container

DOCKER_GO      = docker run --rm -v $(PWD):/workspace -w /workspace golang:1.26-alpine
DOCKER_ELIXIR  = docker run --rm -v $(PWD):/workspace -w /workspace elixir:1.19-alpine
DOCKER_BUF     = docker run --rm -v $(PWD):/workspace -w /workspace bufbuild/buf

# Proto Code-Generation
proto:
	$(DOCKER_BUF) generate

# Build
build-gateway:
	docker compose -f docker-compose.build.yml build gateway

build-core:
	docker compose -f docker-compose.build.yml build core

build:
	docker compose -f docker-compose.build.yml build

# Tests
test-unit-go:
	$(DOCKER_GO) sh -c "cd gateway && go test ./..."

test-unit-elixir:
	$(DOCKER_ELIXIR) sh -c "cd core && mix test"

test-unit: test-unit-go test-unit-elixir

test-integration:
	docker compose up -d
	$(DOCKER_GO) sh -c "cd test/integration && go test ./..."
	docker compose down

# Dev
dev:
	docker compose up

migrate:
	docker compose run --rm gateway ./gateway --migrate-only

# Setup
setup:
	cp .env.example .env
	docker compose run --rm core mix test.setup
```

**Multi-Stage Dockerfiles** (Muster für alle drei Binaries):

```dockerfile
# gateway/Dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /gateway ./cmd/gateway

FROM gcr.io/distroless/static AS runtime
COPY --from=builder /gateway /gateway
ENTRYPOINT ["/gateway"]
```

```dockerfile
# core/Dockerfile
FROM elixir:1.19-alpine AS builder
WORKDIR /app
RUN mix local.hex --force && mix local.rebar --force
COPY mix.exs mix.lock ./
COPY apps/*/mix.exs ./apps/
RUN mix deps.get --only prod
COPY . .
RUN MIX_ENV=prod mix release

FROM alpine:3.19 AS runtime
RUN apk add --no-cache libstdc++ openssl ncurses
COPY --from=builder /app/_build/prod/rel/nebu ./
ENTRYPOINT ["./bin/nebu", "start"]
```

---

### Architektur-Grenzen

```
Externe Grenze:
  Internet → [TLS 1.3] → Go Gateway     (Port 443/8443)
  Internet → [TLS 1.3] → Go Media GW    (Port 8448)

Interne Grenze (Docker-Netz, nicht exposed):
  Go Gateway  → [gRPC]          → Elixir Core  (Port 9000)
  Go Media GW → [gRPC]          → Elixir Core  (Port 9000)
  Go Gateway  → [HTTP intern]   → /internal/nodes/*  (Node-Registry)

Daten-Grenze:
  Elixir Core → [TLS] → PostgreSQL  (Port 5432) — Business-Logic-Writes
  Go Gateway  → [TLS] → PostgreSQL  (Port 5432) — Migrations + message_buffer
  Go Media    → [TLS] → PostgreSQL  (Port 5432) — Media-Keys

Schema-Ownership:
  Go Gateway: alleiniger Schema-Owner via golang-migrate
  Elixir: kein Schema-Write-Zugriff
```

### Requirements → Verzeichnis Mapping

| FR-Gruppe | Primärer Ort |
|---|---|
| FR1–6 (Auth + Bootstrap) | `gateway/internal/auth/` |
| FR7–16 (Messaging + Rooms) | `gateway/internal/matrix/` + `core/apps/room_manager/` |
| FR17–24 (Room-Config) | `core/apps/room_manager/` + `core/apps/permissions/` |
| FR25–29 (Ed25519 + PII) | `core/apps/signature/` + `gateway/migrations/` (Ed25519 signing + X25519 encryption, separate Schlüsselpaare) |
| FR30–35 (Compliance + Audit) | `gateway/internal/admin/compliance.go` + `core/apps/permissions/` |
| FR36–40 (Admin CRUD) | `gateway/internal/admin/` |
| FR41–42 (Notifications) | `core/apps/event_dispatcher/` |
| FR43–47 (Ops + TLS) | `gateway/internal/admin/metrics.go` + `docker-compose.yml` |
| FR48–52 (Admin UI + API + OpenAPI) | `gateway/internal/ui/` + `gateway/api/openapi.json` |
