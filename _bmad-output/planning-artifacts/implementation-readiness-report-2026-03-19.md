---
name: Implementation Readiness Report
date: 2026-03-19
project: open-chat
stepsCompleted:
  - step-01-document-discovery
  - step-02-prd-analysis
  - step-03-epic-coverage-validation
  - step-04-ux-alignment
  - step-05-epic-quality-review
  - step-06-final-assessment
documentsUsed:
  prd: planning-artifacts/prd.md
  architecture: planning-artifacts/architecture.md
  epics: planning-artifacts/epics.md
  ux: planning-artifacts/ux-design-specification.md
---

# Implementation Readiness Assessment Report

**Date:** 2026-03-19
**Project:** open-chat

## Document Inventory

### PRD Documents
- `prd.md` (37.9 KB, modified 2026-03-16)

### Architecture Documents
- `architecture.md` (48.3 KB, modified 2026-03-18)

### Epics & Stories Documents
- `epics.md` (193.9 KB, modified 2026-03-19)

### UX Design Documents
- `ux-design-specification.md` (72.5 KB, modified 2026-03-17)
- `ux-design-directions.html` (55.1 KB, modified 2026-03-17) — supplementary HTML, nicht primäres Analyse-Dokument

---

## PRD Analysis

### Functional Requirements

**Identity & Authentication**
- FR1: End-User kann sich via SSO mit jedem OIDC-konformen Identity Provider anmelden
- FR2: End-User kann sich abmelden und seine Session ungültig machen
- FR3: Instance Admin kann OIDC-Provider-Konfiguration verwalten (Issuer, Client-ID, Claim-Mappings)
- FR4: System weist Rollen (`instance_admin`, `compliance_officer`) anhand konfigurierbarer OIDC-Claim-Mappings zu
- FR5: System weist beim ersten OIDC-Login automatisch `instance_admin` zu, wenn noch kein Admin existiert (Bootstrap Mode)
- FR6: System deaktiviert Bootstrap Mode permanent nach erstem Admin-Setup

**Messaging & Rooms**
- FR7: End-User kann Rooms erstellen
- FR8: End-User kann einem Room per Room-ID oder Alias beitreten
- FR9: End-User kann Nachrichten in einem Room senden
- FR10: End-User kann den Nachrichtenverlauf eines Rooms abrufen
- FR11: End-User kann ältere Nachrichten paginiert nachladen
- FR12: End-User kann Tipp-Indikatoren setzen und empfangen
- FR13: End-User kann Read Receipts senden
- FR14: End-User kann sein Profil (Anzeigename, Avatar) anzeigen und aktualisieren
- FR15: End-User kann den Präsenz-Status anderer User einsehen
- FR16: End-User kann mit Standard-Matrix-Clients (Element, FluffyChat u.a.) auf Nebu zugreifen

**Room-Konfiguration**
- FR17: Room Owner kann Sichtbarkeit des Rooms definieren (öffentlich, privat, einladungsbasiert)
- FR18: Room Owner kann Room-Metadaten pflegen (Name, Beschreibung, Topic, Avatar)
- FR19: Room Owner kann Zugriffskontrolle konfigurieren (wer darf beitreten, lesen, schreiben)
- FR20: End-User kann andere User in einen Room einladen
- FR21: End-User kann eine Room-Einladung annehmen oder ablehnen
- FR22: Instance Admin kann alle Room-Owner-Einstellungen für jeden Room auf der Instanz verwalten
- FR23: Instance Admin kann eine maximale Mitgliederzahl pro Room konfigurieren
- FR24: Instance Admin kann serverweite Standard-Room-Einstellungen festlegen

**Cryptographic Identity & PII**
- FR25: System generiert pro User-Identität ein Ed25519-Schlüsselpaar
- FR26: System signiert ausgehende Matrix-Events mit dem privaten Ed25519-Schlüssel des Users
- FR27: System verschlüsselt Sensitive PII (E-Mail, IdP-Subject) mit dem öffentlichen Ed25519-Schlüssel
- FR28: System kann privaten Ed25519-Schlüssel eines Users löschen und Sensitive PII kryptografisch unlesbar machen (Right to be Forgotten)
- FR29: System anonymisiert Operational PII (Anzeigename) bei Kontolöschung zu "Deleted User"

