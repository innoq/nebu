---
stepsCompleted: ['step-01-init', 'step-02-discovery', 'step-02b-vision', 'step-02c-executive-summary', 'step-03-success', 'step-04-journeys', 'step-05-domain', 'step-06-innovation', 'step-07-project-type', 'step-08-scoping', 'step-09-functional', 'step-10-nonfunctional', 'step-11-polish']
inputDocuments: ['README.md']
workflowType: 'prd'
classification:
  projectType: 'api_backend'
  domain: 'Enterprise Sovereign Communications'
  complexity: 'high'
  projectContext: 'greenfield'
  operatorPersona: '2-3 Admins, Docker/VM, 5000+ users/instance'
---

# Product Requirements Document - Nebu

**Author:** Phil
**Date:** 2026-03-16

## Executive Summary

Nebu ist ein Matrix Client-Server API-kompatibler Chat-Server für Organisationen die digitale Kommunikationssouveränität benötigen — ohne Lizenzkosten, ohne Vendor-Lock-in, ohne Infrastruktur-Overkill. Die technische Grundlage — Go als API-Gateway, Elixir/OTP als Messaging-Core — macht NATS, Redis und vergleichbare Middleware überflüssig: Elixir/OTP ist ein erprobter Spezialist für Nachrichtenverarbeitung, der komplette Middleware-Schichten ersetzt und damit den Ops-Footprint radikal reduziert. Eine Standard-Container-Laufzeitumgebung genügt für den Produktivbetrieb.

Nebu adressiert zwei gleichwertige Zielgruppen:
- **Selbstbetreiber** — Unternehmen, Behörden, Parteien und Organisationen die eine souveräne Chat-Infrastruktur auf eigener Infrastruktur betreiben wollen; 2-3 Admins können damit Tausende Nutzer versorgen.
- **IT-Dienstleister und Hoster** — Anbieter die Nebu als DSGVO-konformen Managed Service für ihre Kunden bereitstellen, analog zum NextCloud-Hoster-Ökosystem; Apache 2.0 erlaubt kommerzielle Nutzung ohne Lizenzeinschränkungen.

### What Makes This Special

Bestehende Matrix-Server-Implementierungen haben ein unlösbares Trilemma: Synapse/Dendrite sind lizenzrechtlich problematisch (AGPLv3) oder nicht produktionsreif; die Element Server Suite liefert Enterprise-Features, schafft aber neue Abhängigkeit und Lizenzkosten; Mattermost und Rocket.Chat sind kein Matrix und haben eigene Lizenzfallen. Nebu durchbricht dieses Trilemma durch drei gleichzeitige Eigenschaften:

1. **Apache 2.0** — keine Copyleft-Pflicht, kommerzielle Nutzung und Hosting-Betrieb erlaubt
2. **Minimaler Ops-Footprint** — Go + Elixir/OTP als Duo ersetzt mehrere Middleware-Systeme; keine Spezialkenntnisse erforderlich
3. **Enterprise-Features ohne Paywall** — Ed25519-Nachrichtensignaturen, OIDC-first Auth, Audit Logging, Compliance-Zugriff mit Vier-Augen-Prinzip ab MVP

**Federation ist bewusst kein MVP-Scope.** Standard-Matrix-Clients verbinden sich mit jeder Nebu-Instanz. Cross-Instanz-Kommunikation via Federation ist architektonisch vorbereitet, aber für spätere Phasen geplant.

## Project Classification

| Dimension | Wert |
|---|---|
| Project Type | API Backend — Container-Native Infrastructure |
| Domain | Enterprise Sovereign Communications |
| Complexity | Hoch (Distributed Systems, Matrix-Protokoll-Implementierung, Go + Elixir/OTP) |
| Project Context | Greenfield |
| Zielgruppen | Selbstbetreiber (Org/Behörden) + IT-Dienstleister/Hoster |

## Success Criteria

### User Success

Ein Operator gilt als erfolgreich wenn:
- SSO-Login via OIDC-Provider (Keycloak, Azure AD, Google) mit Standard-Clients (Element, FluffyChat) ohne Client-seitige Konfigurationsänderungen funktioniert
- Eine Gruppe von 10–20 realen Nutzern eine Woche lang produktiv über Nebu kommuniziert ohne Operator-Eingriff
- Nachrichten, Räume und Präsenz-Status korrekt zwischen allen Clients synchronisieren

### Business Success

Post-Launch-Metriken (nicht MVP-relevant):
- GitHub Stars, Forks, Downloads als Adoptions-Indikatoren
- Anzahl bekannter Produktiv-Deployments in der Community
- Hoster-Ökosystem: Erste IT-Dienstleister die Nebu als Managed Service anbieten

### Technical Success

Nebu gilt als technisch erfolgreich wenn auf einem **realistischen Minimal-Setup** (≥2 Instanzen, AWS EC2 m5.large oder vergleichbar) gestaffelte Performance-Ziele erreicht werden:

| Level | Concurrent Users / Node | Bedeutung |
|---|---|---|
| 🥈 Silber | >500 | MVP-Gate — Pflicht für Release |
| 🥇 Gold | >1.000 | Growth-Validierung (Elixir Clustering) |
| 🏆 Platin | >5.000 | Architektur-Vision (Elixir/OTP Potenzial) |

**Testmethodik:** Kombination aus realen Nutzern (10–20 Personen) und LLM-Agenten bzw. Message-Bots die kontinuierlichen, realistischen Chat-Traffic simulieren. Konkrete Lastziele werden in der Architekturphase ausgearbeitet.

### Measurable Outcomes

