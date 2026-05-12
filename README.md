# Nebu

<!-- markdownlint-disable MD013 MD042 MD051 -->
<!-- Repo status -->
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square&color=185FA5)](LICENSE)
[![Status](https://img.shields.io/badge/status-pre--alpha-BA7517?style=flat-square)](#)

<!-- Build status -->
[![GitHub Actions](https://github.com/innoq/nebu/actions/workflows/ci.yml/badge.svg)](https://github.com/innoq/nebu/actions)
[![GitLab CI](https://gitlab.opencode.de/nebu/nebu-server/badges/main/pipeline.svg)](https://gitlab.opencode.de/nebu/nebu-server/-/pipelines)

<!-- Tech stack -->
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Erlang/OTP](https://img.shields.io/badge/Erlang%2FOTP-27+-A90533?style=flat-square&logo=erlang&logoColor=white)](https://www.erlang.org)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16+-336791?style=flat-square&logo=postgresql&logoColor=white)](https://postgresql.org)
[![Docker](https://img.shields.io/badge/Docker-compose-2496ED?style=flat-square&logo=docker&logoColor=white)](docker-compose.yml)

<!-- Protocol -->
[![Matrix](https://img.shields.io/badge/Matrix-Client--Server%20API-0DBD8B?style=flat-square&logo=matrix&logoColor=white)](https://spec.matrix.org/latest/client-server-api/)
[![OIDC](https://img.shields.io/badge/OIDC-Keycloak%20ready-E8572A?style=flat-square)](#authentication)

<!-- Repos -->
[![GitHub](https://img.shields.io/badge/GitHub-innoq%2Fnebu-181717?style=flat-square&logo=github&logoColor=white)](https://github.com/innoq/nebu)
[![OpenCode](https://img.shields.io/badge/OpenCode-nebu%2Fnebu--server-2a6fff?style=flat-square)](https://gitlab.opencode.de/nebu/nebu-server)
[![Contributions welcome](https://img.shields.io/badge/contributions-welcome-0DBD8B?style=flat-square)](CONTRIBUTING.md)

<!-- Sovereign -->
[![Sovereign](https://img.shields.io/badge/deployment-sovereign%20self--hosted-0F6E56?style=flat-square)](#deployment)
<!-- markdownlint-enable MD013 MD042 MD051 -->

> An enterprise-grade, Matrix-compatible chat server — Apache 2.0, no federation, horizontally scalable.
>
> _Named after Nebuchadnezzar, the ship of the free from The Matrix._

Nebu is a chat server that speaks the [Matrix Client-Server API](https://spec.matrix.org/latest/client-server-api/), so any standard Matrix client (Element, Cinny, FluffyChat, …) works out of the box. It's built for organizations that need full data sovereignty: on-premise deployment, no vendor lock-in, no telemetry, no federation overhead.

<!-- markdownlint-disable-next-line MD013 -->
**Status:** Pre-alpha. Core chat functionality implemented (Epics 1–8 complete). Not production-ready — see [Current Limitations](#current-limitations) before evaluating.

---

## Why Nebu?

Existing Matrix servers each have a trade-off that makes them hard to adopt in enterprise contexts — AGPLv3 licensing, immature codebases, or operational complexity that only pays off with federation you don't need. Nebu is focused on a narrower goal:

- **Apache 2.0** — no copyleft, commercial use welcome
- **Matrix Client-Server API compatible** — standard clients, no fork required
- **No federation** — ~40% less protocol complexity, optimized for single-org deployments
- **Minimal dependencies** — Go + Elixir/OTP + PostgreSQL. No Redis, no NATS, no Kafka
- **OIDC-first auth** — Keycloak, Azure AD, Google Workspace, Dex out of the box
- **Ed25519 message signatures** — authenticity and non-repudiation built in
- **Compliance-ready** — audit logging, legal export, four-eyes principle for privileged access
- **Horizontally scalable** — stateless Go gateway + clustered Elixir core (libcluster + Horde)

**Intentional non-goals:** federation, end-to-end encryption (by design — see [Current Limitations](#current-limitations)).

---

## Architecture

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

**Three runtime components:** a Go binary (gateway + media), an Elixir release (core), and PostgreSQL. That's it.

<!-- markdownlint-disable MD013 -->
Deep dives: [`docs/architecture/`](docs/architecture/) · ADRs: [`docs/architecture/adr/`](docs/architecture/adr/)
<!-- markdownlint-enable MD013 -->

---

## Current Limitations

<!-- markdownlint-disable MD013 -->
These are **deliberate design decisions**, not oversights. Open an issue only if you believe a limitation should be reconsidered — not to ask when it will be "fixed."

### No End-to-End Encryption

Nebu does **not** implement E2EE. Message content is encrypted in transit (TLS 1.3) and at rest (AES-256-GCM for media), but the server can read all messages.

**Why this is intentional:** Nebu targets organizations that require compliance access — audit logging, legal export, four-eyes-principle review of privileged content (Epic 5). True E2EE makes server-side compliance access impossible. A future "Managed E2EE" model (client encrypts, server holds escrow keys) is on the roadmap but not yet designed. See [ADR-007](docs/architecture/adr/ADR-007-ed25519-x25519-keypairs.md).

**Practical impact for client users:** The Matrix E2EE setup dialogs in Element Web are silenced via stubs (`keys/upload`, `keys/query`). You will not see "Unable to set up keys" errors, but messages are not end-to-end encrypted.

### No Federation

Nebu does not implement the [Matrix Server-Server API](https://spec.matrix.org/latest/server-server-api/). Users on a Nebu instance cannot communicate with users on other Matrix homeservers (Synapse, Conduit, etc.).

**Why this is intentional:** Federation adds ~40% protocol surface area and is incompatible with the closed-network data-sovereignty model Nebu is built for. See [ADR-002](docs/architecture/adr/ADR-002-no-redis-nats.md).

### No Full-Text Search

`POST /_matrix/client/v3/search` is not yet implemented. It requires ADR-010 (FTS strategy: PostgreSQL `tsvector` vs. pgvector semantic search) to be decided first.

### Pre-Alpha Quality

No stability guarantees. Database migrations may require manual intervention between versions. The API surface is not yet frozen.
<!-- markdownlint-enable MD013 -->

---

## Development Methodology

Nebu is developed using the **[BMad Method](https://docs.bmad-method.org/)**
(BMAD — _Build More Architect Dreams_), an agent-driven framework where
each story passes through defined gates: Story Creation → Acceptance-Test
Scaffold (ATDD) → Implementation → Test Review → Code Review → conditional
Security Review. Each gate is executed by a dedicated AI agent role
(SM, TEA, Dev, Reviewer), with the human maintainer as the final
decision-maker at every merge.

**AI assistance:** This project was developed with AI assistance via Claude
(Opus 4.6/4.7, Sonnet 4.5/4.6) through [Claude Code](https://claude.ai/code),
Anthropic's CLI. Claude served as the AI backend for all BMAD agent roles.
All generated code was reviewed, tested against acceptance criteria, and
accepted by the maintainer.

For the full BMAD workflow, coding conventions, and how to contribute
using or without the BMAD pipeline, see [CONTRIBUTING.md](CONTRIBUTING.md).

---

## Quick Start

**Prerequisites:** Docker Desktop, `make`, `git`. No local Go or Elixir installation required — all builds run in containers.

```bash
git clone <your-fork-url> nebu
cd nebu
make setup     # generates .secrets/internal_secret and dev credentials
make dev       # starts gateway, core, postgres, dex (OIDC) via docker compose
```

| Service               | URL                                         | Purpose                    |
|-----------------------|---------------------------------------------|----------------------------|
| Admin UI + Matrix API | http://localhost:8008                       | Main endpoint              |
| Health / Metrics      | http://localhost:8080                       | Prometheus, health checks  |
| Dex (OIDC provider)   | http://localhost:5556                       | Dev identity provider      |
| PostgreSQL            | localhost:5432 (user `nebu`, db `nebu`)     | Database                   |
| MinIO Console         | http://localhost:9001                       | Object storage console     |
| MinIO S3 API          | http://localhost:9000                       | Object storage (S3-compat) |

> **MinIO credentials:** `make setup` generates `.secrets/minio_root_user` and `.secrets/minio_root_password` automatically. These are example credentials for local development only. **Replace before first production start.** The `.secrets/` directory is gitignored and never committed.

**One-time setup:** add `dex` to `/etc/hosts` so the OIDC redirect resolves in your browser:

```bash
sudo sh -c 'echo "127.0.0.1 dex" >> /etc/hosts'
```

Open http://localhost:8008/admin and complete the Bootstrap Wizard (OIDC issuer: `http://dex:5556/dex`, client id: `nebu-admin`, client secret: `nebu-admin-secret`). Log in with one of the dev users:

| Email                      | Password   | Role                 |
|----------------------------|------------|----------------------|
| `kai@example.com`          | `changeme` | `instance_admin`     |
| `compliance@example.com`   | `changeme` | `compliance_officer` |
| `alex@example.com`         | `changeme` | `user`               |

<!-- markdownlint-disable-next-line MD013 -->
Full setup walkthrough: [`docs/getting-started.md`](docs/getting-started.md)

---

## Matrix API Scope

Nebu implements the core of the Matrix Client-Server API. Clients that rely only on these endpoints work without modification.

**Implemented:** login, logout, sync, send, messages, createRoom, join, typing, receipts, profile, presence, media upload/download/thumbnail, `keys/upload`, `keys/query`.

**Intentionally excluded:** `/_matrix/federation/*`, `/_matrix/identity/*`, `/_matrix/client/v3/keys/claim` (no E2EE).

<!-- markdownlint-disable-next-line MD013 -->
Full endpoint list: [`docs/matrix-api-scope.md`](docs/matrix-api-scope.md)

---

## Tech Stack

| Layer               | Technology                                        |
|---------------------|---------------------------------------------------|
| API Gateway         | Go 1.26+                                          |
| Media Gateway       | Go 1.26+                                          |
| Core / Messaging    | Elixir/OTP 1.19+ (libcluster, Horde, ETS, pg)     |
| Gateway ↔ Core      | gRPC (protobuf)                                   |
| Database            | PostgreSQL 16+ (pgcrypto, tsvector)               |
| Session / Cache     | ETS (no Redis)                                    |
| Pub/Sub             | pg process groups (no NATS)                       |
| Migrations          | golang-migrate (owned by gateway)                 |
| Container           | Docker + Docker Compose                           |

All dependencies are Apache 2.0, MIT, BSD, or PostgreSQL License. No AGPLv3, no BSL, no SSPL.

---

## Roadmap

<!-- markdownlint-disable MD013 MD060 -->
| Phase | Focus                                                            | Status      |
|-------|------------------------------------------------------------------|-------------|
| 1–2   | OIDC login, rooms, power levels                                  | Done        |
| 3     | Ed25519 signatures, audit log, media (AES-256-GCM)              | Done        |
| 4     | Full Matrix Client-Server API MVP (sync, send, presence, …)     | Done        |
| 5     | Security hardening, rate limiting, compliance access             | Done        |
| 6     | Admin API (user/room management)                                 | In progress |
| 7     | Admin UI (full day-to-day operations)                            | Done        |
| 8     | Public open-source release (GitHub/GitLab, CI, docs scaffold)   | Done        |
| 9     | Full-text search (ADR-010 required), Managed E2EE (ADR-011 req.)| Planned     |
| 10    | Clustering & horizontal scale, GDPR/retention hardening         | Planned     |
<!-- markdownlint-enable MD013 MD060 -->

Detailed roadmap: [`docs/roadmap.md`](docs/roadmap.md)

---

## Contributing

Contributions are welcome — bug reports, feature discussions, code, docs, tests. Start here:

- Read [`CONTRIBUTING.md`](CONTRIBUTING.md) for dev setup, coding conventions, and the PR workflow
- Check the [issue tracker](../../issues) for `good-first-issue` labels
- Join discussions by opening an issue before large changes

For security issues, please follow the process in [`SECURITY.md`](SECURITY.md) — do not open public issues.

All participants are expected to follow the [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).

---

## License

Apache License 2.0 — see [`LICENSE`](LICENSE).

Commercial use, proprietary extensions, and redistribution are all permitted. Attribution required.
