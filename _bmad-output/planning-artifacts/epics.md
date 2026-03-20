---
stepsCompleted: ['step-01-validate-prerequisites', 'step-02-design-epics', 'step-03-epic-1', 'step-03-epic-2', 'step-03-epic-3', 'step-03-epic-4', 'step-03-epic-5', 'step-03-epic-6', 'step-03-epic-7']
inputDocuments:
  - '_bmad-output/planning-artifacts/prd.md'
  - '_bmad-output/planning-artifacts/architecture.md'
  - '_bmad-output/planning-artifacts/ux-design-specification.md'
---

# open-chat - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for open-chat (Nebu), decomposing the requirements from the PRD, Architecture, and UX Design Specification into implementable stories.

## Requirements Inventory

### Functional Requirements

FR1: End-User kann sich via SSO mit jedem OIDC-konformen Identity Provider anmelden
FR2: End-User kann sich abmelden und seine Session ungültig machen
FR3: Instance Admin kann OIDC-Provider-Konfiguration verwalten (Issuer, Client-ID, Claim-Mappings)
FR4: System weist Rollen (`instance_admin`, `compliance_officer`) anhand konfigurierbarer OIDC-Claim-Mappings zu
FR5: System weist beim ersten OIDC-Login automatisch `instance_admin` zu, wenn noch kein Admin existiert (Bootstrap Mode)
FR6: System deaktiviert Bootstrap Mode permanent nach erstem Admin-Setup
FR7: End-User kann Rooms erstellen
FR8: End-User kann einem Room per Room-ID oder Alias beitreten
FR9: End-User kann Nachrichten in einem Room senden
FR10: End-User kann den Nachrichtenverlauf eines Rooms abrufen
FR11: End-User kann ältere Nachrichten paginiert nachladen
FR12: End-User kann Tipp-Indikatoren setzen und empfangen
FR13: End-User kann Read Receipts senden
FR14: End-User kann sein Profil (Anzeigename, Avatar) anzeigen und aktualisieren
FR15: End-User kann den Präsenz-Status anderer User einsehen
FR16: End-User kann mit Standard-Matrix-Clients (Element, FluffyChat u.a.) auf Nebu zugreifen
FR17: Room Owner kann Sichtbarkeit des Rooms definieren (öffentlich, privat, einladungsbasiert)
FR18: Room Owner kann Room-Metadaten pflegen (Name, Beschreibung, Topic, Avatar)
FR19: Room Owner kann Zugriffskontrolle konfigurieren (wer darf beitreten, lesen, schreiben)
FR20: End-User kann andere User in einen Room einladen
FR21: End-User kann eine Room-Einladung annehmen oder ablehnen
FR22: Instance Admin kann alle Room-Owner-Einstellungen für jeden Room auf der Instanz verwalten
FR23: Instance Admin kann eine maximale Mitgliederzahl pro Room konfigurieren
FR24: Instance Admin kann serverweite Standard-Room-Einstellungen festlegen (Default für neue Rooms)
FR25: System generiert pro User-Identität ein Ed25519-Schlüsselpaar (Signing) und ein X25519-Schlüsselpaar (Encryption)
FR26: System signiert ausgehende Matrix-Events mit dem privaten Ed25519-Schlüssel des Users
FR27: System verschlüsselt Sensitive PII (E-Mail, IdP-Subject) mit dem öffentlichen X25519-Schlüssel
FR28: System kann private Schlüssel eines Users löschen und Sensitive PII kryptografisch unlesbar machen (Right to be Forgotten)
FR29: System anonymisiert Operational PII (Anzeigename) bei Kontolöschung zu "Deleted User"
FR30: Compliance Officer kann temporären Zugriff auf Nachrichteninhalte mit dokumentierter Begründung beantragen
FR31: System erzwingt Vier-Augen-Prinzip für Compliance-Zugriffsanträge
FR32: System begrenzt Compliance-Zugriffssessions auf maximal 24 Stunden
FR33: Compliance Officer kann Nachrichtendaten mit Ed25519-signiertem Audit-Trail exportieren
FR34: System führt Append-Only Audit Log aller Compliance-Zugriffsevents
FR35: System protokolliert alle administrativen Aktionen im Audit Log
FR36: Instance Admin kann alle User einer Instanz auflisten
FR37: Instance Admin kann User-Accounts anlegen und deaktivieren
FR38: Instance Admin kann User-Attribute aktualisieren
FR39: Instance Admin kann alle Rooms einer Instanz auflisten
FR40: Instance Admin kann Room-Mitgliedschaft und -Einstellungen verwalten
FR41: System benachrichtigt User über neue Nachrichten gemäß konfigurierbarer Push-Regeln (Matrix-native)
FR42: End-User kann Push-Benachrichtigungsregeln pro Room und Event-Typ konfigurieren
FR43: Operator kann Nebu mittels Docker Compose deployen und betreiben
FR44: System exponiert Health- und Readiness-Endpunkte für Monitoring-Integrationen
FR45: System exponiert Metrics-Endpunkt kompatibel mit Standard-Monitoring (Prometheus)
FR46: System unterstützt horizontale Skalierung des Go-Gateways ohne Session-Affinity
FR47: System unterstützt TLS-Terminierung (mTLS optional konfigurierbar)
FR48: Instance Admin kann alle Verwaltungsfunktionen über eine web-basierte Admin UI bedienen
FR49: Instance Admin kann Live-Server-Metriken (Durchsatz, aktive Sessions, Node-Health) in der Admin UI einsehen
FR50: Alle Admin-UI-Zustände sind über URLs adressierbar (bookmarkbar, teilbar)
FR51: Developer/Operator kann alle Admin-Funktionen programmatisch über eine REST-API nutzen
FR52: System stellt OpenAPI-Spezifikation der Admin API bereit

### NonFunctional Requirements

NFR-P1: Nachrichtenversand (End-to-End via Matrix API) ≤ 500ms Latenz unter Silber-Last (500 concurrent users auf m5.large)
NFR-P2: Matrix `/sync`-Endpunkt antwortet ≤ 1s unter Normallast
NFR-P3: System erreicht Silber-Tier (>500 concurrent/m5.large) ohne Redis/NATS/Kafka
NFR-P4: Gateway-Kaltstart-Zeit ≤ 5s (stateless, schneller Neustart)
NFR-S1: Alle externen Verbindungen via TLS 1.2 minimum (TLS 1.3 bevorzugt)
NFR-S2: Sensitive PII ist at-rest verschlüsselt (X25519-Keypair); Operational PII ist at-rest verschlüsselt
NFR-S3: Audit Log ist append-only und Ed25519-signiert — Manipulation nachweisbar
NFR-S4: OIDC-Token-Validierung bei jedem API-Request — kein Session-State im Gateway
NFR-S5: Ed25519/X25519-Schlüssellöschung ist irreversibel — kein Recovery-Pfad by design (DSGVO-Konformität)
NFR-S6: Bootstrap Mode deaktiviert sich permanent und unwiderruflich nach erstem Admin-Setup
NFR-SC1: Go Gateway horizontal skalierbar ohne Session-Affinity (beliebig viele Instanzen hinter Load Balancer)
NFR-SC2: Elixir/OTP Core unterstützt Cluster-Betrieb (Phase 2: automatisch via libcluster)
NFR-SC3: Kein externer Middleware-Layer erforderlich — PostgreSQL ist einziger Persistenz-Layer
NFR-R1: Elixir/OTP Process-Isolation: Absturz eines Room-Prozesses betrifft keine anderen Rooms oder Sessions
NFR-R2: Kein Datenverlust bei Gateway-Neustart — PostgreSQL ist Single Source of Truth
NFR-R3: Rolling Updates ohne vollständige Downtime möglich (Stateless Gateway ermöglicht zero-downtime Deploys)
NFR-O1: Vollständiges Deployment via `docker-compose up` auf einer frischen Instanz in ≤ 10 Minuten
NFR-O2: Health/Readiness-Endpunkte antworten ≤ 200ms auch unter Last
NFR-O3: Admin UI vollständig im Gateway-Binary integriert — keine externen Abhängigkeiten zur Laufzeit
NFR-O4: Alle Admin-UI-Zustände via URL reproduzierbar — kein Browser-State, kein LocalStorage-Zwang
NFR-C1: Right to be Forgotten implementiert durch kryptografische Schlüssellöschung — DSGVO-konform ohne Bruch der Audit-Log-Integrität
NFR-C2: Audit-Log-Aufbewahrungsdauer konfigurierbar (Default: 7 Jahre)
NFR-C3: Alle Daten liegen ausschließlich in der konfigurierten PostgreSQL-Instanz — kein Cloud-Service erforderlich
NFR-M1: Matrix Client-Server API kompatibel mit Element, FluffyChat, Hydrogen — Inkompatibilitäten gelten als Bugs
NFR-M2: OIDC-Integration via `m.login.sso` gemäß Matrix OIDC Specification
NFR-A1: Admin UI erfüllt WCAG 2.1 Level AA
NFR-A2: Admin UI ist vollständig per Tastatur navigierbar
NFR-A3: Admin UI ist mit gängigen Screen Readern nutzbar (semantisches HTML, ARIA-Labels)
NFR-L1: Admin UI unterstützt Deutsch und Englisch; Sprache aus OIDC-Claim oder User-Profil
NFR-I1: Ausschließlich offene Standards (Matrix, OIDC, OpenAPI, Prometheus) — keine proprietären Protokolle
NFR-I2: Matrix-Events haben konfigurierbare maximale Payload-Größe (Default: 65KB)
NFR-CR1: Kryptografische Primitive intern modularisiert und austauschbar (Crypto-Agilität)

### Additional Requirements

- **Project Scaffolding:** Manuelles Scaffolding (kein Starter-Template) — Go Modules + Elixir Umbrella (`mix new core --umbrella` mit 6 Apps: room_manager, session_manager, presence, event_dispatcher, signature, permissions)
- **Migrations:** golang-migrate (`github.com/golang-migrate/migrate/v4`) — SQL-Dateien in `gateway/migrations/`, synchron beim Go-Start vor HTTP-Listener; Go ist alleiniger Schema-Owner
- **message_buffer:** Tabellen `message_buffer` + `message_dead_letter` in PostgreSQL für Gateway-Resilienz bei Core-Ausfall
- **Drain-Strategy Pattern:** Pluggbares Strategy Interface (DrainStrategy) — MVP: Linear (100 msg/s Default), Phase 2: AIMD basierend auf `load_factor`; Retry-Limit 3, Dead-Letter nach N Retries
- **Node Security (MVP):** Pre-Shared Secret via Docker Compose Secrets (`make setup` generiert `.secrets/internal_secret`); Go validiert `Authorization: Bearer <secret>` auf `/internal/*`
- **Content-Hash Event-ID:** Format `$<base64url(SHA-256(canonical_json(event)))>` — Matrix Room Version 6+; implementiert in `Nebu.EventId` Elixir-Modul
- **server_name Immutabilität:** PostgreSQL RLS verhindert UPDATE auf `server_config` Tabelle technisch; Admin-UI zeigt schreibgeschützte Anzeige mit Warnung
- **Zwei Schlüsselpaare pro User:** Ed25519 (Signing, OTP `:crypto.sign/4 :eddsa`) + X25519 (Encryption via ECDH → AES-256-GCM, OTP `:crypto.generate_key(:ecdh, :x25519)`); beide native in OTP 24+
- **Kryptografische Deletion:** PostgreSQL-Transaktion: DELETE signing_private_key + DELETE encryption_private_key + UPDATE sensitive_pii_marker atomar; Audit-Log-Eintrag auch bei Fehler
- **Sync API:** Hybrid ETS + PostgreSQL; since-Token-Format `v1_<base64url(server_ts_ms + cursor_map)>`; Cold-Sync bei fehlendem Token
- **gRPC:** Server-Streaming EventBus + Unary Fallback; GRÜN/GELB/ROT Status direkt aus gRPC-Verbindungsstatus; ein EventBus-Stream pro Go-Instanz
- **Horde:** `Horde.Registry` + `Horde.DynamicSupervisor`, `members: :auto` via libcluster — Single-Node + Cluster ohne Code-Änderung
- **Media Gateway (Minimal MVP):** `POST /_matrix/media/v3/upload` + `GET /_matrix/media/v3/download/{server}/{id}`; AES-256-GCM per File; lokales Filesystem `/var/nebu/media/{shard}/{id}.enc`; `media_keys` Tabelle in PostgreSQL
- **CI/CD:** Unit Tests (Go `go test`, Elixir `mix test`) + Docker Compose Stack + Gherkin Integration Tests (separater CI-Job auf main/PR); Gherkin ist primäres Quality Gate
- **OpenAPI Spec-First:** `gateway/api/openapi.yaml` ist Single Source of Truth; `make gen-api` via oapi-codegen erzeugt `api_gen.go` mit Typen + ServerInterface
- **Naming Conventions:** PostgreSQL snake_case plural · Go PascalCase exports + snake_case JSON · Elixir `Nebu.{Domain}.{Name}` · Proto PascalCase Messages
- **Timestamps:** PostgreSQL BIGINT ms · Proto int64 ms · Matrix API int64 ms · Admin API ISO 8601 String
- **Auth Token Flow:** Go validiert OIDC-Token, übergibt nur `x-user-id` + `x-system-role` als gRPC-Metadata an Elixir; Elixir validiert nichts selbst
- **Dex Dev Setup:** `dev/dex/config.yaml` mit 3 Test-Usern (kai@example.com → instance_admin, compliance@example.com → compliance_officer, alex@example.com → user); alle mit Passwort `changeme`; Dex läuft auf Port 5556
- **Health/Ready Response Format:** Detailliertes JSON-Format für Go (database, core_grpc mit nebu_status GRÜN/GELB/ROT, migrations) und Elixir (load_factor, components)
- **Silver Tier Load Test:** Traffic-Mix: 60% GET /sync, 20% PUT send, 10% presence+typing, 5% createRoom/join, 5% sonstige; 10 Rooms × 50 Members = 500 concurrent; 2× m5.large

### UX Design Requirements

UX-DR1: Dark-first color system "Obsidian" als CSS Custom Properties implementiert (7 Basis-Tokens, 4 Primary/Action-Tokens, 6 semantische Status-Tokens) — niemals hardcodierte Hex-Werte in Components
UX-DR2: Typografie-System — Inter Variable Font (Body) + JetBrains Mono (technische Strings: Room-IDs, Event-IDs, Fingerprints); beide als self-hosted WOFF2 im Gateway-Binary eingebettet
UX-DR3: Design System Build — Tailwind CSS + DaisyUI; Tailwind Standalone CLI als Build-Step (kein Node.js Runtime); DaisyUI Custom Theme via CSS Custom Properties
UX-DR4: C1 StatusCard — Ampel-Card für Systemkomponenten (ok/warn/error/loading), top-border 3px farbig, SSE-getrieben; `role="status"`, `aria-live="polite"`; Normal + Compact Variant
UX-DR5: C2 AlertItem — Dashboard-Alert mit Titel, einzeiliger Erklärung, Direktlink zur Problemstelle; warn/error severity; `role="alert"` (error) / `role="status"` (warn)
UX-DR6: C3 TopbarStatusIndicator — Kompakter Status-Pill im Topbar auf allen Seiten (bei ok: nur grüner Dot; bei warn/error: Dot + Kurztext); SSE-getrieben; `aria-live="polite"`
UX-DR7: C4 MasterDetailLayout — Dreispaltiges CSS-Grid (300px fix Master + fluid Detail) für alle Verwaltungsseiten (Users, Rooms, Roles, Compliance-Liste); `role="navigation"` + `role="region"`
UX-DR8: C5 MasterListItem — Listeneintrag mit aktivem Left-Border-Indikator (3px primary), Skeleton-State; `role="option"`, `aria-selected`
UX-DR9: C6 WizardCard — Wizard-Schritt-Container: Frage (h2) + Hint + C14 FormField + Mini-Zusammenfassung bisheriger Angaben + Vor/Zurück; Loading + Error States; `aria-describedby`
UX-DR10: C6b BootstrapWizardCard — Erweiterung von C6 mit eingebettetem OIDC-Verbindungstest-Block; zwei Fehlerpfade (config-error vs. network-error); `aria-live="assertive"` bei Fehler
UX-DR11: C7 DraftHint — Persistenter Hinweis "Zwischenstand gespeichert · URL kann geteilt werden"; saved + saving (animierter Dot); `aria-live="polite"`
UX-DR12: C8 EmptyState — Zwei Variants: `none-selected` ("Wähle einen Eintrag") + `no-results` ("Kein Ergebnis für '…'" + Reset-Link); `role="status"`
UX-DR13: C9 ConfirmDialog — Tiered Bestätigung (Low: Text+OK/Abbrechen · Medium: Warnung+Konsequenz+OK/Abbrechen · High: Ressourcenname eintippen); `role="dialog"`, `aria-modal="true"`, Focus-Trap, Escape schließt, Auto-Focus auf Abbrechen
UX-DR14: C10 Toast — SSR-kompatibler Flash-Cookie-Mechanismus; Server setzt Cookie, Go-Template rendert Toast einmalig, Vue.js blendet nach 3s aus; Auto-close nur success/warn; `role="status"` / `role="alert"`
UX-DR15: C11 PageHeader — Reusable Partial: Seitentitel (h1) + Subtitel + optionaler Aktions-Button rechts
UX-DR16: C12 InlineEdit — Klick-to-Edit im Detailpanel; Display → Editing → Saving → Error States; Enter speichert, Escape bricht ab; `aria-label="[Feld] bearbeiten"`
UX-DR17: C13 ExportDownload — Compliance-Export-Download; nach Download: Fingerprint (Ed25519, SHA256) + `nebu verify --file export.pdf` CLI-Hinweis; `aria-describedby` auf Fingerprint-Block
UX-DR18: C14 FormField — Reusable Wrapper: Label + Input/Textarea/Select + Hint + Validierungsfehler; `<label for>` verknüpft; `aria-invalid` + `aria-errormessage` bei Fehler
UX-DR19: Skeleton-States — alle async-Komponenten (C1, C2, C4, C5, Detail-Panel) haben animierten Platzhalter via Tailwind `animate-pulse` auf `bg-raised`-Blöcken
UX-DR20: URL-Konvention vollständig implementiert — alle 14 definierten Routes (`/admin/`, `/admin/users/`, `/admin/users/{id}`, `/admin/rooms/`, `/admin/rooms/{id}`, `/admin/roles/`, `/admin/compliance/`, `/admin/compliance/new/step-{n}`, `/admin/compliance/{id}`, `/admin/audit/`, `/admin/config/`, `/admin/crypto/`, `/admin/bootstrap/`)
UX-DR21: Bootstrap-Flow (4-Step Forge-Wizard): Schritt 1 Instanzname+URL → Schritt 2 OIDC-Konfiguration+Verbindungstest → Schritt 3 Ed25519-Schlüsselpaar automatisch generiert+Fingerprint → Schritt 4 Erster Admin-Account via OIDC-Login → Token invalidiert → Dashboard
UX-DR22: Compliance-Wizard (4-Step Forge-Wizard): Schritt 1 Zeitraum → Schritt 2 Betroffene Person → Schritt 3 Begründung (1-2 Sätze) → Schritt 4 Zusammenfassung → POST /api/v1/compliance/draft/submit; jeder Schritt eigene URL + Server-Side Draft
UX-DR23: Sentinel Dashboard View — 4 StatusCards (Gateway, Chat-Server, Datenbank, Nachrichtenzustellung) above the fold; AlertItem-Liste für aktive Probleme; Live-Metriken sekundär; Vue.js SSE für C1+C3
UX-DR24: WCAG 2.1 AA vollständig — semantisches HTML; ARIA-Attribute in allen 14 Custom Components; Focus-Indikatoren (`ring-2 ring-primary ring-offset-2`); aria-live regions für Status-Updates
UX-DR25: Reduced-Motion-Support — alle Animationen respektieren `prefers-reduced-motion: reduce`; OS High Contrast via `forced-colors: active` Media Query
UX-DR26: Sidebar-Navigation — 240px, kollabierbar auf 64px Icon-Mode; 8 Nav-Sections: Dashboard, Benutzer, Räume, Rollen & Rechte, Compliance, Audit Log, Konfiguration, Kryptografie; Badge-Hints für ausstehende Aktionen
UX-DR27: Button-Hierarchie — Primary/Secondary/Danger/Ghost; max. ein Primary pro View; Danger öffnet immer ConfirmDialog; Async-Aktionen: Loading-Spinner; Enter löst fokussierten Button aus, Escape schließt Modals
UX-DR28: Feedback-Patterns — AlertItem (persistent, Systemzustand), Toast (transient, 3s), FormField inline (Validierung); Fehlermeldungen immer dreiteilig: "Was · Warum · Was jetzt"; keine Stack-Traces
UX-DR29: Form-Patterns — alle Inputs via C14 FormField; Validierung onBlur; Wizard auto-save Draft; Konfigurationsseite: expliziter "Änderungen speichern"-Button; Submit ohne Pflichtfelder: alle leeren Felder gleichzeitig markieren
UX-DR30: Search & Filter — Echtzeit-Filter debounced 200ms (Vue.js) in Master-Listen; Audit Log: Zeitraum + Ereignistyp + Nutzer-Freitext kombinierbar; Pagination via "Load More"-Button (SSR-kompatibel, kein Infinite Scroll)
UX-DR31: Instanzname + Umgebung prominent im Topbar (verhindert Staging/Prod-Verwechslungen); optionales Instanz-Branding (Phase 2 via DaisyUI CSS Custom Properties Theming)

### FR Coverage Map

FR1: Epic 2 — OIDC-Login via m.login.sso
FR2: Epic 2 — Logout + Session invalidation
FR3: Epic 2 — OIDC-Provider-Konfiguration (Issuer, Client-ID, Claim-Mappings)
FR4: Epic 2 — Rollen-Zuweisung via OIDC-Claims (instance_admin, compliance_officer)
FR5: Epic 2 (Backend-Logik) + Epic 3 (Bootstrap-UI) — Erster OIDC-Login → auto instance_admin
FR6: Epic 2 (Backend-Logik) + Epic 3 (Bootstrap-UI) — Bootstrap Mode permanent deaktivieren
FR7: Epic 4 — Room erstellen
FR8: Epic 4 — Room per ID oder Alias beitreten
FR9: Epic 4 — Nachrichten senden
FR10: Epic 4 — Nachrichtenverlauf abrufen
FR11: Epic 4 — Paginiertes Nachladen älterer Nachrichten
FR12: Epic 4 — Typing Indicators senden + empfangen
FR13: Epic 4 — Read Receipts senden
FR14: Epic 4 — Profil anzeigen + aktualisieren
FR15: Epic 4 — Präsenz-Status anderer User einsehen
FR16: Epic 4 — Standard-Matrix-Client-Kompatibilität (Element, FluffyChat)
FR17: Epic 4 — Room-Sichtbarkeit (öffentlich, privat, einladungsbasiert)
FR18: Epic 4 — Room-Metadaten (Name, Beschreibung, Topic, Avatar)
FR19: Epic 4 — Room-Zugriffskontrolle konfigurieren
FR20: Epic 4 — User in Room einladen
FR21: Epic 4 — Room-Einladung annehmen oder ablehnen
FR22: Epic 6 (API) + Epic 7 (UI) — Instance Admin überschreibt Room-Einstellungen
FR23: Epic 6 (API) + Epic 7 (UI) — Maximale Mitgliederzahl pro Room
FR24: Epic 6 (API) + Epic 7 (UI) — Serverweite Standard-Room-Einstellungen
FR25: Epic 2 — Ed25519 + X25519 Schlüsselpaar-Generierung pro User
FR26: Epic 4 — Matrix-Events mit Ed25519 signieren
FR27: Epic 2 — Sensitive PII mit X25519 verschlüsseln (bei User-Registrierung)
FR28: Epic 5 — Private Keys löschen + DSGVO-Deletion (Right to be Forgotten)
FR29: Epic 5 — Operational PII anonymisieren bei Kontolöschung
FR30: Epic 5 — Compliance-Zugriff beantragen mit Begründung
FR31: Epic 5 — Vier-Augen-Prinzip erzwingen
FR32: Epic 5 — Compliance-Session auf 24h begrenzen
FR33: Epic 5 — Ed25519-signierter Export
FR34: Epic 5 — Append-Only Audit Log für Compliance-Events
FR35: Epic 5 — Audit Log für alle Admin-Aktionen
FR36: Epic 6 (API) + Epic 7 (UI) — User-Liste abrufen
FR37: Epic 6 (API) + Epic 7 (UI) — User anlegen + deaktivieren
FR38: Epic 6 (API) + Epic 7 (UI) — User-Attribute aktualisieren
FR39: Epic 6 (API) + Epic 7 (UI) — Room-Liste abrufen
FR40: Epic 6 (API) + Epic 7 (UI) — Room-Mitgliedschaft + Einstellungen verwalten
FR41: Epic 4 — Matrix-native Push-Regeln (Apple/Google Push: Phase 2)
FR42: Epic 4 — Push-Benachrichtigungsregeln pro Room + Event-Typ konfigurieren
FR43: Epic 1 — Docker Compose Deployment
FR44: Epic 1 — Health + Readiness Endpoints
FR45: Epic 1 — Prometheus Metrics Endpoint
FR46: Epic 1 — Horizontale Skalierung (stateless Gateway, keine Session-Affinity)
FR47: Epic 1 — TLS-Terminierung (mTLS optional)
FR48: Epic 3 (Minimal) + Epic 7 (Vollständig) — Web-basierte Admin UI
FR49: Epic 3 (Minimal Dashboard) + Epic 7 (Vollständig) — Live-Server-Metriken in Admin UI
FR50: Epic 3 (Basis) + Epic 7 (Vollständig) — URL-adressierbare UI-Zustände
FR51: Epic 6 — Admin REST-API
FR52: Epic 6 — OpenAPI-Spezifikation der Admin API

