# Nebu Roadmap

This document provides a roadmap derived from Nebu's epic structure and current sprint status.
Epics 1–8 are complete (pre-alpha). Epic 9+ is planned.

## Current Status (2026-05-01)

**Pre-alpha.** Core chat functionality is implemented and the codebase is in public open-source
readiness. Not production-ready — see [Current Limitations](../README.md#current-limitations).

---

## Completed Epics

### Epic 1–2: Foundation + OIDC Login

**Status:** Done

- Go Gateway scaffolding (migrations, health, config)
- Elixir/OTP Core umbrella (Room Manager, Session Manager, Presence, Event Dispatcher)
- gRPC CoreService (protobuf definitions + generated stubs)
- OIDC authentication via Dex (Authorization Code + PKCE)
- Bootstrap Wizard (first-run experience, first admin setup)
- PostgreSQL schema migrations (golang-migrate)

**Key FRs:** FR1–6 (Identity & Authentication), FR43–47 (Server Operations)

---

### Epic 3: Ed25519 Signatures + Audit Log + Media

**Status:** Done

- Ed25519 keypair generation per user (Signing + X25519 Encryption)
- Message signing via `Nebu.Signature` (all events)
- Content-hash event IDs (`Nebu.EventId.generate/1`)
- Append-only audit log with Ed25519-signed entries
- RLS policies on `audit_logs` table
- Minimal Media Gateway (AES-256-GCM upload/download)

**Key FRs:** FR25–29 (Cryptographic Identity), FR34–35 (Audit Log)

---

### Epic 4: Full Matrix Client-Server API MVP

**Status:** Done

- `/sync` long-poll with ETS + gRPC EventBus
- `PUT /rooms/{id}/send` → gRPC SendEvent
- `GET /rooms/{id}/messages` (paginated)
- `POST /createRoom`, `POST /join`, presence, typing, receipts
- Profile (GET/PUT displayname + avatar_url)
- `GET /presence/{userId}/status`
- message_buffer (GRÜN/GELB/ROT state machine + linear drain)

**Key FRs:** FR7–16 (Messaging & Rooms)

---

### Epic 5: Security Hardening + Compliance Access

**Status:** Done

- Rate limiting (per-IP, 5 tiers)
- Body size limits (1 MiB Matrix, 64 KiB admin)
- CSRF double-submit cookie for Admin UI
- Security headers middleware (CSP, HSTS, X-Frame-Options)
- Compliance Access API (four-eyes approval, 24h session)
- Compliance Data Export (Ed25519-signed JSON/PDF)
- GDPR User Key Deletion + PII Anonymization
- Server timeout hardening (Slowloris protection)
- Audit log retention scheduler (configurable, default 7 years)
- Compliance signing key AES-256-GCM encryption

**Key FRs:** FR30–35 (Compliance & Audit)

---

### Epic 6: Admin API (OpenAPI Spec-First)

**Status:** Done (story 6-7 in progress)

- OpenAPI 3.1 spec (`gateway/api/openapi.yaml`)
- `oapi-codegen` StrictServerInterface
- Admin API: List/Get users (cursor pagination, email masking)
- User deactivation + reactivation + session invalidation
- Role assignment API (DB override with 60s TTL cache)
- Room List + Get API

**Key FRs:** FR36–40 (User & Room Administration), FR51–52 (Admin API + OpenAPI)

---

### Epic 7: Admin UI (Full Day-to-Day Operations)

**Status:** Done

- Dashboard with SSE live metrics (message throughput, active sessions)
- User management UI (list, detail, update role, deactivate/reactivate)
- Room management UI (list, detail, update name, archive/unarchive)
- Compliance Access Requests UI (four-eyes approval)
- Audit Log UI (read-only view with pagination)
- Server Configuration page
- Role Mapping configuration page
- All Matrix API gap stories closed (push rules, devices, account_data, tags, public rooms, event context, notifications, moderation, room state, joined members, aliases)
- Security fixes (SEC Gate 2): kick/ban caller_id, system-role bypass in room state

**Key FRs:** FR48–50 (Admin UI), complete Matrix API surface

---

### Epic 8: Public Open-Source Release

**Status:** Done

- README, CONTRIBUTING.md, SECURITY.md, CODE_OF_CONDUCT.md
- GitHub / GitLab dual-host CI (GitHub Actions + GitLab CI)
- Issue and PR templates
- Secret scanning (gitleaks)
- Release readiness gate
- Full-Stack Acceptance Test (Playwright e2e, 6 flows)

---

## Planned Epics

### Epic 9: Documentation + FTS + Managed E2EE

**Status:** In progress

| Story | Description | Status |
|---|---|---|
| 9-1 | arc42 architecture docs from BMAD artifacts + bmad-generate-arc42 skill | In progress |
| 9-2 | ADR-010: Full-Text Search (PostgreSQL tsvector vs. pgvector) | Planned |
| 9-3 | ADR-011: Managed E2EE Key Escrow design | Planned |

---

### Epic 10: Clustering + GDPR Hardening

**Status:** Planned

- Elixir multi-node clustering via `libcluster` (automatic node discovery)
- GDPR Right-to-be-Forgotten full pipeline (key deletion + audit trail + export)
- Audit log retention policy UI
- Performance: Gold-tier target (>1000 concurrent users)

---

### Phase 3 Vision (Future)

| Feature | Description |
|---|---|
| Matrix Federation | Interoperability with Synapse/Conduit via Server-Server API |
| Apple/Google Push | APNs/FCM push notifications for mobile clients |
| S3 Media Backend | S3-compatible storage for media files |
| Admin-UI-as-Chat-Agent | Room-based admin interaction via Nebu itself |
| MCP Support | Integration in AI tooling ecosystem |
| Platinum-Tier Scale | >5000 concurrent users per node in cluster |

---

## Performance Tier Goals

| Tier | Target | Gate |
|---|---|---|
| Silver | >500 concurrent users / m5.large | MVP release gate |
| Gold | >1000 concurrent users / cluster | Epic 10 |
| Platinum | >5000 concurrent users / cluster | Phase 3 |

Traffic mix for loadtest: 60% sync, 20% send, 10% presence+typing, 5% room ops, 5% profile+misc.

_Source: `_bmad-output/planning-artifacts/epics.md`; `_bmad-output/planning-artifacts/prd.md`, §Scoping & Release Strategy; `_bmad-output/implementation-artifacts/sprint-status.yaml`_