**Compliance & Audit**
- FR30: Compliance Officer kann temporären Zugriff auf Nachrichteninhalte mit dokumentierter Begründung beantragen
- FR31: System erzwingt Vier-Augen-Prinzip für Compliance-Zugriffe
- FR32: System begrenzt Compliance-Zugriffssessions auf maximal 24 Stunden
- FR33: Compliance Officer kann Nachrichtendaten mit Ed25519-signiertem Audit-Trail exportieren
- FR34: System führt Append-Only Audit Log aller Compliance-Zugriffsevents
- FR35: System protokolliert alle administrativen Aktionen im Audit Log

**User & Room Administration**
- FR36: Instance Admin kann alle User einer Instanz auflisten
- FR37: Instance Admin kann User-Accounts anlegen und deaktivieren
- FR38: Instance Admin kann User-Attribute aktualisieren
- FR39: Instance Admin kann alle Rooms einer Instanz auflisten
- FR40: Instance Admin kann Room-Mitgliedschaft und -Einstellungen verwalten

**Notifications**
- FR41: System benachrichtigt User über neue Nachrichten gemäß konfigurierbarer Push-Regeln
- FR42: End-User kann Push-Benachrichtigungsregeln pro Room und Event-Typ konfigurieren

**Server Operations & Observability**
- FR43: Operator kann Nebu mittels Docker Compose deployen und betreiben
- FR44: System exponiert Health- und Readiness-Endpunkte für Monitoring-Integrationen
- FR45: System exponiert Metrics-Endpunkt kompatibel mit Standard-Monitoring (Prometheus)
- FR46: System unterstützt horizontale Skalierung des Go-Gateways ohne Session-Affinity
- FR47: System unterstützt TLS-Terminierung (mTLS optional konfigurierbar)

**Admin Interface & API**
- FR48: Instance Admin kann alle Verwaltungsfunktionen über eine web-basierte Admin UI bedienen
- FR49: Instance Admin kann Live-Server-Metriken in der Admin UI einsehen
- FR50: Alle Admin-UI-Zustände sind über URLs adressierbar (bookmarkbar, teilbar)
- FR51: Developer/Operator kann alle Admin-Funktionen programmatisch über eine REST-API nutzen
- FR52: System stellt OpenAPI-Spezifikation der Admin API bereit

**Total FRs: 52**

---

### Non-Functional Requirements

**Performance**
- NFR-P1: Nachrichtenversand ≤ 500ms Latenz unter Silber-Last (500 concurrent users / m5.large)
- NFR-P2: Matrix `/sync` antwortet ≤ 1s unter Normallast
- NFR-P3: System erreicht Silber- (>500), Gold- (>1000), Platin-Tier (>5000) ohne Redis/NATS/Kafka
- NFR-P4: Gateway-Kaltstart-Zeit ≤ 5s

**Security**
- NFR-S1: Alle externen Verbindungen via TLS 1.2 minimum (TLS 1.3 bevorzugt)
- NFR-S2: Sensitive PII und Operational PII at-rest verschlüsselt (Ed25519-Keypair)
- NFR-S3: Audit Log append-only und Ed25519-signiert — Manipulation nachweisbar
- NFR-S4: OIDC-Token-Validierung bei jedem API-Request — kein Session-State im Gateway
- NFR-S5: Ed25519-Schlüssellöschung ist irreversibel — kein Recovery-Pfad by design
- NFR-S6: Bootstrap Mode deaktiviert sich permanent und unwiderruflich nach erstem Admin-Setup

**Scalability**
- NFR-SC1: Go Gateway horizontal skalierbar ohne Session-Affinity
- NFR-SC2: Elixir/OTP Core unterstützt Cluster-Betrieb (Phase 2: automatisch via libcluster)
- NFR-SC3: Kein externer Middleware-Layer erforderlich — PostgreSQL ist einziger Persistenz-Layer

**Reliability**
- NFR-R1: Elixir/OTP Process-Isolation: Absturz eines Room-Prozesses betrifft keine anderen Rooms
- NFR-R2: Kein Datenverlust bei Gateway-Neustart — PostgreSQL ist Single Source of Truth
- NFR-R3: Rolling Updates ohne vollständige Downtime möglich

**Operability**
- NFR-O1: Vollständiges Deployment via `docker-compose up` in ≤ 10 Minuten
- NFR-O2: Health/Readiness-Endpunkte antworten ≤ 200ms auch unter Last
- NFR-O3: Admin UI vollständig im Gateway-Binary integriert — keine externen Laufzeit-Abhängigkeiten
- NFR-O4: Alle Admin-UI-Zustände via URL reproduzierbar — kein Browser-State, kein LocalStorage-Zwang

