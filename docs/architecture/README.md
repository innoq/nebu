# 1 Introduction and Goals

Nebu is an enterprise-grade, Matrix Client-Server API compatible chat server built for organizations
that require full data sovereignty — on-premise deployment, no vendor lock-in, no telemetry, no
federation overhead. It is licensed under Apache 2.0, enabling commercial use and hosted services
without copyleft restrictions.

## Quality Goals

| Priority | Quality Goal | Motivation |
|---|---|---|
| 1 | Matrix Client-Server API compatibility | Standard clients (Element, FluffyChat, Cinny) work without modification |
| 2 | Data sovereignty | All data resides in operator-controlled PostgreSQL; no cloud service required |
| 3 | Minimal operational footprint | Go + Elixir/OTP replace Redis, NATS, Kafka; `docker compose up` in ≤10 min |
| 4 | Enterprise compliance-ready | Append-only audit log, four-eyes principle, Ed25519 non-repudiation built-in |
| 5 | Horizontal scalability | Stateless Go gateway, clustered Elixir core (libcluster + Horde) |

## Stakeholders

| Role | Concern |
|---|---|
| **Operator (Marcus)** | Self-hosted deployment via Docker Compose; OIDC out-of-the-box; stable upgrade path |
| **End User (Leila)** | Transparent Matrix client experience; presence, typing indicators, read receipts |
| **Compliance Officer (Dr. Petra)** | Four-eyes compliance access; Ed25519-signed export; 24h session limit |
| **Instance Admin (Kai)** | Admin UI for day-to-day ops; Prometheus metrics; user and room management |
| **Hoster (Sandra)** | Apache 2.0 for commercial managed-service; scriptable provisioning |

## System Context

```
          ┌──────────────────────────────────────────┐
          │  Matrix Clients (Element, Cinny, …)      │
          └───────────────────┬──────────────────────┘
                              │ HTTPS / WSS (TLS 1.3)
          ┌───────────────────▼──────────────────────┐
          │  Go Gateway        │  Go Media Gateway   │
          │  (stateless)       │  (AES-256-GCM)      │
          └───────────────────┬──────────────────────┘
                              │ gRPC
          ┌───────────────────▼──────────────────────┐
          │  Elixir/OTP Core                         │
          │  Horde · libcluster · ETS · pg groups    │
          │  GenServers for rooms, sessions, presence│
          └───────────────────┬──────────────────────┘
                              │
          ┌───────────────────▼──────────────────────┐
          │  PostgreSQL 16+                          │
          │  event log · signatures · audit · FTS    │
          └──────────────────────────────────────────┘
```

**Three runtime components:** a Go binary (gateway + media), an Elixir release (core), and PostgreSQL.
No Redis, no NATS, no Kafka.

## Functional Requirements Overview

52 functional requirements across 8 categories:
- **Identity & Authentication (FR1–6):** OIDC-only SSO, bootstrap mode, role assignment
- **Messaging & Rooms (FR7–16):** create, join, send, sync, profile, presence, typing, receipts
- **Room Configuration (FR17–24):** visibility, membership, moderation, power levels
- **Cryptographic Identity & PII (FR25–29):** Ed25519 keypairs, signing, PII encryption, GDPR deletion
- **Compliance & Audit (FR30–35):** four-eyes access, append-only audit log, Ed25519-signed export
- **User & Room Administration (FR36–40):** list, create, deactivate, manage via Admin API
- **Notifications (FR41–42):** push rules, pushers (Apple/Google push: Phase 2)
- **Server Operations & Observability (FR43–52):** Docker Compose, health/ready/metrics, Admin UI

_Source: `_bmad-output/planning-artifacts/prd.md`, §Goals, §User-Personas, §Functional Requirements_
