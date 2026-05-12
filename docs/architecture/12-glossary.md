# 12 Glossary

## Domain Terms

| Term | Definition |
|---|---|
| **Nebu** | Short for Nebuchadnezzar, the ship of the free in The Matrix. The name of this chat server. |
| **Matrix** | An open standard and communication protocol for real-time communication (https://matrix.org) |
| **Homeserver** | A Matrix server that stores user accounts, rooms, and events for a domain |
| **Room** | A Matrix concept: a named, persistent channel where users exchange messages and state events |
| **Event** | The fundamental unit in Matrix: a JSON object representing a message, state change, or reaction |
| **Thread** | A Matrix concept (spec §11.12) for grouping related replies under a parent event using `m.relates_to` with `rel_type: "m.thread"` |
| **Bundled Aggregations** | Inline relation metadata (e.g. thread reply count, latest reply) delivered in `unsigned.m.relations` of a parent event during `/sync` to avoid extra HTTP round-trips |
| **m.relates_to** | A Matrix event content key that links a reply or reaction to its parent event via `event_id` + `rel_type` |
| **Sync** | Matrix's long-polling endpoint (`GET /sync`) that delivers new events and room state to clients |
| **since-token** | An opaque cursor used with `/sync` to retrieve only events after a given point in time |
| **Content-Hash Event ID** | Event ID format `$<base64url(SHA-256(canonical_json(event)))>` — tamper-evident, deterministic |
| **Server-Server API** | The Matrix federation protocol — deliberately excluded from Nebu |
| **Power Level** | A numeric value (0–100) controlling what a room member can do in a room |
| **EDU / PDU** | Ephemeral Data Unit (typing, presence) / Persistent Data Unit (messages, state) in Matrix terminology |

## Technical Terms

| Term | Definition |
|---|---|
| **Go Gateway** | The stateless Go binary that handles all HTTP traffic, TLS termination, OIDC validation, and Admin UI serving |
| **Elixir Core** | The Elixir/OTP application (umbrella) managing stateful chat logic via GenServers |
| **gRPC** | Google Remote Procedure Call — the protocol used for Go Gateway ↔ Elixir Core communication |
| **EventBus** | A gRPC server-streaming service from Elixir Core to Go Gateway for real-time event delivery |
| **message_buffer** | A PostgreSQL table that holds outgoing events when the Elixir Core is temporarily unavailable (ROT status) |
| **Drain Worker** | A Go goroutine that processes buffered messages from `message_buffer` when the Core reconnects |
| **GRÜN/GELB/ROT** | German for Green/Yellow/Red — the three-state health model for the gRPC connection status |
| **Horde** | An Elixir library providing CRDT-based distributed process registry and supervision (https://hexdocs.pm/horde) |
| **ETS** | Erlang Term Storage — an in-memory key-value store built into the Erlang/OTP runtime (no Redis needed) |
| **libcluster** | Elixir library for automatic cluster node discovery (Phase 2) |
| **golang-migrate** | Go library for PostgreSQL schema migrations; the Go Gateway is the sole schema owner |
| **MinIO** | S3-compatible object storage server used as the local-dev media backend (Epic 12). Apache 2.0 licensed. |
| **nebu-media** | The MinIO bucket (created by the `createbuckets` init container) where all media uploads are stored |
| **oapi-codegen** | A Go code generator that produces types and a `ServerInterface` from an OpenAPI 3.1 YAML spec |
| **PSK** | Pre-Shared Key — used for node registration security in the MVP (Docker Compose secrets) |
| **mTLS** | Mutual TLS — ephemeral certificate-based authentication planned for Phase 2 node registration |
| **AIMD** | Additive Increase Multiplicative Decrease — the adaptive drain rate algorithm for Phase 2 |
| **OIDC** | OpenID Connect — the authentication protocol used for all user logins (Keycloak, Azure AD, Dex, Google) |
| **PKCE** | Proof Key for Code Exchange — OIDC extension required for public clients |
| **Bootstrap Mode** | First-run state where the first OIDC login automatically receives `instance_admin` role |
| **Four-Eyes Principle** | Compliance access requires approval from two `instance_admin` users |
| **Ed25519** | A fast elliptic-curve signing algorithm used for message non-repudiation |
| **X25519** | An elliptic-curve Diffie-Hellman key exchange algorithm used for PII encryption |
| **Canonical JSON** | Deterministic JSON serialization (keys sorted alphabetically) used for event hashing |
| **GDPR / DSGVO** | General Data Protection Regulation (EU) / Datenschutz-Grundverordnung (German) |
| **arc42** | A lean architecture documentation framework — see https://arc42.org |
| **BMAD** | Build More Architect Dreams — the agent-driven development method used for Nebu development |

## Roles

| Role | Definition |
|---|---|
| **instance_admin** | Can manage users, rooms, and configuration; can approve compliance requests |
| **compliance_officer** | Can request and access compliance exports (four-eyes approval required) |
| **user** | Regular chat user; no administrative privileges |
| **Operator** | The organization or individual who deploys and runs a Nebu instance |
| **Hoster** | An IT service provider who runs Nebu as a managed service for customers |

_Source: `CLAUDE.md`, §Tech Stack, §Matrix API Scope; `_bmad-output/planning-artifacts/architecture.md`, §Naming Conventions; `_bmad-output/planning-artifacts/prd.md`, §User Journeys; Story 9-28 (Thread, Bundled Aggregations, m.relates_to)_