| Outcome | Messung | Phase |
|---|---|---|
| SSO-Login | Element + FluffyChat Login via OIDC | MVP |
| Pilot-Test | 10–20 Nutzer, 1 Woche, kein Eingriff | MVP |
| Silber-Lasttest | >500 concurrent users auf 2x m5.large | MVP |
| Community-Adoption | GitHub Downloads/Stars | Post-Launch |

## Product Scope

### MVP — Minimum Viable Product

Gate-Kriterium: **SSO-Login + Pilot 10–20 Nutzer + Silber-Lasttest (>500 concurrent users)**

Enthält:
- Matrix Client-Server API: login, logout, sync, send, createRoom, join, typing, receipts, profile, presence
- OIDC-Auth out-of-the-box (Keycloak, Azure AD, Google)
- Ed25519-Nachrichtensignaturen
- Audit Logging + Compliance-Zugriff (Vier-Augen-Prinzip)
- Docker Compose — Production-ready (nicht nur Dev-Setup)
- Elixir/OTP Core: Room Process, Session Manager, Presence, Event Dispatch

### Growth Features (Post-MVP / Sales-MVP-Optionen)

- Elixir Multi-Node Clustering via `libcluster` (automatisch, kein Sidecar)
- Apple/Google Push Notifications (nativer Push über APNs/FCM)
- Room-Archivierung (read-only, Audit-Log bleibt)
- Audit-Log Feed/Hook-API für SIEM-Integration (Splunk, Elastic)
- Media Server (S3-kompatibel)
- SAML 2.0 + LDAP via OIDC-Proxy
- Developer Integrations (Webhooks, Bot-Framework)
- Gold-Lasttest-Validierung (>1.000 concurrent users im Cluster)

### Vision (Phase 3)

- Federation (Cross-Instanz via Matrix Federation Protocol)
- E2EE für private Räume (optional, client-seitig)
- Hardware-aware Room Routing (Node-Pinning nach Room-Größe/Last)
- Admin-UI als Chat-Agent (Room-basierte Admin-Interaktion via Nebu)
- MCP-Support (Integration in KI-Tooling-Ökosystem)
- Platin-Ziel: >5.000 concurrent users/Node im Cluster

### Explicit Non-Scope

- **Kubernetes:** Technisch möglich, kein supported Deployment-Target — reduziert Ops-Komplexität für die Kernzielgruppe; wer es will, kann es selbst aufsetzen
- **Matrix Federation (MVP):** Architektonisch vorbereitet, bewusst ausgeschlossen um ~40% Implementierungskomplexität zu vermeiden
- **E2EE (MVP):** Zu komplex für MVP; Server-seitige Sichtbarkeit ist für Compliance-Zielgruppe gewünscht
- **Developer Integrations (MVP):** Bots, Ticket-System-Anbindungen, Custom Clients — Post-MVP
- **Multi-Instance-Dashboard (MVP):** Single-Instance-Admin reicht für MVP

## User Journeys

### Journey 1: Der Operator — "Endlich kein Lizenzgespräch mehr"

**Persona:** Marcus, 38, Senior Sysadmin bei einer mittelgroßen Stadtverwaltung. 400 Mitarbeiter, drei IT-Mitarbeiter. Datenschutzbeauftragter hat gemahnt dass Teams-Daten auf US-Servern liegen.

**Opening Scene:** Marcus sucht seit Wochen nach einer Alternative. Synapse war ein Albtraum, der AGPLv3-Hinweis hat die Rechtsabteilung gestoppt. Element Enterprise hat ein Angebot geschickt das er nie öffnen wird.

**Rising Action:** Er findet Nebu auf GitHub. README gelesen in 10 Minuten. `docker compose up`. Drei Minuten später läuft der Stack.

**Climax:** Element Web, Nebu-URL eingetragen, Keycloak-Login funktioniert beim ersten Versuch. Erster Raum, erste Nachricht. *"Ich glaube das läuft."*

**Resolution:** Zwei Wochen später haben 50 Pilotnutzer Zugriff. Marcus hat nichts anfassen müssen. Keine Lizenzverhandlung. Kein Konzern-Support-Ticket.

**Revealed Requirements:** Docker Compose Production-ready, OIDC out-of-the-box, stabile sync-API, minimale Konfiguration

---

### Journey 1b: Marcus Edge Case — "Das Update ist schief gegangen"

**Opening Scene:** Marcus updated Nebu auf eine neue Minor-Version. Der Elixir-Core startet nicht — eine Konfigurationsvariable wurde umbenannt.

**Rising Action:** Der `/ready`-Endpoint antwortet mit 503. Docker Compose zeigt den Core als `unhealthy`. Marcus rollt zurück mit der alten Image-Version — Stack startet sofort wieder, kein Datenverlust.

**Climax:** Er liest das Changelog (Pflichtbestandteil jedes Releases), passt die eine Config-Variable an, updated erneut. Diesmal startet alles.

**Resolution:** 20 Minuten Downtime, kein Datenverlust, kein manueller DB-Eingriff. Rückwärtskompatibilität zwischen Minor Versions ist gewährleistet.

**Revealed Requirements:** `/health` + `/ready` Endpoints, Prometheus `/metrics`, Rückwärtskompatibilität Minor Updates, dokumentierter Migrationspfad für Major Updates, Changelog als Release-Pflicht

---

### Journey 2: Der End-User — "Ich merke gar nicht dass es kein Slack ist"

**Persona:** Leila, 29, Sachbearbeiterin in der gleichen Stadtverwaltung. Nutzt Element täglich auf Laptop und Handy.

**Opening Scene:** IT hat ihr eine neue Server-URL geschickt. Sie trägt sie in Element ein, loggt sich mit ihrem normalen Dienstaccount ein.