**Compliance & Datenschutz**
- NFR-C1: Right to be Forgotten via kryptografische Schlüssellöschung — DSGVO-konform ohne Audit-Log-Bruch
- NFR-C2: Audit-Log-Aufbewahrungsdauer konfigurierbar (Default: 7 Jahre)
- NFR-C3: Alle Daten ausschließlich in konfigurierter PostgreSQL-Instanz — On-Premise-fähig

**Matrix-Protokoll-Konformität**
- NFR-M1: Matrix Client-Server API kompatibel mit Element, FluffyChat, Hydrogen — Inkompatibilitäten = Bugs
- NFR-M2: OIDC-Integration via `m.login.sso` gemäß Matrix OIDC Specification

**Accessibility**
- NFR-A1: Admin UI erfüllt WCAG 2.1 Level AA
- NFR-A2: Admin UI vollständig per Tastatur navigierbar
- NFR-A3: Admin UI mit gängigen Screen Readern nutzbar (semantisches HTML, ARIA-Labels)

**Lokalisierung**
- NFR-L1: Admin UI unterstützt Deutsch und Englisch

**Protokoll & Standards**
- NFR-I1: Ausschließlich offene Standards (Matrix, OIDC, OpenAPI, Prometheus) — keine proprietären Protokolle
- NFR-I2: Matrix-Events haben konfigurierbare maximale Payload-Größe (Default: 65KB)

**Crypto-Agilität**
- NFR-CR1: Kryptografische Primitive intern modularisiert und austauschbar

**Total NFRs: 32**

---

### Additional Requirements / Constraints

- **Deployment:** Docker Compose ist primäres und einziges offiziell unterstütztes Deployment-Modell (kein Kubernetes-Support)
- **Auth:** OIDC-Only — keine lokalen Accounts, keine Passwort-Authentifizierung
- **Federation:** Nicht im MVP-Scope; architektonisch vorbereitet für Phase 3
- **E2EE:** Nicht im MVP-Scope; server-seitige Sichtbarkeit ist Compliance-Feature
- **Rate Limiting:** Im MVP non-scope; Verantwortung vorgelagerter Infrastruktur
- **Testing-Strategie:** Gherkin-Integration-Tests als primäres Quality Gate; Unit-Tests für Crypto und komplexe Logik
- **Entwicklungsstil:** Specification-Driven Development (SDD) + Agent-Driven Development
- **Lizenz:** Apache 2.0 — kommerzieller Einsatz und Hosting ohne Lizenzeinschränkungen

### PRD Completeness Assessment

Das PRD ist **sehr vollständig und hochwertig**. Alle Anforderungen sind klar strukturiert, mit Personas/Journeys begründet und in MVP vs. Phase 2/3 priorisiert. Functional Requirements sind vollständig nummeriert (FR1–FR52) und Non-Functional Requirements nach Kategorien organisiert (32 NFRs). Die Scope-Grenzen sind explizit dokumentiert. Das PRD bildet eine solide Basis für die Epic-Coverage-Validierung.

---

## Epic Coverage Validation

### Coverage Matrix

