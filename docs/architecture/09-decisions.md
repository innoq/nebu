# 9 Architecture Decisions

This section is an index of all Architecture Decision Records (ADRs) for Nebu.
Individual ADR files live in [`docs/architecture/adr/`](adr/).

## ADR Index

| ADR | Title | Status | Date |
|---|---|---|---|
| [ADR-001](adr/ADR-001-elixir-otp.md) | Elixir/OTP as Core Runtime | Accepted | 2026-03-18 |
| [ADR-002](adr/ADR-002-no-redis-nats.md) | No Redis, No NATS — ETS + pg Process Groups | Accepted | 2026-03-18 |
| [ADR-003](adr/ADR-003-content-hash-event-id.md) | Content-Hash Event IDs (Matrix Room Version 6+) | Accepted | 2026-03-18 |
| [ADR-004](adr/ADR-004-horde-registry.md) | Horde Registry + DynamicSupervisor for Room GenServers | Accepted | 2026-03-18 |
| [ADR-005](adr/ADR-005-grpc-eventbus.md) | gRPC Server-Streaming EventBus + Unary Fallback | Accepted | 2026-03-18 |
| [ADR-006](adr/ADR-006-message-buffer-drain.md) | message_buffer Drain Strategy (Linear MVP, AIMD Phase 2) | Accepted | 2026-03-18 |
| [ADR-007](adr/ADR-007-ed25519-x25519-keypairs.md) | Ed25519 (Signing) + X25519 (Encryption) — Two Key Pairs per User | Accepted | 2026-03-18 |
| [ADR-008](adr/ADR-008-node-registration-psk.md) | Node Registration: PSK via Compose Secrets (MVP) → Ephemeral mTLS (Phase 2) | Accepted | 2026-03-18 |
| [ADR-009](adr/ADR-009-openapi-spec-first.md) | OpenAPI Spec-First with oapi-codegen | Accepted | 2026-03-18 |
| [ADR-010](adr/ADR-010-fts-strategy.md) | Full-Text Search Strategy | Accepted | 2026-05-08 |
| [ADR-011](adr/ADR-011-managed-e2ee-key-escrow.md) | Managed E2EE Key Escrow | Proposed | — |

## Decision Drivers

All ADRs 001–009 were finalized during the architecture phase (2026-03-18) after evaluation of
alternatives. The key drivers were:

1. **Minimize operational complexity** — fewer moving parts means fewer failure modes
2. **Matrix protocol compatibility** — design choices must not create incompatibilities
3. **GDPR compliance** — cryptographic deletion requires a specific key architecture
4. **Horizontal scalability from day one** — Horde and stateless gateway design decisions front-loaded
5. **Apache 2.0 ecosystem** — all dependency choices verified against license compatibility

ADR-010 (FTS strategy) was accepted on 2026-05-08: PostgreSQL native `tsvector`/`tsquery` with
a GIN index on the `events` table (migration 000042, Story 11-1). No external search service
is introduced. See [`ADR-010`](adr/ADR-010-fts-strategy.md) for the full rationale.

ADR-011 (Managed E2EE) is pending because it requires additional investigation and community
input before the design can be finalized.

_Source: `CLAUDE.md`, §Resolved Architecture Decisions; `_bmad-output/planning-artifacts/architecture.md`, §Core Architectural Decisions_