**Climax:** Drei Wochen später tippt sie auf dem Handy während der Mittagspause. Nebu existiert für sie nicht — nur Element, ihr Team, ihre Nachrichten.

**Resolution:** Leila hat nie gewusst dass der Server gewechselt hat. Das ist der Erfolg.

**Revealed Requirements:** Vollständige Matrix Client-Server API, Presence, Typing Indicators, Read Receipts, Profil

---

### Journey 3: Der Compliance Officer — "Ich brauche diese Nachricht — und einen Beweis"

**Persona:** Dr. Petra, 52, Compliance-Beauftragte eines mittelständischen Unternehmens. Behördliche Anfrage: alle Kommunikation zwischen zwei Mitarbeitern über 6 Monate.

**Rising Action:** Petra meldet sich in der Nebu Admin UI an als `compliance_officer`. Stellt Antrag mit Pflichtfeldern: Behörde, Aktenzeichen, Zeitraum, Begründung. Zwei `instance_admins` erhalten Benachrichtigung. Einer bestätigt (Vier-Augen-Prinzip). Zugriff-Timer: 24 Stunden.

**Climax:** Export als PDF mit Ed25519-Signaturen — Absender, Zeitstempel, Inhalt kryptografisch nachweisbar. Der Anwalt ist zufrieden.

**Resolution:** Nach 24 Stunden automatischer Zugriffsentzug. Audit-Log zeigt lückenlos: wer, was, wann, warum. DSGVO-konform, rechtssicher.

**Revealed Requirements:** Compliance-Zugriff mit Vier-Augen-Prinzip, Audit Log (append-only), Ed25519-Signaturen, Export-Funktion, zeitbegrenzter Zugriff, Benachrichtigungssystem

---

### Journey 4 (MVP): Der Instance Admin — "Ich sehe auf einen Blick ob alles läuft"

**Persona:** Kai, 34, DevOps-Engineer bei einem IT-Dienstleister, verwaltet eine Nebu-Instanz für eine Gemeinde.

**Opening Scene:** Montagmorgen. Kai öffnet das Nebu Admin Dashboard. Die Instanz zeigt erhöhte DB-Latenz. Er sieht den Grund: 200 neue Nutzer gestern ongeboardet.

**Rising Action:** Er deaktiviert einen gemeldeten Nutzer direkt aus der UI. Weist einer neuen Kollegin die Rolle `compliance_officer` zu. Prüft Message-Throughput und aktive Sessions — alles im grünen Bereich.

**Resolution:** Kai verwaltet die Instanz ohne CLI-Kenntnisse. Alles was er täglich braucht ist in der Admin UI.

**Revealed Requirements:** Admin UI/API, User Management (anlegen/deaktivieren/Rollen), Metriken-Dashboard (Message-Throughput, aktive Sessions, Node-Health, DB-Latenz)

---

### Journey 4b (Growth): Kai Multi-Instance — "Sechs Instanzen, ein Überblick"

Kai verwaltet 6 Instanzen für 3 Gemeinden über ein zentrales Dashboard. Eine Instanz fällt auf — er sieht es sofort ohne sich einzeln anzumelden.

**Revealed Requirements:** Multi-Instance-Dashboard (Growth-Feature)

---

### Journey 5: Der Hoster — "Ich biete das meinen Kunden an ohne Lizenzanwalt"

**Persona:** Sandra, 45, Geschäftsführerin eines kleinen IT-Dienstleisters. Betreibt NextCloud und Gitea für 40 Kunden aus Kommunen und Vereinen.

**Rising Action:** Apache 2.0 gelesen in 5 Minuten. Kein Anruf beim Hersteller. Sie baut ein Provisioning-Skript: neue Instanz per `docker compose`, OIDC-Config eingetragen, fertig.

**Climax:** Drei Monate später, 8 Kunden auf Nebu. Monatliche Rechnung: Hosting-Kosten plus Aufwand. Nebu bekommt nichts. Das ist das Modell.

**Resolution:** Neues Produkt im Portfolio ohne rechtliches Risiko — genau wie NextCloud.

**Revealed Requirements:** Klare Apache 2.0 Dokumentation, skriptbares Provisioning, stabile Upgrade-Pfade, Changelog

---

### Journey Requirements Summary

| Capability | Journey | Phase |
|---|---|---|
| OIDC Auth out-of-the-box | Marcus | MVP |
| Docker Compose Production-ready | Marcus, Sandra | MVP |
| `/health`, `/ready`, `/metrics` Endpoints | Marcus Edge-Case, Kai | MVP |
| Rückwärtskompatibilität Minor Updates | Marcus Edge-Case | MVP |
| Changelog als Release-Pflicht | Marcus Edge-Case, Sandra | MVP |
| Vollständige Matrix Client-Server API | Leila | MVP |
| Ed25519-Signaturen + Export | Dr. Petra | MVP |
| Compliance-Zugriff + Vier-Augen-Prinzip | Dr. Petra | MVP |
| Audit Log (append-only) | Dr. Petra | MVP |
| Admin UI/API + Metriken-Dashboard | Kai MVP | MVP |
| User Management via UI | Kai MVP | MVP |
| Skriptbares Provisioning | Sandra | MVP |
| Multi-Instance-Dashboard | Kai Growth | Growth |

## Domain-Specific Requirements

### Compliance & Regulatory