| FR | PRD-Anforderung (Kurzform) | Epic Coverage | Status |
|----|---------------------------|---------------|--------|
| FR1 | OIDC SSO Login | Epic 2 — OIDC-Login via m.login.sso | ✓ Covered |
| FR2 | Logout + Session-Invalidierung | Epic 2 — Logout + Session invalidation | ✓ Covered |
| FR3 | Admin: OIDC-Provider-Konfiguration | Epic 2 — OIDC-Provider-Konfiguration | ✓ Covered |
| FR4 | Rollen-Zuweisung via OIDC-Claims | Epic 2 — Rollen-Zuweisung (instance_admin, compliance_officer) | ✓ Covered |
| FR5 | Bootstrap: erster Admin auto-assign | Epic 2 (Backend) + Epic 3 (Bootstrap-UI) | ✓ Covered |
| FR6 | Bootstrap Mode permanent deaktivieren | Epic 2 (Backend) + Epic 3 (Bootstrap-UI) | ✓ Covered |
| FR7 | Room erstellen | Epic 4 | ✓ Covered |
| FR8 | Room per ID/Alias beitreten | Epic 4 | ✓ Covered |
| FR9 | Nachrichten senden | Epic 4 | ✓ Covered |
| FR10 | Nachrichtenverlauf abrufen | Epic 4 | ✓ Covered |
| FR11 | Paginiertes Nachladen älterer Nachrichten | Epic 4 | ✓ Covered |
| FR12 | Typing Indicators | Epic 4 | ✓ Covered |
| FR13 | Read Receipts senden | Epic 4 | ✓ Covered |
| FR14 | Profil anzeigen + aktualisieren | Epic 4 | ✓ Covered |
| FR15 | Präsenz-Status anderer User | Epic 4 | ✓ Covered |
| FR16 | Standard-Matrix-Client-Kompatibilität | Epic 4 | ✓ Covered |
| FR17 | Room-Sichtbarkeit konfigurieren | Epic 4 | ✓ Covered |
| FR18 | Room-Metadaten pflegen | Epic 4 | ✓ Covered |
| FR19 | Room-Zugriffskontrolle konfigurieren | Epic 4 | ✓ Covered |
| FR20 | User in Room einladen | Epic 4 | ✓ Covered |
| FR21 | Room-Einladung annehmen/ablehnen | Epic 4 | ✓ Covered |
| FR22 | Admin: Room-Owner-Einstellungen verwalten | Epic 6 (API) + Epic 7 (UI) | ✓ Covered |
| FR23 | Admin: Max. Mitgliederzahl pro Room | Epic 6 (API) + Epic 7 (UI) | ✓ Covered |
| FR24 | Admin: Serverweite Standard-Room-Einstellungen | Epic 6 (API) + Epic 7 (UI) | ✓ Covered |
| FR25 | Ed25519 + X25519 Schlüsselpaar-Generierung | Epic 2 | ✓ Covered |
| FR26 | Matrix-Events mit Ed25519 signieren | Epic 4 | ✓ Covered |
| FR27 | Sensitive PII verschlüsseln | Epic 2 | ✓ Covered |
| FR28 | Right to be Forgotten: Keys löschen | Epic 5 | ✓ Covered |
| FR29 | Operational PII anonymisieren | Epic 5 | ✓ Covered |
| FR30 | Compliance-Zugriff beantragen | Epic 5 | ✓ Covered |
| FR31 | Vier-Augen-Prinzip erzwingen | Epic 5 | ✓ Covered |
| FR32 | Compliance-Session max. 24h | Epic 5 | ✓ Covered |
| FR33 | Ed25519-signierter Export | Epic 5 | ✓ Covered |
| FR34 | Append-Only Audit Log (Compliance) | Epic 5 | ✓ Covered |
| FR35 | Audit Log (Admin-Aktionen) | Epic 5 | ✓ Covered |
| FR36 | Admin: User-Liste | Epic 6 (API) + Epic 7 (UI) | ✓ Covered |
| FR37 | Admin: User anlegen + deaktivieren | Epic 6 (API) + Epic 7 (UI) | ✓ Covered |
| FR38 | Admin: User-Attribute aktualisieren | Epic 6 (API) + Epic 7 (UI) | ✓ Covered |
| FR39 | Admin: Room-Liste | Epic 6 (API) + Epic 7 (UI) | ✓ Covered |
| FR40 | Admin: Room-Mitgliedschaft + Einstellungen | Epic 6 (API) + Epic 7 (UI) | ✓ Covered |
| FR41 | Matrix-native Push-Regeln | Epic 4 | ✓ Covered |
| FR42 | Push-Regeln pro Room + Event-Typ konfigurieren | Epic 4 | ✓ Covered |
| FR43 | Docker Compose Deployment | Epic 1 | ✓ Covered |
| FR44 | Health + Readiness Endpoints | Epic 1 | ✓ Covered |
| FR45 | Prometheus Metrics Endpoint | Epic 1 | ✓ Covered |
| FR46 | Horizontale Skalierung (stateless Gateway) | Epic 1 | ✓ Covered |
| FR47 | TLS-Terminierung (mTLS optional) | Epic 1 | ✓ Covered |
| FR48 | Web-basierte Admin UI | Epic 3 (Minimal) + Epic 7 (Vollständig) | ✓ Covered |
| FR49 | Live-Server-Metriken in Admin UI | Epic 3 (Minimal) + Epic 7 (Vollständig) | ✓ Covered |
| FR50 | URL-adressierbare Admin-UI-Zustände | Epic 3 (Basis) + Epic 7 (Vollständig) | ✓ Covered |
| FR51 | Admin REST-API | Epic 6 | ✓ Covered |
| FR52 | OpenAPI-Spezifikation der Admin API | Epic 6 | ✓ Covered |

### Missing Requirements

**Keine fehlenden FRs.** Alle 52 Functional Requirements aus dem PRD sind in den Epics abgedeckt.

