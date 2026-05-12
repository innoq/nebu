# 7 Deployment View

## Docker Compose Stack (Development + Production)

All builds run via Docker containers — no local Go, Elixir, or buf installation required.

```yaml
# Simplified docker-compose.yml topology
services:
  gateway:        # Go binary (port 8008: Matrix API + Admin; port 8080: metrics)
  core:           # Elixir OTP release (port 4000: health; port 9000: gRPC)
  postgres:       # PostgreSQL 16 (port 5432)
  dex:            # OIDC provider for development (port 5556)
  minio:          # MinIO S3-compatible object store (port 9000: API; port 9001: console) — Story 12.1
  createbuckets:  # One-shot init container that creates the nebu-media bucket — Story 12.1
```

**Port map:**

| Service | External Port | Purpose |
|---|---|---|
| Go Gateway | 8008 | Matrix Client-Server API + Admin UI + Admin API |
| Go Gateway | 8080 (dev) / 8443 (TLS) | Health, Readiness, Prometheus metrics |
| Elixir Core | 4000 | Health endpoint (internal Docker network only) |
| Elixir Core | 9000 | gRPC CoreService (internal Docker network only) |
| PostgreSQL | 5432 | Database (internal Docker network only) |
| Dex (dev) | 5556 | OIDC provider (development only) |
| MinIO S3 API | 9000 | Object storage API (S3-compatible) — local dev only |
| MinIO Console | 9001 | Object storage management UI — local dev only |

## Network Boundaries

```
External boundary (exposed to clients):
  Internet → [TLS 1.3] → Go Gateway     (Port 8008)
  Internet → [TLS 1.3] → Go Media GW    (Port 8008 or separate)

Internal boundary (Docker network, not exposed):
  Go Gateway  → [gRPC]          → Elixir Core  (Port 9000)
  Go Media GW → [gRPC]          → Elixir Core  (Port 9000)
  Go Gateway  → [HTTP PSK]      → /internal/nodes/* (Node Registry)
  All services → [TLS]          → PostgreSQL   (Port 5432)
```

## Makefile Targets

```makefile
make setup              # Generate .secrets/internal_secret, MinIO credentials, and dev credentials
make dev                # docker compose up (gateway, core, postgres, dex)
make build-gateway      # Docker multi-stage build for Go gateway
make build-core         # Docker multi-stage build for Elixir core
make test-unit-go       # go test ./... in container
make test-unit-elixir   # mix test in container
make test-integration   # Full Docker Compose stack + Godog Gherkin tests
make proto              # buf generate (in container)
make gen-api            # oapi-codegen: openapi.yaml → api_gen.go
```

## Multi-Stage Dockerfiles

**Go Gateway pattern:**
```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /workspace
RUN go build -o /gateway ./cmd/gateway

FROM gcr.io/distroless/static AS runtime
COPY --from=builder /gateway /gateway
ENTRYPOINT ["/gateway"]
```

**Elixir Core pattern:**
```dockerfile
FROM elixir:1.19-alpine AS builder
RUN MIX_ENV=prod mix release

FROM alpine:3.23 AS runtime
COPY --from=builder /app/_build/prod/rel/nebu ./
ENTRYPOINT ["./bin/nebu", "start"]
```

## Health Checks

```yaml
core:
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:4000/health"]
    interval: 10s
    timeout: 5s
    retries: 3
    start_period: 30s
  restart: always
```

Gateway readiness (`GET :8080/ready`) reflects the GRÜN/GELB/ROT core status derived from
the gRPC stream health. Docker Compose uses liveness (`GET :4000/health`).

## Secret Management

Secrets are never passed as environment variables directly. They are mounted via Docker Compose
secrets and referenced via `NEBU_*_FILE` environment variables pointing to the mounted file:

```bash
make setup   # generates .secrets/internal_secret, .secrets/minio_root_user,
             # and .secrets/minio_root_password via openssl rand
# docker-compose.yml mounts all three as Docker secrets
# Gateway reads internal secret via NEBU_INTERNAL_SECRET_FILE=/run/secrets/internal_secret
# MinIO reads credentials via MINIO_ROOT_USER_FILE / MINIO_ROOT_PASSWORD_FILE
# WARNING: These are example credentials for local development only.
# Replace before first production start.
```

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Infrastructure & Deployment, §Build-Container-Strategie, §Resilienz & Selbst-Heilung_