- **Zertifizierungs-Neutralität:** Nebu ist als Open-Source-Projekt nicht selbst zertifizierbar. Architektur darf keiner Betreiber-Zertifizierung im Wege stehen (BSI Grundschutz, ISO 27001, DSGVO). Audit Logs, Zugriffssteuerung, Verschlüsselung und Datentrennung sind als Nachweise nutzbar.
- **DSGVO / Right-to-be-forgotten:** Gelöst durch dreistufige PII-Architektur (siehe unten). Datenhaltung vollständig beim Betreiber (PostgreSQL, standort-agnostisch). DSGVO-Export und Right-to-be-forgotten sind Growth-Features, architektonisch ab MVP vorbereitet.
- **Non-Repudiation:** Ed25519-Signaturen auf jeder Nachricht — rechtssichere Nachweisbarkeit ohne externe Trust-Infrastruktur.

### Cryptographic Identity Architecture (ADR-Kandidat)

Jeder Nebu-Nutzer besitzt ein Ed25519-Keypair mit Doppelfunktion:

| Schlüssel | Funktion | Lebensdauer |
|---|---|---|
| Public Key | Verifikation von Nachricht-Signaturen | Permanent (Non-Repudiation) |
| Private Key | Verschlüsselung sensitiver PII (E-Mail, HR-Daten) | Bis Right-to-be-forgotten |

**Drei PII-Tiers:**

1. **Operational PII** (Display Name, Avatar) — Encryption at rest, Server entschlüsselt für Matrix Profile-API während User aktiv ist. Right-to-be-forgotten = Überschreiben mit `"Deleted User [anonymized-id]"`. Historische Nachrichten zeigen danach anonymisierten Sender.

2. **Sensitive PII** (E-Mail, weitere persönliche Daten) — Mit User's Private Key verschlüsselt in DB. Right-to-be-forgotten = Private Key löschen → nicht mehr entschlüsselbar, FK-Referenz bleibt für DB-Integrität.

3. **Message Content** — Kein PII, bleibt unverändert im Audit-Log. Signatur mit Public Key dauerhaft verifizierbar.

**Resultat:** Aktive Nutzer sind im Unternehmenskontext immer im Klartext sichtbar. Ausgeschiedene Nutzer werden DSGVO-konform vergessen ohne Audit-Log-Integrität zu verletzen — identisches Verhalten zu Slack/Teams beim Account-Löschen.

### Technical Constraints

- **Transport-Verschlüsselung extern:** TLS 1.3 auf allen Client↔Gateway-Verbindungen — Pflicht.
- **Transport-Verschlüsselung intern (Gateway↔Elixir-Core):**
  - MVP: Auto-negotiierter Schlüssel im Docker-Container-Netz, kein manuelles CA-Setup
  - Growth: Konfigurierbar — eigene CA oder automatische Aushandlung
- **Datenhaltung:** Standort-agnostisch — PostgreSQL beliebig hostbar (on-premise, EU-Cloud, etc.). Nebu macht keine Annahmen über Speicherort.
- **Availability by Architecture:** Robustheit durch Go (stateless, crash-resistant) + Elixir/OTP (Supervisor Trees, self-healing). HA ist Betreiber-Entscheidung:
  - Single Node: Robust, kein HA
  - Multi-Node Cluster: HA (Growth via `libcluster`)

### Risk Mitigations

| Risiko | Mitigation |
|---|---|
| Betreiber-Compliance scheitert wegen fehlender Audit-Trails | Append-only Audit Log ab MVP |
| DSGVO-Konflikt mit Append-Only-Log | Dreistufige PII-Architektur mit Cryptographic Deletion |
| Datenverlust bei Updates | Rückwärtskompatibilität Minor Versions, Migrationspfad Major Versions |
| Single Node als SPOF | Elixir Multi-Node Clustering (Growth), OTP reduziert MTTR |
| Unberechtigter Nachrichten-Zugriff | Vier-Augen-Prinzip, zeitbegrenzter Zugriff, vollständiges Audit |
| Fehlender Security-Audit blockiert Enterprise-Adoption | Externer Security-Audit als geplantes Roadmap-Item post-MVP |

## Innovation & Novel Patterns

### Detected Innovation Areas

**1. Cryptographic DSGVO-Compliance Architecture**
Nebu löst den Widerspruch zwischen unveränderlichem Audit-Log und DSGVO Right-to-be-forgotten durch einen neuartigen kryptografischen Ansatz: Ed25519-Keypairs werden doppelt genutzt — für Nachricht-Signing (Non-Repudiation) und PII-Verschlüsselung (Cryptographic Deletion). Key-Deletion anonymisiert den Sender ohne Message-Integrität zu verletzen. Kein bekannter Matrix-Server implementiert dieses Muster.

**2. Compliance-First statt Privacy-First**
Bewusste Umkehrung des Mainstream-Trends: Kein E2EE by design wird als Compliance-Feature positioniert — Server-Sichtbarkeit ermöglicht eDiscovery, Audit und Strafverfolgung ohne Schlüssel-Escrow-Komplexität. Zielgruppe: Enterprises und Behörden die Messaging-Souveränität mit rechtlicher Nachweispflicht kombinieren müssen.

**3. Middleware-Elimination durch Elixir/OTP**
Architektur-These: Go + Elixir/OTP macht dedizierte Message-Broker (NATS, Kafka) und Cache-Systeme (Redis) strukturell überflüssig. Elixir/OTP übernimmt diese Aufgaben nativ — weniger Moving Parts, geringere Ops-Komplexität, nachgewiesene WhatsApp-Skalierung auf der gleichen VM-Basis.

**4. Apache 2.0 als Go-to-Market-Instrument**
Lizenz-Design als Wachstumsstrategie: Apache 2.0 ermöglicht kommerzielle Hoster ohne Lizenzvertrag — ein NextCloud-analoges Ökosystem entsteht durch finanziellen Eigenantrieb der IT-Dienstleister. Kein Marketing-Budget nötig für Adoption.