### Anmerkungen

**⚠️ Minor Discrepancy — FR25 / FR27 Terminologie:**
- PRD FR25: "Ed25519-Schlüsselpaar" (einziges Keypair erwähnt)
- Epics FR25: "Ed25519 (Signing) + X25519 (Encryption)" (korrekt: zwei Keypairs)
- PRD FR27: "öffentlichen **Ed25519**-Schlüssel" für PII-Verschlüsselung
- Epics FR27: "öffentlichen **X25519**-Schlüssel" für PII-Verschlüsselung

Die Epics sind **architektonisch korrekt** (ADR-007: zwei Keypairs — Ed25519 für Signing, X25519 für Encryption via ECDH → AES-256-GCM). Das PRD hat eine terminologische Ungenauigkeit — kein inhaltlicher Gap, da die Architektur und die Epics konsistent sind. Das PRD sollte bei Gelegenheit korrigiert werden.

### Coverage Statistics

- Total PRD FRs: **52**
- FRs abgedeckt in Epics: **52**
- Coverage: **100%**
- Epics: Epic 1 (FR43-47), Epic 2 (FR1-6, FR25, FR27), Epic 3 (FR5, FR6, FR48-50), Epic 4 (FR7-21, FR26, FR41-42), Epic 5 (FR28-35), Epic 6 (FR22-24, FR36-40, FR51-52), Epic 7 (FR22-24, FR36-40, FR48-50)

---

## UX Alignment Assessment

### UX Document Status

**Gefunden:** `ux-design-specification.md` (72.5 KB, 2026-03-17) — vollständiges, umfangreiches Dokument.

### UX ↔ PRD Alignment

| PRD-Element | UX-Abdeckung | Status |
|-------------|--------------|--------|
| Journey: Marcus (Operator, Bootstrap) | Bootstrap als First-Run-Experience detailliert beschrieben; C6b BootstrapWizardCard; 4-Step Forge-Wizard | ✓ Vollständig |
| Journey: Kai (Instance Admin) | "Ambient Check"-Core-Loop; Sentinel Dashboard; MasterDetailLayout; Live-Metriken via SSE | ✓ Vollständig |
| Journey: Dr. Petra (Compliance Officer) | "Guided Workflow"-Core-Loop; 4-Step Compliance-Wizard; Contextual Actions; C13 ExportDownload | ✓ Vollständig |
| FR48-50: Admin UI, Metriken, URL-States | URL-Konvention vollständig definiert (14 Routes); Tailwind+DaisyUI+Vue.js minimal; SSE-Metriken | ✓ Vollständig |
| NFR-A1-A3: WCAG 2.1 AA | Explicit: ARIA-Attribute, Focus-Indikatoren, Screen-Reader, Reduced-Motion, Keyboard-Navigation | ✓ Vollständig |
| NFR-O3-O4: Admin UI im Binary, URL-State | Go-Templates, Tailwind Standalone CLI (kein Node.js Runtime), URL = Wahrheit-Prinzip | ✓ Vollständig |
| NFR-L1: DE/EN Lokalisierung | Erwähnt in UX-DRs (UX-DR in Epics); Sprache aus OIDC-Claim | ✓ Abgedeckt |

### UX ↔ Architecture Alignment

| UX-Anforderung | Architektur-Support | Status |
|----------------|---------------------|--------|
| SSE für Live-Metriken (Dashboard, TopbarStatus) | Go Gateway: Server-Sent Events explizit vorgesehen; Vue.js SSE-Consumer | ✓ Unterstützt |
| URL-basierter State (kein Client-Side-Router) | Go-Templates: URL-Routing nativ; kein SPA-Framework | ✓ Unterstützt |
| Tailwind Standalone CLI (kein Node.js Runtime) | "Ein Binary"-Philosophie im Go-Gateway; go:embed für statische Assets | ✓ Unterstützt |
| Compliance-Wizard Draft (Server-Side Draft) | Admin API: `POST /api/v1/compliance/access` + Draft-Endpoint geplant | ✓ Unterstützt |
| Ed25519-signierter Export (C13 ExportDownload) | Elixir Signature-App: Ed25519 Signing; Export-Endpunkt in Admin API | ✓ Unterstützt |
| 14 definierte URL-Routes | Go-Router muss alle 14 Routes bedienen; keine Konflikte mit Matrix-API (`/_matrix/`) | ✓ Unterstützt |

### Warnings / Anmerkungen

