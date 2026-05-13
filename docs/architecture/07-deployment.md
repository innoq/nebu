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
  createbuckets:  # Init container: creates nebu-media bucket + nebu-app IAM user+policy — Story 12.1/12.3
  media:          # Go Media Gateway (port 8009: upload/download) — Story 12.3
```

**Port map:**

| Service | External Port | Purpose |
|---|---|---|
| Go Gateway | 8008 | Matrix Client-Server API + Admin UI + Admin API |
| Go Gateway | 8080 (dev) / 8443 (TLS) | Health, Readiness, Prometheus metrics |
| Go Media Gateway | 8009 | Matrix Media API (upload + download) |
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
             # .secrets/minio_root_password, .secrets/minio_app_access_key,
             # and .secrets/minio_app_secret_key via openssl rand
# docker-compose.yml mounts all five as Docker secrets
# Gateway reads internal secret via NEBU_INTERNAL_SECRET_FILE=/run/secrets/internal_secret
# MinIO reads root credentials via MINIO_ROOT_USER_FILE / MINIO_ROOT_PASSWORD_FILE
# Media Gateway reads app credentials via NEBU_MINIO_ACCESS_KEY_FILE / NEBU_MINIO_SECRET_KEY_FILE
# createbuckets creates the nebu-app MinIO user and attaches the nebu-app-policy (PutObject+GetObject only)
# WARNING: These are example credentials for local development only.
# Replace before first production start.
```

### MinIO IAM Hardening (Story 12.3)

The media gateway uses a dedicated `nebu-app` MinIO user with a least-privilege IAM policy:

```json
{
  "Statement": [{
    "Effect": "Allow",
    "Action": ["s3:PutObject", "s3:GetObject"],
    "Resource": ["arn:aws:s3:::nebu-media/*"]
  }]
}
```

Intentionally absent: `s3:DeleteObject` (soft-delete only), `s3:ListBucket` (prevents enumeration), `s3:*` (no admin ops from app). The `createbuckets` init container creates and attaches this policy at startup. The media gateway runs as this user, never as the MinIO root. Root credentials are never passed to the media gateway.

Policy source: `dev/minio/nebu-app-policy.json`.

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Infrastructure & Deployment, §Build-Container-Strategie, §Resilienz & Selbst-Heilung_

---

## Production Deployment (OpenTofu IaC)

Story 13-1 introduces a production-grade Infrastructure-as-Code layer under `deploy/`. Three target platforms are supported — see [ADR-014](adr/ADR-014-deployment-strategy-iac.md) for the full decision rationale.

### deploy/ Directory Structure

```
deploy/
  tofu/
    modules/
      nebu-core/      # Shared variables, validations, outputs (no provider resources)
      nebu-aws/       # AWS: ECS Fargate + RDS + S3 + ACM (Story 13-2)
      nebu-stackit/   # STACKIT: VMs + Docker Compose + ALB + DBaaS (Story 13-3)
      nebu-k8s/       # Kubernetes: Helm Release wrapper (Story 13-4)
    examples/
      aws/            # AWS quick-start root module
      stackit/        # STACKIT quick-start root module
      k8s/            # Kubernetes quick-start root module
  helm/
    nebu/             # Standalone Helm Chart (usable without OpenTofu)
```

### Platform Targets

| Platform | Mechanism | Backend |
|---|---|---|
| AWS | ECS Fargate + RDS PostgreSQL | S3 + DynamoDB |
| STACKIT | VMs + Docker Compose + ALB | STACKIT Object Storage (S3-compatible) |
| Kubernetes | Helm Chart (`deploy/helm/nebu/`) | S3-compatible or PostgreSQL |

### Local IaC Validation

```bash
make test-iac-validate   # tofu fmt -check + tofu validate (all examples, no cloud credentials)
```

Equivalent CI gate: `validate-iac` job in `.gitlab-ci.yml` (runs on every push touching `deploy/**`).

### Shared Module: nebu-core

`deploy/tofu/modules/nebu-core/` defines shared input variables consumed by all platform modules: `nebu_version`, `domain_name`, `admin_email`, `postgres_db_name`, `image_registry`. All variables carry validation constraints (non-empty checks, semver regex for `nebu_version`).

### Helm Chart

`deploy/helm/nebu/` is a standalone Helm Chart usable independently of OpenTofu. Image tag defaults to `""` and must be overridden via `--set image.tag=<version>` or a values file — preventing accidental deployment of an unversioned image.
