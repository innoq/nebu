# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Nebu** (short for Nebuchadnezzar, the ship of the free from The Matrix film) – An enterprise-grade, Matrix Client-Server API compatible chat server. Apache 2.0 licensed, no federation, horizontally scalable. Designed to replace Slack/Teams with full data sovereignty.

This project is currently in early development. The `main.go` is a placeholder; the architecture described in `README.md` is the implementation target.

## Tech Stack

| Layer | Technology |
|---|---|
| API Gateway | Go 1.26+ |
| Media Gateway | Go 1.26+ |
| Core / Messaging | Elixir/OTP 1.19+ |
| Gateway ↔ Core | gRPC (protobuf) |
| Database | PostgreSQL 16+ |
| Sessions / Cache | ETS (Elixir built-in) — kein Redis |
| Pub/Sub | pg Process Groups (built-in) — kein NATS |
| Clustering | libcluster + Horde |

## Architecture

Three-tier design:

1. **Go Gateway** (`gateway/`) — Handles all Matrix Client-Server API HTTP traffic, TLS termination, rate limiting, auth middleware (OIDC-only). Stateless, horizontally scalable. Communicates with Core via gRPC.

2. **Elixir/OTP Core** (`core/`) — Actor-model runtime for Room GenServers (via Horde), Session Manager (ETS + PostgreSQL), Presence, and Event Dispatch. Handles all stateful chat logic. Clusters via libcluster.

3. **Infrastructure** — PostgreSQL as append-only event log and schema owner (via golang-migrate). No Redis. No NATS. No Kafka.

## Planned Directory Structure

```
gateway/
  cmd/gateway/         ← entrypoint (migrate → registry → HTTP)
  internal/
    auth/              ← OIDC-only (no SAML/LDAP directly)
    matrix/            ← Matrix API handlers
    middleware/        ← rate limiting, logging, CORS
    grpc/              ← Core communication (stream + fallback)
    buffer/            ← message_buffer drain strategy
    admin/             ← Admin API + UI (Go Templates + go:embed)
    registry/          ← Elixir node registry
  migrations/          ← golang-migrate SQL files
media/
  cmd/media/           ← entrypoint
  internal/
    upload/ download/ crypto/ storage/
core/                  ← Elixir/OTP Umbrella
  apps/
    room_manager/      ← Horde + Room GenServer
    session_manager/   ← ETS + PostgreSQL hybrid since-token
    presence/          ← Presence Manager
    event_dispatcher/  ← EventBus gRPC Stream + pg Process Groups
    signature/         ← Ed25519 signing + X25519 encryption + Nebu.EventId
    permissions/       ← System roles + room power levels
proto/                 ← gRPC .proto definitions + generated stubs
docs/
  architecture/        ← SAD.md, data-model.md, ADRs
  stories/             ← BMAD epics & stories
```

## Commands

All builds run via Docker containers — no local Go, Elixir, or buf installation required.

### Go Gateway (via build container)

```bash
make build-gateway                # docker multi-stage build
make test-unit-go                 # go test ./... in container
make gen-api                      # oapi-codegen: openapi.yaml → api_gen.go
```

### Elixir/OTP Core (via build container)

```bash
make build-core                   # docker multi-stage build
make test-unit-elixir             # mix test in container
```

### Proto / gRPC

```bash
make proto                        # buf generate (in container)
```

### Local Development (Docker)

```bash
make dev                          # docker compose up (gateway, core, postgres, keycloak)
make setup                        # first-time setup: generate .secrets/internal_secret + test keys
make test-integration             # full stack + Godog Gherkin tests
```

Environment variables for gateway (prefix: `NEBU_`):
- `NEBU_CORE_GRPC_ADDR` — gRPC address of the Elixir core (default: `core:9000`)
- `NEBU_DB_URL` — PostgreSQL connection string
- `NEBU_OIDC_ISSUER` — OIDC provider URL
- `NEBU_INTERNAL_SECRET_FILE` — path to shared node-registration secret file

## Matrix API Scope

**Implemented (MVP target):**
- `POST /_matrix/client/v3/login`
- `POST /_matrix/client/v3/logout`
- `GET  /_matrix/client/v3/sync`
- `PUT  /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}`
- `GET  /_matrix/client/v3/rooms/{roomId}/messages`
- `POST /_matrix/client/v3/createRoom`
- `POST /_matrix/client/v3/join/{roomIdOrAlias}`
- `PUT  /_matrix/client/v3/rooms/{roomId}/typing/{userId}`
- `POST /_matrix/client/v3/rooms/{roomId}/receipt/{receiptType}/{eventId}`
- `GET/PUT /_matrix/client/v3/profile/{userId}`
- `GET  /_matrix/client/v3/presence/{userId}/status`

**Intentionally excluded:** All `/_matrix/federation/*`, `/_matrix/identity/*`, `/_matrix/key/*` (no federation by design).

## Resolved Architecture Decisions

All major decisions are documented in `_bmad-output/planning-artifacts/architecture.md`.
ADRs are tracked in `docs/architecture/adr/`:

| ADR | Decision |
|---|---|
| 001 | Elixir/OTP (not Erlang) — libcluster, Mix tooling |
| 002 | No Redis, No NATS — ETS + pg Process Groups replace both |
| 003 | Content-Hash Event-ID (Matrix Room Version 6+) |
| 004 | Horde Registry + DynamicSupervisor for Room GenServers |
| 005 | gRPC Server-Streaming EventBus + Unary fallback |
| 006 | message_buffer drain strategy (linear MVP, AIMD Phase 2) |
| 007 | Ed25519 (signing) + X25519 (encryption) — two key pairs per user |
| 008 | Node registration: PSK via Compose secrets (MVP) → Ephemeral mTLS (Phase 2) |
| 009 | OpenAPI Spec-First with oapi-codegen |

## BMAD Workflow

This project uses BMAD agents for structured development. Architecture is complete.
Next step: `bmad-create-epics-and-stories` to break the architecture into implementable stories.
