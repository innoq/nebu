# ADR-004: Horde Registry + DynamicSupervisor for Room GenServers

## Status

Accepted — 2026-03-18

## Context

Nebu's architecture assigns one Elixir GenServer per Matrix room — the Room GenServer is the
authoritative source for room membership, power levels, event ordering, and presence within a room.

On a single node, a local `Registry` and `DynamicSupervisor` work fine. But for horizontal
scaling (Phase 2 with `libcluster`), we need:
- A process registry that spans multiple Elixir nodes
- Supervision that restarts a Room GenServer on any healthy node after a crash or netsplit
- No split-brain: two nodes must not each believe they are the authority for the same room

Alternatives:
- **Global registry (Erlang `:global`):** Does not handle netsplits well; can deadlock.
- **Custom distributed registry:** High implementation cost; re-solving a solved problem.
- **Horde:** A CRDT-based (Delta-CRDT) distributed registry and dynamic supervisor.
  `Horde.Registry` uses a CRDT for conflict-free process registration. `Horde.DynamicSupervisor`
  manages process lifecycle across the cluster.

## Decision

We use **`Horde.Registry` and `Horde.DynamicSupervisor`** for all Room GenServers and
the Presence GenServer from MVP onwards.

Configuration: `members: :auto` with `libcluster` — the Horde cluster auto-discovers nodes
as libcluster connects them. Single-node MVP and multi-node Phase 2 use identical code;
the difference is only in the libcluster topology configuration.

## Consequences

**Positive:**
- CRDT-based: netsplit-safe; no split-brain for Room processes
- `members: :auto`: zero code change for Phase 2 clustering
- Proven in production (many Elixir services use Horde at scale)
- OTP supervision semantics preserved — GenServer crashes are supervised as usual

**Negative:**
- CRDT state propagation adds slight latency for cluster membership changes (~100ms order)
- Additional Hex dependency (`horde`); must track upstream for breaking changes
- CRDT delta sync uses network bandwidth proportional to cluster churn

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Core Architectural Decisions (G3); `CLAUDE.md`, §ADR Table_