## Epic List

## Epic 1: Operators Can Deploy and Observe a Running Nebu Instance

Ein Operator kann `docker compose up` ausführen und hat innerhalb von 10 Minuten einen laufenden, beobachtbaren Nebu-Server mit Health-Checks, Prometheus-Metriken und einer grünen CI-Pipeline.
**FRs covered:** FR43, FR44, FR45, FR46, FR47
**Zusätzliche Deliverables:** Projekt-Scaffolding (Go Modules + Elixir Umbrella mit 6 Apps), golang-migrate Setup, gRPC Proto-Definition (`core.proto`) + Codegen (Go + Elixir), message_buffer + message_dead_letter Tabellen, PSK Node-Security (Docker Compose Secrets), CI-Pipeline (Unit Tests Go+Elixir + Docker Compose Gherkin Integration Test Skeleton)

### Story 1.1: Go Gateway & Media Gateway — Repository Scaffolding

As a developer,
I want the Go repository structure initialized with modules and a Makefile skeleton,
So that all subsequent stories have a consistent, buildable foundation.

**Acceptance Criteria:**

**Given** a fresh repository,
**When** `go mod init github.com/nebu/nebu` is run,
**Then** `go.mod` exists with the correct module path `github.com/nebu/nebu`

**Given** the repository is initialized,
**When** the directory structure is created,
**Then** the following directories exist: `gateway/cmd/gateway/`, `gateway/internal/{auth,matrix,middleware,grpc,registry,buffer,admin}/`, `gateway/migrations/`, `gateway/api/`, `media/cmd/media/`, `media/internal/{upload,download,crypto,storage}/`, `proto/`

**Given** the scaffold is complete,
**When** `gateway/cmd/gateway/main.go` exists with a placeholder,
**Then** `go build ./...` returns exit code 0

**Given** the Makefile exists in the project root,
**When** placeholder targets are defined (`build-gateway`, `build-core`, `dev`, `setup`, `test-unit-go`, `test-unit-elixir`, `test-integration`, `proto`, `gen-api`),
**Then** running `make -n <target>` on any defined target does not error

**Given** the gateway `main.go` placeholder,
**When** it starts,
**Then** it logs `"Nebu Gateway starting"` and exits cleanly with code 0

**Given** NEBU_ environment variable prefix convention,
**When** gateway configuration code is inspected,
**Then** `NEBU_CORE_GRPC_ADDR`, `NEBU_DB_URL`, `NEBU_OIDC_ISSUER`, `NEBU_INTERNAL_SECRET_FILE`, `NEBU_SERVER_NAME` are defined as named constants or config struct fields

---

### Story 1.2: Elixir/OTP Core Umbrella — Repository Scaffolding

As a developer,
I want the Elixir/OTP umbrella project initialized with all 6 sub-applications,
So that Elixir development can begin with a correct supervision tree structure.

**Acceptance Criteria:**

**Given** a fresh `core/` directory,
**When** `mix new core --umbrella` is run,
**Then** `core/mix.exs` exists as an umbrella project configuration

**Given** the umbrella exists,
**When** 6 apps are created with `--sup` flag (`room_manager`, `session_manager`, `presence`, `event_dispatcher`, `signature`, `permissions`),
**Then** each app directory exists under `core/apps/` with its own `mix.exs` and supervision tree

**Given** all 6 apps exist,
**When** `mix compile` is run from `core/`,
**Then** compilation succeeds with 0 errors and 0 warnings

**Given** each app,
**When** its `application.ex` is inspected,
**Then** it defines a `Supervisor` as the root process with `strategy: :one_for_one`

**Given** all apps,
**When** `mix test` is run from `core/`,
**Then** all placeholder test suites pass (0 failures)

**Given** Elixir modules across all apps,
**When** module naming is verified,
**Then** modules follow the `Nebu.{Domain}.{Name}` pattern (e.g., `Nebu.Room.Manager`, `Nebu.Session.Manager`, `Nebu.Signature`)

---

### Story 1.3: golang-migrate Setup + PostgreSQL Docker Service

As an operator,
I want database migrations to run automatically when the gateway starts,
So that the database schema is always in sync with the application without manual intervention.

**Acceptance Criteria:**

**Given** `github.com/golang-migrate/migrate/v4` is added to `go.mod`,
**When** `go build ./...` runs,
**Then** it compiles successfully with no import errors

**Given** a `docker-compose.yml` in the project root,
**When** a `postgres` service is defined with a healthcheck,
**Then** `docker compose up postgres` starts PostgreSQL on port 5432 with `POSTGRES_DB=nebu`, `POSTGRES_USER=nebu`, and `POSTGRES_PASSWORD` from environment or secret

**Given** the gateway starts with a reachable `NEBU_DB_URL`,
**When** the application initializes,
**Then** migrations run synchronously before the HTTP listener binds to its port

**Given** a `gateway/migrations/000001_init.up.sql` file,
**When** it runs,
**Then** the `schema_migrations` tracking table is created and migration version 1 is recorded

**Given** `GET /ready` is called after startup,
**When** migrations have run successfully,
**Then** response includes `"migrations": {"status": "UP", "version": 1}`

**Given** an already-migrated database,
**When** the gateway restarts and migrations run again,
**Then** they are idempotent — no error, no duplicate application

**Given** an unreachable database at startup,
**When** the gateway tries to connect,
**Then** it logs `"database connection failed: <error>"` and exits with a non-zero code (no panic, no nil-pointer crash)

---

### Story 1.4: message_buffer + message_dead_letter Schema Migration

As a developer,
I want the message buffer tables created via migration,
So that Epic 4's gateway resilience implementation has its schema ready without requiring further migrations.

**Acceptance Criteria:**

**Given** migration `000002_message_buffer.up.sql`,
**When** it runs,
**Then** `message_buffer` table exists with columns: `id BIGSERIAL PRIMARY KEY`, `txn_id TEXT NOT NULL`, `room_id TEXT NOT NULL`, `sender TEXT NOT NULL`, `payload JSONB NOT NULL`, `received_at BIGINT NOT NULL`, `status TEXT NOT NULL DEFAULT 'pending'`, `retry_count SMALLINT NOT NULL DEFAULT 0`, `processed_at BIGINT`

**Given** the same migration,
**When** it runs,
**Then** `message_dead_letter` table exists with columns: `id BIGSERIAL PRIMARY KEY`, `buffer_id BIGINT NOT NULL`, `txn_id TEXT NOT NULL`, `payload JSONB NOT NULL`, `failed_at BIGINT NOT NULL`, `last_error TEXT`

**Given** the `status` column on `message_buffer`,
**When** an INSERT with a value other than `'pending'` or `'held'` is attempted,
**Then** the database rejects it with a CHECK constraint violation

**Given** a corresponding `000002_message_buffer.down.sql`,
**When** it runs,
**Then** both tables are dropped cleanly (rollback support)

---

### Story 1.5: server_config Table + PostgreSQL RLS Policy

As an operator,
I want the server_name to be permanently immutable once set,
So that all Matrix IDs derived from it (`@user:server.name`, `!room:server.name`) remain consistent throughout the deployment lifetime.

**Acceptance Criteria:**

**Given** migration `000003_server_config.up.sql`,
**When** it runs,
**Then** `server_config` table exists with columns: `key TEXT PRIMARY KEY`, `value TEXT NOT NULL`, `set_at BIGINT NOT NULL`

**Given** RLS is configured via `ALTER TABLE server_config ENABLE ROW LEVEL SECURITY` and `CREATE POLICY config_insert_only ON server_config FOR INSERT WITH CHECK (true)`,
**When** an INSERT of `('server_name', 'chat.example.com', <timestamp_ms>)` is executed,
**Then** it succeeds

**Given** RLS is active,
**When** an UPDATE on any row in `server_config` is attempted,
**Then** PostgreSQL rejects it with a permission denied error

**Given** RLS is active,
**When** a DELETE on any row in `server_config` is attempted,
**Then** PostgreSQL rejects it with a permission denied error

**Given** gateway startup with `server_name` not yet in `server_config`,
**When** `NEBU_SERVER_NAME` env var is set,
**Then** gateway INSERTs `('server_name', <value>, <now_ms>)` and logs `"Server name set: <value>"`

**Given** gateway startup with `server_name` already in `server_config`,
**When** gateway reads the config,
**Then** it uses the value from the database and ignores any `NEBU_SERVER_NAME` env var

---

### Story 1.6: gRPC Proto Contract Definition + buf Configuration

As a developer,
I want the complete gRPC service contract defined in a `.proto` file with buf tooling configured,
So that Go and Elixir can independently generate type-safe stubs from a single source of truth.

**Acceptance Criteria:**

**Given** `proto/core.proto` exists,
**When** parsed by `buf lint`,
**Then** no lint errors are reported

**Given** the proto file,
**When** `CoreService` RPCs are listed,
**Then** it includes all of: `SendEvent`, `CreateRoom`, `JoinRoom`, `GetMessages`, `SetPresence`, `SetTyping`, `ValidateToken`, `GetPendingEvents` (unary fallback), and `EventBus` (server-streaming: `returns (stream Event)`)

**Given** `buf.yaml` and `buf.gen.yaml` exist in `proto/`,
**When** `make proto` runs (buf generate in container),
**Then** it exits with code 0

**Given** `make proto` completes,
**When** output directories are inspected,
**Then** generated Go stubs exist in `gateway/internal/grpc/pb/` and generated Elixir stubs exist in `core/apps/event_dispatcher/lib/pb/`

**Given** all proto message field names,
**When** naming is verified,
**Then** all fields use `snake_case` (e.g., `room_id`, `sender_id`, `origin_ts`, `event_type`)

**Given** `make proto` runs,
**When** generated files already exist,
**Then** they are overwritten cleanly (idempotent regeneration)

---

### Story 1.7: Go gRPC Client Skeleton

As a developer,
I want the Go gateway to have a configured gRPC client package that connects to the Elixir core,
So that subsequent stories can implement individual RPC calls without repeating connection setup.

**Acceptance Criteria:**

**Given** `NEBU_CORE_GRPC_ADDR` env var (default: `core:9000`),
**When** gateway starts,
**Then** a gRPC `ClientConn` is established to that address using the generated `CoreService` client

**Given** the `gateway/internal/grpc/` package,
**When** it is inspected,
**Then** it exports a `Client` struct with methods for all `CoreService` RPCs (stub implementations returning `nil, nil` or an empty response struct)

**Given** gateway startup,
**When** gRPC connection to core cannot be established within 5 seconds,
**Then** gateway logs a warning but does NOT exit — connection is non-blocking (lazy dial)

**Given** the gRPC client,
**When** it is used in later stories,
**Then** it accepts a `context.Context` as the first parameter on all method calls (standard Go gRPC pattern)

**Given** Go naming conventions,
**When** the package is inspected,
**Then** package name is `grpc` (lowercase), exported type is `Client`

---

### Story 1.8: Elixir gRPC Server Skeleton

As a developer,
I want the Elixir core to have a configured gRPC server that accepts connections from the Go gateway,
So that subsequent stories can add individual RPC handlers without server infrastructure setup.

**Acceptance Criteria:**

**Given** a gRPC server library (e.g., `grpc` hex package) added to `core/apps/event_dispatcher/mix.exs`,
**When** `mix deps.get` runs from `core/`,
**Then** it completes successfully with no conflicts

**Given** generated Elixir stubs from Story 1.6 in `core/apps/event_dispatcher/lib/pb/`,
**When** `Nebu.EventDispatcher.Server` implements the generated gRPC behaviour,
**Then** `mix compile` completes with 0 errors and 0 warnings

**Given** Elixir core application start,
**When** the supervision tree starts,
**Then** the gRPC server process starts and listens on port 9000

**Given** all RPC handler stubs,
**When** each returns `{:ok, %{}}` (empty response),
**Then** the server starts without errors and accepts incoming TCP connections

**Given** `mix test` in the `event_dispatcher` app,
**When** run,
**Then** existing tests pass with stub handlers in place

---

### Story 1.9: Docker Compose Stack + make setup + PSK Node Security

As an operator,
I want to start the complete development stack with a single command and have node security configured automatically,
So that local development requires no manual service configuration or secret management.

**Acceptance Criteria:**

**Given** `docker-compose.yml` in the project root defining services `gateway`, `core`, `postgres`, `dex`,
**When** `docker compose config` runs,
**Then** it validates without errors

**Given** `make setup` runs,
**When** `.secrets/` directory is created,
**Then** `.secrets/internal_secret` contains a freshly generated 32-byte hex string (`openssl rand -hex 32`)

**Given** `.gitignore`,
**When** it is checked,
**Then** `.secrets/` is listed as an ignored path

**Given** `docker-compose.yml` secrets configuration,
**When** `secrets:` block references `file: .secrets/internal_secret`,
**Then** both `gateway` and `core` services mount it at `/run/secrets/internal_secret`

**Given** gateway environment configuration,
**When** `NEBU_INTERNAL_SECRET_FILE: /run/secrets/internal_secret` is set,
**Then** gateway reads the PSK from the file path — never from an env var directly

**Given** `docker compose up`,
**When** all 4 services start,
**Then** `docker compose ps` shows all services as `running` or `healthy` within 2 minutes

**Given** `make dev` in Makefile,
**When** executed,
**Then** it runs `docker compose up` (delegates to Compose)

---

### Story 1.10: Elixir Node Registration (/internal/nodes/register)

As an operator,
I want the Elixir core to register itself with the Go gateway on startup,
So that the gateway knows the core is available and can begin routing requests.

**Acceptance Criteria:**

**Given** the gateway HTTP server is running,
**When** `POST /internal/nodes/register` receives a request with a valid `Authorization: Bearer <psk>` header,
**Then** it responds `200 OK` with `{"status": "registered"}`

**Given** `POST /internal/nodes/register` receives a request with wrong or missing authorization,
**When** processed by the gateway,
**Then** it responds `401 Unauthorized` with no body leaking internal details

**Given** Elixir core application startup,
**When** the Application `start/2` callback completes,
**Then** a startup hook calls `POST /internal/nodes/register` on the gateway URL (configured via `NEBU_GATEWAY_INTERNAL_URL`)

**Given** the registration request fails (gateway not yet ready),
**When** the startup hook receives a connection error,
**Then** Elixir retries up to 5 times with 2-second backoff before logging `"Gateway registration failed after retries"` and continuing startup

**Given** `GET /internal/nodes` with valid PSK,
**When** called on the gateway after Elixir has registered,
**Then** it returns a JSON list containing the registered node entry

---

### Story 1.11: Health + Readiness Endpoints — Go Gateway

As an operator,
I want standardized health and readiness endpoints on the Go gateway,
So that Docker Compose, monitoring systems, and CI pipelines can reliably verify gateway operational status.

**Acceptance Criteria:**

**Given** the gateway is running,
**When** `GET :8080/health` is called,
**Then** response is `200 OK` with body `{"status": "UP", "version": "0.1.0"}`

**Given** the gateway is running with DB connected and core reachable,
**When** `GET :8080/ready` is called,
**Then** response is `200 OK` with body `{"status": "READY", "checks": {"database": {"status": "UP"}, "core_grpc": {"status": "UP", "nebu_status": "GRÜN"}, "migrations": {"status": "UP", "version": N}}}`

**Given** the database is unreachable,
**When** `GET :8080/ready` is called,
**Then** response is `503 Service Unavailable` with `"database": {"status": "DOWN"}` and `"status": "NOT_READY"`

**Given** the Elixir core is unreachable,
**When** `GET :8080/ready` is called,
**Then** response includes `"core_grpc": {"status": "DOWN", "nebu_status": "ROT"}` and overall `"status": "NOT_READY"`

**Given** the gateway container has just started,
**When** `GET :8080/health` is called within 5 seconds of container start,
**Then** it responds `200 OK` (NFR-P4: cold start ≤5s)

**Given** the health endpoint under load,
**When** response time is measured,
**Then** `GET :8080/health` responds in ≤200ms (NFR-O2)

**Given** the gateway codebase,
**When** all handler and middleware code is reviewed,
**Then** no in-memory request session state exists — all persistent state reads go against PostgreSQL or Elixir via gRPC (NFR-SC1 stateless constraint)

---

### Story 1.12: Health Endpoint — Elixir Core

As an operator,
I want a health endpoint on the Elixir core,
So that Docker Compose and the Go gateway can monitor core liveness and component status.

**Acceptance Criteria:**

**Given** the Elixir core is running,
**When** `GET :4000/health` is called,
**Then** response is `200 OK` with body `{"status": "UP", "load_factor": 1.0, "version": "0.1.0", "node": "nebu@core-1", "components": {"database": {"status": "UP"}, "room_registry": {"status": "UP", "room_count": 0}, "event_bus": {"status": "UP", "connected_gateways": 0}}}`

**Given** a non-critical component is degraded,
**When** `GET :4000/health` is called,
**Then** response contains `"status": "DEGRADED"` and HTTP `200` (not 503)

**Given** a critical component is down,
**When** `GET :4000/health` is called,
**Then** response contains `"status": "DOWN"` and HTTP `503 Service Unavailable`

**Given** MVP runtime,
**When** `load_factor` is read from the response,
**Then** it always returns `1.0` (real adaptive calculation is Phase 2)

**Given** `docker-compose.yml` healthcheck for the `core` service,
**When** defined as `test: ["CMD", "curl", "-f", "http://localhost:4000/health"]` with `interval: 10s`, `timeout: 5s`, `retries: 3`, `start_period: 30s`,
**Then** Docker reports the core as `healthy` after startup completes

**Given** the health endpoint,
**When** response time is measured under normal load,
**Then** it responds in ≤200ms (NFR-O2)

---

### Story 1.13: Prometheus Metrics Endpoint — Go Gateway

As an operator,
I want a Prometheus-compatible metrics endpoint on the gateway,
So that standard monitoring tools can scrape operational data without custom instrumentation.

**Acceptance Criteria:**

**Given** the gateway is running,
**When** `GET :8080/metrics` is called,
**Then** response is `200 OK` with `Content-Type: text/plain; version=0.0.4` (Prometheus exposition format)

**Given** the metrics response,
**When** parsed by a Prometheus scraper,
**Then** it includes standard Go runtime metrics: `go_goroutines`, `go_memstats_alloc_bytes`, `go_gc_duration_seconds`

**Given** the metrics endpoint,
**When** `nebu_http_requests_total` counter is present,
**Then** it increments on each completed API request and includes `method` and `status_code` labels

**Given** the metrics endpoint,
**When** `nebu_grpc_status` gauge is present,
**Then** it reflects current gateway status: `2` = GRÜN, `1` = GELB, `0` = ROT

**Given** `prometheus/client_golang` added to `go.mod`,
**When** `go build ./...` runs,
**Then** compilation succeeds

**Given** the `/metrics` endpoint,
**When** accessed without authentication,
**Then** it responds `200 OK` (metrics endpoint requires no auth — ops/internal use only)

---

### Story 1.14: TLS Configuration for External Connections

As an operator,
I want TLS configurable for external client-to-gateway connections,
So that production deployments are encrypted while local development remains frictionless.

**Acceptance Criteria:**

**Given** `NEBU_TLS_CERT_FILE` and `NEBU_TLS_KEY_FILE` env vars are both set,
**When** the gateway starts,
**Then** it listens on HTTPS with TLS 1.2 as minimum protocol version and TLS 1.3 preferred

**Given** TLS env vars are NOT set,
**When** the gateway starts,
**Then** it listens on plain HTTP and logs a warning: `"TLS disabled — not suitable for production"`

**Given** a TLS connection from a client using TLS 1.1 or lower,
**When** processed by the gateway,
**Then** the connection is rejected with a TLS handshake error

**Given** the `docker-compose.yml` for local development,
**When** TLS env vars are absent from the gateway service,
**Then** the gateway runs on plain HTTP — no certificate setup required for local development

**Given** `NEBU_TLS_CLIENT_CA_FILE` is NOT set (mTLS Phase 2),
**When** the gateway starts,
**Then** mTLS is disabled — client certificates are not required (forward-compatible placeholder)

---

### Story 1.15: Go Unit Test Target

As a developer,
I want `make test-unit-go` to run all Go unit tests in a container,
So that CI can verify Go code correctness without requiring a local Go installation.

**Acceptance Criteria:**

**Given** `Makefile` with a `test-unit-go` target,
**When** `make test-unit-go` runs,
**Then** it executes `go test -race ./...` inside the Go build container defined by `DOCKER_GO`

**Given** at least one unit test file exists (e.g., `gateway/internal/grpc/client_test.go` with a trivial passing test),
**When** `make test-unit-go` runs,
**Then** it exits with code 0 and prints test results

**Given** a deliberately failing test,
**When** `make test-unit-go` runs,
**Then** it exits with a non-zero code and prints the failing test name and package

**Given** `DOCKER_GO` variable in Makefile,
**When** it is used in `test-unit-go`,
**Then** it references the same Go build image used for `make build-gateway` (single source of truth for Go version)

---

### Story 1.16: Elixir Unit Test Target

As a developer,
I want `make test-unit-elixir` to run all Elixir unit tests in a container,
So that CI can verify Elixir code correctness without requiring a local Elixir installation.

**Acceptance Criteria:**

**Given** `Makefile` with a `test-unit-elixir` target,
**When** `make test-unit-elixir` runs,
**Then** it executes `mix test --warnings-as-errors` inside the Elixir build container defined by `DOCKER_ELIXIR`, from the `core/` directory

**Given** all 6 umbrella apps have placeholder test files,
**When** `make test-unit-elixir` runs,
**Then** it exits with code 0 and prints `"X tests, 0 failures"`

**Given** a deliberately failing Elixir test,
**When** `make test-unit-elixir` runs,
**Then** it exits with a non-zero code and prints the failing test module and line number

**Given** `DOCKER_ELIXIR` variable in Makefile,
**When** it is used in `test-unit-elixir`,
**Then** it references the same Elixir build image used for `make build-core`

---

### Story 1.17: make gen-api — oapi-codegen Build Target

As a developer,
I want `make gen-api` to generate Go types and server interface from the OpenAPI spec,
So that the Admin API always stays in sync with `gateway/api/openapi.yaml` as its single source of truth.

**Acceptance Criteria:**

**Given** `gateway/api/openapi.yaml` exists with a minimal valid spec (one endpoint: `GET /api/v1/health` returning `{"status": "string"}`),
**When** `make gen-api` runs,
**Then** it executes oapi-codegen inside the Go container and exits code 0

**Given** `make gen-api` completes,
**When** `gateway/internal/admin/api_gen.go` is inspected,
**Then** it contains a `ServerInterface` with at least one method signature and Go structs for request/response types

**Given** `api_gen.go` is generated,
**When** `go build ./...` runs,
**Then** it compiles successfully

**Given** `gateway/api/openapi.yaml` with an intentionally invalid schema,
**When** `make gen-api` runs,
**Then** it exits non-zero with a descriptive error message

**Given** the generated file header,
**When** `api_gen.go` is opened,
**Then** it contains the comment `// Code generated by oapi-codegen — DO NOT EDIT`

---