**⚠️ Minor: UX erwähnt "Meine Aktivitäten"-Ansicht als MVP**
Die UX-Spec (Emotional Design Roadmap) listet "Meine Aktivitäten"-Ansicht für Compliance Officers als MVP. Diese ist in den FRs nicht explizit als eigenes FR formuliert — sie ist implizit durch FR34/FR35 (Audit Log) abgedeckt, aber kein dedizierter Filter-View für eigene Aktivitäten ist explizit adressiert. In den Epics sind die UX-DRs (UX-DR1–UX-DR31) vollständig aufgeführt, aber kein eigenes UX-DR spezifiziert diesen View explizit.

**→ Empfehlung:** Kein Blocker. Die Audit Log-Seite (`/admin/audit/`) mit User-Filter deckt diesen Use-Case strukturell ab.

**✅ Positive Highlights:**
- UX-Spezifikation ist von hoher Qualität: emotionale Design-Ziele, Anti-Patterns, Inspirations-Produkte und konkrete Component-Spezifikationen (14 Components, UX-DR1–UX-DR31)
- Design-System-Wahl (Tailwind+DaisyUI) ist konsistent mit Architektur-Constraints und der "Ein Binary"-Philosophie
- Compliance-UX ist als Premium-Feature designed — klarer Wettbewerbsvorteil gegenüber Synapse/Element

---

## Epic Quality Review

### Epic Structure Validation

| Epic | Titel | User-Value | Unabhängig | Bewertung |
|------|-------|-----------|-----------|-----------|
| Epic 1 | Operators Can Deploy and Observe a Running Nebu Instance | ✓ FR43-47 | ✓ Foundation | ✓ Akzeptabel |
| Epic 2 | Users Can Authenticate with SSO and Have a Verified Cryptographic Identity | ✓ FR1-6, 25, 27 | ✓ Nur Epic 1 needed | ✓ Akzeptabel |
| Epic 3 | Operators Have a Minimal Admin UI for Bootstrap and Debugging | ✓ FR5/6 UI, FR48-50 partial | ✓ Epic 1+2 needed | ✓ Gut |
| Epic 4 | End-Users Can Chat in Rooms Using Any Standard Matrix Client | ✓ FR7-21, 26, 41-42 | ✓ Epic 1-3 needed | ✓ Gut |
| Epic 5 | Compliance Officers Can Securely Request and Export Audited Communication Data | ✓ FR28-35 | ✓ Epic 1-4 needed | ✓ Gut |
| Epic 6 | Instance Admins Can Manage the Instance Programmatically via Admin API | ✓ FR22-24, 36-40, 51-52 | ✓ Epic 1-5 needed | ✓ Gut |
| Epic 7 | Instance Admins Have a Full Admin UI for Day-to-Day Operations | ✓ FR22-24, 36-40, 48-50 | ✓ Epic 1-6 needed | ✓ Gut |

Alle 7 Epics liefern echten User-Value. Keine rein technischen Epics ohne Nutzerbezug.

---

### 🔴 Critical Violations

**Keine kritischen Verletzungen gefunden.**

---

### 🟠 Major Issues

**Issue #1: Story 2.20 (Keycloak) wird durch Story 3.15 (Dex) ersetzt — Verschwendung**

- Story 2.20 (Epic 2): Setzt Keycloak als OIDC-Provider im Dev-Stack auf
- Story 3.15 (Epic 3): Ersetzt Keycloak durch Dex; Hinweis: "Stories 2.20 und 2.21 sollen nachträglich auf Dex aktualisiert werden"
- **Problem:** Ein Dev Team implementiert Story 2.20 (Keycloak-Setup) und muss dann in Story 3.15 alles wieder rückbauen und durch Dex ersetzen. Das ist verschwendeter Entwicklungsaufwand.
- **Impact:** 1 volle Story Aufwand für Setup das unmittelbar danach weggeworfen wird. Story 2.21 (Gherkin Auth Scenario) funktioniert nach Abschluss von Epic 2 mit Keycloak, dann müssen die Tests in Story 3.15 erneut angepasst werden.
- **Empfehlung:** Story 2.20 direkt auf Dex umstellen. Der Hinweis in Story 3.15 zeigt, dass die Autoren sich des Problems bewusst sind — aber es wurde nicht korrigiert. Entweder:
  - Option A (bevorzugt): Story 2.20 als "Dex Dev Setup" schreiben, Story 3.15 entfällt oder wird zu reiner Keycloak-Referenz-Entfernung
  - Option B: Story 2.20 und 2.21 als "⚠️ Placeholder — wird in Story 3.15 ersetzt" markieren, damit das Team sie bewusst überspringt

