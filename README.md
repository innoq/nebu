# Nebu

> An enterprise-grade, Matrix-compatible chat server — Apache 2.0, no federation, horizontally scalable.
>
> _Named after Nebuchadnezzar, the ship of the free from The Matrix._

Nebu is a chat server that speaks the [Matrix Client-Server API](https://spec.matrix.org/latest/client-server-api/), so any standard Matrix client (Element, Cinny, FluffyChat, …) works out of the box. It's built for organizations that need full data sovereignty: on-premise deployment, no vendor lock-in, no telemetry, no federation overhead.

**Status:** Early development. Epic 4 complete, Epic 5 in progress. Not production-ready.

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

**Intentional non-goals:** federation, end-to-end encryption (by design — server-side compliance access is a requirement, not a bug).

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

Deep dives: [`docs/architecture/`](docs/architecture/) · ADRs: [`docs/architecture/adr/`](docs/architecture/adr/)

---

## Development Methodology

Nebu is developed using **BMAD** (Brain Model Agile Development), a
structured agent-driven pipeline where each story passes through defined
gates: Story Creation → Acceptance-Test Scaffold (ATDD) → Implementation
→ Test Review → Code Review → conditional Security Review. Each gate is
executed by a dedicated AI agent role (SM, TEA, Dev, Reviewer), with the
human maintainer as the final decision-maker at every merge.

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

Full setup walkthrough: [`docs/getting-started.md`](docs/getting-started.md)

---

## Matrix API Scope

Nebu implements the core of the Matrix Client-Server API. Clients that rely only on these endpoints work without modification.

**Implemented:** login, logout, sync, send, messages, createRoom, join, typing, receipts, profile, presence, media upload/download/thumbnail, `keys/upload`, `keys/query`.

**Intentionally excluded:** `/_matrix/federation/*`, `/_matrix/identity/*`, `/_matrix/client/v3/keys/claim` (no E2EE).

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

| Phase | Focus                                  | Status         |
|-------|----------------------------------------|----------------|
| 1     | OIDC login, first message              | Done           |
| 2     | Rooms, power levels, admin API         | Done           |
| 3     | Ed25519 signatures, audit log          | Done           |
| 4     | Media upload/download (AES-256-GCM)    | Done           |
| 5     | Rate limiting, compliance search       | In progress    |
| 6     | Full-text search (PostgreSQL FTS)      | Planned        |
| 7     | Clustering & horizontal scale          | Planned        |
| 8     | Enterprise hardening (GDPR, retention) | Planned        |
| 9     | Semantic search via pgvector (opt-in)  | Exploratory    |

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
