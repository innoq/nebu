# ADR-008: Node Registration — PSK via Compose Secrets (MVP) → Ephemeral mTLS (Phase 2)

## Status

Accepted — 2026-03-18

## Context

The Elixir Core must register itself with the Go Gateway at startup so the Gateway knows where
to send gRPC requests. The `/internal/nodes/register` endpoint must be authenticated to prevent
unauthorized nodes from registering.

Two security models were considered:

**MVP — Pre-Shared Secret (PSK):**
A random 32-byte hex secret is generated at `make setup` and mounted as a Docker Compose secret.
Both Gateway and Core read it from the mounted file at startup. The Gateway validates
`Authorization: Bearer <secret>` on all `/internal/*` requests.

- Simple, no certificate management
- Secret must be rotated manually on `make setup` after `docker compose down`
- Never passed as environment variable directly (no `docker inspect` leak)

**Phase 2 — Ephemeral mTLS:**
`generate-dev-certs.sh` (run at `make setup`) creates a short-lived CA + Gateway cert + Core
client cert. Go Gateway validates incoming Elixir connections via mTLS. Certs are Docker secrets,
rotated on every `make setup`.

- Automatic key rotation on restart
- No central secret store needed
- More complex setup

## Decision

**MVP:** PSK via Docker Compose secrets, read from file via `NEBU_INTERNAL_SECRET_FILE`.
**Phase 2:** Ephemeral mTLS (Consul-Connect pattern), same `make setup` workflow, same Docker
secrets mechanism.

```bash
# make setup
mkdir -p .secrets && openssl rand -hex 32 > .secrets/internal_secret
```

The secret is read from the file path at startup — never from the environment variable directly.
This prevents secret leakage via `docker inspect` or `/proc/{pid}/environ`.

## Consequences

**Positive:**
- MVP: no certificate management overhead
- Phase 2 path is clear and non-breaking (same secrets mechanism)
- Secret never in env vars → not visible in docker inspect output
- `docker compose down && up` forces `make setup` → automatic rotation

**Negative:**
- PSK is a shared secret — if it leaks, any node can register
- Manual rotation required after `make setup` (not automatic)
- mTLS Phase 2 adds setup complexity for first-time operators

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Infrastructure & Deployment (V3); `CLAUDE.md`, §ADR Table_