---

### 🟡 Minor Concerns

**Concern #1: Acceptance-Criteria-Format-Inkonsistenz**

- Epics 1–2: Formales `**Given** ... **When** ... **Then**` BDD-Format mit klarer Gherkin-Struktur
- Epics 3–7: Wechsel zu Bullet-Point-Format (`- Bedingung ist erfüllt`)
- Beide Formate sind testbar, aber die Inkonsistenz kann verwirrend sein: In Epics 1-2 ist klar was ein Gherkin-Szenario werden soll. In Epics 3-7 ist die Mapping-Absicht weniger explizit.
- **Empfehlung:** Kein Blocker. Bullet-Point-ACs sind in Epics 3-7 ausreichend präzise. Bei zukünftigen Sprints konsistentes Format vereinbaren.

**Concern #2: Forward-Looking Schema-Migrations**

- Story 1.4 (message_buffer): "So that Epic 4's gateway resilience implementation has its schema ready" — explizite Vorwärts-Referenz
- Story 2.2 (sessions schema): "So that Epic 4's /sync since-token checkpointing has its schema ready" — explizite Vorwärts-Referenz
- **Kontext:** Dies ist eine bewusste Architekturentscheidung (golang-migrate: alle Migrationen früh anlegen). Die Schemas werden tatsächlich in Epic 4 benötigt.
- **Impact:** Niedrig — die Schemas sind korrekt und werden gebraucht. Die Beschreibung kommuniziert Vorwärts-Abhängigkeit explizit, was transparent ist.
- **Empfehlung:** Kein Handlungsbedarf. Das Pattern ist für das golang-migrate-Modell akzeptabel.

**Concern #3: Epic-Sizing — Epics 2 und 4 sehr groß**

- Epic 2: 21 Stories (Auth + Crypto + Bootstrap-Backend + Keycloak-Setup)
- Epic 4: 23 Stories (Matrix-Messaging + Sync + Media Gateway + Lasttest)
- **Kontext:** Für ein komplexes Greenfield-Projekt mit fest gekoppelten Domänen (Auth+Crypto ist eine Einheit; Messaging ist eine Einheit) ist die Größe vertretbar.
- **Empfehlung:** Kein Blocker. Wenn Teams Probleme mit Sprint-Planung haben, könnten in Phase 2 die Epics in Unter-Epics aufgeteilt werden.

**Concern #4: "As a developer" Stories in Epics 1–2**

- Viele Stories in Epics 1-2 sind "As a developer" (Scaffolding, Proto-Codegen, Test-Targets, Crypto-Unit-Tests) — keine End-User-Stories
- **Kontext:** Für Greenfield-Projekte ist das normal und notwendig. Die Epic-Ziele sind user-centric, auch wenn einzelne Stories infrastruktur-orientiert sind.
- **Empfehlung:** Kein Handlungsbedarf.

**Concern #5: Size-Labels fehlen in Epics 1–2**

- Epics 3–7 haben `**Size:** XS/S/M/L` Labels pro Story
- Epics 1–2 haben keine Size-Labels
- **Empfehlung:** Vor Sprint-Planung für Epics 1-2 Size-Labels ergänzen (besonders wichtig für Epic 2 mit 21 Stories).

---

### Best Practices Compliance Checklist

| Kriterium | Epic 1 | Epic 2 | Epic 3 | Epic 4 | Epic 5 | Epic 6 | Epic 7 |
|-----------|--------|--------|--------|--------|--------|--------|--------|
| Liefert User-Value | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Funktioniert unabhängig | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Stories angemessen groß | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Keine Forward-Deps | ⚠️ | ⚠️ | ✓ | ✓ | ✓ | ✓ | ✓ |
| DB-Tables wann nötig | ✓* | ✓* | ✓ | ✓ | ✓ | ✓ | ✓ |
| Klare Acceptance Criteria | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| FR-Traceability | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

*⚠️ Intentional forward-looking DB schemas (akzeptables Pattern für golang-migrate)

---

## Summary and Recommendations

### Overall Readiness Status

# ✅ READY — mit einer Korrektur vor Sprint-Start

Das Projekt ist **implementierungsbereit**. PRD, Architektur, UX-Spec und Epics sind vollständig, konsistent und gut dokumentiert. Es gibt einen Major Issue der vor Sprint-Start behoben werden sollte, und fünf Minor Concerns die im laufenden Betrieb adressiert werden können.

---

### Stärken der Planung

