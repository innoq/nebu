# 5 Building Block View

## Level 1 — Top-Level Decomposition

```
nebu/
├── gateway/          ← Go API Gateway (+ Admin UI)
├── media/            ← Go Media Gateway
├── core/             ← Elixir/OTP Umbrella
├── proto/            ← Shared gRPC .proto definitions
└── docs/             ← Architecture docs (this file)
```

## Level 2 — Go Gateway Internal Structure

```
gateway/
├── cmd/gateway/main.go         ← Startup: migrate → registry → HTTP routing
└── internal/
    ├── auth/                   ← OIDC token validation, bootstrap mode
    │   ├── oidc.go             ← go-oidc provider, token validation
    │   └── bootstrap.go        ← First-admin bootstrap mode
    ├── matrix/                 ← Matrix Client-Server API handlers
    │   ├── login.go            ← POST /_matrix/client/v3/login (SSO + OIDC)
    │   ├── sync.go             ← GET /_matrix/client/v3/sync (long-poll)
    │   ├── send.go             ← PUT /rooms/{id}/send/...
    │   ├── rooms.go            ← POST /createRoom, POST /join/{id}
    │   ├── profile.go          ← GET/PUT /profile/{userId}
    │   ├── presence.go         ← GET/PUT /presence/{userId}/status
    │   └── ...                 ← typing, receipts, messages, moderation, keys
    ├── admin/                  ← Admin UI (Go Templates + SSR) + Admin API
    │   ├── api.go              ← /api/v1/* Router (oapi-codegen StrictHandler)
    │   ├── users.go            ← User CRUD UI + API
    │   ├── rooms.go            ← Room Management UI + API
    │   ├── compliance.go       ← Four-eyes compliance UI
    │   └── templates/          ← Embedded HTML templates (go:embed)
    ├── grpc/                   ← gRPC CoreService client
    │   ├── client.go           ← gRPC connection, CoreService stub
    │   ├── stream.go           ← EventBus server-streaming + exponential backoff
    │   └── fallback.go         ← Unary GetPendingEvents (GELB status)
    ├── buffer/                 ← message_buffer for ROT-status writes
    │   ├── buffer.go           ← In-memory ring buffer per user
    │   ├── drain.go            ← Drain worker + DrainStrategy interface
    │   └── strategy/           ← linear.go (MVP), aimd.go (Phase 2)
    ├── middleware/             ← Auth, rate limiting, body limit, CORS, security headers
    ├── registry/               ← Elixir node registry (/internal/nodes/*)
    ├── compliance/             ← Compliance API handlers (four-eyes, export, anonymize)
    ├── health/                 ← /health + /ready handlers
    └── config/                 ← NEBU_* env-var configuration
```

## Level 2 — Elixir/OTP Core Internal Structure

```
core/apps/
├── nebu_db/          ← Shared Ecto Repo (DB connection)
├── room_manager/     ← FR7–24: Horde.DynamicSupervisor + Room GenServer
│   └── lib/nebu/room/
│       ├── manager.ex      ← Horde.DynamicSupervisor
│       ├── server.ex       ← Room GenServer (state, history, power levels)
│       └── power_level.ex  ← Room policy enforcement
├── session_manager/  ← ETS + PostgreSQL Hybrid since-Token
│   └── lib/nebu/session/
│       ├── manager.ex      ← GenServer owning ETS table
│       └── token.ex        ← v1_<base64url(ts+cursor_map)> format
├── presence/         ← FR15: Presence status (online/offline/unavailable)
├── event_dispatcher/ ← EventBus gRPC streaming + pg Process Groups fanout
│   └── lib/nebu/event/
│       ├── dispatcher.ex   ← Routes events to rooms + subscribers
│       └── bus.ex          ← gRPC ServerStream to Go Gateway
├── signature/        ← FR25–29: Ed25519 signing + Canonical JSON + Event-ID
│   └── lib/nebu/
│       ├── signature.ex         ← :crypto.sign/4 with eddsa
│       ├── event_id.ex          ← Nebu.EventId.generate/1 (SHA-256 content hash)
│       └── canonical_json.ex    ← RFC 8785 canonical JSON
└── permissions/      ← System roles + room power levels
    └── lib/nebu/permissions/
        ├── system_role.ex       ← instance_admin | compliance_officer | user
        └── room_policy.ex       ← Power-level checks for room operations
```

## Level 2 — Proto / gRPC Contract

```
proto/
├── core.proto              ← CoreService: all RPC definitions
└── gen/
    ├── go/                 ← Generated Go stubs (buf generate)
    └── elixir/             ← Generated Elixir stubs
```

Key gRPC services: `SendEvent`, `CreateRoom`, `JoinRoom`, `GetMessages`, `GetRoomState`, `SetPresence`,
`SetTyping`, `ValidateToken`, `GetPendingEvents` (fallback), `EventBus` (streaming).

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Project Structure & Boundaries, §Complete Project Directory Structure_
