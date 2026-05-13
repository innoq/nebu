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

### AWS Networking Module (nebu-aws)

`deploy/tofu/modules/nebu-aws/network.tf` provisions the AWS network foundation: one VPC, two public and two private subnets across two AZs, a single NAT Gateway (cost-optimized; one per AZ for HA production), and scoped security groups for ALB (80/443 from internet), ECS (ports 8008 + 9000 from ALB SG), and RDS (5432 from ECS SG only, egress limited to VPC CIDR). Resource names incorporate the `environment` variable (e.g. `nebu-prod-alb-sg`) for multi-environment deployments.

### AWS Database Module (nebu-aws — database.tf)

`deploy/tofu/modules/nebu-aws/database.tf` provisions the RDS layer:

- `aws_db_subnet_group` — uses private subnets from network.tf (no public access)
- `aws_db_instance` — PostgreSQL 16, Multi-AZ, `db.t3.medium` (default), 20 GB gp3 encrypted storage, 7-day automated backups, Performance Insights enabled
- DB master password: `var.db_password` (sensitive, minimum 8 chars). Secrets Manager integration for app credentials is added in story 13.2c.

Key variables: `db_instance_class`, `db_password`, `skip_final_snapshot` (default `true` for dev), `enable_performance_insights`.

### AWS ALB Module (nebu-aws — alb.tf)

`deploy/tofu/modules/nebu-aws/alb.tf` provisions the internet-facing Application Load Balancer:

- `aws_lb` — internet-facing ALB, placed in public subnets, `drop_invalid_header_fields = true` (header injection protection)
- `aws_lb_target_group.gateway` — target type `ip` (required for Fargate awsvpc networking), port 8008, health check on `GET /_matrix/client/v3/versions` (HTTP 200)
- `aws_lb_listener.https` — port 443, TLS policy `ELBSecurityPolicy-TLS13-1-2-2021-06` (TLS 1.3 minimum), ACM certificate via `var.acm_certificate_arn`, forwards to gateway target group
- `aws_lb_listener.http_redirect` — port 80, permanent `HTTP_301` redirect to HTTPS/443; no plaintext traffic ever reaches the gateway

Module outputs added: `alb_dns_name`, `alb_zone_id` (for Route 53 ALIAS records).

Key variable: `acm_certificate_arn` — required for `tofu apply`; empty string is accepted for `tofu validate` only.

### AWS Secrets Manager Module (nebu-aws — secrets.tf)

`deploy/tofu/modules/nebu-aws/secrets.tf` provisions all Nebu runtime credentials as individual Secrets Manager secrets under the `nebu/${environment}/` namespace:

| Secret path | Purpose |
|---|---|
| `nebu/{env}/db_url` | Full PostgreSQL DSN (used by gateway + core) |
| `nebu/{env}/db_password` | RDS master password (plain string; kept separate for RDS rotation support) |
| `nebu/{env}/internal_secret` | PSK for gateway ↔ core node registration (ADR-008) |
| `nebu/{env}/oidc_client_secret` | OIDC client secret for identity provider registration |
| `nebu/{env}/oidc_issuer` | OIDC issuer URL |
| `nebu/{env}/release_cookie` | Erlang distribution cookie for OTP cluster authentication |

All `aws_secretsmanager_secret_version` resources are provisioned with `PLACEHOLDER_*` initial values and `lifecycle { ignore_changes = [secret_string] }` — preventing `tofu apply` from overwriting operator-rotated values. Operators must set real values before go-live (see RUNBOOK.md).

### AWS Compute Module (nebu-aws — compute.tf)

`deploy/tofu/modules/nebu-aws/compute.tf` provisions the full ECS Fargate compute layer:

- `aws_ecs_cluster` — Fargate cluster with Container Insights enabled (`"nebu-${environment}"`)
- `aws_iam_role.ecs_task_execution` — Task execution role with `AmazonECSTaskExecutionRolePolicy` + inline policy for `secretsmanager:GetSecretValue` scoped to `arn:aws:secretsmanager:*:*:secret:nebu/${environment}/*` (least-privilege; no wildcard resource)
- `aws_cloudwatch_log_group` — `/ecs/nebu-{env}-gateway` and `/ecs/nebu-{env}-core`, 30-day retention
- `aws_ecs_task_definition.gateway` — image `{registry}/nebu-gateway:{version}`, CPU 256 / Memory 512, port 8008, health check `GET /_matrix/client/v3/versions`, `secrets` field references individual Secrets Manager ARNs for `NEBU_DB_URL`, `NEBU_OIDC_ISSUER`, `NEBU_OIDC_CLIENT_SECRET`, `NEBU_INTERNAL_SECRET`
- `aws_ecs_task_definition.core` — image `{registry}/nebu-core:{version}`, CPU 256 / Memory 512, port 9000, health check `GET /health`, `secrets` field references for `DATABASE_URL`, `RELEASE_COOKIE`, `NEBU_INTERNAL_SECRET`
- `aws_ecs_service.gateway` — Fargate service in private subnets, attached to the ALB target group (port 8008), `lifecycle { ignore_changes = [task_definition, desired_count] }` for GitOps rolling deployments
- `aws_ecs_service.core` — Fargate service in private subnets (no ALB attachment; accessed by gateway via gRPC on port 9000 within the VPC)

**Security invariants:** No `environment` field used for secrets in any task definition — all sensitive values are injected exclusively via the `secrets` field with Secrets Manager ARNs. IAM permissions are scoped to the namespace `nebu/${environment}/*` only.

Key variables: `aws_region` (CloudWatch Logs), `image_registry`, `nebu_version`, `acm_certificate_arn`, `ecs_desired_count` (default 1).

Module outputs: `ecs_cluster_arn`, `db_endpoint`, `task_execution_role_arn`, `alb_dns_name`, `alb_zone_id`.

### AWS Deployment Topology (Complete)

```
Internet
  │ HTTPS/443 (TLS 1.3)
  ▼
aws_lb (ALB, internet-facing)
  │ port 443 → aws_lb_listener.https → aws_lb_target_group.gateway (port 8008)
  │ port 80  → aws_lb_listener.http_redirect (HTTP 301 → HTTPS)
  ▼
aws_ecs_service.gateway (private subnets, ECS Fargate, no public IP)
  │ gRPC port 9000 (VPC-internal, ECS SG)
  ▼
aws_ecs_service.core (private subnets, ECS Fargate, no public IP)
  │ port 5432 (private subnets, RDS SG)
  ▼
aws_db_instance (RDS PostgreSQL 16, Multi-AZ, private subnets)

Secrets Manager (nebu/{env}/*) ──► ECS task execution role (secretsmanager:GetSecretValue)
                                        │
                              injected into gateway + core containers
                              via ECS secrets field at task start
```

Day-2 operations (rolling updates, secret rotation, teardown) are documented in `deploy/tofu/examples/aws/RUNBOOK.md`.

### Stackit VM + Networking Module (nebu-stackit — 13-3a)

`deploy/tofu/examples/stackit/main.tf` provisions the STACKIT compute and network foundation:

- `stackit_network` — private routed network with configurable CIDR (`var.network_cidr`, default `10.0.0.0/24`)
- `stackit_security_group` + rules — stateful SG with inbound rules for 443 (HTTPS), 8008 (Matrix API), 22 (SSH); egress unrestricted
- `stackit_network_interface` — VM NIC attached to the network and SG
- `stackit_key_pair` — account-level SSH key pair (global resource; no `project_id`)
- `stackit_server` — Ubuntu 24.04 LTS VM; machine type via `var.vm_plan_id` (default `g2i.2`); AZ via `var.availability_zone` (default `eu01-1`)
- `stackit_public_ip` — Floating IP associated to the VM NIC. If the VM is recreated, re-attach manually via STACKIT portal or `stackit beta network-interface public-ip attach`
- `stackit_loadbalancer` — ALB with `PROTOCOL_TCP` listener on port 443 → target pool on VM port 8008; health check is TCP-only (no HTTP path checks); plan via `var.alb_plan_id` (default `p10`)

**HTTPS at ALB (upgrade path):** `enable_beta_resources = true` is already set in the provider block. Once `stackit` provider >= 0.96 exposes `PROTOCOL_HTTPS` in its stable schema, change the listener protocol to `PROTOCOL_HTTPS` and set `certificate_reference.name = var.stackit_tls_certificate_arn` (Stackit-managed certificate ARN). Until then, TLS is terminated at the gateway on port 8008.