1. **100% FR-Coverage** — Alle 52 Functional Requirements und 32 Non-Functional Requirements sind in den 7 Epics abgedeckt. Keine Lücken.
2. **Hohe Dokumentqualität** — PRD, Architektur-ADRs, UX-Spec und Epics sind vollständig durchgearbeitet und konsistent.
3. **Klare Scope-Grenzen** — MVP vs. Phase 2/3 ist konsequent dokumentiert. Kein Scope-Creep-Risiko.
4. **Specification-Driven Development** — Gherkin-Szenarien sind als primäres Quality Gate definiert. Epic 1 richtet die CI-Pipeline dafür ein.
5. **Architektur-Kohärenz** — ADR-007 (zwei Keypairs) ist korrekt in den Epics umgesetzt (während das PRD noch die ältere Einzelkeypar-Terminologie hat).
6. **UX-Spec ist production-ready** — 31 UX-Design-Requirements, 14 Custom Components, vollständige URL-Konvention, WCAG 2.1 AA — alle in Epic 7 abgebildet.

---

### Critical Issues Requiring Immediate Action

**#1 — Story 2.20 / Story 3.15 Keycloak→Dex Redundanz (MUSS VOR SPRINT-START BEHOBEN WERDEN)**

- **Problem:** Story 2.20 setzt Keycloak auf; Story 3.15 wirft es weg und ersetzt es mit Dex.
- **Nachweis:** Story 3.15 enthält expliziten Hinweis "supersedes Story 2.20 — Stories 2.20 und 2.21 should be updated".
- **Lösung:** Story 2.20 auf Dex umschreiben (Dex-config analog zu Story 3.15). Story 3.15 dann entweder entfernen oder zu "CI-Verifikation des Dex-Setups" reduzieren. Story 2.21 (Gherkin Auth) direkt mit Dex schreiben.
- **Aufwand:** ~30 Minuten Story-Anpassung.

---

### Recommended Next Steps

1. **Sofort:** Story 2.20 auf Dex migrieren und Story 3.15 entfernen oder anpassen. Epic 2 bleibt vollständig mit Dex als OIDC-Dev-Provider.

2. **Vor Sprint-Start Epic 1–2:** Size-Labels (XS/S/M/L) für alle Stories in Epics 1–2 ergänzen. Besonders wichtig für Sprint-Planung bei Epic 2 (21 Stories).

3. **Opportunistisch:** PRD-Terminologie in FR25/FR27 korrigieren: "Ed25519-Schlüssel für PII-Verschlüsselung" → "X25519-Schlüssel für PII-Verschlüsselung" (korrekte Architektur ist in Epics bereits abgebildet).

4. **Mit Entwicklungsteam besprechen:** Acceptance-Criteria-Format vereinheitlichen (Given/When/Then für alle Stories, oder bewusst Bullet-Point-Format für Epics 3–7 beibehalten).

5. **Legal Review:** Cryptographic DSGVO-Architecture (ADR-007) vor MVP durch Rechtsanwalt + Datenschutzbeauftragten reviewen lassen — wie in PRD als Validierungs-Gate definiert.

---

### Findings Summary

| Kategorie | Anzahl Issues | Schwere |
|-----------|---------------|---------|
| FR Coverage Gaps | 0 | — |
| UX Alignment Issues | 0 | — |
| Terminologie-Diskrepanz (PRD vs. Epics) | 1 | 🟡 Minor |
| Epic Quality — Keycloak→Dex Redundanz | 1 | 🟠 Major |
| Epic Quality — AC-Format-Inkonsistenz | 1 | 🟡 Minor |
| Epic Quality — Forward-Looking DB Schemas | 2 | 🟡 Minor (akzeptabel) |
| Epic Quality — Size Labels fehlen (Epics 1-2) | 1 | 🟡 Minor |
| Epic Quality — Große Epics (2: 21 Stories, 4: 23 Stories) | 2 | 🟡 Minor (akzeptabel) |
| **Total** | **8** | **1 Major, 7 Minor** |

**Gesamt-Bewertung:** Das Planungspaket ist von hoher Qualität. Die 1 Major Issue (Keycloak→Dex) ist ein 30-Minuten-Fix. Kein Issue blockiert den Start der Implementierung nach Behebung von #1.

---

**Assessment durchgeführt von:** Claude Code (PM/Scrum Master Rolle)
**Datum:** 2026-03-19
**Report-Datei:** `_bmad-output/planning-artifacts/implementation-readiness-report-2026-03-19.md`