### Story 1.18: Godog Integration Test Framework Setup

As a developer,
I want the Godog BDD framework configured and runnable against the Docker Compose stack,
So that Gherkin scenarios can be executed in CI without manual stack management.

**Acceptance Criteria:**

**Given** `github.com/cucumber/godog` added to `go.mod`,
**When** `go build ./...` runs,
**Then** it compiles successfully

**Given** `gateway/features/` directory exists,
**When** it contains a placeholder `health.feature` file,
**Then** it is valid Gherkin syntax (parseable by godog)

**Given** `gateway/test/integration/main_test.go` with Godog `TestSuite` configuration,
**When** `go test ./test/integration/... -v` runs (without a live stack),
**Then** it compiles and reports "no scenarios" or skipped scenarios without panicking

**Given** `make test-integration` target in Makefile,
**When** executed,
**Then** it: (1) runs `docker compose up -d`, (2) waits for all services to be healthy (polling `/health`), (3) runs Godog test suite, (4) runs `docker compose down` regardless of test result

**Given** `NEBU_TEST_GATEWAY_URL` env var,
**When** set to a custom URL,
**Then** Godog uses it as the base URL for all HTTP calls (default: `http://localhost:8080`)

---

### Story 1.19: First Gherkin Scenario — Stack Health Smoke Test

As an operator,
I want a passing end-to-end Gherkin scenario that verifies the complete stack starts and is healthy,
So that CI has a definitive green signal that the entire deployment infrastructure works.

**Acceptance Criteria:**

**Given** `gateway/features/health.feature` contains the Stack Health scenario,
**When** `make test-integration` runs against a started stack,
**Then** the scenario passes with exit code 0

**Given** the feature file,
**When** read,
**Then** it contains the following steps:
- `Given the docker compose stack is started`
- `When I call GET /health on the gateway`
- `Then the response status is 200`
- `And the response body contains "UP"`
- `When I call GET /ready on the gateway`
- `Then the response status is 200`
- `And the response body contains "READY"`
- `When I call GET :4000/health on the core`
- `Then the response status is 200`
- `And the response body contains "UP"`

**Given** a cold stack start,
**When** `make test-integration` runs from `docker compose up` to passing scenario,
**Then** total elapsed time is ≤3 minutes

**Given** a CI pipeline configuration file (`.github/workflows/ci.yml` or `.gitlab-ci.yml`),
**When** it exists in the repository,
**Then** it defines a `unit-tests` job running `make test-unit-go` and `make test-unit-elixir`, and an `integration-test` job running `make test-integration` — both triggered on push to `main` and on pull/merge requests

## Epic 2: Users Can Authenticate with SSO and Have a Verified Cryptographic Identity

End-User können sich mit dem OIDC-Provider ihrer Organisation anmelden; der erste Operator erhält Admin-Rechte via Bootstrap-Logik; jeder User bekommt ein kryptografisches Schlüsselpaar (Ed25519 Signing + X25519 Encryption); PII-Tiers sind in der DB verankert.
**FRs covered:** FR1, FR2, FR3, FR4, FR5 (Backend), FR6 (Backend), FR25, FR27
**Zusätzliche Deliverables:** OIDC-Validierungs-Middleware, Bootstrap-Mode-Backend (DB + Logik), Ed25519+X25519-Keypair-Generierung (OTP native), Three-PII-Tier-Datenbankstruktur, OIDC Claim-to-Role-Mapping, Admin UI OIDC Client mit PKCE, Dex Dev-Setup (dev/dex/config.yaml mit 3 Test-Usern: kai/compliance/alex), Auth-Token-Flow (Go → Elixir via gRPC-Metadata)

### Story 2.1: users + user_keys Schema Migration

As a developer,
I want the core user and key tables created via migration,
So that user provisioning and cryptographic key storage have a schema foundation for all subsequent auth stories.

**Acceptance Criteria:**

**Given** migration `000004_users.up.sql`,
**When** it runs,
**Then** `users` table exists with columns: `user_id TEXT PRIMARY KEY`, `display_name_encrypted BYTEA`, `display_name_nonce BYTEA`, `avatar_url_encrypted BYTEA`, `avatar_url_nonce BYTEA`, `system_role TEXT NOT NULL DEFAULT 'user'`, `is_active BOOLEAN NOT NULL DEFAULT true`, `signing_key_id TEXT`, `encryption_key_id TEXT`, `created_at BIGINT NOT NULL`, `last_seen_at BIGINT`

**Given** the same migration,
**When** it runs,
**Then** `user_keys` table exists with columns: `key_id TEXT PRIMARY KEY`, `user_id TEXT NOT NULL REFERENCES users(user_id)`, `key_type TEXT NOT NULL`, `algorithm TEXT NOT NULL`, `public_key BYTEA NOT NULL`, `private_key BYTEA` (nullable — NULL after DSGVO deletion), `created_at BIGINT NOT NULL`, `deleted_at BIGINT`

**Given** the `system_role` column,
**When** a value other than `'user'`, `'instance_admin'`, or `'compliance_officer'` is inserted,
**Then** the database rejects it with a CHECK constraint violation

**Given** the `key_type` column,
**When** a value other than `'signing'` or `'encryption'` is inserted,
**Then** the database rejects it with a CHECK constraint violation

**Given** a corresponding `000004_users.down.sql`,
**When** it runs,
**Then** both `user_keys` and `users` tables are dropped cleanly

---

### Story 2.2: sessions Schema Migration

As a developer,
I want a sessions table created via migration,
So that Epic 4's `/sync` since-token checkpointing has its schema ready without requiring later migrations.

**Acceptance Criteria:**

**Given** migration `000005_sessions.up.sql`,
**When** it runs,
**Then** `sessions` table exists with columns: `session_id TEXT PRIMARY KEY`, `user_id TEXT NOT NULL REFERENCES users(user_id)`, `since_token TEXT`, `device_id TEXT NOT NULL`, `last_active_at BIGINT NOT NULL`, `created_at BIGINT NOT NULL`

**Given** an index on `user_id`,
**When** the migration runs,
**Then** `CREATE INDEX sessions_user_id_idx ON sessions (user_id)` is applied

**Given** a corresponding down migration,
**When** it runs,
**Then** the `sessions` table is dropped cleanly

---

### Story 2.3: OIDC Provider Configuration

As an operator,
I want the gateway to discover and cache the OIDC provider's public keys on startup,
So that token validation works without a network call on every request.

**Acceptance Criteria:**

**Given** `NEBU_OIDC_ISSUER`, `NEBU_OIDC_CLIENT_ID`, and `NEBU_OIDC_CLIENT_SECRET` env vars are set,
**When** the gateway starts,
**Then** it fetches the OIDC discovery document from `<issuer>/.well-known/openid-configuration` and caches the JWKS URI

**Given** the JWKS URI is known,
**When** the gateway starts,
**Then** it fetches and caches the JWKS (public keys) for JWT signature validation

**Given** the OIDC provider is unreachable at startup,
**When** the gateway tries to fetch the discovery document,
**Then** it logs a warning `"OIDC provider unreachable — token validation will fail until resolved"` but does NOT crash

**Given** cached JWKS,
**When** 1 hour has elapsed since last fetch,
**Then** the gateway refreshes the JWKS in the background (key rotation support)

**Given** MVP scope constraint,
**When** OIDC configuration is read,
**Then** it is sourced exclusively from environment variables — no runtime update via API (FR3 MVP = Env-Var only; runtime config management is Post-MVP)

---

### Story 2.4: OIDC JWT Token Validation Middleware

As an end-user,
I want my OIDC token to be validated on every Matrix API request,
So that only authenticated users can access the API and my identity is reliably established per request.

**Acceptance Criteria:**

**Given** a Matrix API request with a valid `Authorization: Bearer <jwt>` header,
**When** the middleware processes it,
**Then** it validates the JWT signature against the cached JWKS, checks expiry, issuer, and audience — and passes the request to the handler with claims in context

**Given** a request with an expired token,
**When** the middleware processes it,
**Then** it returns `401 Unauthorized` with Matrix error body `{"errcode": "M_UNKNOWN_TOKEN", "error": "Token has expired"}`

**Given** a request with an invalid signature,
**When** the middleware processes it,
**Then** it returns `401 Unauthorized` with `{"errcode": "M_UNKNOWN_TOKEN", "error": "Invalid token"}`

**Given** a request with no `Authorization` header,
**When** the middleware processes it,
**Then** it returns `401 Unauthorized` with `{"errcode": "M_MISSING_TOKEN", "error": "Missing access token"}`

**Given** the middleware scope,
**When** its implementation is reviewed,
**Then** it performs JWT validation only — no OAuth2 code exchange, no PKCE, no redirect logic

**Given** validated claims,
**When** extracted from the JWT,
**Then** `sub`, `preferred_username`, `email`, and `nebu_role` are made available in the request context for downstream handlers

---

### Story 2.5: OIDC Claim-to-Role Mapping

As an operator,
I want the OIDC `nebu_role` claim to be mapped to Nebu system roles,
So that role-based access control works correctly for admins and compliance officers.

**Acceptance Criteria:**

**Given** a JWT with `"nebu_role": "instance_admin"`,
**When** the claim is mapped,
**Then** the resulting `system_role` is `"instance_admin"`

**Given** a JWT with `"nebu_role": "compliance_officer"`,
**When** the claim is mapped,
**Then** the resulting `system_role` is `"compliance_officer"`

**Given** a JWT with any other `nebu_role` value or no `nebu_role` claim,
**When** the claim is mapped,
**Then** the resulting `system_role` defaults to `"user"`

**Given** `NEBU_OIDC_CLAIM_ROLE` env var set to a custom claim name (e.g., `"roles"`),
**When** the gateway starts,
**Then** it uses that custom claim name instead of the default `"nebu_role"`

**Given** a unit test,
**When** it covers all three role mapping cases,
**Then** it passes with `go test -race ./...`

---

### Story 2.6: Admin UI OIDC Client — Authorization Code Flow with PKCE

As an operator,
I want the Admin UI login to use a secure PKCE-protected OIDC flow,
So that authorization codes cannot be intercepted or replayed by attackers.

**Acceptance Criteria:**

**Given** `GET /admin/auth/login` is called,
**When** handled by the gateway,
**Then** it generates a cryptographically random `code_verifier` (43–128 chars, Base64URL, via `golang.org/x/oauth2.GenerateVerifier()`), derives `code_challenge` using S256 (`oauth2.S256ChallengeOption(verifier)`), generates a random `state` parameter, stores both server-side with a 10-minute TTL (signed cookie), and redirects to the OIDC provider authorization endpoint with `code_challenge`, `code_challenge_method=S256`, and `state`

**Given** `GET /admin/auth/callback?code=<code>&state=<state>` is called,
**When** handled by the gateway,
**Then** it validates the `state` parameter against the stored value (returns `400 Bad Request` on mismatch — CSRF protection)

**Given** a valid `state` match,
**When** the callback is processed,
**Then** it exchanges the `code` + `code_verifier` for an access token and ID token via the OIDC token endpoint

**Given** a successful token exchange,
**When** the ID token is validated,
**Then** the gateway extracts the `nebu_role` claim, sets a secure HTTP-only session cookie, and redirects to `/admin/`

**Given** a failed token exchange (e.g., invalid code),
**When** the callback is processed,
**Then** it redirects to `/admin/auth/login` with an `error=auth_failed` query parameter

**Given** `golang.org/x/oauth2` is added to `go.mod`,
**When** `go build ./...` runs,
**Then** compilation succeeds

---

### Story 2.7: Auth Token Flow — Go to Elixir via gRPC Metadata

As a developer,
I want validated user identity passed from Go to Elixir as gRPC metadata,
So that Elixir handlers have reliable user context without re-validating tokens.

**Acceptance Criteria:**

**Given** a validated Matrix API request with a known `user_id` and `system_role`,
**When** the Go gateway makes any gRPC call to Elixir,
**Then** it sets `"x-user-id": "@<sub>:<server_name>"` and `"x-system-role": "<role>"` in the gRPC outgoing metadata

**Given** an Elixir gRPC handler receiving a request,
**When** it reads the gRPC metadata,
**Then** `x-user-id` and `x-system-role` are accessible via the metadata map

**Given** Elixir's trust model,
**When** Elixir handlers are reviewed,
**Then** no handler re-validates the OIDC token — metadata values are trusted as authoritative

**Given** a unit test in Go,
**When** it verifies the metadata is set on outgoing gRPC calls,
**Then** it passes with `go test -race ./...`

---

### Story 2.8: Ed25519 Keypair Generation + Unit Tests

As a developer,
I want the Elixir `signature` app to generate Ed25519 keypairs using OTP native crypto,
So that message signing has a tested, dependency-free foundation.

**Acceptance Criteria:**

**Given** `Nebu.Signature.generate_signing_keypair/0` function in the `signature` app,
**When** called,
**Then** it returns `{public_key, private_key}` as binary tuples using `:crypto.generate_key(:eddsa, :ed25519)`

**Given** a generated Ed25519 keypair,
**When** the key lengths are checked,
**Then** public key is 32 bytes and private key is 64 bytes

**Given** a unit test `sign_and_verify`,
**When** a message is signed with the private key via `:crypto.sign(:eddsa, :none, message, [private_key], [:ed25519])` and verified with the public key,
**Then** the verification returns `true`

**Given** a unit test `tampered_message`,
**When** a signed message is modified and re-verified,
**Then** the verification returns `false`

**Given** `mix test --warnings-as-errors` in the `signature` app,
**When** run,
**Then** all unit tests pass with 0 failures

---

### Story 2.9: X25519 Keypair Generation + Unit Tests

As a developer,
I want the Elixir `signature` app to generate X25519 keypairs using OTP native crypto,
So that PII encryption has a tested, dependency-free foundation.

**Acceptance Criteria:**

**Given** `Nebu.Signature.generate_encryption_keypair/0` function,
**When** called,
**Then** it returns `{public_key, private_key}` as binary tuples using `:crypto.generate_key(:ecdh, :x25519)`

**Given** a generated X25519 keypair,
**When** key lengths are checked,
**Then** both public key and private key are 32 bytes

**Given** a unit test `ecdh_shared_secret`,
**When** two X25519 keypairs perform ECDH exchange (Alice's private × Bob's public, Bob's private × Alice's public),
**Then** both computations produce the identical shared secret

**Given** `Nebu.Signature.derive_aes_key/1` function that takes a shared secret,
**When** called with a 32-byte shared secret,
**Then** it returns a 32-byte AES-256 key (via HKDF-SHA256 or SHA256 derivation)

**Given** `mix test --warnings-as-errors` in the `signature` app,
**When** run,
**Then** all unit tests pass with 0 failures

---

### Story 2.10: Operational PII Encryption at Rest + Unit Tests

As a developer,
I want display names and avatar URLs encrypted with a server-side key,
So that Tier 1 PII (Operational PII) is protected at rest and can be anonymised on account deletion.

**Acceptance Criteria:**

**Given** `Nebu.Signature.encrypt_operational_pii/2` taking `plaintext` and `server_key`,
**When** called,
**Then** it returns `{ciphertext, nonce}` using AES-256-GCM with a freshly generated 12-byte random nonce

**Given** `Nebu.Signature.decrypt_operational_pii/3` taking `ciphertext`, `nonce`, and `server_key`,
**When** called with values from a prior encryption,
**Then** it returns the original plaintext

**Given** `NEBU_PII_ENCRYPTION_KEY` env var (32-byte hex string),
**When** the `signature` app starts,
**Then** it reads and validates the key length — exits with a clear error if missing or wrong length

**Given** a unit test `encrypt_decrypt_roundtrip`,
**When** the same plaintext is encrypted twice,
**Then** the two ciphertexts differ (random nonce per encryption) but both decrypt to the original plaintext

**Given** a unit test `wrong_key_fails`,
**When** decryption is attempted with a different server key,
**Then** AES-GCM authentication fails and returns `{:error, :decryption_failed}`

**Given** `mix test --warnings-as-errors` in the `signature` app,
**When** run,
**Then** all unit tests pass with 0 failures

---

### Story 2.11: Sensitive PII Encryption (X25519 ECDH + AES-256-GCM) + Unit Tests

As a developer,
I want email and IdP subject encrypted with the user's X25519 public key,
So that Tier 2 PII (Sensitive PII) becomes irrecoverable when the user's private key is deleted (DSGVO Right to be Forgotten).

**Acceptance Criteria:**

**Given** `Nebu.Signature.encrypt_sensitive_pii/2` taking `plaintext` and `recipient_public_key`,
**When** called,
**Then** it generates an ephemeral X25519 keypair, performs ECDH to derive a shared secret, derives an AES-256 key, encrypts with AES-256-GCM, and returns `{ciphertext, ephemeral_public_key, nonce}`

**Given** `Nebu.Signature.decrypt_sensitive_pii/4` taking `ciphertext`, `ephemeral_public_key`, `nonce`, and `recipient_private_key`,
**When** called with values from a prior encryption,
**Then** it returns the original plaintext

**Given** a unit test `encrypt_decrypt_roundtrip`,
**When** sensitive PII is encrypted with a recipient's public key and decrypted with the matching private key,
**Then** the original plaintext is recovered

**Given** a unit test `deletion_makes_irrecoverable`,
**When** the private key is set to `nil` and decryption is attempted,
**Then** it returns `{:error, :no_private_key}` — the data is effectively deleted (NFR-S5, NFR-C1)

**Given** `mix test --warnings-as-errors` in the `signature` app,
**When** run,
**Then** all unit tests pass with 0 failures

---

### Story 2.12: User-Record DB-Write on First Login

As a developer,
I want the Elixir core to create a user record in the database on first login,
So that subsequent requests have a persistent user identity to reference.

**Acceptance Criteria:**

**Given** a `ValidateToken` gRPC call with a `user_id` that does not exist in the `users` table,
**When** processed by the `session_manager` app,
**Then** a new row is INSERTed into `users` with `user_id`, `system_role`, `created_at = now_ms()`, `is_active = true`

**Given** a `ValidateToken` gRPC call with a `user_id` that already exists,
**When** processed,
**Then** `last_seen_at` is updated and the existing record is returned — no duplicate INSERT

**Given** an INSERT that conflicts on `user_id` (concurrent first-login race condition),
**When** processed,
**Then** the INSERT uses `ON CONFLICT (user_id) DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at` to resolve safely

**Given** a unit test with a mock DB,
**When** first-login provisioning is called twice with the same `user_id`,
**Then** exactly one user record exists after both calls

---

### Story 2.13: User Provisioning Orchestration — Keypairs + PII Encryption

As a developer,
I want keypair generation and PII encryption to run automatically when a user record is first created,
So that every user has cryptographic identity and protected PII from their first login.

**Acceptance Criteria:**

**Given** a new user record created in Story 2.12,
**When** provisioning completes,
**Then** two keypairs are generated and stored: one Ed25519 signing keypair and one X25519 encryption keypair in `user_keys` with `key_type = 'signing'` and `key_type = 'encryption'` respectively

**Given** keypairs are stored,
**When** the `users` table row is inspected,
**Then** `signing_key_id` and `encryption_key_id` reference the respective `key_id` values in `user_keys`

**Given** `preferred_username` and `email` claims from the JWT,
**When** a new user is provisioned,
**Then** `display_name` is encrypted with the server PII key (Tier 1) and stored as `display_name_encrypted + display_name_nonce`, and `email` is encrypted with the user's X25519 public key (Tier 2) and stored separately

**Given** the entire provisioning process,
**When** it runs,
**Then** it executes within a single PostgreSQL transaction — either all succeed or all roll back

---

### Story 2.14: ValidateToken gRPC Handler

As a developer,
I want the Elixir `ValidateToken` gRPC handler to look up or provision users and return their identity,
So that the Go gateway can confirm user identity on every Matrix API request.

**Acceptance Criteria:**

**Given** a `ValidateToken` gRPC request with `user_id` and `system_role` in metadata,
**When** the user exists in the DB with `is_active = true`,
**Then** the handler returns a `ValidateTokenResponse` with `user_id`, `system_role`, `display_name` (decrypted), and `is_active: true`

**Given** a `ValidateToken` request for a user that does not yet exist,
**When** processed,
**Then** the handler triggers Stories 2.12 + 2.13 provisioning and returns the new user's data

**Given** a `ValidateToken` request for a user with `is_active = false`,
**When** processed,
**Then** the handler returns a gRPC `PERMISSION_DENIED` error with message `"user account is deactivated"`

**Given** a gRPC unit test,
**When** `ValidateToken` is called with a known `user_id`,
**Then** it returns the correct user data without hitting the database (mock DB in test)

---

### Story 2.15: Bootstrap Mode — Auto Instance Admin Assignment

As an operator,
I want the first OIDC login to automatically receive `instance_admin` rights,
So that a fresh deployment can be configured without a chicken-and-egg problem.

**Acceptance Criteria:**

**Given** a fresh deployment with no users in the `users` table,
**When** the first `ValidateToken` call is processed,
**Then** the resulting user record is assigned `system_role = 'instance_admin'` regardless of the `nebu_role` claim

**Given** bootstrap mode is active,
**When** checked via `server_config` table,
**Then** `SELECT value FROM server_config WHERE key = 'bootstrap_active'` returns `'true'`

**Given** concurrent first-login requests (race condition),
**When** two requests arrive simultaneously,
**Then** exactly one user receives `instance_admin` — the other receives the role from their `nebu_role` claim (atomic check using PostgreSQL transaction)

**Given** bootstrap mode is active and `GET /admin/bootstrap` is called,
**When** processed,
**Then** it responds `200 OK` (bootstrap route is accessible)

---

### Story 2.16: Bootstrap Mode — Permanent Deactivation

As an operator,
I want bootstrap mode to deactivate permanently after the first admin is established,
So that no subsequent login can inadvertently gain admin rights through the bootstrap mechanism.

**Acceptance Criteria:**

**Given** the first `instance_admin` user has been created,
**When** provisioning completes,
**Then** `INSERT INTO server_config (key, value, set_at) VALUES ('bootstrap_completed', 'true', <now_ms>)` is executed

**Given** `bootstrap_completed` is set in `server_config`,
**When** any subsequent `ValidateToken` call is processed,
**Then** bootstrap mode is NOT triggered — roles are assigned solely from `nebu_role` OIDC claim

**Given** RLS on `server_config` (Story 1.5),
**When** `bootstrap_completed` is inserted,
**Then** it cannot be updated or deleted — deactivation is permanent and irreversible (NFR-S6)

**Given** `GET /admin/bootstrap` after deactivation,
**When** called,
**Then** it returns `404 Not Found` — the bootstrap route no longer exists

---

### Story 2.17: GET /_matrix/client/v3/login — SSO Flow Discovery

As an end-user,
I want my Matrix client to discover the supported login methods,
So that it can initiate the correct SSO flow automatically.

**Acceptance Criteria:**

**Given** `GET /_matrix/client/v3/login` is called without authentication,
**When** processed by the gateway,
**Then** response is `200 OK` with body `{"flows": [{"type": "m.login.sso", "identity_providers": [{"id": "oidc", "name": "<issuer_display_name>", "icon": null}]}]}`

**Given** the Matrix spec requirement,
**When** the response is validated,
**Then** `Content-Type` is `application/json` and the response matches the Matrix Client-Server API format (NFR-M1)

**Given** Element Web pointing to a Nebu server,
**When** the login page loads,
**Then** it displays the SSO login button (functional validation via Dex dev setup)

---

### Story 2.18: POST /_matrix/client/v3/login — OIDC Token Exchange

As an end-user,
I want to exchange my OIDC token for a Matrix access token,
So that I can authenticate with any standard Matrix client using my organisation's SSO.

**Acceptance Criteria:**

**Given** `POST /_matrix/client/v3/login` with body `{"type": "m.login.token", "token": "<valid_oidc_jwt>"}`,
**When** processed,
**Then** the JWT is validated via the middleware (Story 2.4), user is provisioned if new (Stories 2.12–2.13), and response is `200 OK` with `{"access_token": "<oidc_jwt>", "device_id": "<uuid>", "user_id": "@<sub>:<server_name>", "token_type": "Bearer"}`

**Given** the `access_token` in the response,
**When** used in a subsequent Matrix API request as `Authorization: Bearer <token>`,
**Then** it is accepted — the OIDC JWT IS the Matrix access token (stateless validation per request, NFR-S4)

**Given** `POST /login` with an invalid or expired token,
**When** processed,
**Then** response is `403 Forbidden` with `{"errcode": "M_FORBIDDEN", "error": "Invalid or expired token"}`

