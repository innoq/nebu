# ADR-006: message_buffer Drain Strategy (Linear MVP, AIMD Phase 2)

## Status

Accepted — 2026-03-18

## Context

When the Elixir Core is temporarily unavailable (ROT status), the Go Gateway must not fail Matrix
client write requests. Matrix clients expect `200 OK` with an `event_id` — they don't know or
care that the server is temporarily degraded. The Gateway must buffer writes and deliver them
when the Core recovers.

Concerns:
- **Buffer storage:** In-memory is fast but lost on Gateway restart. PostgreSQL is durable.
- **Drain rate:** Flooding the recovering Core at full speed after ROT→GRÜN transition could
  overwhelm it. A controllable drain rate is needed.
- **Backpressure signal:** In Phase 2, the Core's `load_factor` (0.0–1.0 from `/health`) can
  signal how fast the drain should proceed.

## Decision

**Buffer storage:** PostgreSQL `message_buffer` table (survives Gateway restarts). FIFO by
`received_at`. After `retry_count` retries (default 3), events move to `message_dead_letter`.

**Drain strategy:** Pluggable interface:
```go
type DrainStrategy interface {
    Rate(loadFactor float64, bufferSize int64) float64 // msg/s
}
```

**MVP (Linear):** `Rate = BASE_RATE` (constant, configurable, default 100 msg/s).

**Phase 2 (AIMD-Adaptive):**
```
Rate = max(MIN_RATE, BASE_RATE × (1 - load_factor) × slope + intercept)
```
The Admin UI visualizes the drain function as an interactive graph; operators can tune slope +
intercept and see the resulting rate curve live.

## Consequences

**Positive:**
- Matrix clients receive 200 OK + event_id even during Core unavailability (protocol conformant)
- Durable buffer: no event loss on Gateway restart
- Dead-letter queue + Prometheus metric: operators see when events fail permanently
- Pluggable strategy: Linear → AIMD upgrade without architectural change

**Negative:**
- PostgreSQL `message_buffer` adds write latency on every event during ROT status
- Dead-letter events require manual operator intervention (no auto-retry after exhaustion)
- Drain rate misconfiguration could re-overwhelm a recovering Core (AIMD mitigates this in Phase 2)

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Infrastructure & Deployment (G13); `CLAUDE.md`, §ADR Table_