**Authentication:** provider uses `service_account_key_path` (JSON key file) instead of a token. Path configured via `var.stackit_key_path` (sensitive).

Key variables: `stackit_project_id`, `stackit_key_path`, `ssh_public_key`, `ubuntu_image_id`, `network_cidr`, `availability_zone`, `vm_plan_id`, `alb_plan_id`, `stackit_tls_certificate_arn`.

### Stackit cloud-init Bootstrap (nebu-stackit — 13-3b)

`deploy/tofu/examples/stackit/cloud-init.tftpl` is rendered by `templatefile()` in `main.tf` and injected as `user_data` (base64-encoded) into the VM at provision time. On first boot, the VM:

1. Applies OS security patches (`package_upgrade: true`)
2. Installs Docker CE + Docker Compose plugin via the official Docker apt repository
3. Writes `/opt/nebu/` directory tree (permissions `0700`) with:
   - `.secrets/internal_secret` (mode `0600`, quoted scalar — no trailing newline)
   - `.env` (mode `0600`) — all runtime secrets injected from OpenTofu variables, including `NEBU_DB_URL` and `NEBU_DB_URL_MIGRATE` (same `nebu_app` user, simplifying PostgreSQL grants)
   - `init-db.sql` — mounted into `/docker-entrypoint-initdb.d/` on the Postgres container to create the `keycloak` database + user and grant `nebu_app` schema-level access
   - `docker-compose.yml` (mode `0640`) — four services: `postgres`, `keycloak`, `core`, `gateway`
4. Installs `/etc/systemd/system/nebu.service` — `Type=simple`, no `-d` flag; systemd owns the process lifecycle with `Restart=on-failure RestartSec=10`
5. Starts Docker (`systemctl start docker.service && sleep 2`) before starting `nebu.service`

**Security invariants:**
- `DATABASE_URL` bash parameter substitution removed — `nebu_app` user handles the `postgresql://` scheme natively
- `nebu_migrate` user eliminated — `NEBU_DB_URL_MIGRATE` uses the same `nebu_app` credentials
- `/opt/nebu/` is `0700` (root-only); `docker-compose.yml` is `0640`
- `nebu_version` variable rejects `"latest"` — a specific semver tag is required
- Secrets in Terraform state: use encrypted Stackit Object Storage backend (see `RUNBOOK.md` warning)

Day-2 operations (updates, pg_dump backup, teardown) are documented in `deploy/tofu/examples/stackit/RUNBOOK.md`.

### Helm Chart

`deploy/helm/nebu/` is a standalone Helm Chart usable independently of OpenTofu.

**Kubernetes resource topology (Story 13-4a + 13-4b):**

| Resource | Name pattern | Conditional | Purpose |
|---|---|---|---|
| Deployment | `{release}-nebu-gateway` | always | Go gateway; loads ConfigMap + optional Secret via `envFrom` |
| Deployment | `{release}-nebu-core` | always | Elixir/OTP core |
| Service (ClusterIP) | `{release}-nebu-gateway` | always | Port 8008 → gateway pods |
| Service (ClusterIP) | `{release}-nebu-core` | always | Port 9000 (gRPC) + 4000 (HTTP) → core pods |
| ConfigMap | `{release}-nebu-config` | always | `NEBU_OIDC_ISSUER`, `NEBU_SERVER_NAME`, `NEBU_CORE_GRPC_ADDR` (non-secret) |
| Ingress | `{release}-nebu` | `ingress.enabled: true` | nginx Ingress with configurable hostname and optional TLS secret |
| PersistentVolumeClaim | `{release}-nebu-postgres` | `postgres.external: false` | Postgres storage when running in-cluster |
| HorizontalPodAutoscaler | `{release}-nebu-gateway` | `autoscaling.gateway.enabled: true` | CPU-based HPA for gateway Deployment |

`NEBU_CORE_GRPC_ADDR` is derived deterministically from the release name as `{release}-nebu-core:9000` — rendered by the ConfigMap template, not a values entry.

**Secret management (ExistingSecret pattern):**

