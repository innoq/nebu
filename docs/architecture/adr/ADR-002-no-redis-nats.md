# ADR-002: No Redis, No NATS — ETS + pg Process Groups Replace Both

## Status

Accepted — 2026-03-18

## Context

Many chat server architectures require Redis for session caching and pub/sub, and NATS or Kafka
for reliable event delivery between services. Each additional component increases operational
complexity: another container to manage, another failure domain, another skill required.

Nebu's primary target audience (2–3 admins, self-hosted) cannot afford this operational overhead.
The architecture principle is: every external dependency must be justified; defaults must minimize
moving parts.

Elixir/OTP provides two built-in capabilities that make Redis and NATS redundant:

1. **ETS (Erlang Term Storage):** An in-memory key-value store built into the BEAM runtime.
   Accessible from all processes in the same node. Survives GenServer crashes (table is owned by
   the supervisor). For since-token cursor state, ETS provides microsecond-latency reads.

2. **pg Process Groups:** Erlang's built-in distributed pub/sub mechanism. Processes subscribe to
   a named group; publishing sends to all members. No external broker required.

## Decision

We eliminate Redis and NATS/Kafka from the architecture entirely.

**Session state and since-token cursors:** Stored in ETS (hot path) with PostgreSQL checkpoints
(recovery after restart). No Redis.

**Pub/sub fanout (EventBus):** Elixir `pg` process groups route events to subscribed EventBus
gRPC streams. No NATS, no Kafka.

**Message buffer (ROT status):** PostgreSQL `message_buffer` table holds writes when Core is
unavailable. Linear drain worker in Go (MVP), AIMD-adaptive (Phase 2). No Kafka.

## Consequences

**Positive:**
- Zero additional infrastructure to deploy and monitor
- ETS read latency: microseconds (vs. Redis: ~1ms over network)
- No NATS/Kafka configuration, monitoring, or capacity planning
- `docker compose up` stays at 4 services (gateway, core, postgres, dex)

**Negative:**
- ETS is not persistent — if Elixir crashes without checkpoint, since-tokens are lost (mitigated by PostgreSQL checkpoints)
- pg Process Groups are not durable — if a message arrives when no subscriber exists, it is lost (mitigated by message_buffer)
- Redis might still be needed if Nebu scales beyond what a single PostgreSQL instance can handle for session state (Phase 3 concern)

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Core Architectural Decisions (G1), §Infrastructure; `CLAUDE.md`, §ADR Table_