**Given** `POST /login` with an unsupported `type` field,
**When** processed,
**Then** response is `400 Bad Request` with `{"errcode": "M_UNKNOWN", "error": "Unsupported login type"}`

---

### Story 2.19: POST /_matrix/client/v3/logout — Session Invalidation

As an end-user,
I want to log out and invalidate my session,
So that my access token can no longer be used after I explicitly log out.

**Acceptance Criteria:**

**Given** `POST /_matrix/client/v3/logout` with a valid `Authorization: Bearer <token>` header,
**When** processed,
**Then** the token is added to a short-lived server-side denylist (keyed by token hash, TTL = token expiry) and response is `200 OK` with `{}`

**Given** a denylisted token,
**When** used in any subsequent Matrix API request,
**Then** the middleware returns `401 Unauthorized` with `{"errcode": "M_UNKNOWN_TOKEN", "error": "Token has been logged out"}`

**Given** `POST /_matrix/client/v3/logout` with no `Authorization` header,
**When** processed,
**Then** response is `401 Unauthorized` with `{"errcode": "M_MISSING_TOKEN", "error": "Missing access token"}`

---

### Story 2.20: Dex Dev Setup

As a developer,
I want Dex (lightweight OIDC) in the Docker Compose dev stack,
So that all three user personas can authenticate against a real OIDC provider during development and testing, with fast startup and zero manual configuration after `make dev`.

**Acceptance Criteria:**

**Given** `docker-compose.yml` in the project root,
**When** the `dex` service is defined using image `dexidp/dex:v2.41.1` (or latest stable),
**Then** no `keycloak` service exists in `docker-compose.yml`

**Given** `dev/dex/config.yaml` committed to the repository,
**When** inspected,
**Then** it contains:
- `issuer: http://dex:5556/dex`
- `storage.type: sqlite3` with `config.file: /var/dex/dex.db`
- `staticClients`: one entry — `id: nebu-admin`, `secret: nebu-admin-secret`, `redirectURIs: ["http://localhost:8080/admin/callback"]`, `name: Nebu Admin UI`
- `oauth2.responseTypes: [code]`, `oauth2.skipApprovalScreen: true`
- `staticPasswords`: three entries with `nebu_role` claim via custom connector:
  - `email: kai@example.com`, `username: kai`, `userID: 00000000-0000-0000-0000-000000000001` (role: `instance_admin`)
  - `email: compliance@example.com`, `username: compliance`, `userID: 00000000-0000-0000-0000-000000000002` (role: `compliance_officer`)
  - `email: alex@example.com`, `username: alex`, `userID: 00000000-0000-0000-0000-000000000003` (role: `user`)
  - All three with bcrypt hash of `changeme` as dev password

**Given** the `dex` compose service,
**When** configured,
**Then** it mounts `./dev/dex/config.yaml:/etc/dex/config.yaml:ro` and exposes port `5556`

**Given** `docker compose up`,
**When** Dex is healthy,
**Then** `GET http://localhost:5556/dex/.well-known/openid-configuration` returns a valid OIDC discovery document

**Given** Dex startup time,
**When** measured,
**Then** Dex is ready within 3 seconds of container start

**Given** `make setup` runs,
**When** completed,
**Then** dev credentials for all three test users are printed to stdout: `kai@example.com / changeme (instance_admin)`, `compliance@example.com / changeme (compliance_officer)`, `alex@example.com / changeme (user)`

---

### Story 2.21: Gherkin Auth Scenario — End-to-End OIDC Login

As an operator,
I want a passing Gherkin scenario that verifies the complete OIDC login flow,
So that CI catches any regression in authentication before it reaches production.

**Acceptance Criteria:**

**Given** `gateway/features/auth.feature` contains the OIDC login scenario,
**When** `make test-integration` runs against the full stack (including Dex),
**Then** the scenario passes with exit code 0

**Given** the feature file,
**When** read,
**Then** it contains steps verifying:
- `GET /_matrix/client/v3/login` returns `200` with `m.login.sso` in the flows
- A valid OIDC token obtained from Dex via the static password flow for `kai@example.com` / `changeme`
- `POST /_matrix/client/v3/login` with that token returns `200` with `access_token` and `user_id: "@kai@example.com:<server_name>"`
- A subsequent authenticated request using the `access_token` returns `200`
- `POST /_matrix/client/v3/logout` returns `200`
- The logged-out token rejected with `401` on subsequent use

**Given** the Dex service in Docker Compose,
**When** `make test-integration` runs,
**Then** the test obtains a real JWT from Dex (not a mock) using the static password flow (`POST http://dex:5556/dex/token` with `grant_type=password`, `username=kai@example.com`, `password=changeme`)

### Epic 3: Operators Have a Minimal Admin UI for Bootstrap and Debugging
Operator kann den Bootstrap-Wizard durchführen (OIDC konfigurieren, ersten Admin anlegen). Ein grobes Dashboard zeigt System-Status (GRÜN/GELB/ROT), Message-Durchsatz und aktive Sessions — als Debugging-Aid während Chat-Tests in Epic 4.
**FRs covered:** FR5 (UI), FR6 (UI), FR48 (partial), FR49 (partial), FR50 (partial)
**UX-DRs covered:** UX-DR3 (Tailwind+DaisyUI Build-Setup), UX-DR21 (Bootstrap-Wizard C6b), UX-DR23 (Sentinel Dashboard rudimentär), UX-DR4 (C1 StatusCard), UX-DR5 (C2 AlertItem), UX-DR6 (C3 TopbarStatusIndicator), UX-DR26 (Sidebar-Basis)
**Zusätzliche Deliverables:** Tailwind Standalone CLI Build-Integration, Go-Template-Basisstruktur, Bootstrap-Wizard (4 Steps: Instanzname → OIDC-Config+Test → Ed25519-Keygen → Erster Admin), Minimal Sentinel Dashboard (StatusCards + AlertItems), SSE Live-Metriken (Vue.js minimal für C1+C3), URL-Routing-Basis

### Epic 4: End-Users Can Chat in Rooms Using Any Standard Matrix Client
Nutzer können Rooms erstellen, beitreten, kryptografisch signierte Nachrichten senden und empfangen, Präsenz und Typing-Indikatoren sehen, Dateien hochladen — alles mit Element, FluffyChat oder anderen Standard-Clients. Message-Durchsatz im Debug-Dashboard von Epic 3 sichtbar.
**FRs covered:** FR7, FR8, FR9, FR10, FR11, FR12, FR13, FR14, FR15, FR16, FR17, FR18, FR19, FR20, FR21, FR26, FR41 (Matrix-native Push-Rules only — Apple/Google Push: Phase 2), FR42
**Zusätzliche Deliverables:** /sync (Hybrid ETS+PostgreSQL since-Token), SendEvent + Ed25519-Signing + Content-Hash EventID, Horde Registry + DynamicSupervisor, gRPC EventBus Streaming + Unary Fallback, message_buffer Drain-Strategy (Linear MVP), Minimal Media Gateway (Upload/Download, AES-256-GCM, lokales Filesystem), Room-Power-Levels, Silber-Tier-Lasttest (>500 concurrent auf 2× m5.large)

### Epic 5: Compliance Officers Can Securely Request and Export Audited Communication Data
Compliance Officers können Zugriffsanträge stellen, nach Vier-Augen-Genehmigung 24h auf Nachrichtendaten zugreifen, kryptografisch signierte Exporte herunterladen. Der gesamte Prozess ist lückenlos im Append-Only Audit Log nachweisbar. DSGVO-Löschung ist technisch erzwungen und atomar.
**FRs covered:** FR28, FR29, FR30, FR31, FR32, FR33, FR34, FR35
**Zusätzliche Deliverables:** Append-Only Audit Log (PostgreSQL + RLS), Four-Eyes Approval Flow (Backend + Notification), Ed25519-signierter PDF-Export, Atomare DSGVO-Deletion-Transaktion (Ed25519+X25519 Delete + Audit-Eintrag auch bei Fehler), Audit-Log-Aufbewahrung konfigurierbar (Default: 7 Jahre)