The chart never creates a Kubernetes Secret. Sensitive `NEBU_*` variables (`NEBU_DB_URL`, `NEBU_INTERNAL_SECRET`, `NEBU_OIDC_CLIENT_SECRET`) must be stored in a pre-existing Kubernetes Secret, created out-of-band by the operator:

```bash
kubectl create secret generic nebu-sensitive \
  --from-literal=NEBU_DB_URL='postgres://user:pass@host:5432/nebu' \
  --from-literal=NEBU_INTERNAL_SECRET='<64-char-random-hex>' \
  --from-literal=NEBU_OIDC_CLIENT_SECRET='<secret>'
```

The Secret name is referenced in `values.yaml` as `existingSecret.name`. When set, the gateway Deployment loads it via `envFrom.secretRef`. If the name is empty, no Secret is mounted and the gateway will fail to start — this is intentional (GitOps pre-deploy pattern) and documented in NOTES.txt.

Image tags must be set independently per component. Omitting either tag causes `helm install` to fail with an explicit error — preventing silent deployment of unversioned images.

```bash
# Minimal production install:
helm install nebu deploy/helm/nebu/ \
  --set gateway.image.tag=1.0.0 \
  --set core.image.tag=1.0.0 \
  --set config.oidcIssuer=https://your-idp.example.com \
  --set config.serverName=your-server-name \
  --set existingSecret.name=nebu-sensitive

# With Ingress + HPA:
helm install nebu deploy/helm/nebu/ \
  --set gateway.image.tag=1.0.0 \
  --set core.image.tag=1.0.0 \
  --set existingSecret.name=nebu-sensitive \
  --set ingress.enabled=true \
  --set ingress.hostname=nebu.example.com \
  --set ingress.tls.secretName=nebu-tls \
  --set autoscaling.gateway.enabled=true
```

### OpenTofu Kubernetes Wrapper (nebu-k8s — Story 13-4c)

`deploy/tofu/modules/nebu-k8s/` is a thin OpenTofu wrapper around a single `helm_release` resource. It manages the Nebu Helm release on any Kubernetes cluster. The Kubernetes and Helm providers must be configured in the calling root module.

**Module interface:**

| Variable | Type | Default | Purpose |
|---|---|---|---|
| `release_name` | string | `"nebu"` | Helm release name |
| `chart_path` | string | required | Path to the Nebu Helm chart directory |
| `namespace` | string | `"nebu"` | Kubernetes namespace (created if absent) |
| `gateway_image_tag` | string | required | Container image tag for the gateway component |
| `core_image_tag` | string | required | Container image tag for the core component |
| `ingress_enabled` | bool | `false` | Enable the Ingress resource |
| `helm_timeout` | number | `300` | Seconds to wait for all pods to reach Ready state |
| `values_files` | list(string) | `[]` | Extra values files; must be non-empty absolute paths |

`wait = true` is set explicitly — `tofu apply` blocks until all pods are Ready or `helm_timeout` seconds elapse. This prevents silent partial deployments.

`values_files` paths must be absolute (use `"${path.module}/..."` in the calling root module) — relative paths are resolved from the `tofu apply` working directory, not the module directory.

**Quick-start example (`deploy/tofu/examples/k8s/`):**

```hcl
module "nebu_k8s" {
  source = "../../modules/nebu-k8s"

  chart_path        = var.chart_path
  namespace         = var.namespace
  gateway_image_tag = var.gateway_image_tag
  core_image_tag    = var.core_image_tag
  ingress_enabled   = var.ingress_enabled
  values_files      = ["${path.module}/../../../helm/nebu/values-dev.yaml"]
}
```

**Local smoke test (kind):**

```bash
# 1. Start a local kind cluster
kind create cluster --name nebu-dev

# 2. Validate IaC configuration (no cluster required)
make test-iac-validate

# 3. Install via Helm directly (fastest smoke test path)
helm install nebu deploy/helm/nebu/ \
  -f deploy/helm/nebu/values-dev.yaml \
  --set gateway.image.tag=dev \
  --set core.image.tag=dev

# 4. Wait for pods
kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=nebu --timeout=180s -n default

# 5. Teardown
kind delete cluster --name nebu-dev
```

Full operator procedures (upgrade, rollback, HPA configuration, teardown) are in `deploy/tofu/examples/k8s/RUNBOOK.md`.