**5. OIDC-Only Authentication — Innovation durch Weglassen**
Nebu implementiert OIDC als einzigen Auth-Pfad — keine lokalen Accounts, kein Passwort-Hashing, kein Account-Recovery-Flow, kein Brute-Force-Schutz auf Auth-Ebene. Der Identity Provider übernimmt vollständig. Bei bestehenden Matrix-Servern ist SSO/OIDC ein Enterprise-Lizenz-Feature oder aufwändige Konfiguration. Bei Nebu ist es der einzige Weg.

Konsequenzen:
- Weniger Code = kleinere Angriffsfläche
- Kein Shadow-Directory, kein Passwort-Sync-Problem für Hoster
- Nahtlose Integration in bestehende Enterprise-Identity-Infrastruktur
- Bewusste Zielgruppen-Entscheidung: wer keinen OIDC-Provider hat, ist nicht die Zielgruppe

### Market Context & Competitive Landscape

Kein existierender Matrix-Server vereint alle fünf Innovationsmuster gleichzeitig:
- Synapse/Dendrite: Kein kryptografisches DSGVO-Muster, AGPLv3, SSO nur mit Aufwand
- Element Server Suite: Lizenzkosten, kein Hoster-Ökosystem-Modell, SSO hinter Paywall
- Conduit/Conduwuit: Kein Enterprise-Compliance-Fokus, kein OIDC-First

### Validation Approach

| Innovation | Validierung | Gate |
|---|---|---|
| Cryptographic DSGVO-Architecture | Rechtsanwalt + Datenschutzbeauftragter reviewen Konzept | Pre-MVP |
| Middleware-Elimination These | Silber-Lasttest: >500 concurrent users ohne Redis/NATS | MVP |
| OIDC-Only Auth | Login mit Keycloak + Azure AD + Google out-of-the-box | MVP |
| Hoster-Ökosystem | Erster externer IT-Dienstleister deployt Nebu produktiv | Post-Launch |

### Risk Mitigation

| Innovationsrisiko | Mitigation |
|---|---|
| Cryptographic Deletion rechtlich nicht anerkannt | Frühzeitiger Legal-Review, Fallback: physische Pseudonymisierung |
| Elixir/OTP reicht nicht für Middleware-Ersatz | Lasttest validiert These früh; Architektur lässt Redis optional |
| OIDC-Only schließt zu viele Nutzer aus | Scope-Entscheidung bewusst — Zielgruppe hat OIDC-Provider |
| Hoster-Ökosystem entsteht nicht organisch | Community-Building, Hoster-Dokumentation ab MVP |

## API Backend Specific Requirements

### Project-Type Overview

Nebu exponiert zwei distinkte API-Oberflächen: das Matrix Client-Server API (Protokoll-Standard) und eine eigene Nebu Admin API. Zusätzlich liefert der Go-Gateway eine integrierte Admin-UI — kein separates Frontend-Deployment.

### API Structure & Versioning

| Namespace | Prefix | Zweck |
|---|---|---|
| Matrix Protocol API | `/_matrix/client/v3/` | Standard Matrix Clients |
| Nebu Admin API | `/api/v1/` | Programmatische Integrationen, OpenAPI-dokumentiert |
| Admin UI | `/admin/` | Server-gerendert, URL-basierter State |

### Admin UI Architecture

**Prinzip:** URLs tragen den vollständigen UI-Zustand — jeder View ist bookmarkbar und teilbar ohne Client-Side-State.

- **Go-Templates** — Server-Side Rendering des HTML-Gerüsts
- **Vue.js (minimal)** — Nur für reaktive Komponenten: Live-Metriken-Charts, Real-Time-Notifications
- **Server-Sent Events** — Live-Metriken-Streaming (Message-Throughput, aktive Sessions, Node-Health)
- **Kein Client-Side-Router, kein State-Management-Framework**
- In Go-Gateway integriert — ein Binary, ein Container, ein Update

### Authentication Model

**Matrix API:** OIDC-only via `m.login.sso` — keine lokalen Accounts, kein Passwort-Auth.

**Admin UI + Admin API:** OIDC-basiert mit konfiguriertem Claim-to-Role Mapping:

```yaml
oidc:
  issuer: https://keycloak.example.com/realms/company
  client_id: nebu
  claim_mappings:
    instance_admin:
      claim: "roles"
      value: "nebu_admin"
    compliance_officer:
      claim: "roles"
      value: "nebu_compliance"
```

Provider-agnostisch — kompatibel mit Keycloak, Azure AD, Google und jedem OIDC-konformen Provider.

**Bootstrap-Modus:** Beim ersten Start ohne konfigurierten `instance_admin` wird der erste OIDC-Login automatisch als `instance_admin` gesetzt. Bootstrap-Modus danach permanent deaktiviert. Kein Default-Passwort, kein unsicherer Fallback.

### Endpoint Specification (MVP)

