# 6 Runtime View

## Scenario 1: Matrix Client Message Send (Happy Path — GRÜN Status)

```
Matrix Client      Go Gateway          Elixir Core         PostgreSQL
     │                   │                   │                   │
     │  PUT /rooms/send  │                   │                   │
     │──────────────────►│                   │                   │
     │                   │  JWT validate      │                   │
     │                   │──────────────────►│                   │
     │                   │  user_id, role     │                   │
     │                   │◄──────────────────│                   │
     │                   │  gRPC SendEvent    │                   │
     │                   │──────────────────►│                   │
     │                   │                   │  INSERT event      │
     │                   │                   │──────────────────►│
     │                   │                   │  Ed25519 sign      │
     │                   │                   │  EventId.generate  │
     │                   │  {event_id}        │                   │
     │                   │◄──────────────────│                   │
     │  200 {event_id}   │                   │                   │
     │◄──────────────────│                   │                   │
```

## Scenario 2: gRPC EventBus Stream (GRÜN/GELB/ROT State Machine)

```
Go Gateway Status Machine:

  ┌─────┐   Stream healthy      ┌──────┐
  │ ROT │──────────────────────►│ GRÜN │
  └─────┘                       └──────┘
     ▲                             │ Stream lost
     │ Unary polling fails          │
  ┌──────┐◄────────────────────────┘
  │ GELB │  Stream lost, Unary OK
  └──────┘
     │ Unary also fails
     └──────────────────────────►┌─────┐
                                 │ ROT │
                                 └─────┘
                                    │ Writes → message_buffer
                                    │ Drain on reconnect
```

**GRÜN:** EventBus stream healthy — direct gRPC streaming to Matrix clients.
**GELB:** Stream lost, Unary polling succeeds — writes to message_buffer, polling continues.
**ROT:** Stream AND Unary fail — all writes held in message_buffer, 200 OK returned to clients
(Matrix-conformant); Docker `restart: always` heals the Elixir core.

## Scenario 3: Matrix Client Sync (Long-Poll)

```
Matrix Client      Go Gateway          MessageBuffer       Elixir Core
     │                   │                   │                   │
     │  GET /sync        │                   │                   │
     │──────────────────►│                   │                   │
     │                   │  check buffer     │                   │
     │                   │──────────────────►│                   │
     │                   │  empty (wait)     │                   │
     │                   │◄──────────────────│                   │
     │                   │  (holds connection for up to 30s)     │
     │                   │  EventBus event arrives               │
     │                   │◄──────────────────────────────────────│
     │  200 {events}     │                   │                   │
     │◄──────────────────│                   │                   │
```

The Go Gateway distributes EventBus events from a single streaming connection to all waiting
Matrix client long-poll connections via the in-memory per-user ring buffer.

## Scenario 4: Compliance Four-Eyes Export Flow

```
Compliance Officer → POST /api/v1/compliance/access-requests (JWT auth)
Instance Admin 1 → POST /approve (four-eyes gate: needs 2 approvals)
Instance Admin 2 → POST /approve (gate satisfied → access session issued)
Compliance Officer → GET /api/v1/compliance/export (X-Compliance-Token, 24h TTL)
Export → Ed25519-signed JSON/PDF with event content + audit trail
Auto-expiry → session invalidated after 24 hours
```

## Scenario 5: Elixir Core Restart Recovery

On restart, Horde re-discovers Room GenServers across the cluster via CRDT registry.
Session Manager GenServer reads since-token checkpoints from PostgreSQL (no cold-sync forced on clients).
EventBus stream re-connects to Go Gateway after exponential backoff (max 30s + jitter).

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Implementation Patterns, §API & Kommunikation, §Resilienz & Selbst-Heilung_
