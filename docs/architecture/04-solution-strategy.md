# 4 Solution Strategy

## Core Architectural Decisions

### Technology Choices

| Goal | Technology Choice | Rationale |
|---|---|---|
| Stateless, horizontally-scalable gateway | Go 1.26 | Minimal memory footprint, fast startup, no GC pauses under load |
| Stateful actor-model runtime | Elixir/OTP 1.19 (Erlang/OTP 27) | OTP Supervisor trees, Horde CRDT clustering, ETS in-memory state |
| Reliable event log + schema authority | PostgreSQL 16 | Append-only event log, golang-migrate ownership, RLS policies |
| No Redis | ETS (Elixir built-in) | Session state, since-token cursors — eliminates a stateful dependency |
| No NATS/Kafka | pg Process Groups (Elixir built-in) | Pub/sub fanout — eliminates an external broker |
| Inter-process RPC | gRPC (protobuf) | Type-safe, streaming-capable (EventBus), well-defined contract |
| Auth | OIDC-only (Authorization Code + PKCE) | No local accounts, no password hashing; IdP owns identity lifecycle |

### Key Architectural Patterns

**1. Three-tier design with explicit boundary protocol**

The Go gateway is stateless (except in-flight requests and the message_buffer). The Elixir core
owns all stateful chat logic: room processes (Horde), session state (ETS + PostgreSQL), presence,
and event dispatch. PostgreSQL is append-only for events and audit logs.

**2. gRPC EventBus Stream + Unary Fallback (GRÜN/GELB/ROT)**

The gateway maintains one persistent gRPC server-streaming connection to the Elixir core per
gateway instance. If the stream drops, the gateway transitions to GELB (yellow) status and uses
Unary polling fallback. If both fail, it transitions to ROT (red) and holds messages in the
message_buffer. On reconnect, a drain worker processes the buffer.

**3. Content-Hash Event IDs (Matrix Room Version 6+)**

Event IDs are computed as `$<base64url(SHA-256(canonical_json(event)))>` — tamper-evident,
federation-compatible, reproducible at recovery. No UUID generation.

**4. Spec-First Admin API**

`gateway/api/openapi.yaml` is the single source of truth for the Admin API. `oapi-codegen`
generates Go types and the `ServerInterface`. No freestyle routing.

**5. message_buffer Drain Strategy Pattern**

When the Elixir core is temporarily unavailable, writes are held in a PostgreSQL `message_buffer`
table. The drain worker uses a pluggable strategy pattern (linear for MVP, AIMD-adaptive for
Phase 2) controlled by the core's `load_factor` health signal.

**6. PSK Node Registration (MVP) → Ephemeral mTLS (Phase 2)**

Elixir nodes register with the gateway via `POST /internal/nodes/register` authenticated by a
pre-shared secret (Docker Compose secret). Phase 2 replaces this with ephemeral mTLS certificates
generated at `make setup` time.

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Core Architectural Decisions, §Implementation Patterns & Consistency Rules_