### Epic 6: Instance Admins Can Manage the Instance Programmatically via Admin API
Admins können alle Verwaltungsoperationen (User CRUD, Room-Verwaltung, Rollen, Compliance-Übersicht) programmatisch via REST-API ausführen. Die OpenAPI-Spec ist live abrufbar.
**FRs covered:** FR22, FR23, FR24, FR36, FR37, FR38, FR39, FR40, FR51, FR52
**Zusätzliche Deliverables:** OpenAPI Spec-First Workflow (gateway/api/openapi.yaml → oapi-codegen → api_gen.go), alle Admin API Endpoints (/api/v1/admin/*, /api/v1/compliance/*), Cursor-basierte Pagination, Admin Action Audit Log-Integration (via Epic 5), Admin API Response Format (data/meta/error Wrapper)

### Epic 7: Instance Admins Have a Full Admin UI for Day-to-Day Operations
Admins können Nutzer, Rooms und Rollen über eine vollständige Web-UI verwalten, den Compliance-Workflow genehmigen, den Audit Log einsehen — alles WCAG 2.1 AA konform, vollständig tastaturnavigierbar, mit bookmarkbaren URLs.
**FRs covered:** FR22, FR23, FR24, FR36, FR37, FR38, FR39, FR40, FR48, FR49, FR50 (vollständig)
**UX-DRs covered:** UX-DR1 (Obsidian Color System vollständig), UX-DR2 (Typografie), UX-DR7–UX-DR20 (C4–C14 alle Custom Components), UX-DR22 (Compliance-Wizard UI), UX-DR24–UX-DR31 (WCAG, Reduced-Motion, Search+Filter, Load-More, Confirmation-Patterns, etc.)
**Zusätzliche Deliverables:** MasterDetailLayout (C4/C5) für User/Rooms/Roles, Compliance-Wizard UI (C6+C9+C13), Audit Log View (Atlas-Pattern), InlineEdit (C12), vollständige ARIA/WCAG-Implementierung, Skeleton-States vollständig, Search+Filter (Echtzeit debounced + Load-More), Instanz-Branding vorbereitet (Phase 2)

---

## Epic 3: Operators Have a Minimal Admin UI for Bootstrap and Debugging

### Story 3.1: Go Template Engine Setup + go:embed

**As an** operator,
**I want** the Admin UI to be served as pre-rendered HTML from the Go gateway binary,
**so that** no separate frontend server is needed and the binary is fully self-contained.

**Size:** XS

**Acceptance Criteria:**

- `gateway/internal/admin/` package exists with a `Handler` struct that embeds templates via `//go:embed templates/**/*`
- Templates live under `gateway/internal/admin/templates/`; the embed directive compiles them into the binary at build time
- A `render(w http.ResponseWriter, name string, data any)` helper executes the named template and writes the response; it sets `Content-Type: text/html; charset=utf-8`
- If template execution fails, it writes `500 Internal Server Error` (no panic)
- `go build ./...` succeeds with all embedded files present
- A unit test verifies that `render()` with a valid template name produces non-empty HTML output

---

### Story 3.2: Tailwind CSS + DaisyUI Build Pipeline

**As a** developer,
**I want** Tailwind CSS (standalone CLI) + DaisyUI to compile into a single `admin.css` that is embedded in the binary,
**so that** the Admin UI has consistent styling without a CDN dependency.

**Size:** XS

**Acceptance Criteria:**

- `Makefile` target `make build-admin-css` runs the Tailwind standalone CLI (via Docker container, no local install) reading `gateway/internal/admin/tailwind.config.js` and writing output to `gateway/internal/admin/static/admin.css`
- `tailwind.config.js` declares `daisyui` as a plugin and scans `gateway/internal/admin/templates/**/*.html` for class usage
- `gateway/internal/admin/static/admin.css` is embedded via `//go:embed static/admin.css` into the admin handler package
- The CSS is served at `GET /admin/static/admin.css` with `Content-Type: text/css` and a `Cache-Control: public, max-age=31536000, immutable` header
- The Obsidian dark theme variables from UX-DR1 are declared as CSS custom properties in `tailwind.config.js` (e.g., `--color-base-300`, `--color-primary`)
- `make build-gateway` runs `build-admin-css` as a prerequisite so the CSS is always fresh before the Go build

---

### Story 3.3: Self-hosted WOFF2 Fonts + Static Asset Serving

**As an** operator,
**I want** all fonts (Inter, JetBrains Mono) served from the gateway binary,
**so that** the Admin UI loads correctly in air-gapped environments with no external font requests.

**Size:** XS

**Acceptance Criteria:**

- WOFF2 font files for Inter (Regular, Medium, SemiBold) and JetBrains Mono (Regular) are placed in `gateway/internal/admin/static/fonts/`
- All font files are embedded via `//go:embed static/fonts/*`
- Font files are served at `GET /admin/static/fonts/<filename>` with `Content-Type: font/woff2` and `Cache-Control: public, max-age=31536000, immutable`
- A `@font-face` declaration in the base CSS references `/admin/static/fonts/<filename>` (not an external URL)
- Tailwind config maps `fontFamily.sans` to `Inter` and `fontFamily.mono` to `JetBrains Mono`
- `go build ./...` succeeds with fonts embedded; binary size increase is acceptable (fonts < 1 MB total)
- No browser request to `fonts.googleapis.com` or any CDN appears when loading the Admin UI

---

### Story 3.4: Base Layout Template (Nav, Shell, Slots)

**As an** operator,
**I want** a consistent shell layout (topbar, sidebar nav, content area) for all Admin UI pages,
**so that** every page looks coherent without duplicating markup.

**Size:** S

**Acceptance Criteria:**

- `gateway/internal/admin/templates/layouts/base.html` defines blocks: `{{ block "title" . }}`, `{{ block "content" . }}`, `{{ block "scripts" . }}`
- The layout includes the topbar (logo + `TopbarStatusIndicator` C3 placeholder: amber dot "Connecting..." on initial load), a left sidebar nav, and a main content area
- Sidebar nav items: **Bootstrap** (shown only when server is in bootstrap mode), **Dashboard**, **Logout**
- Each nav item renders an `<a>` tag with `href` and a `data-navkey` attribute matching its route key (e.g., `data-navkey="dashboard"`)
- The layout loads `/admin/static/admin.css` and `/admin/static/fonts/inter.woff2` (via `@font-face` in CSS)
- The `{{ block "scripts" . }}` slot is empty by default; pages that need Vue.js append their script tags here
- All HTML is valid and passes basic linting (no unclosed tags, no duplicate IDs)
- A smoke test renders the base layout with empty content and asserts the topbar and sidebar are present in the output

---

### Story 3.5: Active Navigation URL State

**As an** operator,
**I want** the current page's nav item to be visually highlighted in the sidebar,
**so that** I always know where I am in the Admin UI.

**Size:** XS

**Acceptance Criteria:**

- The `render` helper (Story 3.1) accepts a `data` struct that includes `ActiveNav string`
- The base layout template compares each nav item's `data-navkey` to `ActiveNav`; if they match, the DaisyUI `active` class is applied to the `<a>` tag
- Each admin route handler sets `ActiveNav` to the correct key before calling `render`
- A unit test renders the base layout with `ActiveNav: "dashboard"` and asserts the dashboard nav item has the `active` class while others do not
- The topbar title reflects the page title set via the `{{ block "title" . }}` slot

---

### Story 3.6: Bootstrap Detection Middleware

**As a** gateway,
**I want** to detect whether the server is in Bootstrap Mode on every admin request,
**so that** unauthenticated operators are redirected to the Bootstrap Wizard and authenticated operators see the full Admin UI.

**Size:** XS

**Acceptance Criteria:**

- `gateway/internal/admin/middleware.go` contains a `BootstrapGuard` middleware
- `BootstrapGuard` queries `server_config` via the existing DB connection: `SELECT value FROM server_config WHERE key = 'bootstrap_completed'`
- If `bootstrap_completed` is not set (or `false`) AND the request path is not `/admin/bootstrap*`, redirect `302` to `/admin/bootstrap`
- If `bootstrap_completed = true` AND the request path is `/admin/bootstrap*`, redirect `302` to `/admin/login`
- The middleware is registered on the `/admin/*` route group before any page handler
- A unit test with a mock DB covers: (a) bootstrap not complete → redirect, (b) bootstrap complete, accessing bootstrap URL → redirect to login, (c) bootstrap complete, accessing dashboard → no redirect

---

### Story 3.7: Bootstrap UI: Welcome Page + Setup Form

**As an** operator,
**I want** a guided Bootstrap Wizard with four steps,
**so that** I can configure my Nebu instance (instance name, OIDC provider, keys) through a clear UI without editing config files.

**Size:** S

**Acceptance Criteria:**

- `GET /admin/bootstrap` serves the Bootstrap Wizard page (step 1 of 4)
- Step 1 — Instance Name: a form field for `instance_name` (required, 3–64 chars, alphanumeric + hyphens)
- Step 2 — OIDC Configuration: fields for `oidc_issuer` (URL), `oidc_client_id` (string), `oidc_client_secret` (password input); a **Test Connection** button that calls `POST /admin/bootstrap/test-oidc` and displays success/error inline without full page reload (fetch API, no Vue)
- Step 3 — Key Generation: a static info panel explaining Ed25519 + X25519 keypairs will be generated server-side; a **Generate Keys** button that calls `POST /admin/bootstrap/generate-keys` and displays the public Ed25519 fingerprint for verification
- Step 4 — First Admin: instruction text "Complete setup by logging in with your OIDC provider. The first user to log in will be assigned `instance_admin`."
- Each step has a **Next** / **Back** button; state is preserved via hidden form fields across step navigation
- Client-side validation (HTML5 `required`, `pattern`) prevents form submission with empty required fields
- The wizard page renders within the base layout with `ActiveNav: "bootstrap"`
- A unit test renders each step and asserts the correct form fields are present

---

### Story 3.8: Bootstrap API Handler (POST /admin/bootstrap)

**As a** gateway,
**I want** a `POST /admin/bootstrap` handler that validates and persists the Bootstrap configuration,
**so that** the wizard data is saved to `server_config` and Bootstrap Mode is permanently deactivated.

**Size:** S

**Acceptance Criteria:**

- `POST /admin/bootstrap` accepts `application/x-www-form-urlencoded` with fields: `instance_name`, `oidc_issuer`, `oidc_client_id`, `oidc_client_secret`
- Handler validates: `instance_name` matches `^[a-zA-Z0-9-]{3,64}$`; `oidc_issuer` is a valid HTTPS URL; `oidc_client_id` non-empty; `oidc_client_secret` non-empty
- On validation failure: re-render the wizard step with an inline `AlertItem` (C2) showing the error; HTTP 422
- On success: inserts into `server_config`:
  - `instance_name` → `instance_name`
  - `oidc_issuer` → `oidc_issuer`
  - `oidc_client_id` → `oidc_client_id`
  - `oidc_client_secret` → `oidc_client_secret` (stored encrypted via the internal secret)
  - `bootstrap_completed` → `true`
- The insert uses the append-only RLS policy (INSERT only, no UPDATE/DELETE possible)
- After successful insert: redirect `303` to `/admin/login`
- `POST /admin/bootstrap/test-oidc` performs OIDC Discovery (`GET <issuer>/.well-known/openid-configuration`) and returns `200 {"ok": true}` or `200 {"ok": false, "error": "<reason>"}`
- `POST /admin/bootstrap/generate-keys` calls `gRPC CoreService.GenerateUserKeys` (or generates via Go `crypto/ed25519` + `golang.org/x/crypto/curve25519` if Core not yet available) and returns `200 {"ed25519_public_fingerprint": "<hex>"}`
- All three endpoints have unit tests covering happy path and error cases

---

### Story 3.9: Admin OIDC Login Flow (PKCE + State)

**As an** operator,
**I want** to log in to the Admin UI via OIDC with PKCE,
**so that** the Admin UI never handles my password and the authorization code cannot be intercepted.

**Size:** S

**Acceptance Criteria:**

- `GET /admin/login` renders a login page with a single **Login with SSO** button that links to `GET /admin/login/start`
- `GET /admin/login/start`:
  - Generates a PKCE code verifier using `golang.org/x/oauth2.GenerateVerifier()` and the S256 challenge via `oauth2.S256ChallengeOption`
  - Generates a cryptographically random `state` parameter (16 bytes, hex-encoded)
  - Stores `code_verifier` and `state` in a short-lived signed cookie (`admin_oidc_state`, `HttpOnly`, `Secure`, `SameSite=Lax`, TTL 10 min)
  - Redirects `302` to the OIDC authorization endpoint with `response_type=code`, `scope=openid profile email`, `code_challenge`, `code_challenge_method=S256`, `state`, and `redirect_uri=/admin/callback`
- The OIDC issuer URL is read from `server_config` (populated during Bootstrap)
- If `server_config` does not contain `oidc_issuer`, the handler returns `503` with a human-readable error page
- A unit test verifies the redirect URL contains `code_challenge_method=S256` and a non-empty `state`
- A unit test verifies the `admin_oidc_state` cookie is set with `HttpOnly` and `Secure` flags

---

### Story 3.10: OIDC Callback Handler + Session Cookie

**As a** gateway,
**I want** to validate the OIDC callback, exchange the authorization code for tokens, and create an admin session,
**so that** only authenticated and authorized operators can access the Admin UI.

**Size:** S

**Acceptance Criteria:**

- `GET /admin/callback` handler:
  - Reads `state` and `code` from query params
  - Reads `admin_oidc_state` cookie; verifies `state` matches; returns `400` if mismatch (CSRF protection)
  - Exchanges `code` + `code_verifier` (from cookie) for tokens via `oauth2.Exchange` with `oauth2.VerifierOption(verifier)` (PKCE)
  - Validates the returned ID token (signature, `iss`, `aud`, `exp`) using the OIDC provider's JWKS
  - Extracts `sub`, `email` claims from the ID token
  - Checks that the user has `instance_admin` role via claim mapping configured in `server_config`; if not: `403` with error page
  - Creates a signed admin session token (HMAC-SHA256 with internal secret, payload: `sub`, `email`, `exp = now+8h`)
  - Sets `admin_session` cookie: `HttpOnly`, `Secure`, `SameSite=Strict`, TTL 8h
  - Deletes the `admin_oidc_state` cookie
  - Redirects `303` to `/admin/dashboard`
- `GET /admin/logout`:
  - Deletes the `admin_session` cookie (sets `Max-Age=0`)
  - Redirects `303` to `/admin/login`
- Unit tests cover: state mismatch → 400, role check failure → 403, valid flow → session cookie set + redirect

---

### Story 3.11: Admin Session Middleware (Cookie Validation)

**As a** gateway,
**I want** all protected Admin UI routes to validate the admin session cookie before serving content,
**so that** unauthenticated requests are always redirected to `/admin/login`.

**Size:** XS

**Acceptance Criteria:**

- `gateway/internal/admin/middleware.go` contains a `SessionGuard` middleware
- `SessionGuard` reads the `admin_session` cookie; if absent or unparseable: redirect `302` to `/admin/login`
- Validates the HMAC-SHA256 signature using the internal secret; if invalid: redirect `302` to `/admin/login`
- Checks `exp` claim; if expired: redirect `302` to `/admin/login`
- If valid: stores `sub` and `email` in the request context (`context.WithValue`) for downstream handlers
- `SessionGuard` is applied to all `/admin/*` routes except `/admin/login*`, `/admin/callback`, `/admin/bootstrap*`, and `/admin/static/*`
- Unit tests cover: missing cookie → redirect, invalid signature → redirect, expired → redirect, valid → handler called with context values

---

### Story 3.12: Error Pages (401, 403, 404, 500)

**As an** operator,
**I want** clear, on-brand error pages for all HTTP error conditions in the Admin UI,
**so that** I understand what went wrong without seeing a raw browser error or blank page.

**Size:** XS

**Acceptance Criteria:**

- `gateway/internal/admin/templates/errors/` contains: `401.html`, `403.html`, `404.html`, `500.html`
- Each template extends the base layout and renders within the same shell (topbar + sidebar)
- 401 page: heading "Authentication Required", message "Your session has expired or you are not logged in.", link **Log in again** → `/admin/login`
- 403 page: heading "Access Denied", message "You do not have permission to access this page.", link **Back to Dashboard** → `/admin/dashboard`
- 404 page: heading "Page Not Found", message "The page you requested does not exist.", link **Back to Dashboard** → `/admin/dashboard`
- 500 page: heading "Internal Server Error", message "Something went wrong. Please try again or contact your system administrator.", no stack trace exposed
- `gateway/internal/admin/errors.go` exports helpers `Error401(w, r)`, `Error403(w, r)`, `Error404(w, r)`, `Error500(w, r)` that set the correct HTTP status code and render the corresponding template
- The `/admin/*` router registers a `NotFound` handler that calls `Error404`
- A unit test for each helper asserts correct HTTP status code and that the response body contains the expected heading

---

### Story 3.13: Dashboard Page (SSR Metrics Skeleton)

**As an** operator,
**I want** a Dashboard page that shows system health at a glance,
**so that** I can immediately see whether Nebu is functioning correctly during Epic 4 chat tests.

**Size:** S

**Acceptance Criteria:**

- `GET /admin/dashboard` is served only to authenticated sessions (via `SessionGuard`)
- The page renders within the base layout with `ActiveNav: "dashboard"`
- SSR section — Status Cards (C1): three `StatusCard` components rendered server-side showing:
  - **Gateway** — always GREEN (if this page loads, the gateway is up)
  - **Core (gRPC)** — status fetched via `gRPC CoreService.HealthCheck` at request time; GREEN / AMBER / RED with label
  - **Database** — status from a `SELECT 1` ping at request time; GREEN / RED with label
- SSR section — Server Info: instance name (from `server_config`), server uptime (from `os.ReadFile("/proc/uptime")` or equivalent), Go version
- SSE Live Metrics section: an empty `<div id="live-metrics">` placeholder that Vue.js (Story 3.14) will hydrate; renders a static "Connecting…" skeleton while JS loads
- If `gRPC CoreService.HealthCheck` fails, the Core card shows RED with message "Core unreachable"; no page-level error
- The `TopbarStatusIndicator` (C3) in the base layout reflects the worst status of the three cards: GREEN all OK, AMBER one degraded, RED one down
- A unit test renders the dashboard with mocked HealthCheck=OK and DB ping=OK and asserts all three cards show "green" CSS class

---

### Story 3.14: Vue.js Self-hosted + SSE Live Metrics Widget

**As an** operator,
**I want** live-updating metrics (message throughput, active sessions) on the Dashboard,
**so that** I can monitor Nebu in real time without page reloads during chat tests.

**Size:** S

**Acceptance Criteria:**

- `vue.esm-browser.prod.js` (Vue 3, ESM build) is placed in `gateway/internal/admin/static/vendor/` and embedded via `//go:embed`; served at `/admin/static/vendor/vue.esm-browser.prod.js` — no CDN
- `GET /admin/sse/metrics` is an SSE endpoint (Content-Type: `text/event-stream`):
  - Sends an initial `event: metrics` with JSON payload: `{"msg_per_sec": <float>, "active_sessions": <int>, "room_count": <int>}` immediately on connect
  - Sends updated payload every 5 seconds
  - Metrics are fetched from `gRPC CoreService.GetMetrics`; if gRPC fails, sends `event: error` with `{"error": "core unavailable"}` and keeps connection open for retry
  - Sends `event: ping` every 30 seconds to keep the connection alive
- The Dashboard template's `{{ block "scripts" . }}` block loads the self-hosted Vue.js and a `metrics-widget.js` inline script
- `metrics-widget.js` mounts a Vue app on `#live-metrics`, connects to `/admin/sse/metrics` via `EventSource`, and renders:
  - `msg/s`: formatted number (e.g., `12.4 msg/s`)
  - `Active Sessions`: integer
  - `Rooms`: integer
  - On SSE error: show "Core unreachable" badge in amber
- The `TopbarStatusIndicator` (C3) in the topbar is updated by the Vue app to reflect current Core health (GREEN / AMBER / RED) via a reactive class binding
- An integration test verifies `GET /admin/sse/metrics` returns `Content-Type: text/event-stream` and the first `event: metrics` line within 2 seconds

---

### Story 3.15: Gherkin: Bootstrap + Dashboard Flow

**As a** developer,
**I want** Gherkin acceptance tests that verify the full Bootstrap Wizard and Dashboard access,
**so that** regressions in the admin setup flow are caught automatically in CI.

**Size:** S

**Acceptance Criteria:**

- `tests/features/admin_bootstrap.feature` contains a scenario: **Bootstrap Wizard completes successfully**
  - **Given** the server has no `bootstrap_completed` in `server_config`
  - **When** `GET /admin/dashboard` is requested
  - **Then** response redirects to `/admin/bootstrap`
  - **When** `POST /admin/bootstrap` is submitted with valid instance name, Dex issuer (`http://dex:5556/dex`), client ID (`nebu-admin`), client secret (`nebu-admin-secret`)
  - **Then** `server_config` contains `bootstrap_completed = true`
  - **And** response redirects to `/admin/login`

- `tests/features/admin_bootstrap.feature` contains a second scenario: **Dashboard accessible after authentication**
  - **Given** bootstrap is complete
  - **And** a valid admin session cookie (obtained via OIDC login with Dex static user `admin@nebu.local / changeme`)
  - **When** `GET /admin/dashboard` is requested with the session cookie
  - **Then** response is `200` and body contains "Dashboard"
  - **And** body contains a StatusCard for "Gateway" with class "green"

- `tests/features/admin_bootstrap.feature` contains a third scenario: **Unauthenticated request is redirected**
  - **Given** bootstrap is complete
  - **When** `GET /admin/dashboard` is requested without a session cookie
  - **Then** response redirects `302` to `/admin/login`

- All step definitions are implemented in Go (Godog) under `tests/steps/admin_bootstrap_steps.go`
- The scenarios run as part of `make test-integration` against the full Docker Compose stack (gateway + dex + postgres)
- All three scenarios pass green

---

## Epic 4: End-Users Can Chat in Rooms Using Any Standard Matrix Client

### Story 4.1: Horde Registry + DynamicSupervisor Setup

**As a** Core developer,
**I want** Horde Registry and DynamicSupervisor configured in the `room_manager` OTP application,
**so that** Room GenServers can be started, found, and recovered across a clustered Elixir node topology.

**Size:** XS

**Acceptance Criteria:**

- `core/apps/room_manager/mix.exs` declares `{:horde, "~> 0.9"}` as a dependency
- `RoomManager.Application` starts two supervised children: `Horde.Registry` (name: `RoomManager.Registry`) and `Horde.DynamicSupervisor` (name: `RoomManager.Supervisor`, strategy: `:one_for_one`)
- Both are configured with `members: :auto` so they automatically join all nodes in the libcluster topology
- A `RoomManager.RoomSupervisor` module exposes:
  - `start_room(room_id)` — calls `Horde.DynamicSupervisor.start_child/2` with a `{RoomManager.RoomServer, room_id}` child spec
  - `lookup_room(room_id)` — calls `Horde.Registry.lookup/2`; returns `{:ok, pid}` or `{:error, :not_found}`
- `mix test` passes with `--warnings-as-errors`; a unit test verifies `start_room/1` registers the PID in the Horde Registry and `lookup_room/1` returns `{:ok, pid}`

---

### Story 4.2: Room GenServer: Lifecycle (create, join, leave)

**As a** Core developer,
**I want** a `RoomManager.RoomServer` GenServer that manages room membership state,
**so that** Rooms can be created, users can join and leave, and the current member list is always available in memory.

**Size:** S

**Acceptance Criteria:**

- `RoomManager.RoomServer` is a `GenServer` with state: `%{room_id: String.t(), members: MapSet.t(), power_levels: map(), created_at: DateTime.t()}`
- `init/1` loads existing membership from PostgreSQL (`SELECT user_id FROM room_members WHERE room_id = $1`) on start; if the room does not exist in the DB, it inserts a new row into `rooms` and returns empty members
- Handles the following `call` messages:
  - `:get_state` → returns full state map
  - `{:join, user_id}` → adds `user_id` to `members`, inserts into `room_members` DB table, returns `:ok` or `{:error, :already_member}`
  - `{:leave, user_id}` → removes from `members`, soft-deletes from `room_members` (sets `left_at = NOW()`), returns `:ok` or `{:error, :not_member}`
- On any DB write error, the GenServer returns `{:error, reason}` and does not update in-memory state (fail-safe)
- `mix test --warnings-as-errors` passes; unit tests cover: join idempotency (second join returns `:already_member`), leave from non-member returns `:not_member`, state recovery from DB on `init`

---

### Story 4.3: Ed25519 Unit Tests + `Nebu.EventId` Content-Hash Module

**As a** Core developer,
**I want** a dedicated `Nebu.EventId` module that generates Matrix Room Version 6 content-hash Event IDs,
**so that** every event has a deterministic, verifiable, content-addressable identifier.

**Size:** XS

**Acceptance Criteria:**

- `core/apps/signature/lib/nebu/event_id.ex` implements `Nebu.EventId.generate/1`:
  - Accepts an event content map
  - Serializes to canonical JSON (keys sorted, no whitespace) using Jason
  - Computes SHA-256 of the canonical JSON bytes
  - URL-safe Base64-encodes (no padding) the hash
  - Returns `"$" <> encoded_hash` (e.g., `"$abc123..."`)
- `Nebu.EventId.verify/2` accepts an event content map and an event ID string; returns `true` if recomputed ID matches, `false` otherwise
- Unit tests in `test/nebu/event_id_test.exs` cover:
  - Same content always produces the same ID (determinism)
  - Different content produces different IDs (collision resistance)
  - Key ordering does not affect the ID (canonical JSON)
  - `verify/2` returns `true` for a matching ID and `false` for a tampered ID
- Existing Ed25519 unit tests from Story 1.x pass with `--warnings-as-errors`

---

### Story 4.4: Room GenServer: Send Event (Ed25519 Signing + txnId Idempotency)

**As a** Core developer,
**I want** the Room GenServer to process, sign, and persist send-event requests with full txnId idempotency,
**so that** duplicate client requests never result in duplicate events in the room timeline.

**Size:** S

**Acceptance Criteria:**

- `RoomManager.RoomServer` handles `call` message `{:send_event, user_id, event_type, content, txn_id}`:
  - Checks ETS idempotency table `NebuTxnDedup` for `{room_id, user_id, txn_id}`; if found, returns `{:ok, existing_event_id}` immediately without re-processing
  - Constructs the full event map: `%{room_id, event_type, sender: user_id, content, origin_server_ts: DateTime.utc_now() |> DateTime.to_unix(:millisecond)}`
  - Calls `Nebu.EventId.generate/1` to produce the `event_id`
  - Calls `Signature.Ed25519.sign/2` (from Story 2.x) to sign the event; attaches `signatures` field
  - Persists the signed event to PostgreSQL `events` table (append-only)
  - Inserts `{room_id, user_id, txn_id} → event_id` into ETS `NebuTxnDedup`
  - Broadcasts `{:new_event, signed_event}` via `pg` Process Group `"room:#{room_id}"`
  - Returns `{:ok, event_id}`
- ETS table `NebuTxnDedup` is created in `RoomManager.Application.start/2` with type `:set`, access `:public`
- On DB write failure: do not insert into ETS, return `{:error, reason}`
- Unit tests cover: happy path returns deterministic event_id, duplicate txn_id returns same event_id, DB failure does not pollute ETS cache

---

### Story 4.5: Session Manager: ETS Session Store

**As a** Core developer,
**I want** an in-memory ETS-backed Session Manager for active user sessions,
**so that** session lookups during /sync are O(1) and do not hit PostgreSQL on every request.

**Size:** S

**Acceptance Criteria:**

- `core/apps/session_manager/lib/session_manager/ets_store.ex` implements `SessionManager.EtsStore`:
  - ETS table `NebuSessions` created at application start with type `:set`, access `:public`
  - `put_session(user_id, session)` — upserts `{user_id, session_map}` where `session_map` includes `access_token_hash`, `device_id`, `created_at`, `last_seen_at`
  - `get_session(user_id)` → `{:ok, session_map}` or `{:error, :not_found}`
  - `delete_session(user_id)` → `:ok`
  - `list_sessions()` → `[session_map]` (for Admin metrics)
- `access_token_hash` stores `Base16.encode(:crypto.hash(:sha256, access_token))` — never the raw token
- `SessionManager.Application` starts `EtsStore` as a supervised worker
- Unit tests cover: put + get round-trip, delete removes entry, get on missing key returns `{:error, :not_found}`, list returns all current sessions

---

### Story 4.6: Session Manager: PostgreSQL since-Token + Invalidation

**As a** Core developer,
**I want** the Session Manager to persist since-tokens to PostgreSQL and support session invalidation,
**so that** incremental /sync correctly resumes after gateway restarts and logout invalidates the session cluster-wide.

**Size:** S

**Acceptance Criteria:**

- `core/apps/session_manager/lib/session_manager/pg_store.ex` implements `SessionManager.PgStore`:
  - `persist_since_token(user_id, since_token, last_event_id)` — upserts row in `sync_tokens` table (`user_id`, `since_token`, `last_event_id`, `updated_at`)
  - `get_since_token(user_id)` → `{:ok, %{since_token, last_event_id}}` or `{:error, :not_found}`
  - `invalidate_session(user_id)` — deletes from `sync_tokens` and `sessions` tables in a single transaction; also calls `SessionManager.EtsStore.delete_session/1`
- `since_token` is an opaque string: `Base64.encode("#{user_id}:#{last_event_id}:#{System.monotonic_time()}")`
- Migration `20240004_sync_tokens.sql` creates `sync_tokens(user_id TEXT PRIMARY KEY, since_token TEXT NOT NULL, last_event_id TEXT, updated_at TIMESTAMPTZ DEFAULT NOW())`
- `SessionManager.SessionSupervisor` module provides `create_session/2` (calls both ETS + PG stores) and `destroy_session/1` (calls `invalidate_session/1`)
- Unit tests cover: persist + get round-trip, invalidate removes from both ETS and PG, opaque token is not a sequential integer (not guessable)

---

### Story 4.7: Presence Manager

**As a** Core developer,
**I want** a Presence Manager that tracks and broadcasts user online/offline status,
**so that** Matrix clients can display accurate presence indicators.

**Size:** XS

**Acceptance Criteria:**

- `core/apps/presence/lib/presence/manager.ex` implements `Presence.Manager` as a `GenServer`:
  - State: ETS table `NebuPresence` with entries `{user_id, status, last_active_at}` where `status ∈ [:online, :offline, :unavailable]`
  - `set_presence(user_id, status)` — upserts the ETS entry; broadcasts `{:presence_update, user_id, status}` via `pg` Process Group `"presence"`
  - `get_presence(user_id)` → `{:ok, %{status, last_active_at}}` or `{:error, :not_found}`; missing users default to `offline`
  - Heartbeat: if no `set_presence` call for a user within 60 seconds, auto-transition to `:unavailable`; after 5 minutes, transition to `:offline`
- `Presence.Application` starts `Presence.Manager` as a supervised worker
- Unit tests cover: set online → get returns online, heartbeat expiry sets unavailable after mock timeout, missing user defaults to offline

---

### Story 4.8: gRPC EventBus Server-Streaming + GetRoomState Unary

**As a** gateway developer,
**I want** the Core to expose a gRPC `EventBus` server-streaming RPC and a `GetRoomState` unary RPC,
**so that** the gateway can receive real-time events and query current room state without polling.

**Size:** S

**Acceptance Criteria:**

- `proto/core_service.proto` adds to `CoreService`:
  ```protobuf
  rpc EventBus(EventBusRequest) returns (stream EventEnvelope);
  rpc GetRoomState(GetRoomStateRequest) returns (GetRoomStateResponse);
  ```
- `EventBusRequest` contains `gateway_id: string` and `room_ids: repeated string` (subscribe to specific rooms; empty = all)
- `EventEnvelope` contains `room_id`, `event_id`, `event_type`, `sender`, `content_json` (serialized JSON string), `origin_server_ts`
- `GetRoomStateRequest` contains `room_id: string`; `GetRoomStateResponse` contains `members: repeated string`, `power_levels_json: string`, `room_name: string`
- Elixir implementation (`EventDispatcher.GrpcHandler`) subscribes to `pg` Process Group `"room:*"` on `EventBus` stream open; forwards received events as `EventEnvelope` messages to the gRPC stream
- On stream disconnect: handler unsubscribes from `pg`; no crash, no message loss for other streams
- Go gateway `gateway/internal/grpc/event_bus_client.go` opens the `EventBus` stream on startup; on disconnect, retries with exponential backoff (max 30s)
- `make proto` regenerates Go + Elixir stubs without errors
- Integration test: gateway subscribes to EventBus, a test sends an event to Core, the event appears on the stream within 1 second

---

### Story 4.9: POST /createRoom

**As an** end-user,
**I want** to create a new room via the Matrix API,
**so that** I have a space to invite others and exchange messages.

**Size:** S

**Acceptance Criteria:**

- `POST /_matrix/client/v3/createRoom` is authenticated (JWT middleware from Story 2.x)
- Request body (JSON): `room_alias_name` (optional), `name` (optional), `topic` (optional), `visibility` (`public`|`private`, default `private`), `invite` (optional list of user IDs), `preset` (`private_chat`|`public_chat`|`trusted_private_chat`, optional)
- Handler calls `gRPC CoreService.CreateRoom` with the request params; Core starts a new `RoomServer` via `RoomSupervisor.start_room/1`
- Returns `200 {"room_id": "!<random>:<server_name>"}` on success
- Returns `400 M_BAD_JSON` if body is not valid JSON
- Returns `400 M_ROOM_IN_USE` if `room_alias_name` is already taken
- Returns `403 M_FORBIDDEN` if the user is not allowed to create rooms (power level check — all users can create rooms by default in MVP)
- `gRPC CoreService` proto is extended with `rpc CreateRoom(CreateRoomRequest) returns (CreateRoomResponse)`
- Unit test: valid request → 200 with room_id; duplicate alias → 400 M_ROOM_IN_USE

---

### Story 4.10: POST /join + Room Invitations (FR20/21)

**As an** end-user,
**I want** to join a room directly or accept an invitation,
**so that** I can participate in conversations I've been invited to or discover public rooms.

**Size:** S

**Acceptance Criteria:**

- `POST /_matrix/client/v3/join/{roomIdOrAlias}` — joins by room ID or alias; calls `gRPC CoreService.JoinRoom`; returns `200 {"room_id": "!<id>:<server>"}` or `403 M_FORBIDDEN` (private room, not invited) or `404 M_NOT_FOUND`
- `POST /_matrix/client/v3/rooms/{roomId}/invite` — body `{"user_id": "@target:<server>"}`:
  - Caller must be a member of the room
  - Calls `gRPC CoreService.InviteUser`; Core stores the invitation in a `room_invitations` table and broadcasts an `m.room.member` event (membership: `invite`) to the invited user's sync stream
  - Returns `200 {}` on success, `403 M_FORBIDDEN` if caller lacks invite power level, `400 M_BAD_JSON` on invalid body
- `POST /_matrix/client/v3/rooms/{roomId}/join` — accepts a pending invitation; calls `gRPC CoreService.JoinRoom`; returns `200 {"room_id": ...}` or `403 M_FORBIDDEN` if no invitation exists
- `gRPC CoreService` proto adds: `rpc JoinRoom(JoinRoomRequest) returns (JoinRoomResponse)` and `rpc InviteUser(InviteUserRequest) returns (InviteUserResponse)`
- Unit tests: join public room → 200, join private room without invite → 403, invite then join → 200

---

### Story 4.11: PUT /rooms/{roomId}/send/{eventType}/{txnId}

**As an** end-user,
**I want** to send messages (and other events) to a room,
**so that** other members can read my messages in real time.

**Size:** S

**Acceptance Criteria:**

- `PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}` is authenticated
- Handler calls `gRPC CoreService.SendEvent` with `room_id`, `user_id` (from JWT), `event_type`, `content` (JSON body), `txn_id`
- Returns `200 {"event_id": "$<hash>"}` on success
- Returns `403 M_FORBIDDEN` if the user is not a member or lacks send power level
- Returns `400 M_BAD_JSON` if body is not valid JSON
- **Idempotency**: if the same `txn_id` (scoped to `user_id` + `room_id`) is sent twice, the second request returns `200` with the same `event_id` as the first (no duplicate event in timeline)
- `gRPC CoreService` proto adds `rpc SendEvent(SendEventRequest) returns (SendEventResponse)` with `txn_id` field
- Unit tests: happy path returns event_id, duplicate txn_id returns same event_id, non-member returns 403

---

### Story 4.12: GET /rooms/{roomId}/messages

**As an** end-user,
**I want** to fetch the paginated message history of a room,
**so that** I can read previous messages when opening a conversation.

**Size:** S

**Acceptance Criteria:**

- `GET /_matrix/client/v3/rooms/{roomId}/messages` is authenticated; query params: `from` (pagination token, optional), `dir` (`b` backward | `f` forward, default `b`), `limit` (integer 1–100, default 10)
- Handler calls `gRPC CoreService.GetRoomMessages` with `room_id`, `from_token`, `dir`, `limit`
- Returns `200` with Matrix-standard response body:
  ```json
  {
    "start": "<token>",
    "end": "<token>",
    "chunk": [<event objects>],
    "state": []
  }
  ```
- Returns `403 M_FORBIDDEN` if user is not a member of the room
- Returns `404 M_NOT_FOUND` if room does not exist
- Pagination tokens are opaque strings (cursor-based, not offset-based)
- `gRPC CoreService` proto adds `rpc GetRoomMessages(GetRoomMessagesRequest) returns (GetRoomMessagesResponse)`
- Core fetches from PostgreSQL `events` table ordered by `origin_server_ts`; uses keyset pagination on `(origin_server_ts, event_id)`
- Unit tests: first page returns up to `limit` events + `end` token, second page with `from=end` returns next batch, 403 for non-member

---

### Story 4.13: Room Power Levels Enforcement

**As a** room owner,
**I want** power levels to control who can send events, invite, kick, and modify room state,
**so that** rooms have fine-grained access control without requiring a separate permissions service.

**Size:** S

**Acceptance Criteria:**

- `RoomManager.RoomServer` state includes `power_levels` map with defaults:
  ```elixir
  %{
    ban: 50, kick: 50, invite: 0, redact: 50,
    state_default: 50, events_default: 0,
    users_default: 0,
    users: %{},   # per-user overrides
    events: %{}   # per-event-type overrides
  }
  ```
- Room creator is assigned power level `100` in `users` map on room creation
- `RoomManager.PowerLevels.can?/3` accepts `(power_levels, user_id, action)` where `action ∈ [:send_event, :invite, :kick, :ban, :change_state]`; returns `true` or `false`
- `send_event`, `invite`, `join`, `leave` calls in `RoomServer` run the power level check before processing; return `{:error, :forbidden}` if check fails
- `PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}` handler (power level state event `m.room.power_levels`) calls `gRPC CoreService.SetPowerLevels`; only users with level ≥ current state_default can update
- Unit tests: default user can send (level 0 ≥ events_default 0), default user cannot ban (level 0 < 50), room creator (level 100) can do everything, power level update by non-admin is rejected

---

### Story 4.14: GET /sync — Initial Sync

**As an** end-user,
**I want** an initial /sync response that gives me the full current state of all my rooms,
**so that** my Matrix client can build its initial state on first connection.

**Size:** S

**Acceptance Criteria:**

- `GET /_matrix/client/v3/sync` with no `since` parameter performs an initial sync for the authenticated user
- Handler calls `gRPC CoreService.GetInitialSync` with `user_id`; Core returns:
  - All rooms the user is a member of
  - For each room: current `state` events (membership, name, topic, power levels), last N messages (up to 20) in `timeline`, `unread_notifications`
- Response follows Matrix sync response format:
  ```json
  {
    "next_batch": "<since_token>",
    "rooms": {
      "join": { "<room_id>": { "state": {...}, "timeline": {...} } },
      "invite": {},
      "leave": {}
    },
    "presence": { "events": [] }
  }
  ```
- `next_batch` is an opaque since-token from `SessionManager.PgStore.persist_since_token/3`
- If the user has no rooms, returns `200` with empty `rooms` object
- Timeout: initial sync must complete within 5 seconds; if Core is unreachable, returns `503 M_UNAVAILABLE`
- `gRPC CoreService` proto adds `rpc GetInitialSync(GetInitialSyncRequest) returns (GetInitialSyncResponse)`
- Unit test: user with 2 rooms → response contains both room_ids in `rooms.join`; user with no rooms → empty rooms object

---

### Story 4.15: GET /sync — Incremental Sync (Long-Polling + since-token)

**As an** end-user,
**I want** incremental /sync with long-polling to receive new events in near-real-time,
**so that** my Matrix client stays up to date without constant polling.

**Size:** S

**Acceptance Criteria:**

- `GET /_matrix/client/v3/sync?since=<token>&timeout=<ms>` performs an incremental sync
- Handler decodes `since` token via `gRPC CoreService.GetSyncDelta` with `user_id`, `since_token`, `timeout_ms`
- Core subscribes to EventBus events for the user's rooms for `timeout_ms` (max 30000); returns immediately if events are pending since last sync
- If no events arrive before timeout: returns `200` with `{"next_batch": "<same_or_new_token>", "rooms": {}, "presence": {"events": []}}`
- If events arrive: returns `200` with delta containing only rooms with new events since `since_token`
- `next_batch` in the response is always a new token from `SessionManager.PgStore`
- Invalid or expired `since` token: fall back to initial sync response (same as no `since` parameter)
- `gRPC CoreService` proto adds `rpc GetSyncDelta(GetSyncDeltaRequest) returns (GetSyncDeltaResponse)` with `timeout_ms` field
- Integration test: client sends message to room, second client's `/sync?since=<token>` returns that message in the response within 500ms

---

### Story 4.16: message_buffer Drain Strategy (Linear MVP)

**As a** gateway developer,
**I want** a `message_buffer` that absorbs event spikes from the EventBus stream and drains them to connected /sync clients at a controlled rate,
**so that** the gateway does not overwhelm clients or drop events under load.

**Size:** S

**Acceptance Criteria:**

- `gateway/internal/buffer/message_buffer.go` implements `MessageBuffer`:
  - Per-user ring buffer with configurable capacity (default: 500 events, set via `NEBU_BUFFER_CAPACITY`)
  - `Put(user_id, event)` — appends to the user's ring buffer; if full, drops the oldest event and increments a `buffer_overflow_total` Prometheus counter
  - `DrainFor(user_id, max_events int) []Event` — returns up to `max_events` events and removes them from the buffer; non-blocking
  - `WaitFor(ctx context.Context, user_id string) <-chan struct{}` — returns a channel that is closed when at least one event is available for `user_id` (used by long-poll sync handler)
- The EventBus client (Story 4.8) calls `Put` for each received event, routing to the correct user's buffer based on room membership
- The `/sync` long-poll handler calls `WaitFor` with the request context; on context cancellation (client disconnect), returns cleanly
- `buffer_overflow_total` counter is registered in the Prometheus registry from Story 1.x
- Unit tests cover: Put + DrainFor round-trip, overflow drops oldest not newest, WaitFor unblocks after Put, WaitFor returns on context cancellation

---

### Story 4.17: Typing Indicators + Read Receipts

**As an** end-user,
**I want** to send typing indicators and read receipts,
**so that** other room members can see when I'm typing and which messages I've read.

**Size:** XS

**Acceptance Criteria:**

- `PUT /_matrix/client/v3/rooms/{roomId}/typing/{userId}` is authenticated; body: `{"typing": true|false, "timeout": <ms>}`
  - Calls `gRPC CoreService.SetTyping`; Core broadcasts an `m.typing` ephemeral event to all room members via the EventBus
  - Returns `200 {}`; returns `403 M_FORBIDDEN` if `userId` in path does not match the authenticated user
  - Typing state automatically expires after `timeout` ms (max 30000); Core uses a `Process.send_after` in the Room GenServer to clear the typing flag
- `POST /_matrix/client/v3/rooms/{roomId}/receipt/{receiptType}/{eventId}` is authenticated; `receiptType` must be `m.read`
  - Calls `gRPC CoreService.SendReceipt`; Core updates the user's read marker in the `read_receipts` table
  - Returns `200 {}`; returns `403 M_FORBIDDEN` if user is not a member
- `gRPC CoreService` proto adds `rpc SetTyping(SetTypingRequest) returns (SetTypingResponse)` and `rpc SendReceipt(SendReceiptRequest) returns (SendReceiptResponse)`
- Unit tests: typing=true → 200, userId mismatch → 403, valid receipt → 200, non-member receipt → 403

---

### Story 4.18: Profile + Presence API

**As an** end-user,
**I want** to view and update my profile and check the presence status of other users,
**so that** my display name and avatar are correct and I can see who is online.

**Size:** XS

**Acceptance Criteria:**

- `GET /_matrix/client/v3/profile/{userId}` — unauthenticated; returns `200 {"displayname": "...", "avatar_url": "mxc://..."}` or `404 M_NOT_FOUND`
- `PUT /_matrix/client/v3/profile/{userId}/displayname` — authenticated; body `{"displayname": "..."}` (1–128 chars); updates `profiles` table; returns `200 {}` or `403 M_FORBIDDEN` if path `userId` ≠ authenticated user
- `PUT /_matrix/client/v3/profile/{userId}/avatar_url` — authenticated; body `{"avatar_url": "mxc://..."}` (must be `mxc://` URI); updates `profiles` table; returns `200 {}` or `403 M_FORBIDDEN` or `400 M_INVALID_PARAM` if URL is not an mxc URI
- `GET /_matrix/client/v3/presence/{userId}/status` — authenticated; calls `gRPC CoreService.GetPresence`; returns `200 {"presence": "online"|"offline"|"unavailable", "last_active_ago": <ms>}` or `404 M_NOT_FOUND`
- `gRPC CoreService` proto adds `rpc GetPresence(GetPresenceRequest) returns (GetPresenceResponse)` and `rpc UpdateProfile(UpdateProfileRequest) returns (UpdateProfileResponse)`
- Unit tests: GET profile for existing user → 200 with fields, PUT displayname by correct user → 200, PUT by wrong user → 403, invalid avatar_url → 400, GET presence → correct status

---

### Story 4.19: Media Gateway: Upload (AES-256-GCM + Size Limit)

**As an** end-user,
**I want** to upload files and images to Nebu,
**so that** I can share media in rooms with other members.

**Size:** S

**Acceptance Criteria:**

- `POST /_matrix/media/v3/upload` is authenticated; accepts `Content-Type: */*`, body is the raw file bytes
- **Size limit**: Gateway reads `Content-Length` header before streaming; if `Content-Length > NEBU_MEDIA_MAX_UPLOAD_BYTES` (default 50 MB), returns `413 M_TOO_LARGE` before reading body
- If `Content-Length` absent, streams body and enforces limit via a counting reader; aborts at limit
- Handler:
  1. Generates a random `media_id` (UUID v4)
  2. Generates a random 256-bit AES key and 96-bit nonce
  3. Encrypts file bytes with AES-256-GCM: `ciphertext || auth_tag`
  4. Writes encrypted bytes to `NEBU_MEDIA_STORAGE_PATH/<server_name>/<media_id>` (local filesystem)
  5. Stores `{media_id, server_name, content_type, file_size, aes_key_hex, nonce_hex, uploader_user_id, uploaded_at}` in `media_files` table
- Returns `200 {"content_uri": "mxc://<server_name>/<media_id>"}`
- Migration creates `media_files` table with above columns
- Unit tests: valid upload returns mxc URI, oversized upload returns 413, encrypted file on disk is not plaintext

---

### Story 4.20: Media Gateway: Download + Decryption

**As an** end-user,
**I want** to download media files from Nebu,
**so that** I can view images and files shared in rooms.

**Size:** XS

**Acceptance Criteria:**

- `GET /_matrix/media/v3/download/{serverName}/{mediaId}` — unauthenticated (Matrix spec allows unauthenticated media download)
- Handler looks up `media_files` by `server_name` + `media_id`; returns `404 M_NOT_FOUND` if absent
- Reads encrypted file from `NEBU_MEDIA_STORAGE_PATH/<server_name>/<media_id>`
- Decrypts with AES-256-GCM using stored key + nonce; if authentication tag verification fails, returns `500 M_UNKNOWN` (tampered file)
- Streams decrypted bytes with original `Content-Type` header and `Content-Disposition: inline; filename="<media_id>"`
- `GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}` returns `501 M_UNRECOGNIZED` (thumbnails: Phase 2)
- Unit test: upload then download round-trip returns identical bytes; tampered file returns 500

---

### Story 4.21: Gherkin: Room Create + Send + Receive (End-to-End)

**As a** developer,
**I want** Gherkin acceptance tests covering the full chat flow from room creation to message receipt,
**so that** regressions in the core messaging path are caught automatically in CI.

**Size:** S

**Acceptance Criteria:**

- `tests/features/chat_flow.feature` contains scenario: **User creates a room and sends a message**
  - **Given** two authenticated users `alice` and `bob` (OIDC tokens from Dex)
  - **When** `alice` calls `POST /_matrix/client/v3/createRoom` with `name: "test-room"`
  - **Then** response is `200` with a `room_id`
  - **When** `alice` calls `POST /_matrix/client/v3/rooms/{room_id}/invite` with `bob`'s user_id
  - **And** `bob` calls `POST /_matrix/client/v3/join/{room_id}`
  - **And** `alice` calls `PUT /_matrix/client/v3/rooms/{room_id}/send/m.room.message/{txnId}` with body `{"msgtype":"m.text","body":"hello"}`
  - **Then** response is `200` with an `event_id`
  - **When** `bob` calls `GET /_matrix/client/v3/sync?since=<bob_token>`
  - **Then** the sync response contains the message with body "hello" in `rooms.join.<room_id>.timeline.events`

- `tests/features/chat_flow.feature` contains scenario: **txnId idempotency**
  - **Given** `alice` is in a room
  - **When** `alice` sends the same `txnId` twice
  - **Then** both responses return `200` with the identical `event_id`
  - **And** the room timeline contains the message exactly once

- All step definitions implemented under `tests/steps/chat_flow_steps.go`
- Scenarios run as part of `make test-integration`; both pass green

---

### Story 4.22: Matrix Client Smoke Test (matrix-js-sdk HTTP Level)

**As a** developer,
**I want** a smoke test that verifies a real Matrix client SDK can connect and exchange a message with Nebu,
**so that** Matrix protocol compatibility is continuously validated.

**Size:** S

**Acceptance Criteria:**

- `tests/matrix_compat/smoke_test.js` uses `matrix-js-sdk` (pinned version, installed via `npm ci` in a Node.js test container)
- Test sequence:
  1. Create a `MatrixClient` pointing to `http://gateway:8080` with a valid Dex-issued access token
  2. Call `client.startClient()`; verify `client.isInitialSyncComplete()` becomes `true` within 10 seconds
  3. Create a room via `client.createRoom({name: "sdk-smoke"})`
  4. Send a message via `client.sendMessage(roomId, {msgtype: "m.text", body: "smoke"})`
  5. Wait for the `Room.timeline` event; assert the message body equals "smoke"
  6. Call `client.stopClient()`
- `Makefile` target `make test-matrix-compat` runs the Node.js container with `node tests/matrix_compat/smoke_test.js`; exits 0 on success
- Test is listed as an optional gate in CI (not blocking `make test-integration`) but documented in `README.md` as the canonical Matrix compatibility check
- The test must not use any Nebu-internal APIs — only standard Matrix Client-Server endpoints

---

### Story 4.23: Load Test: Silber-Tier ≥500 Concurrent (k6 Setup + Run)

**As a** developer,
**I want** an automated load test that validates Nebu meets the Silber-Tier performance target,
**so that** performance regressions are caught before they reach production.

**Size:** S

**Acceptance Criteria:**

- `tests/load/k6_chat.js` is a k6 script (k6 v0.50+):
  - Ramp up to 500 virtual users over 60 seconds
  - Each VU: authenticate via Dex, create or join a room, send 1 message per second for 120 seconds, poll `/sync` after each send
  - Thresholds declared in the script:
    - `http_req_duration{name:send_event}` p95 < 200ms
    - `http_req_duration{name:sync}` p95 < 500ms
    - `http_req_failed` rate < 0.1%
- `Makefile` target `make test-load` runs k6 via Docker (`grafana/k6:0.50.0`) against `NEBU_LOAD_TARGET_URL` (default: `http://gateway:8080`)
- The load test runs against a 2-node Elixir cluster + 1 PostgreSQL instance (representative of the Silber deployment profile)
- A `tests/load/README.md` documents how to interpret k6 output and what "Silber-Tier" means in terms of hardware
- `make test-load` is not part of `make test-integration` (too slow for CI); it is documented as a manual pre-release gate
- After a successful run at 500 VUs, all thresholds pass green and k6 exits with code 0

---

## Epic 5: Compliance Officers Can Securely Request and Export Audited Communication Data

### Story 5.1: Audit Log Schema + RLS + Aufbewahrungskonfiguration

**As an** instance admin,
**I want** an append-only audit log table in PostgreSQL with row-level security,
**so that** every compliance-relevant and admin action is permanently recorded and cannot be altered or deleted.

**Size:** XS

**Acceptance Criteria:**

- Migration `20240005_audit_log.sql` creates table `audit_log`:
  - `id BIGSERIAL PRIMARY KEY`
  - `event_time TIMESTAMPTZ NOT NULL DEFAULT NOW()`
  - `actor_user_id TEXT NOT NULL`
  - `action TEXT NOT NULL` (e.g., `compliance_access_requested`, `bootstrap_completed`, `user_deleted`)
  - `target_type TEXT` (e.g., `user`, `room`, `compliance_request`)
  - `target_id TEXT`
  - `metadata JSONB`
  - `outcome TEXT NOT NULL` (e.g., `success`, `failure`, `attempted`)
  - `error_detail TEXT`
- PostgreSQL RLS policy on `audit_log`: `INSERT` allowed for the application DB role; `UPDATE` and `DELETE` explicitly denied (`USING (false)`)
- `server_config` key `audit_log_retention_days` is seeded with default `2555` (7 years) during Bootstrap (Story 3.8 inserts it alongside `bootstrap_completed`)
- A `pg_cron` job (or equivalent application-level scheduled task) deletes rows where `event_time < NOW() - INTERVAL '1 day' * retention_days`; the retention value is read from `server_config` at runtime
- Unit test verifies: INSERT succeeds, DELETE raises a policy violation error

---

### Story 5.2: Audit Log Writer (generisch, alle Admin-Aktionen, atomare Garantie)

**As a** developer,
**I want** a generic Audit Log Writer module usable by all application layers,
**so that** every admin action, compliance event, and system event is consistently recorded — including error cases where the primary operation failed.

**Size:** S

**Acceptance Criteria:**

- `core/apps/compliance/lib/compliance/audit_writer.ex` implements `Compliance.AuditWriter`:
  - `log(actor_user_id, action, target_type, target_id, metadata, outcome, error_detail \\ nil)` — inserts one row into `audit_log` in a separate DB transaction from the calling operation
  - Uses `Ecto.Multi` internally but runs in its own `Repo.transaction/1` — decoupled from the caller's transaction so the audit entry is written even if the caller's transaction rolls back
  - Returns `:ok` on success; on DB failure, logs the write failure to the Elixir Logger at `:error` level and returns `{:error, :audit_write_failed}` — never raises
- `Compliance.AuditWriter` is available as a supervised worker in `Compliance.Application`
- Go gateway `gateway/internal/audit/writer.go` exposes `LogEvent(ctx, actorUserID, action, targetType, targetID, metadata, outcome, errorDetail string) error` which calls `gRPC CoreService.WriteAuditLog`
- `gRPC CoreService` proto adds `rpc WriteAuditLog(WriteAuditLogRequest) returns (WriteAuditLogResponse)`
- Integration points documented: Story 3.7/3.8 (Bootstrap), Story 3.9/3.10 (Admin login/logout), Story 4.9/4.10 (Room operations) must call `AuditWriter.log/6` for their key actions — ACs for those stories are considered incomplete without this call
- Unit tests: successful log inserts a row, DB failure returns `{:error, :audit_write_failed}` without raising, the audit insert transaction is independent of the caller's transaction (test with deliberate caller rollback)

---

### Story 5.3: Compliance Access Request API

**As a** compliance officer,
**I want** to submit a formal access request for specific room message data within a defined time range,
**so that** I can initiate the Four-Eyes review process with a documented justification.

**Size:** S

**Acceptance Criteria:**

- `POST /api/v1/compliance/access-requests` is authenticated (JWT middleware); caller must have `compliance_officer` role (checked via claim mapping); returns `403 M_FORBIDDEN` otherwise
- Request body (JSON):
  - `room_id: string` (required) — must be a valid room ID existing in the DB; returns `404` if not found
  - `time_range_start: string` (required) — ISO 8601 timestamp
  - `time_range_end: string` (required) — ISO 8601 timestamp; must be after `time_range_start`; returns `400` if invalid
  - `justification: string` (required) — minimum 20 characters; returns `400 M_BAD_JSON` if too short
- Handler inserts into `compliance_requests` table:
  - `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
  - `requester_user_id TEXT NOT NULL`
  - `room_id TEXT NOT NULL`
  - `time_range_start TIMESTAMPTZ NOT NULL`
  - `time_range_end TIMESTAMPTZ NOT NULL`
  - `justification TEXT NOT NULL`
  - `status TEXT NOT NULL DEFAULT 'pending'` (`pending` | `approved` | `rejected`)
  - `approver_user_id TEXT`
  - `created_at TIMESTAMPTZ DEFAULT NOW()`
- On success: calls `AuditWriter.log(requester, "compliance_access_requested", "room", room_id, %{justification_length: len}, "success")`
- Returns `201 {"request_id": "<uuid>", "status": "pending"}`
- Migration `20240006_compliance_requests.sql` creates the `compliance_requests` table
- Unit tests: valid request → 201 with UUID, missing justification → 400, non-compliance-officer → 403, unknown room_id → 404

---

### Story 5.4: Four-Eyes Approval API + Admin-Dashboard Pending-Badge

**As a** second compliance officer,
**I want** to view and approve or reject pending compliance access requests,
**so that** no single officer can gain unilateral access to message data.

**Size:** S

**Acceptance Criteria:**

- `GET /api/v1/compliance/access-requests?status=pending` — authenticated, `compliance_officer` role required; returns list of pending requests excluding requests submitted by the caller (self-approval forbidden); response:
  ```json
  {"data": [{"request_id": "...", "requester_user_id": "...", "room_id": "...", "time_range_start": "...", "time_range_end": "...", "justification": "...", "created_at": "..."}], "meta": {"total": N}}
  ```
- `POST /api/v1/compliance/access-requests/{requestId}/approve` — authenticated, `compliance_officer` role; body: `{"note": "..."}` (optional):
  - Verifies caller is NOT the original requester; returns `403 M_FORBIDDEN` with message "Self-approval is not permitted" if they are
  - Verifies request is in `pending` status; returns `409 M_CONFLICT` if already approved or rejected
  - Updates `compliance_requests` row: `status = 'approved'`, `approver_user_id = caller`, `approved_at = NOW()`
  - Calls `AuditWriter.log(caller, "compliance_access_approved", "compliance_request", request_id, %{note: note}, "success")`
  - Returns `200 {"request_id": "...", "status": "approved"}`
- `POST /api/v1/compliance/access-requests/{requestId}/reject` — same guards; updates status to `rejected`; logs `compliance_access_rejected`; returns `200 {"status": "rejected"}`
- `GET /admin/api/compliance/pending-count` — authenticated admin session; returns `{"pending_count": N}` — used by Admin Dashboard
- Admin Dashboard page (Story 3.13) renders a badge next to "Compliance" in the sidebar nav showing the `pending_count` if > 0; fetched server-side at dashboard render time
- Unit tests: self-approval → 403, double-approval → 409, valid approval by different officer → 200, pending-count reflects DB state

---

### Story 5.5: Compliance Session Handler (24h JWT + sub-Binding + Expiry-Audit)

**As a** compliance officer,
**I want** a time-bounded access token after my request is approved,
**so that** I can access the requested data for up to 24 hours and my access is automatically revoked afterwards.

**Size:** S

**Acceptance Criteria:**

- `POST /api/v1/compliance/access-requests/{requestId}/session` — authenticated, `compliance_officer` role; caller must be the original requester (not the approver):
  - Verifies request status is `approved`; returns `403` if `pending` or `rejected`
  - Verifies no active compliance session already exists for this request (prevents session duplication); returns `409` if one exists
  - Generates a signed JWT (Ed25519, using the server's signing key from `server_config`):
    - Claims: `sub` (requester user_id), `compliance_request_id`, `room_id`, `time_range_start`, `time_range_end`, `exp: now + 86400s`, `iat: now`
  - Inserts into `compliance_sessions` table: `(session_id UUID, request_id, token_hash SHA-256, issued_at, expires_at, revoked_at NULL)`
  - Calls `AuditWriter.log(requester, "compliance_session_issued", "compliance_request", request_id, %{expires_at: exp}, "success")`
  - Returns `201 {"session_token": "<jwt>", "expires_at": "<ISO8601>"}`
- A background process (Elixir `Process.send_after` or pg_cron) checks for expired sessions every hour; for each expired session without `revoked_at`, calls `AuditWriter.log(system, "compliance_session_expired", "compliance_session", session_id, %{}, "success")`
- All compliance data endpoints (Story 5.6) validate the compliance JWT: verify signature, check `exp`, verify `sub` matches authenticated caller, extract `room_id` + `time_range_*` as the access scope
- Migration `20240007_compliance_sessions.sql` creates `compliance_sessions` table
- Unit tests: valid request → 201 with JWT, duplicate session → 409, expired token rejected by data endpoint, `sub` mismatch rejected

---

### Story 5.6: Compliance Data Export + Ed25519-Signatur

**As a** compliance officer,
**I want** to download a cryptographically signed export of message data scoped to my approved request,
**so that** the export is tamper-evident and legally defensible.

**Size:** S

**Acceptance Criteria:**

- `GET /api/v1/compliance/export` — authenticated with both standard JWT (identity) and compliance session token (scope); compliance token passed as `X-Compliance-Token: <jwt>` header:
  - Validates compliance session token (signature, expiry, sub-binding per Story 5.5)
  - Fetches all `m.room.message` events from `events` table where `room_id` matches token's `room_id` AND `origin_server_ts` falls within `time_range_start`–`time_range_end`
  - Produces a JSON export document:
    ```json
    {
      "export_id": "<uuid>",
      "generated_at": "<ISO8601>",
      "compliance_request_id": "<uuid>",
      "room_id": "...",
      "time_range_start": "...",
      "time_range_end": "...",
      "requester": "...",
      "approver": "...",
      "events": [<array of signed event objects>]
    }
    ```
  - Signs the canonical JSON of the export document with the server's Ed25519 private key (from Story 2.x key infrastructure); appends `"server_signature": "<base64>"` field
  - Returns the signed JSON as `Content-Type: application/json` with `Content-Disposition: attachment; filename="compliance-export-<request_id>.json"`
  - Calls `AuditWriter.log(requester, "compliance_export_downloaded", "compliance_request", request_id, %{event_count: N}, "success")`
- Export is strictly scoped: only events in the approved `room_id` and `time_range` are included — no other rooms, no events outside the range
- If the compliance session token scope does not match the requested room_id (tampered token), returns `403 M_FORBIDDEN`
- If no events exist in the range: returns valid export document with `"events": []` and a signature — not a 404
- Unit tests: export contains only events within range, signature verifiable with server's public Ed25519 key, out-of-scope room_id → 403, empty range → valid signed empty export

---

### Story 5.7: Atomare DSGVO-Deletion (Key-Löschung + Audit auch bei Fehler)

**As an** instance admin,
**I want** a DSGVO deletion operation that cryptographically destroys a user's private keys and records the attempt in the audit log even if the deletion fails,
**so that** sensitive PII becomes permanently unreadable and the deletion is always traceable.

**Size:** S

**Acceptance Criteria:**

- `DELETE /api/v1/admin/users/{userId}/keys` — authenticated, `instance_admin` role required; body: `{"reason": "..."}` (required, min 10 chars)
- The deletion executes as an atomic Elixir operation (`Ecto.Multi` or explicit transaction):
  1. Mark user as `deletion_in_progress` in `users` table (prevents concurrent deletions)
  2. Delete Ed25519 private key from `user_keys` table (`key_type = 'ed25519_private'`)
  3. Delete X25519 private key from `user_keys` table (`key_type = 'x25519_private'`)
  4. Update `users.deletion_status = 'keys_deleted'`, `users.keys_deleted_at = NOW()`
  5. Write audit entry: `AuditWriter.log(admin, "user_keys_deleted", "user", user_id, %{reason: reason}, "success")`
- **Failure invariant**: if any step 2–4 fails, the transaction is rolled back AND a separate `AuditWriter.log(admin, "user_keys_deletion_attempted", "user", user_id, %{reason: reason, error: error_detail}, "attempted")` is written in its own independent transaction (not part of the failing transaction)
- Public keys (`ed25519_public`, `x25519_public`) are retained — they are needed to verify existing signed events and to mark the user as deleted
- After key deletion, any subsequent encryption of data for this user returns `{:error, :user_keys_deleted}`; existing encrypted PII (email, IdP subject) stored with the X25519 public key is now permanently unreadable
- Returns `200 {"user_id": "...", "status": "keys_deleted", "keys_deleted_at": "<ISO8601>"}` on success
- Returns `409 M_CONFLICT` if user is already in `deletion_in_progress` or `keys_deleted` state
- Unit tests: happy path deletes both keys and writes success audit, DB failure on step 3 rolls back and writes `attempted` audit in separate transaction, concurrent deletion attempt returns 409

---

### Story 5.8: Operational PII-Anonymisierung ("Deleted User")

**As an** instance admin,
**I want** a user's operational PII (display name, avatar) to be anonymized upon account deletion,
**so that** the user's personal information is removed from all visible surfaces while preserving the integrity of the room timeline.

**Size:** S

**Acceptance Criteria:**

- `POST /api/v1/admin/users/{userId}/anonymize` — authenticated, `instance_admin` role; intended to be called after Story 5.7 key deletion (but operable independently):
  - Updates `profiles` table: `displayname = 'Deleted User'`, `avatar_url = NULL`
  - Updates `users` table: `display_name = 'Deleted User'`, `anonymized_at = NOW()`
  - For the user's avatar media file: if `avatar_url` was an `mxc://` URI, marks the corresponding `media_files` row as `deleted = true` and removes the encrypted file from disk (`NEBU_MEDIA_STORAGE_PATH`); if file removal fails, logs the error but does not abort the anonymization
  - Does NOT modify historical `events` rows — the `sender` field in Matrix events is the Matrix user ID (not the display name); the display name in `unsigned.prev_content` of existing events is not retroactively changed (Matrix spec behaviour)
  - Calls `AuditWriter.log(admin, "user_anonymized", "user", user_id, %{}, "success")`
  - Returns `200 {"user_id": "...", "status": "anonymized"}`
- `GET /_matrix/client/v3/profile/{userId}` after anonymization returns `{"displayname": "Deleted User", "avatar_url": null}` (not 404 — user record remains)
- `GET /_matrix/media/v3/download/{serverName}/{mediaId}` for the deleted avatar returns `404 M_NOT_FOUND`
- Unit tests: after anonymize, profile endpoint returns "Deleted User", avatar download returns 404, existing events in `events` table are unchanged (sender field intact)

---

### Story 5.9: Gherkin: Compliance Flow End-to-End

**As a** developer,
**I want** Gherkin acceptance tests that cover the full compliance workflow from access request to signed export,
**so that** regressions in the audit, four-eyes, and export paths are caught automatically in CI.

**Size:** S

**Acceptance Criteria:**

- `tests/features/compliance_flow.feature` contains scenario: **Full Four-Eyes Compliance Export**
  - **Given** two compliance officers `officer_a` and `officer_b` with valid sessions
  - **And** a room `test-room` with messages from the past 24 hours
  - **When** `officer_a` calls `POST /api/v1/compliance/access-requests` with valid room_id, time_range, and justification
  - **Then** response is `201` with `status: "pending"`
  - **When** `officer_a` tries to approve their own request
  - **Then** response is `403` with message containing "Self-approval"
  - **When** `officer_b` calls `POST /api/v1/compliance/access-requests/{id}/approve`
  - **Then** response is `200` with `status: "approved"`
  - **When** `officer_a` calls `POST /api/v1/compliance/access-requests/{id}/session`
  - **Then** response is `201` with a `session_token` JWT
  - **When** `officer_a` calls `GET /api/v1/compliance/export` with the session token
  - **Then** response is `200` with a JSON body containing `events` and `server_signature`
  - **And** the `server_signature` is verifiable with the server's Ed25519 public key

- `tests/features/compliance_flow.feature` contains scenario: **DSGVO Deletion + Anonymization**
  - **Given** an admin and a user `victim` with profile displayname "Alice"
  - **When** admin calls `DELETE /api/v1/admin/users/victim/keys` with a valid reason
  - **Then** response is `200` with `status: "keys_deleted"`
  - **And** `audit_log` contains a row with `action = 'user_keys_deleted'` and `outcome = 'success'`
  - **When** admin calls `POST /api/v1/admin/users/victim/anonymize`
  - **Then** response is `200`
  - **And** `GET /_matrix/client/v3/profile/victim` returns `displayname: "Deleted User"`

- `tests/features/compliance_flow.feature` contains scenario: **Audit log immutability**
  - **Given** the `audit_log` table has at least one row
  - **When** a direct SQL `DELETE FROM audit_log` is attempted with the application DB role
  - **Then** PostgreSQL raises a policy violation error (RLS enforcement)

- All step definitions implemented in `tests/steps/compliance_flow_steps.go`
- All scenarios run as part of `make test-integration` against the full Docker Compose stack
- All three scenarios pass green

---

## Epic 6: Instance Admins Can Manage the Instance Programmatically via Admin API

### Story 6.1: OpenAPI Spec-First Setup (codegen Pipeline + StrictServerInterface + Live-Endpoint)

**As an** instance admin,
**I want** the Admin API to be defined by an OpenAPI 3.1 specification that is both the source of truth for generated server code and live-browsable,
**so that** the API contract is always consistent with the implementation and tooling can be generated automatically.

**Size:** S

**Acceptance Criteria:**

- `gateway/api/openapi.yaml` is created as an OpenAPI 3.1 document; it defines at minimum:
  - `info.title: "Nebu Admin API"`, `info.version: "1.0.0"`
  - `servers: [{url: "/api/v1"}]`
  - Security scheme: `BearerAuth` (JWT)
  - Placeholder paths for all Admin API route groups (`/admin/users`, `/admin/rooms`, `/admin/config`, `/admin/metrics`, `/compliance/access-requests`)
- `Makefile` target `make gen-api` runs `oapi-codegen` (via Docker container, no local install) with `--generate: strict-server,types,spec` against `openapi.yaml`; outputs to `gateway/internal/api/api_gen.go`
- `oapi-codegen` config (`gateway/api/oapi-codegen.yaml`) specifies `output-options.strict-server: true` so the generated `StrictServerInterface` requires every operation to be implemented — missing implementations cause compile errors
- `GET /api/v1/openapi.yaml` serves the raw `openapi.yaml` content (embedded via `go:embed`) with `Content-Type: application/yaml`; no authentication required (FR51)
- `make build-gateway` runs `gen-api` as a prerequisite so stubs are always regenerated before compilation
- `go build ./...` succeeds after `make gen-api` with zero compiler errors
- A unit test fetches `GET /api/v1/openapi.yaml` and asserts the response contains `"Nebu Admin API"`

---

### Story 6.2: Admin API Response Format + Cursor-Pagination

**As a** developer integrating the Admin API,
**I want** a consistent `{"data": ..., "meta": {...}, "error": null}` envelope and a standardised cursor-based pagination scheme,
**so that** all list endpoints behave predictably and pagination tokens are safe to use across restarts.

**Size:** XS

**Acceptance Criteria:**

- `gateway/internal/api/response.go` defines Go types:
  ```go
  type APIResponse[T any] struct {
      Data  T          `json:"data"`
      Meta  *Meta      `json:"meta,omitempty"`
      Error *APIError  `json:"error,omitempty"`
  }
  type Meta struct {
      Total      int    `json:"total,omitempty"`
      NextCursor string `json:"next_cursor,omitempty"`
      PrevCursor string `json:"prev_cursor,omitempty"`
  }
  type APIError struct {
      Code    string `json:"code"`
      Message string `json:"message"`
  }
  ```
- `gateway/internal/api/pagination.go` implements:
  - `EncodeCursor(afterID, afterCreatedAt string) string` — returns `Base64URLNoPad(json({"after_id":"<uuid>","after_created_at":"<ISO8601>"}))`
  - `DecodeCursor(cursor string) (afterID, afterCreatedAt string, err error)` — inverse; returns `ErrInvalidCursor` on malformed input
- All list endpoints (Stories 6.4, 6.7) use `DecodeCursor` to parse the `cursor` query param and `EncodeCursor` to produce `next_cursor` in `meta`; invalid cursor → `400 M_BAD_JSON`
- On error responses: `data` is `null`, `error` is populated; HTTP status reflects the error type
- Unit tests: `EncodeCursor` + `DecodeCursor` round-trip, malformed cursor returns `ErrInvalidCursor`, error response has `data: null`

---

### Story 6.3: Admin API Router + Role-Auth Middleware

**As a** gateway developer,
**I want** the Admin API routes to be registered via the oapi-codegen `StrictHandler` and protected by a role-checking middleware,
**so that** every route is automatically wired to its handler and access is restricted by role without per-handler boilerplate.

**Size:** XS

**Acceptance Criteria:**

- `gateway/internal/api/server.go` defines `AdminServer struct` that implements the oapi-codegen generated `StrictServerInterface`; unimplemented operations return `501 Not Implemented` by default
- `gateway/internal/api/middleware.go` contains `RequireRole(role string) func(http.Handler) http.Handler`:
  - Reads the JWT from the `Authorization: Bearer <token>` header (validated by the existing JWT middleware from Story 2.x)
  - Checks the `roles` claim in the JWT against the `role_overrides` table (Story 6.6); if the user has the required role via either source → proceeds; otherwise → `403 M_FORBIDDEN`
  - Applied as middleware to the `/api/v1/admin/*` route group requiring `instance_admin`
  - Applied to `/api/v1/compliance/*` route group requiring `compliance_officer`
- `gateway/internal/api/router.go` registers all routes by calling `oapi-codegen`-generated `RegisterHandlersWithBaseURL(router, adminServer, "/api/v1")`
- All Admin API routes are mounted under `/api/v1/` alongside the existing Matrix routes (no port conflict)
- Unit tests: request without JWT → 401, valid JWT with wrong role → 403, valid JWT with correct role → handler called

---

### Story 6.4: User List + Get API

**As an** instance admin,
**I want** to list all users on the instance and retrieve individual user details,
**so that** I have full visibility of who has access to the Nebu instance.

**Size:** S

**Acceptance Criteria:**

- `GET /api/v1/admin/users` — `instance_admin` role required; query params: `cursor` (optional), `limit` (1–100, default 20), `search` (optional, partial match on `display_name` or `email`):
  - Queries `users` table ordered by `(created_at DESC, id)`; applies cursor-based pagination (Story 6.2)
  - Response: `{"data": [<user objects>], "meta": {"total": N, "next_cursor": "..."}}`
  - Each user object: `{"user_id", "display_name", "email_masked", "roles": [], "status", "created_at", "last_seen_at"}`
  - `email_masked`: shows `a***@example.com` format (never the full email — Sensitive PII)
  - `roles`: array populated from JWT claim mapping + `role_overrides` table
  - `status`: `active` | `deactivated` | `keys_deleted` | `anonymized`
- `GET /api/v1/admin/users/{userId}` — `instance_admin` role required:
  - Returns single user object with same fields as list, plus `room_count` (number of rooms user is member of)
  - Returns `404 {"error": {"code": "M_NOT_FOUND", "message": "User not found"}}` if user does not exist
- Both endpoints call `AuditWriter.log(admin, "admin_user_viewed", "user", user_id, %{}, "success")`
- Unit tests: list returns paginated results with correct cursor, search filters by display_name, get unknown user → 404, email is masked in response

---

### Story 6.5: User Deactivation + Reactivation + Session-Invalidierung

**As an** instance admin,
**I want** to deactivate and reactivate user accounts,
**so that** I can immediately revoke access for a user without permanently deleting their data.

**Size:** S

**Acceptance Criteria:**

- `POST /api/v1/admin/users/{userId}/deactivate` — `instance_admin` role required; body: `{"reason": "..."}` (required, min 10 chars):
  - Sets `users.status = 'deactivated'`, `users.deactivated_at = NOW()`, `users.deactivation_reason = reason`
  - Calls `gRPC CoreService.InvalidateUserSessions(user_id)` → Core calls `SessionManager.destroy_session/1` for all active sessions of the user; removes from ETS + deletes `sync_tokens` row
  - The existing JWT middleware (Story 2.x) must reject tokens for deactivated users: after deactivation, any request with that user's token returns `401 M_UNKNOWN_TOKEN`; implement by adding a `SELECT status FROM users WHERE id = $1` check in the JWT validation path (cached in ETS with 60s TTL to avoid per-request DB hits)
  - Returns `200 {"user_id": "...", "status": "deactivated"}`
  - Returns `409 M_CONFLICT` if already deactivated
  - Calls `AuditWriter.log(admin, "user_deactivated", "user", user_id, %{reason: reason}, "success")`
- `POST /api/v1/admin/users/{userId}/reactivate` — `instance_admin` role:
  - Sets `users.status = 'active'`, clears `deactivated_at` and `deactivation_reason`
  - Returns `200 {"user_id": "...", "status": "active"}`
  - Returns `409` if user is in `keys_deleted` or `anonymized` state (cannot reactivate after DSGVO deletion)
  - Calls `AuditWriter.log(admin, "user_reactivated", "user", user_id, %{}, "success")`
- `gRPC CoreService` proto adds `rpc InvalidateUserSessions(InvalidateUserSessionsRequest) returns (InvalidateUserSessionsResponse)`
- Unit tests: deactivate → token rejected with 401, reactivate → token accepted again, deactivate already-deactivated → 409, reactivate anonymized user → 409

---

### Story 6.6: User Role Assignment API (role_overrides Tabelle + Middleware-Integration)

**As an** instance admin,
**I want** to explicitly assign or revoke roles for users independent of their OIDC claims,
**so that** I can grant `compliance_officer` or `instance_admin` to users whose OIDC provider does not emit the required claims.

**Size:** S

**Acceptance Criteria:**

- Migration `20240008_role_overrides.sql` creates table `role_overrides`:
  - `user_id TEXT NOT NULL`
  - `role TEXT NOT NULL` (`instance_admin` | `compliance_officer`)
  - `granted_by TEXT NOT NULL`
  - `granted_at TIMESTAMPTZ DEFAULT NOW()`
  - `PRIMARY KEY (user_id, role)`
- `POST /api/v1/admin/users/{userId}/roles` — `instance_admin` role required; body: `{"role": "instance_admin"|"compliance_officer", "action": "grant"|"revoke"}`:
  - `grant`: upserts into `role_overrides`; returns `200 {"user_id": "...", "role": "...", "action": "granted"}`
  - `revoke`: deletes from `role_overrides`; returns `200 {"user_id": "...", "role": "...", "action": "revoked"}`; returns `404` if override does not exist
  - An admin cannot revoke their own `instance_admin` role (prevents lockout); returns `403 M_FORBIDDEN` with message "Cannot revoke your own admin role"
  - Calls `AuditWriter.log(admin, "role_granted"|"role_revoked", "user", user_id, %{role: role}, "success")`
- The JWT validation middleware (Story 2.x, extended in Story 6.3) checks `role_overrides` in addition to JWT claims when determining a user's effective roles; the DB lookup is cached in ETS with 60s TTL per user
- `GET /api/v1/admin/users/{userId}` (Story 6.4) reflects the effective roles (JWT claims + overrides) in its `roles` array
- Unit tests: grant role → user has role in subsequent requests, revoke role → role removed, self-revoke → 403, effective roles merge JWT claims and overrides correctly

---

### Story 6.7: Room List + Get API

**As an** instance admin,
**I want** to list all rooms on the instance and view individual room details,
**so that** I have full visibility of all spaces on the server.

**Size:** S

**Acceptance Criteria:**

- `GET /api/v1/admin/rooms` — `instance_admin` role required; query params: `cursor` (optional), `limit` (1–100, default 20), `search` (optional, partial match on room `name`), `status` (optional filter: `active` | `archived`):
  - Queries `rooms` table ordered by `(created_at DESC, id)`; applies cursor-based pagination
  - Response: `{"data": [<room objects>], "meta": {"total": N, "next_cursor": "..."}}`
  - Each room object: `{"room_id", "name", "topic", "visibility", "member_count", "status", "created_at", "creator_user_id"}`
  - `member_count`: count from `room_members` where `left_at IS NULL`
  - `status`: `active` | `archived`
- `GET /api/v1/admin/rooms/{roomId}` — `instance_admin` role required:
  - Returns single room object with all list fields, plus `max_members` (from room settings), `message_count` (count of events in `events` table for this room), `power_levels_json`
  - Returns `404` if room does not exist
- Both endpoints call `AuditWriter.log(admin, "admin_room_viewed", "room", room_id, %{}, "success")`
- Unit tests: list with `status=archived` returns only archived rooms, search filters by name, get unknown room → 404, member_count reflects current membership

---

### Story 6.8: Room Settings Update API (max members, visibility, serverweite Defaults)

**As an** instance admin,
**I want** to update room settings and define server-wide default room configuration,
**so that** I can enforce limits and consistent defaults across all rooms on the instance.

**Size:** S

**Acceptance Criteria:**

- `PATCH /api/v1/admin/rooms/{roomId}` — `instance_admin` role required; body (all fields optional):
  - `max_members: integer` (min 2, max 100000) — stored in `rooms.max_members`; the Room GenServer enforces this limit on join: if `member_count >= max_members`, join returns `{:error, :room_full}` and the gateway returns `403 M_FORBIDDEN` with `errcode: "M_ROOM_FULL"`
  - `visibility: "public"|"private"` — updates room visibility
  - `name: string` (1–255 chars)
  - `topic: string` (0–1000 chars)
  - Calls `gRPC CoreService.UpdateRoomSettings` to notify the Room GenServer of the updated max_members in real time (no restart required)
  - Calls `AuditWriter.log(admin, "room_settings_updated", "room", room_id, %{changes: changed_fields}, "success")`
  - Returns `200` with the updated room object
  - Returns `404` if room not found; `400` if validation fails
- `PUT /api/v1/admin/config/room-defaults` — `instance_admin` role required; body: `{"default_max_members": integer, "default_visibility": "public"|"private"}`:
  - Upserts into `server_config` table: `room_default_max_members` and `room_default_visibility`
  - These defaults are applied when `POST /_matrix/client/v3/createRoom` is called without explicit overrides (Story 4.9 reads them at room creation time)
  - Returns `200 {"data": {"default_max_members": N, "default_visibility": "..."}}`
- `gRPC CoreService` proto adds `rpc UpdateRoomSettings(UpdateRoomSettingsRequest) returns (UpdateRoomSettingsResponse)`
- Unit tests: set max_members=2, third join attempt → 403 M_ROOM_FULL, update visibility → reflected in GET, set room-defaults → new rooms use defaults

---

### Story 6.9: Room Archivierung (kein physisches Löschen, Events bleiben erhalten)

**As an** instance admin,
**I want** to archive rooms that are no longer needed,
**so that** their history is preserved for compliance purposes while users can no longer send new messages.

**Size:** S

**Acceptance Criteria:**

- `POST /api/v1/admin/rooms/{roomId}/archive` — `instance_admin` role required; body: `{"reason": "..."}` (required, min 10 chars):
  - Sets `rooms.status = 'archived'`, `rooms.archived_at = NOW()`, `rooms.archive_reason = reason`
  - Calls `gRPC CoreService.ArchiveRoom(room_id)` → Core stops the Room GenServer for this room via `Horde.DynamicSupervisor.terminate_child/2`; the Room GenServer is not restarted (Horde will not restart a deliberately terminated process if flagged as archived)
  - After archival: `PUT /_matrix/client/v3/rooms/{roomId}/send/*` returns `403 M_FORBIDDEN` with `errcode: "M_ROOM_ARCHIVED"`; `GET /messages` and `/sync` continue to work (read-only access)
  - Events in the `events` table are NOT deleted; the append-only constraint is maintained
  - Calls `AuditWriter.log(admin, "room_archived", "room", room_id, %{reason: reason}, "success")`
  - Returns `200 {"room_id": "...", "status": "archived"}`
  - Returns `409` if already archived
- `POST /api/v1/admin/rooms/{roomId}/unarchive` — `instance_admin` role:
  - Sets `rooms.status = 'active'`, clears `archived_at`
  - Calls `gRPC CoreService.UnarchiveRoom(room_id)` → Core restarts the Room GenServer via `RoomSupervisor.start_room/1`
  - Returns `200 {"room_id": "...", "status": "active"}`
- `gRPC CoreService` proto adds `rpc ArchiveRoom(ArchiveRoomRequest) returns (ArchiveRoomResponse)` and `rpc UnarchiveRoom`
- Unit tests: after archive, send-event → 403 M_ROOM_ARCHIVED, get-messages still → 200, events table unchanged, unarchive re-enables sending

---

### Story 6.10: Server Config API + Metrics API

**As an** instance admin,
**I want** to read and update server-wide configuration, and query live instance metrics,
**so that** I can manage the instance settings programmatically and monitor the instance health via API.

**Size:** S

**Acceptance Criteria:**

- `GET /api/v1/admin/config` — `instance_admin` role required:
  - Returns all readable `server_config` keys as a JSON object: `{"instance_name": "...", "oidc_issuer": "...", "oidc_client_id": "...", "room_default_max_members": N, "room_default_visibility": "...", "audit_log_retention_days": N}`
  - `oidc_client_secret` is never returned (write-only field)
- `PATCH /api/v1/admin/config` — `instance_admin` role required; body: partial update with any subset of updatable keys:
  - Updatable: `instance_name`, `oidc_issuer`, `oidc_client_id`, `oidc_client_secret`, `audit_log_retention_days`
  - If `oidc_issuer`, `oidc_client_id`, or `oidc_client_secret` is changed: calls `SessionManager.invalidate_all_admin_sessions()` (all active admin UI sessions are destroyed, next request redirects to `/admin/login`)
  - All changes are upserted into `server_config` via the append-only insert policy; prior values are superseded at the application layer (latest value per key is authoritative)
  - Calls `AuditWriter.log(admin, "server_config_updated", "server", "config", %{changed_keys: keys}, "success")` for each change set
  - Returns `200` with the full updated config object (same format as GET)
- `GET /api/v1/admin/metrics` — `instance_admin` role required:
  - Returns: `{"active_sessions": N, "room_count": N, "archived_room_count": N, "msg_per_sec_1m": float, "registered_users": N, "deactivated_users": N}`
  - `active_sessions`: from `SessionManager.EtsStore.list_sessions()` count
  - `msg_per_sec_1m`: rolling 1-minute average from the Prometheus `message_events_total` counter (Story 1.x)
  - `room_count` / `archived_room_count`: from DB
  - `registered_users` / `deactivated_users`: from DB
- Unit tests: GET config never exposes `oidc_client_secret`, PATCH oidc_issuer → admin sessions invalidated, GET metrics returns all required fields with correct types

---

### Story 6.11: Gherkin: Admin API CRUD Flow

**As a** developer,
**I want** Gherkin acceptance tests that cover the key Admin API operations,
**so that** regressions in user management, room management, and config operations are caught automatically in CI.

**Size:** S

**Acceptance Criteria:**

- `tests/features/admin_api.feature` contains scenario: **User management lifecycle**
  - **Given** an authenticated `instance_admin` user
  - **When** `GET /api/v1/admin/users` is called
  - **Then** response is `200` with `data` array and `meta.total` field
  - **When** `POST /api/v1/admin/users/{userId}/deactivate` is called with a valid reason
  - **Then** response is `200` with `status: "deactivated"`
  - **And** subsequent Matrix API request with the deactivated user's token returns `401`
  - **When** `POST /api/v1/admin/users/{userId}/reactivate` is called
  - **Then** response is `200` with `status: "active"`
  - **And** subsequent Matrix API request with the user's token returns `200`

- `tests/features/admin_api.feature` contains scenario: **Role assignment**
  - **Given** a user without `compliance_officer` role
  - **When** `POST /api/v1/admin/users/{userId}/roles` is called with `{"role": "compliance_officer", "action": "grant"}`
  - **Then** response is `200` with `action: "granted"`
  - **And** the user can access `GET /api/v1/compliance/access-requests`
  - **When** the role is revoked
  - **Then** the user receives `403` on compliance endpoints

- `tests/features/admin_api.feature` contains scenario: **Room archival**
  - **Given** a room with existing messages
  - **When** `POST /api/v1/admin/rooms/{roomId}/archive` is called with a valid reason
  - **Then** response is `200` with `status: "archived"`
  - **And** `PUT /_matrix/client/v3/rooms/{roomId}/send/m.room.message/{txnId}` returns `403 M_ROOM_ARCHIVED`
  - **And** `GET /_matrix/client/v3/rooms/{roomId}/messages` still returns `200` with existing messages

- All step definitions in `tests/steps/admin_api_steps.go`
- All scenarios run as part of `make test-integration`; all pass green

---

## Epic 7: Instance Admins Have a Full Admin UI for Day-to-Day Operations

### Story 7.1: Obsidian Color System vollständig + Typography (UX-DR1, UX-DR2)

**As a** developer,
**I want** the complete Obsidian design token set and typography scale defined in Tailwind config,
**so that** all Admin UI pages use consistent colours and type without hardcoded values.

**Size:** XS

**Acceptance Criteria:**

- `gateway/internal/admin/tailwind.config.js` extends the default Tailwind theme with all Obsidian CSS custom properties from UX-DR1:
  - Background scale: `--color-base-100` through `--color-base-400` (dark greys)
  - Semantic colours: `--color-primary` (electric indigo), `--color-secondary`, `--color-accent`
  - Status colours: `--color-success` (green), `--color-warning` (amber), `--color-error` (red)
  - All tokens are referenced as `theme('colors.*')` in Tailwind utilities — no hardcoded hex values in templates
- DaisyUI theme `obsidian` is declared in `tailwind.config.js` mapping all DaisyUI semantic tokens to the Obsidian palette; `data-theme="obsidian"` is set on the `<html>` element in `base.html`
- Typography scale from UX-DR2 is declared as Tailwind `fontSize` extensions: `display` (2.25rem/700), `heading` (1.5rem/600), `body` (1rem/400), `caption` (0.75rem/400), `mono` (0.875rem/400 JetBrains Mono)
- `make build-admin-css` produces a CSS file where all Obsidian tokens appear as `:root` CSS custom properties
- Visual smoke test: render `base.html` and assert `data-theme="obsidian"` attribute is present on `<html>`

---

### Story 7.2: MasterDetailLayout + DetailPanel (C4, C5) + bookmarkbare URL-Routen

**As an** instance admin,
**I want** a two-column master-detail layout where selecting an item in the list opens its details in a persistent side panel with a bookmarkable URL,
**so that** I can navigate directly to a specific user or room and share links with colleagues.

**Size:** S

**Acceptance Criteria:**

- `gateway/internal/admin/templates/components/master_detail.html` implements the C4 MasterDetailLayout:
  - Two-column grid: list column (fixed 320px) + detail column (flex remaining)
  - On mobile (< 768px): single column, detail panel overlays list as a drawer
  - Accepts Go template data: `ActiveItemID string` — the currently selected item ID
- `gateway/internal/admin/templates/components/detail_panel.html` implements C5 DetailPanel:
  - Header with item title + close button (`×`) that navigates back to the list URL
  - Content slot for item-specific fields
  - Footer slot for action buttons (Deactivate, Archive, etc.)
- Go routes for User detail: `GET /admin/users/{userId}` renders the User List page with the MasterDetailLayout, pre-selecting `userId` in the list and loading the detail panel server-side; the URL `/admin/users/abc123` is bookmarkable and directly shareable
- Go routes for Room detail: `GET /admin/rooms/{roomId}` — same pattern
- If `userId` or `roomId` does not exist, the detail panel renders a 404-within-panel state (not a full-page 404)
- `ActiveItemID` is passed from the route handler to the template; the list highlights the matching item with the DaisyUI `active` class
- WCAG: the detail panel has `role="region"` and `aria-label="Item details"`; the close button has `aria-label="Close detail panel"`
- Unit test: render master-detail with `ActiveItemID = "abc"`, assert the list item with id "abc" has `active` class and detail panel is present in output

---

### Story 7.3: Interaction Components (C6 WizardStepper, C7–C10)

**As a** developer,
**I want** the interaction-focused custom components (WizardStepper, ConfirmationDialog, SearchInput, FilterBar) as reusable Go templates,
**so that** complex UI flows like compliance wizards and list filtering are consistent across all Admin UI pages.

**Size:** S

**Acceptance Criteria:**

- `gateway/internal/admin/templates/components/wizard_stepper.html` implements C6 WizardStepper:
  - Accepts: `Steps []string`, `CurrentStep int` (0-indexed)
  - Renders a horizontal step indicator with step number, label, and status (completed ✓, current, upcoming)
  - Active step has `--color-primary` border; completed steps have `--color-success` fill
  - WCAG: `role="list"`, each step has `aria-current="step"` for the active step
- `gateway/internal/admin/templates/components/confirm_dialog.html` implements C7 ConfirmationDialog:
  - Accepts: `Title`, `Message`, `ConfirmLabel`, `ConfirmClass` (DaisyUI colour), `FormAction` (POST URL), `HiddenFields map[string]string`
  - Renders as a DaisyUI `modal`; the confirm button submits a hidden form to `FormAction`
  - The trigger is a separate `<button>` with `onclick="confirm_dialog.showModal()"` — no JS framework needed
  - WCAG: `role="alertdialog"`, `aria-labelledby`, `aria-describedby`; focus is trapped inside the modal when open
- `gateway/internal/admin/templates/components/search_input.html` implements C8 SearchInput:
  - Accepts: `Placeholder`, `Value`, `ParamName` (query param key)
  - Renders a text input that submits its containing form on change with 300ms debounce via a small inline `<script>` block (vanilla JS, no framework)
- `gateway/internal/admin/templates/components/filter_bar.html` implements C9/C10 FilterBar:
  - Accepts: `Filters []FilterOption` where each option has `Label`, `ParamName`, `Options []string`, `CurrentValue string`
  - Renders a row of `<select>` dropdowns; each change auto-submits the filter form
- Unit test: render each component with valid data; assert no unclosed HTML tags; assert ARIA attributes present

---

### Story 7.4: Display Components (C11–C14 inkl. InlineEdit, AlertBanner)

**As a** developer,
**I want** the display-focused custom components (InlineEdit, SkeletonBlock, LoadMoreButton, AlertBanner) as reusable Go templates,
**so that** item editing, loading states, and error messaging are consistent across all detail panels.

**Size:** S

**Acceptance Criteria:**

- `gateway/internal/admin/templates/components/inline_edit.html` implements C12 InlineEdit:
  - Accepts: `FieldName`, `CurrentValue`, `FormAction` (PATCH URL), `InputType` (`text`|`select`), `Options []string` (for select)
  - Renders the current value as static text with an **Edit** pencil icon button
  - On click (vanilla JS toggle), the static text is replaced with an `<input>` or `<select>` pre-filled with `CurrentValue`, plus **Save** and **Cancel** buttons
  - **Save** submits a minimal `<form method="POST" action="FormAction">` with a `_method=PATCH` hidden field (method override); **Cancel** reverts to static display without a page reload
  - WCAG: the edit button has `aria-label="Edit <FieldName>"`; input gets focus automatically on activation
- `gateway/internal/admin/templates/components/skeleton_block.html` implements C11 SkeletonBlock:
  - Accepts: `Lines int`, `Width string` (Tailwind width class)
  - Renders `Lines` animated shimmer bars using `animate-pulse` DaisyUI utility
  - Used in list and detail panel while data is loading (server renders skeleton on first paint if data fetch is in progress)
- `gateway/internal/admin/templates/components/load_more.html` implements C13 LoadMoreButton:
  - Accepts: `NextCursor string`, `FormAction string`
  - Renders a **Load more** button that submits a GET form appending `cursor=NextCursor` to `FormAction`; if `NextCursor` is empty, the button is not rendered
- `gateway/internal/admin/templates/components/alert_banner.html` implements C14 AlertBanner:
  - Accepts: `Type` (`success`|`warning`|`error`|`info`), `Message string`
  - Renders a DaisyUI `alert` with the appropriate icon and colour token
  - Rendered server-side in response to form submissions (flash messages stored in a short-lived signed cookie)
- Unit test: render each component; assert InlineEdit renders static text initially; SkeletonBlock renders correct number of shimmer bars; LoadMoreButton absent when `NextCursor` is empty

---

### Story 7.5: User List Page (Suche debounced, Load-More, Skeleton, WCAG Landmarks)

**As an** instance admin,
**I want** a User List page with real-time search, status filter, and cursor-based load-more pagination,
**so that** I can quickly find any user on the instance regardless of total user count.

**Size:** S

**Acceptance Criteria:**

- `GET /admin/users` renders the User List page within the MasterDetailLayout (C4, Story 7.2); `ActiveNav: "users"`
- The page uses SearchInput (C8) and FilterBar (C9/C10) components:
  - SearchInput param: `q` — partial match on `display_name`; debounced 300ms (submits the surrounding `<form method="GET">` on input)
  - FilterBar options: `status` (`all`|`active`|`deactivated`|`keys_deleted`|`anonymized`)
  - Both filters are preserved in the URL query string and restored on page load
- User list rows show: avatar placeholder (initials), display name, masked email, role badges, status badge, **View** link → `/admin/users/{userId}`
- LoadMoreButton (C13) appears below the list when `meta.next_cursor` is present; clicking appends the next page (full page reload with `cursor` param, not AJAX)
- On initial load while the API call is in progress: three SkeletonBlock rows (C11) are rendered; replaced with real data on render (server-side — no race condition)
- WCAG: `<main>` landmark wraps the content area; `<nav>` landmark for sidebar (already in base layout); list is a `<ul>` with `role="list"`; each row's **View** link has `aria-label="View user <display_name>"`
- `@media (prefers-reduced-motion: reduce)`: all `animate-pulse` skeleton animations are disabled
- Unit test: render with 3 users + next_cursor → LoadMoreButton present; render with no next_cursor → LoadMoreButton absent; render with `status=deactivated` filter → filter select shows correct selected value

---

### Story 7.6: User Detail Panel (InlineEdit Displayname/Status, bookmarkbare URL)

**As an** instance admin,
**I want** to view and edit a user's display name and status directly in the detail panel,
**so that** I can correct user information without navigating to a separate edit page.

**Size:** S

**Acceptance Criteria:**

- `GET /admin/users/{userId}` renders the User List page with the detail panel (C5) open for the given user; `ActiveItemID` set to `userId`
- Detail panel header: user's display name + Matrix user ID
- Detail panel body sections:
  - **Identity**: display name (InlineEdit C12, PATCH `/api/v1/admin/users/{userId}` with `{"display_name": "..."}`), masked email (read-only), Matrix user ID (read-only, copy-to-clipboard button)
  - **Roles**: list of current effective roles (JWT claims + overrides) with **Manage Roles** link → Story 7.7
  - **Status**: current status badge; if `active` → shows Deactivate button; if `deactivated` → shows Reactivate button (Story 7.7)
  - **Account Info**: `created_at`, `last_seen_at`, `room_count`
- InlineEdit for display name: on Save, the handler calls `PATCH /api/v1/admin/users/{userId}` and re-renders the detail panel with an AlertBanner (C14) "Display name updated" (success) or "Failed to update" (error)
- If `userId` does not exist: detail panel shows "User not found" with a 404-within-panel state
- WCAG: detail panel has `role="region"` `aria-label="User details"`; InlineEdit focuses the input on activation; close button (`aria-label="Close"`) navigates back to `/admin/users`
- Unit test: render detail panel for existing user → correct fields shown; render for unknown user → "User not found" state

---

### Story 7.7: User Role UI + Deactivation Confirmation Dialog

**As an** instance admin,
**I want** to grant and revoke roles and deactivate users through the UI with confirmation dialogs,
**so that** I cannot accidentally perform destructive actions with a misclick.

**Size:** S

**Acceptance Criteria:**

- `GET /admin/users/{userId}/roles` renders a role management sub-page within the detail panel:
  - Lists current roles (from `role_overrides` + JWT claims); each override role has a **Revoke** button
  - A **Grant Role** form with a `<select>` for `instance_admin` | `compliance_officer` and a **Grant** submit button
  - Both Grant and Revoke submit to the Admin API (Story 6.6) and redirect back to `/admin/users/{userId}/roles` with an AlertBanner result
  - Self-revoke attempt (admin revoking their own `instance_admin`) renders AlertBanner error "Cannot revoke your own admin role" (API returns 403, UI surfaces it)
- **Deactivate** button on the User Detail Panel (Story 7.6) opens a ConfirmationDialog (C7):
  - Title: "Deactivate user"
  - Message: "This will immediately invalidate all active sessions for `<display_name>`. Are you sure?"
  - Confirm button label: "Deactivate", class `btn-error`
  - Hidden field: `user_id`, `reason` text input (required, min 10 chars) inside the dialog
  - On confirm: POST to `/api/v1/admin/users/{userId}/deactivate`; redirect to `/admin/users/{userId}` with AlertBanner "User deactivated"
- **Reactivate** button (shown when user is deactivated): opens a simpler ConfirmationDialog ("Reactivate user? This will restore access."); on confirm: POST to `/api/v1/admin/users/{userId}/reactivate`
- `SameSite=Strict` is confirmed on `admin_session` cookie (set in Story 3.10) — all form POSTs are same-origin, CSRF via SameSite is sufficient; no additional CSRF token needed; this is documented in a code comment in `session.go`
- Unit tests: render role management page → current roles listed; deactivate dialog renders reason input; self-revoke shows error banner

---

### Story 7.8: Room List Page (Suche, Filter, Pagination)

**As an** instance admin,
**I want** a Room List page with search and status filter,
**so that** I can find any room on the instance and access its management panel.

**Size:** S

**Acceptance Criteria:**

- `GET /admin/rooms` renders the Room List page within MasterDetailLayout (C4); `ActiveNav: "rooms"`
- Uses the same SearchInput (C8) + FilterBar (C9/C10) + LoadMoreButton (C13) + SkeletonBlock (C11) pattern established in Story 7.5:
  - SearchInput param: `q` — partial match on room `name`
  - FilterBar option: `status` (`all`|`active`|`archived`)
  - Cursor-based pagination via `cursor` query param
- Room list rows show: room name, visibility badge (`public`|`private`), member count, status badge (`active`|`archived`), **View** link → `/admin/rooms/{roomId}`
- `@media (prefers-reduced-motion: reduce)`: skeleton animations disabled (same as Story 7.5)
- WCAG: list is `<ul role="list">`; each row's View link has `aria-label="View room <name>"`
- Unit test: render with 2 rooms + next_cursor → LoadMoreButton present; `status=archived` filter renders correctly selected; no next_cursor → LoadMoreButton absent

---

### Story 7.9: Room Detail Panel + Settings Edit + Archive UI + Confirmation Pattern

**As an** instance admin,
**I want** to view room details, edit settings, and archive rooms from the detail panel,
**so that** all room management actions are accessible in one place without switching pages.

**Size:** S

**Acceptance Criteria:**

- `GET /admin/rooms/{roomId}` renders the Room List page with the detail panel open for the room
- Detail panel body sections:
  - **Info**: room name (InlineEdit C12, PATCH `/api/v1/admin/rooms/{roomId}`), topic (InlineEdit), visibility (InlineEdit with select: `public`|`private`), creator, `created_at`, `message_count`
  - **Limits**: max members (InlineEdit, integer input, PATCH `/api/v1/admin/rooms/{roomId}`)
  - **Members**: member count with link "View members" (→ Phase 2; shows "Coming soon" for now)
  - **Status**: current status badge; if `active` → **Archive** button; if `archived` → **Unarchive** button
- **Archive** button opens a ConfirmationDialog (C7):
  - Title: "Archive room"
  - Message: "Members will no longer be able to send messages. Message history is preserved. Are you sure?"
  - Reason text input inside dialog (required, min 10 chars)
  - On confirm: POST to `/api/v1/admin/rooms/{roomId}/archive`; redirect back with AlertBanner "Room archived"
- **Unarchive** button opens a simpler ConfirmationDialog; on confirm: POST to `/api/v1/admin/rooms/{roomId}/unarchive`
- After archive: room detail panel shows `status: archived` badge; the room also appears in the Room List with `status=archived` filter
- If `roomId` does not exist: detail panel shows "Room not found" 404-within-panel state
- Unit tests: render detail panel for existing room → all sections present; archive dialog includes reason input; unknown roomId → not-found state

---

### Story 7.10: Server Config UI (GET/PATCH /api/v1/admin/config)

**As an** instance admin,
**I want** a Server Config page in the Admin UI where I can update the OIDC provider and instance settings,
**so that** I can manage server configuration without using API tools or editing config files.

**Size:** XS

**Acceptance Criteria:**

- `GET /admin/config` renders a Server Config page; `ActiveNav: "config"`
- Page layout (single-column, no MasterDetailLayout): sections for **Instance Settings** and **OIDC Configuration**
- **Instance Settings** section:
  - `instance_name` (InlineEdit C12, PATCH `/api/v1/admin/config`)
  - `audit_log_retention_days` (InlineEdit, integer input)
- **OIDC Configuration** section:
  - `oidc_issuer` (InlineEdit, URL input)
  - `oidc_client_id` (InlineEdit, text input)
  - `oidc_client_secret` (InlineEdit, password input — value shown as `••••••••`, typing a new value replaces it; leave blank to keep existing)
  - AlertBanner (C14) warning: "Changing OIDC settings will invalidate all active admin sessions" shown statically above the OIDC section
- After successful PATCH, page re-renders with AlertBanner "Settings saved"; if OIDC settings were changed, the response cookie deletes the `admin_session` and redirects to `/admin/login` (session was invalidated server-side)
- Unit test: render config page → all InlineEdit fields present; OIDC warning banner present; blank `oidc_client_secret` on save does not overwrite existing secret (API behaviour, verified in Story 6.10)

---

### Story 7.11: Compliance Access Request List + Four-Eyes Approval UI

**As a** compliance officer,
**I want** a Compliance UI that lists pending access requests and allows me to approve or reject requests from other officers,
**so that** the Four-Eyes process is manageable through the Admin UI without using the raw API.

**Size:** S

**Acceptance Criteria:**

- `GET /admin/compliance` renders a Compliance page; `ActiveNav: "compliance"`; accessible only to users with `compliance_officer` role (SessionGuard + role check); `instance_admin` without `compliance_officer` role receives `403` error page
- Page sections:
  - **My Requests**: table of requests submitted by the current officer; columns: Request ID, Room, Time Range, Status, Created At, Action (View Export if approved)
  - **Pending Approvals**: table of requests from other officers awaiting approval; columns: Requester, Room, Time Range, Justification (truncated to 200 chars with "Show more" toggle), Actions (Approve / Reject buttons)
- **Approve** button opens a ConfirmationDialog (C7):
  - Title: "Approve compliance access request"
  - Message: "You are approving access for `<requester>` to room `<room_id>` from `<start>` to `<end>`."
  - Optional note input
  - On confirm: POST to `/api/v1/compliance/access-requests/{id}/approve`; page reloads with AlertBanner "Request approved"
- **Reject** button opens a ConfirmationDialog with label "Reject" and class `btn-warning`; POST to `/api/v1/compliance/access-requests/{id}/reject`
- Self-submitted requests do NOT appear in Pending Approvals (filtered server-side)
- For approved requests in **My Requests**: a **Download Export** link calls `GET /api/v1/compliance/export` (requires session token — the link leads to a sub-page that first calls `POST /api/v1/compliance/access-requests/{id}/session` then triggers the download)
- WizardStepper (C6) is used on the export sub-page to show the status: Step 1 "Request" (done), Step 2 "Approved" (done), Step 3 "Download Export" (current)
- Unit tests: render pending approvals → own requests absent from that table; approve dialog renders requester and room info

---

### Story 7.12: Audit Log View (Atlas-Pattern, Zeitraum-Filter, read-only)

**As an** instance admin,
**I want** to browse the audit log through the Admin UI with time-range filtering,
**so that** I can investigate admin actions and compliance events without direct database access.

**Size:** S

**Acceptance Criteria:**

- `GET /admin/audit-log` renders the Audit Log page; `ActiveNav: "audit-log"`; requires `instance_admin` role
- Atlas-Pattern layout: full-width chronological list (no MasterDetailLayout); rows sorted by `event_time DESC`
- Each row displays: `event_time` (formatted `YYYY-MM-DD HH:mm:ss UTC`), `actor_user_id`, `action`, `target_type + target_id`, `outcome` badge (success=green, failure=red, attempted=amber)
- FilterBar (C9/C10): `time_range_start` (date input), `time_range_end` (date input), `action` (select with known action values + "All"), `outcome` (`all`|`success`|`failure`|`attempted`)
- Pagination: cursor-based LoadMoreButton (C13), 50 rows per page
- The table is **read-only** — no edit, delete, or action buttons of any kind
- `GET /api/v1/admin/audit-log` — new Admin API endpoint returning paginated audit log entries with the same filter params; added to `openapi.yaml` and generated via `make gen-api`
- If the audit log is empty: renders "No audit log entries yet" empty state with an info AlertBanner (C14)
- WCAG: table uses `<table>`, `<thead>`, `<tbody>` with `<th scope="col">` headers; `role="table"` for screen reader compatibility
- Unit test: render with 3 log entries → rows present; render with no entries → empty state shown; outcome badge has correct CSS class per outcome value

---

### Story 7.13: WCAG Audit + axe-core Report (automatisierter Scan aller Admin-Seiten)

**As a** developer,
**I want** an automated WCAG 2.1 AA accessibility audit that runs against all Admin UI pages,
**so that** accessibility regressions are caught in CI before they reach production.

**Size:** S

**Acceptance Criteria:**

- `tests/accessibility/axe_audit.js` uses `axe-core` + `puppeteer` (or `playwright`) to scan each Admin UI page:
  - Pages scanned: `/admin/login`, `/admin/bootstrap`, `/admin/dashboard`, `/admin/users`, `/admin/users/{id}`, `/admin/rooms`, `/admin/rooms/{id}`, `/admin/config`, `/admin/compliance`, `/admin/audit-log`
  - Each page is loaded with a valid admin session cookie (set up via Dex test credentials)
  - `axe.run()` is called with `runOnly: {type: "tag", values: ["wcag2a", "wcag2aa"]}` ruleset
- The audit **fails** (exits non-zero) if any page has violations with `impact: "critical"` or `impact: "serious"`
- `Makefile` target `make test-accessibility` runs the audit via a Node.js container; outputs a JSON report to `tests/accessibility/report.json` and a human-readable summary to stdout
- `make test-accessibility` is not part of `make test-integration` (requires a running stack); documented as a pre-release gate in `README.md`
- All Admin UI pages introduced in Epics 3 and 7 have zero `critical` or `serious` axe violations after this story is complete
- The audit also verifies `@media (prefers-reduced-motion: reduce)` CSS is present in the compiled `admin.css` (grep check in the Makefile target)

---

### Story 7.14: Gherkin: Admin UI Smoke Flows (User deactivate + Room archive via UI)

**As a** developer,
**I want** Gherkin acceptance tests that exercise the key Admin UI flows at the HTTP level,
**so that** regressions in server-side rendering and form handling are caught in CI.

**Size:** S

**Acceptance Criteria:**

- `tests/features/admin_ui.feature` contains scenario: **User deactivation via UI**
  - **Given** an authenticated admin session cookie
  - **And** a test user `target` exists with `status: active`
  - **When** `GET /admin/users/target` is requested
  - **Then** response is `200` and body contains "Deactivate"
  - **When** `POST /api/v1/admin/users/target/deactivate` is called with `reason: "Smoke test deactivation"`
  - **Then** response is `200` with `status: "deactivated"`
  - **When** `GET /admin/users/target` is requested again
  - **Then** body contains "Reactivate" and status badge shows "deactivated"

- `tests/features/admin_ui.feature` contains scenario: **Room archival via UI**
  - **Given** an authenticated admin session cookie
  - **And** a room `smoke-room` exists with `status: active`
  - **When** `GET /admin/rooms/smoke-room` is requested
  - **Then** response is `200` and body contains "Archive"
  - **When** `POST /api/v1/admin/rooms/smoke-room/archive` is called with `reason: "Smoke test archival"`
  - **Then** response is `200` with `status: "archived"`
  - **When** `GET /admin/rooms/smoke-room` is requested
  - **Then** body contains "Unarchive" and status badge shows "archived"
  - **And** `PUT /_matrix/client/v3/rooms/smoke-room/send/m.room.message/txn1` returns `403`

- `tests/features/admin_ui.feature` contains scenario: **Unauthenticated access is blocked**
  - **When** `GET /admin/users` is requested without a session cookie
  - **Then** response redirects `302` to `/admin/login`

- All step definitions in `tests/steps/admin_ui_steps.go`
- All scenarios run as part of `make test-integration`; all pass green