**Matrix Client-Server API:**
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
GET/PUT /_matrix/client/v3/profile/{userId}
GET  /_matrix/client/v3/presence/{userId}/status
```

**Nebu Admin API (MVP):**
```
GET    /api/v1/health
GET    /api/v1/ready
GET    /api/v1/metrics
GET    /api/v1/admin/users
POST   /api/v1/admin/users
PUT    /api/v1/admin/users/{userId}
DELETE /api/v1/admin/users/{userId}
GET    /api/v1/admin/rooms
GET    /api/v1/compliance/access
POST   /api/v1/compliance/access
GET    /api/v1/compliance/audit-log
GET    /api/v1/openapi.json
```

### Data Schemas

- **Matrix Events:** JSON gemäß Matrix Specification + Ed25519-Signatur-Feld
- **Admin API:** JSON, OpenAPI-Spec unter `/api/v1/openapi.json` ab MVP
- **Persistenz:** PostgreSQL append-only Event Log + relationale Tabellen

### Rate Limits

**Explicit Non-Scope MVP.** Verantwortung vorgelagerter Infrastruktur (Nginx, Traefik, Cloud LB). Langfristig: konfigurierbar via Admin API.

### Implementation Considerations

- Go Gateway: HTTP-Routing, TLS, OIDC-Validierung, Admin-UI-Serving, Go-Templates
- Elixir/OTP Core: Business-Logik, State, Event-Dispatch
- Gateway↔Core: gRPC (protobuf)
- Stateless Gateway — horizontale Skalierung ohne Session-Affinity

## Scoping & Release Strategy

### MVP Philosophy

**Specification-Driven Development (SDD):** Jede Funktion beginnt mit einer Spec — Gherkin-Szenarien werden vor der Implementierung geschrieben und sind das primäre Quality Gate. Implementation follows spec, not the other way around.

**Agent-Driven Development (Entwicklungsstrategie):** Nebu wird mit Hilfe von LLM-Agenten entwickelt. Das ist eine Entwicklungsmethodik, keine Laufzeit-Abhängigkeit — Nebu benötigt zum Betrieb keinen KI-Agent.

**Flat Architecture:** Keine MVC-Layer, keine künstlichen Schichten. Die Architekturgrenze ist die gRPC-Schnittstelle zwischen Gateway und Core. Innerhalb der Komponenten: direkter Weg von Request zu Response.

**Testing-Strategie:**
- **Gherkin-Integration-Tests** — Primäres Quality Gate; alle Must-Have-Features müssen vollständig durch Szenarien abgedeckt sein
- **Unit-Tests für Crypto und komplexe Logik** — Ed25519-Operationen, PII-Verschlüsselung, Compliance-Audit-Signierung zwingend durch Unit-Tests abgesichert
- **Flache Architektur = wenige Units** — Wo keine Abstraktionsschichten, entstehen keine künstlichen Unit-Test-Targets; Fokus liegt auf komplexen, kritischen Einheiten

**Open-Source-Qualitätsstandard:** Code-Qualität muss externes Review standhalten — öffentlich lesbar, verständlich ohne Insider-Wissen.

### MVP Feature Set

| Feature-Bereich | Must-Have | Scope-Begründung |
|---|---|---|
| Matrix Client-Server API (Core) | Ja | Ohne das ist es kein Matrix-Server |
| OIDC-Only Authentication | Ja | Fundament der Sicherheitsarchitektur |
| Bootstrap-Modus | Ja | Deployment ohne Henne-Ei-Problem |
| Ed25519 Keypair (Signing + PII) | Ja | Non-Repudiation + DSGVO-Compliance |
| Three PII Tiers | Ja | DSGVO ohne Audit-Log-Verlust |
| Compliance Access (Four-Eyes) | Ja | Kaufkriterium für Behörden/Parteien |
| Append-Only Audit Log | Ja | Rechtliche Anforderung |
| Admin API (`/api/v1/`) | Ja | Monitoring, User-Management, Compliance |
| Admin UI (Go-Templates + Vue.js minimal) | Ja | Operatoren brauchen UI ohne ext. Tools |
| Observability Endpoints (`/health`, `/ready`, `/metrics`) | Ja | Standard-Monitoring-Integration |
| TLS (mTLS optional) | Ja | Sicherheits-Grundlage |
| PostgreSQL Persistenz | Ja | Einziger Datenspeicher |
| Horizontal Scaling (Stateless Gateway) | Ja | Silber-Tier-Voraussetzung |
| Performance: Silber-Tier (>500 concurrent/m5.large) | Ja | MVP-Qualitätsgate |

### Explicit MVP Non-Scope

| Feature | Status | Begründung |
|---|---|---|
| Matrix Federation | Non-Scope MVP | ~40% Komplexitätsreduktion; architektonisch vorbereitet |
| End-to-End Encryption (E2EE) | Non-Scope MVP | Optional für private Rooms in Phase 2+ |
| Rate Limiting (Nebu-intern) | Non-Scope MVP | Verantwortung vorgelagerter Infrastruktur |
| Kubernetes-Support | Non-Scope permanent | Docker Compose ist Ziel-Deployment; K8s möglich, aber unsupported |
| Multi-Tenancy | Non-Scope MVP | Separate Instanz pro Organisation ist das Modell |
| Developer Integrations (Webhooks, Bots-Framework) | Non-Scope MVP | Phase 2 |
| Admin-UI-Chat-Agent (Room-basiert) | Zukunft (Phase 3+) | Interessant, aber erst wenn Basis stabil |
| MCP-Support | Zukunft (Phase 3+) | Innovativ, aber kein MVP-Kriterium |

### Phase 2 — Growth

**Trigger:** MVP ist stabil und hat in Pilot-Deployments überzeugt. Einzelne Phase-2-Features können vorgezogen werden, wenn sie für ein Sales-MVP mit konkreten Enterprise-Kunden notwendig sind.

| Feature | Priorität |
|---|---|
| libcluster (automatische Elixir-Node-Discovery) | Hoch |
| OIDC Claim-to-Role Mapping (erweitertes Schema) | Hoch |
| E2EE für private Rooms (optional) | Mittel |
| Rate Limiting (konfigurierbar via Admin API) | Mittel |
| Developer Integrations (Webhooks, Bots) | Mittel |
| Performance: Gold-Tier (>1000 concurrent) | Hoch |

### Phase 3 — Vision

| Feature | Beschreibung |
|---|---|
| Matrix Federation | Interoperabilität mit anderen Matrix-Homeservern |
| Hoster-Ökosystem | Deployment-Tooling, Marketplace, Managed-Offering-Basis |
| Admin-UI als Chat-Agent | Room-basierte Admin-Interaktion via Nebu selbst |
| MCP-Support | Integration in KI-Tooling-Ökosystem |
| Performance: Platin-Tier (>5000 concurrent) | Top-Enterprise-Deployments |

### MVP Completion Criteria

MVP gilt als abgeschlossen wenn:
1. Alle Gherkin-Szenarien für Must-Have-Features sind grün
2. Silber-Lasttest bestanden: >500 concurrent users auf m5.large ohne Redis/NATS/Kafka
3. Pilot-Deployment mit realem OIDC-Provider (Keycloak oder Azure AD) erfolgreich
4. Compliance-Access-Flow (Four-Eyes, 24h-Limit, signierter Export) vollständig testbar

### Risk Mitigation

| Risiko | Mitigation |
|---|---|
| Gherkin-Szenarien decken nicht alles ab | Crypto-Operationen und komplexe Logik zusätzlich durch Unit-Tests abgesichert |
| Scope-Creep durch frühe Phase-2-Features | Explizite Sales-MVP-Entscheidung nötig — nicht stillschweigend |
| Flat Architecture wird später bereut | ADR dokumentiert Entscheidung; gRPC-Grenze bietet ausreichend Struktur |
| Agent-driven Dev schafft inkonsistenten Code | Open-Source-Qualitätsstandard + Code-Review als Korrektiv |

## Functional Requirements

### Identity & Authentication

- FR1: End-User kann sich via SSO mit jedem OIDC-konformen Identity Provider anmelden
- FR2: End-User kann sich abmelden und seine Session ungültig machen
- FR3: Instance Admin kann OIDC-Provider-Konfiguration verwalten (Issuer, Client-ID, Claim-Mappings)
- FR4: System weist Rollen (`instance_admin`, `compliance_officer`) anhand konfigurierbarer OIDC-Claim-Mappings zu
- FR5: System weist beim ersten OIDC-Login automatisch `instance_admin` zu, wenn noch kein Admin existiert (Bootstrap Mode)
- FR6: System deaktiviert Bootstrap Mode permanent nach erstem Admin-Setup

### Messaging & Rooms

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

### Room-Konfiguration

- FR17: Room Owner kann Sichtbarkeit des Rooms definieren (öffentlich, privat, einladungsbasiert)
- FR18: Room Owner kann Room-Metadaten pflegen (Name, Beschreibung, Topic, Avatar)
- FR19: Room Owner kann Zugriffskontrolle konfigurieren (wer darf beitreten, lesen, schreiben)
- FR20: End-User kann andere User in einen Room einladen
- FR21: End-User kann eine Room-Einladung annehmen oder ablehnen
- FR22: Instance Admin kann alle Room-Owner-Einstellungen für jeden Room auf der Instanz verwalten
- FR23: Instance Admin kann eine maximale Mitgliederzahl pro Room konfigurieren
- FR24: Instance Admin kann serverweite Standard-Room-Einstellungen festlegen (Default für neue Rooms)

### Cryptographic Identity & PII

- FR25: System generiert pro User-Identität ein Ed25519-Schlüsselpaar
- FR26: System signiert ausgehende Matrix-Events mit dem privaten Ed25519-Schlüssel des Users
- FR27: System verschlüsselt Sensitive PII (E-Mail, IdP-Subject) mit dem öffentlichen Ed25519-Schlüssel
- FR28: System kann privaten Ed25519-Schlüssel eines Users löschen und Sensitive PII kryptografisch unlesbar machen (Right to be Forgotten)
- FR29: System anonymisiert Operational PII (Anzeigename) bei Kontolöschung zu "Deleted User"

### Compliance & Audit

- FR30: Compliance Officer kann temporären Zugriff auf Nachrichteninhalte mit dokumentierter Begründung beantragen
- FR31: System erzwingt Vier-Augen-Prinzip für Compliance-Zugriffe
- FR32: System begrenzt Compliance-Zugriffssessions auf maximal 24 Stunden
- FR33: Compliance Officer kann Nachrichtendaten mit Ed25519-signiertem Audit-Trail exportieren
- FR34: System führt Append-Only Audit Log aller Compliance-Zugriffsevents
- FR35: System protokolliert alle administrativen Aktionen im Audit Log

### User & Room Administration

- FR36: Instance Admin kann alle User einer Instanz auflisten
- FR37: Instance Admin kann User-Accounts anlegen und deaktivieren
- FR38: Instance Admin kann User-Attribute aktualisieren
- FR39: Instance Admin kann alle Rooms einer Instanz auflisten
- FR40: Instance Admin kann Room-Mitgliedschaft und -Einstellungen verwalten

### Notifications

- FR41: System benachrichtigt User über neue Nachrichten gemäß konfigurierbarer Push-Regeln (Matrix-native; Apple/Google-Push: Phase 2 / Sales-MVP-Option)
- FR42: End-User kann Push-Benachrichtigungsregeln pro Room und Event-Typ konfigurieren

### Server Operations & Observability

- FR43: Operator kann Nebu mittels Docker Compose deployen und betreiben
- FR44: System exponiert Health- und Readiness-Endpunkte für Monitoring-Integrationen
- FR45: System exponiert Metrics-Endpunkt kompatibel mit Standard-Monitoring (Prometheus)
- FR46: System unterstützt horizontale Skalierung des Go-Gateways ohne Session-Affinity
- FR47: System unterstützt TLS-Terminierung (mTLS optional konfigurierbar)

### Admin Interface & API

- FR48: Instance Admin kann alle Verwaltungsfunktionen über eine web-basierte Admin UI bedienen
- FR49: Instance Admin kann Live-Server-Metriken (Durchsatz, aktive Sessions, Node-Health) in der Admin UI einsehen
- FR50: Alle Admin-UI-Zustände sind über URLs adressierbar (bookmarkbar, teilbar)
- FR51: Developer/Operator kann alle Admin-Funktionen programmatisch über eine REST-API nutzen
- FR52: System stellt OpenAPI-Spezifikation der Admin API bereit

## Non-Functional Requirements

### Performance

- **NFR-P1:** Nachrichtenversand (End-to-End via Matrix API) ≤ 500ms Latenz unter Silber-Last (500 concurrent users auf m5.large)
- **NFR-P2:** Matrix `/sync`-Endpunkt antwortet ≤ 1s unter Normallast
- **NFR-P3:** System erreicht Silber-Tier (>500 concurrent/m5.large), Gold-Tier (>1000), Platin-Tier (>5000) ohne Redis/NATS/Kafka
- **NFR-P4:** Gateway-Kaltstart-Zeit ≤ 5s (stateless, schneller Neustart)

### Security

- **NFR-S1:** Alle externen Verbindungen via TLS 1.2 minimum (TLS 1.3 bevorzugt)
- **NFR-S2:** Sensitive PII ist at-rest verschlüsselt (Ed25519-Keypair); Operational PII ist at-rest verschlüsselt
- **NFR-S3:** Audit Log ist append-only und Ed25519-signiert — Manipulation nachweisbar
- **NFR-S4:** OIDC-Token-Validierung bei jedem API-Request — kein Session-State im Gateway
- **NFR-S5:** Ed25519-Schlüssellöschung ist irreversibel — kein Recovery-Pfad by design (DSGVO-Konformität)
- **NFR-S6:** Bootstrap Mode deaktiviert sich permanent und unwiderruflich nach erstem Admin-Setup

### Scalability

- **NFR-SC1:** Go Gateway horizontal skalierbar ohne Session-Affinity (beliebig viele Instanzen hinter Load Balancer)
- **NFR-SC2:** Elixir/OTP Core unterstützt Cluster-Betrieb (Phase 2: automatisch via libcluster)
- **NFR-SC3:** Kein externer Middleware-Layer erforderlich — PostgreSQL ist einziger Persistenz-Layer

### Reliability

- **NFR-R1:** Elixir/OTP Process-Isolation: Absturz eines Room-Prozesses betrifft keine anderen Rooms oder Sessions
- **NFR-R2:** Kein Datenverlust bei Gateway-Neustart — PostgreSQL ist Single Source of Truth
- **NFR-R3:** Rolling Updates ohne vollständige Downtime möglich (Stateless Gateway ermöglicht zero-downtime Deploys)

### Operability

- **NFR-O1:** Vollständiges Deployment via `docker-compose up` auf einer frischen Instanz in ≤ 10 Minuten
- **NFR-O2:** Health/Readiness-Endpunkte antworten ≤ 200ms auch unter Last
- **NFR-O3:** Admin UI vollständig im Gateway-Binary integriert — keine externen Abhängigkeiten zur Laufzeit
- **NFR-O4:** Alle Admin-UI-Zustände via URL reproduzierbar — kein Browser-State, kein LocalStorage-Zwang

### Compliance & Datenschutz

- **NFR-C1:** Right to be Forgotten implementiert durch kryptografische Schlüssellöschung — DSGVO-konform ohne Bruch der Audit-Log-Integrität
- **NFR-C2:** Audit-Log-Aufbewahrungsdauer konfigurierbar (Default: 7 Jahre, anpassbar an nationale Aufbewahrungsfristen)
- **NFR-C3:** Alle Daten liegen ausschließlich in der konfigurierten PostgreSQL-Instanz — kein Cloud-Service, kein externer Dienst erforderlich (On-Premise-fähig)

### Matrix-Protokoll-Konformität

- **NFR-M1:** Matrix Client-Server API kompatibel mit gängigen Standard-Clients (Element, FluffyChat, Hydrogen) — Inkompatibilitäten gelten als Bugs
- **NFR-M2:** OIDC-Integration via `m.login.sso` gemäß Matrix OIDC Specification

### Accessibility

- **NFR-A1:** Admin UI erfüllt WCAG 2.1 Level AA (Mindeststandard für öffentliche Stellen in Deutschland/EU)
- **NFR-A2:** Admin UI ist vollständig per Tastatur navigierbar (kein Maus-Zwang)
- **NFR-A3:** Admin UI ist mit gängigen Screen Readern nutzbar (semantisches HTML, ARIA-Labels wo nötig)

### Lokalisierung

- **NFR-L1:** Admin UI unterstützt Deutsch und Englisch; bevorzugte Sprache wird aus OIDC-Claim oder User-Profil-Einstellung bezogen

### Protokoll & Standards

- **NFR-I1:** Nebu verwendet ausschließlich offene Standards (Matrix, OIDC, OpenAPI, Prometheus) — keine proprietären Protokolle, keine externen Cloud-Service-Abhängigkeiten; Integrationen mit Umsystemen (SIEM, Kafka, etc.) sind Extensions, nicht Nebu-Core (Phase 2)
- **NFR-I2:** Matrix-Events haben eine konfigurierbare maximale Payload-Größe (Default: 65KB gemäß Matrix-Spec)

### Crypto-Agilität

- **NFR-CR1:** Kryptografische Primitive (Signing-Algorithmus, Verschlüsselungsverfahren) sind intern modularisiert und durch neuere Verfahren austauschbar — keine Hardcodierung von Ed25519 als einziger möglicher Algorithmus

