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

| Platform | Mechanism | Database | Backend |
|---|---|---|---|
| AWS | ECS Fargate + Aurora Serverless v2 | AWS Aurora PostgreSQL 16 (Serverless v2) | S3 + DynamoDB |
| STACKIT | VMs + Docker Compose + ALB | STACKIT PostgresFlex (managed PostgreSQL 16) | STACKIT Object Storage (S3-compatible) |
| Kubernetes | Helm Chart (`deploy/helm/nebu/`) | External (operator-provided) | S3-compatible or PostgreSQL |

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

`deploy/tofu/modules/nebu-aws/database.tf` provisions the Aurora Serverless v2 layer (Story 13-8 — replaces the previous RDS Multi-AZ `aws_db_instance`):

- `aws_db_subnet_group` — uses private subnets from network.tf (no public access); retained as Aurora clusters also require DB subnet groups
- `aws_rds_cluster` — Aurora PostgreSQL 16.6, `engine_mode = "provisioned"` (Serverless v2 requirement — NOT `"serverless"`), storage encrypted, 7-day automated backups. Serverless v2 scaling is controlled by the `serverlessv2_scaling_configuration` block:
  - `min_capacity = 0` (default dev) — scales to zero and auto-pauses after 1 hour idle (`seconds_until_auto_pause = 3600`). Set `min_capacity = 0.5` for production to avoid cold-start latency.
  - `max_capacity = 4` (default) — sufficient for expected Nebu MVP load (approx. 2 vCPU equivalent). Increase for high-traffic production.
- `aws_rds_cluster_instance` — one instance per cluster, `instance_class = "db.serverless"` (required for Serverless v2); Performance Insights enabled explicitly (7-day retention).
- DB master password: `var.db_password` (sensitive, minimum 8 chars). Secrets Manager integration is provided via `secrets.tf`.
- `db_endpoint` output references `aws_rds_cluster.this.endpoint` (writer endpoint) — Aurora clusters expose a dedicated writer endpoint; the reader endpoint is separate and not used for Nebu's primary DB connection.

Key variables: `aurora_min_capacity` (default `0`), `aurora_max_capacity` (default `4`), `db_password`, `skip_final_snapshot` (default `true` for dev). The former `db_instance_class` and `enable_performance_insights` variables have been removed — Aurora Serverless v2 uses a fixed `db.serverless` class and enables Performance Insights by default.

**Cost model:** Aurora Serverless v2 charges per ACU-second consumed. At `min_capacity = 0`, a completely idle cluster costs near zero (vs. ~$50–100/month for the previous always-on `db.t3.medium` Multi-AZ instance).

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
aws_rds_cluster (Aurora PostgreSQL 16, Serverless v2, private subnets)
  ├── aws_rds_cluster_instance (db.serverless — scales 0–4 ACUs)
  └── writer endpoint: aws_rds_cluster.this.endpoint (port 5432)

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

### Stackit Managed Database (nebu-stackit — Story 13-8)

`deploy/tofu/examples/stackit/main.tf` provisions a dedicated STACKIT PostgresFlex instance alongside the application VM. This replaces the previous `postgres:16-alpine` container bundled in docker-compose.

**Resources provisioned:**

- `stackit_postgresflex_instance.nebu` — managed PostgreSQL 16 cluster with daily automated backups (`0 2 * * *` UTC), configurable replicas (`var.postgres_replicas`, default `1`; use `3` for production HA), and ACL locked to the VM's private network CIDR (`var.network_cidr`) — no public exposure
- `stackit_postgresflex_user.nebu` (`nebu_app`) — application user with `["login"]` role; owns the `nebu` database
- `stackit_postgresflex_user.keycloak` (`keycloak_app`) — dedicated Keycloak user with `["login"]` role; owns the `keycloak` database. Using separate DB users per application is a defence-in-depth measure: a compromise of the Nebu application user does not grant access to Keycloak data.
- `stackit_postgresflex_database.nebu` — `nebu` database, owner `nebu_app`
- `stackit_postgresflex_database.keycloak` — `keycloak` database, owner `keycloak_app`

PostgresFlex connection details (`host`, `port`, `username`, `password`) are computed by the provider after instance creation and passed directly into the cloud-init template as `pg_*` / `kc_*` variables. The password is **not** exposed as a Tofu output — operators retrieve it from Terraform state or via the STACKIT portal.

**Stackit Sizing Variables:**

| Variable | Default | Production recommendation |
|---|---|---|
| `postgres_replicas` | `1` | `3` (HA replication) |
| `postgres_cpu` | `1` | 2+ (depending on load) |
| `postgres_ram` | `4` GB | 8+ GB |
| `postgres_storage_size` | `20` GB | 50+ GB |

### Stackit OIDC Deployment Profiles (nebu-stackit — Story 13-9)

The Stackit deployment supports two OIDC profiles selected via `var.oidc_mode`:

| `oidc_mode` | Bundled IdP | Use case |
|---|---|---|
| `"dex"` | Dex IdP sidecar (`dexidp/dex:v2.45.1`) | Test, demo, integration environments |
| `"external"` _(default)_ | None | Production — operator provides an external OIDC provider |

**`"dex"` profile:**
- Dex runs as a Docker Compose sidecar on port 5556 with a static in-memory config (no database).
- `/opt/nebu/dex/config.yaml` is written at boot (mode `0600`) with a static client (`nebu-gateway`) and a static password user (`operator@example.com`).
- `effective_oidc_issuer` is auto-derived as `http://<server_name>:5556/dex`; the gateway resolves Dex via Docker hairpin NAT (Linux SNAT/masquerade — no extra routing required).
- A conditional SG rule `inbound_dex` opens port 5556 to `var.dex_allowed_cidr` (default `0.0.0.0/0` for demos; restrict to VPN/developer CIDR in shared environments).
- `var.dex_static_password_hash` (required when `oidc_mode = "dex"`) must be a bcrypt hash (`$2a$`/`$2b$`/`$2y$` prefix). Generate with: `htpasswd -bnBC 12 '' 'yourpassword' | tr -d ':' | sed 's/$2y/$2a/'`.
- Expected `docker compose ps` output: `dex` (healthy), `core` (healthy), `gateway` (healthy).

**`"external"` profile:**
- No bundled IdP is deployed.
- `var.oidc_issuer` (required, non-empty) is passed directly into `.env` as `NEBU_OIDC_ISSUER`.
- `var.oidc_client_secret` must match the secret registered in the external OIDC provider.
- Expected `docker compose ps` output: `core` (healthy), `gateway` (healthy).

**Keycloak is fully removed in both profiles.** `stackit_postgresflex_user.keycloak` and `stackit_postgresflex_database.keycloak` no longer exist. There are no Keycloak-specific docker-compose services or secrets.

**New variables (Story 13-9):**

| Variable | Type | Default | Purpose |
|---|---|---|---|
| `oidc_mode` | string | `"external"` | Profile selection; validated against `["dex", "external"]` |
| `dex_allowed_cidr` | string | `"0.0.0.0/0"` | Source CIDR for SG rule on Dex port 5556 (dex mode only) |
| `dex_static_password_hash` | string (sensitive) | `null` | bcrypt hash for Dex static user; required when `oidc_mode = "dex"` |

A `lifecycle { precondition }` block on `stackit_server.nebu` enforces: `oidc_mode == "dex" → dex_static_password_hash != null` and `oidc_mode == "external" → length(oidc_issuer) > 0`.

### DNS Mode — `dns_mode` variable (nebu-aws + nebu-stackit — Story 13-10)

Both the AWS and Stackit deployment examples support a `dns_mode` variable that controls whether OpenTofu creates DNS records automatically or leaves DNS registration to the operator.

| `dns_mode` | AWS behaviour | Stackit behaviour |
|---|---|---|
| `"external"` _(default)_ | No DNS resources created. `dns_name` output holds the ALB DNS hostname for manual CNAME/ALIAS registration. | No DNS resources created. `dns_name` output holds the floating IP for manual A-record registration. |
| `"default"` | Creates `data.aws_route53_zone` + `aws_route53_record` (ALIAS A-record) in Route 53, guarded by `count = 1`. Requires the hosted zone for `var.domain_name` to exist in the AWS account with an exact name match. | Creates `stackit_dns_zone` + `stackit_dns_record_set.nebu` (A-record) for `var.server_name → floating IP`, guarded by `count = 1`. |

Default is `"external"` to prevent accidental DNS changes on existing deployments.

**Stackit-only: `dex_subdomain_enabled`**

When `dns_mode = "default"` and `dex_subdomain_enabled = true`, an additional `stackit_dns_record_set.dex` resource creates a `dex.<server_name>` A-record pointing to the same floating IP. This lays groundwork for future host-based Dex routing (see story 13-9 dev notes). The variable is validated — setting it to `true` when `dns_mode = "external"` raises a validation error at plan time.

**Stackit-only: `dns_contact_email`**

An optional `dns_contact_email` variable (default `""`) sets the `contact_email` on the `stackit_dns_zone` resource. When empty, the field is omitted via null-coalescing (`var.dns_contact_email != "" ? var.dns_contact_email : null`).

**AWS: `dns_name` output**

A new `deploy/tofu/examples/aws/outputs.tf` exports `dns_name = module.nebu_aws.alb_dns_name`. Operators using `dns_mode = "external"` run `tofu output dns_name` to retrieve the ALB hostname to register as a CNAME. Note: CNAME is not supported at the zone apex — use Route 53 ALIAS or an ALIAS/ANAME record at your DNS provider for apex domains.

**RUNBOOK coverage:** Both `deploy/tofu/examples/aws/RUNBOOK.md` and `deploy/tofu/examples/stackit/RUNBOOK.md` include a "DNS Configuration" section describing both modes, manual registration steps for `external` mode, and import instructions for existing Stackit DNS zones.

**New variables (Story 13-10):**

| Variable | Example | Type | Default | Purpose |
|---|---|---|---|---|
| `dns_mode` | AWS + Stackit | string | `"external"` | DNS record creation mode; validated against `["default", "external"]` |
| `dex_subdomain_enabled` | Stackit only | bool | `false` | Create `dex.<server_name>` DNS record when `dns_mode = "default"` |
| `dns_contact_email` | Stackit only | string | `""` | Contact email for Stackit DNS zone (omitted when empty) |

### Stackit cloud-init Bootstrap (nebu-stackit — 13-3b, updated 13-8, 13-9)

`deploy/tofu/examples/stackit/cloud-init.tftpl` is rendered by `templatefile()` in `main.tf` and injected as `user_data` (base64-encoded) into the VM at provision time. On first boot, the VM:

1. Applies OS security patches (`package_upgrade: true`)
2. Installs Docker CE + Docker Compose plugin via the official Docker apt repository
3. Writes `/opt/nebu/` directory tree (permissions `0700`) with:
   - `.secrets/internal_secret` (mode `0600`, quoted scalar — no trailing newline)
   - `.env` (mode `0600`) — all runtime secrets injected from OpenTofu variables, including `NEBU_DB_URL` and `NEBU_DB_URL_MIGRATE` pointing to the PostgresFlex managed instance
   - `/opt/nebu/dex/config.yaml` (mode `0600`, `oidc_mode = "dex"` only) — Dex static configuration
   - `docker-compose.yml` (mode `0640`) — two or three services: `core`, `gateway`, and conditionally `dex` (when `oidc_mode = "dex"`). No `postgres` service (managed PostgresFlex); no `keycloak` service.
4. Installs `/etc/systemd/system/nebu.service` — `Type=simple`, no `-d` flag; systemd owns the process lifecycle with `Restart=on-failure RestartSec=10`
5. Starts Docker (`systemctl start docker.service && sleep 2`) before starting `nebu.service`

**Changes in Story 13-9:**
- `keycloak` docker-compose service removed in both profiles — Keycloak is no longer deployed
- `KC_DB_PASSWORD` removed from `.env` — no Keycloak credentials
- Conditional Dex `write_files` entry and `dex:` service block added (guarded by `%{ if oidc_mode == "dex" ~}` template directive)
- `templatefile()` call no longer passes `kc_user`/`kc_password`; now passes `oidc_mode` and `dex_static_password_hash`
- `local.effective_oidc_issuer` computed in `main.tf` — auto-set to `http://<server_name>:5556/dex` for dex mode; uses `var.oidc_issuer` for external mode

**Security invariants:**
- `pg_password` appears only inside `.env` (mode `0600`), never in plain environment variables or log output
- `dex_static_password_hash` is written only to `/opt/nebu/dex/config.yaml` (mode `0600`)
- ACL on PostgresFlex is set to `[var.network_cidr]` — never `0.0.0.0/0`
- `stackit_postgresflex_user.nebu.password` is stored in Terraform state — operators MUST use encrypted Stackit Object Storage backend (see `RUNBOOK.md`)
- `/opt/nebu/` is `0700` (root-only); `docker-compose.yml` is `0640`
- `nebu_version` variable rejects `"latest"` — a specific semver tag is required
- `oidc_client_secret` must not contain `"` or `\` characters (YAML interpolation constraint, enforced by variable validation)

Day-2 operations (updates, OIDC profile switching, backup, teardown) are documented in `deploy/tofu/examples/stackit/RUNBOOK.md`.

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

---

## Load Test Topology (Story 13-5)

`docker-compose.scale.yml` is a Compose override that removes the fixed host-port binding from the gateway service so Docker Compose can start multiple replicas without port conflicts.

### Multi-Gateway Scale-Up

```bash
# Start full stack with 2 gateway replicas
docker compose -f docker-compose.yml -f docker-compose.scale.yml up \
  --scale gateway=2 --build -d --wait
```

Both replicas connect to the same Core instance via `core:9000` (gRPC) and share the `internal_secret` PSK. For 2-core clustering, see Story 13-6 below.

```
k6 load generator
  │ HTTP  (nebu_default Docker network)
  ▼
[gateway replica 1] ──gRPC:9000──▶ core
[gateway replica 2] ──gRPC:9000──▶ core
                                     │
                                     ▼
                               PostgreSQL 16
```

---

## Core Clustering (Story 13-6): libcluster + Horde Failover

The Elixir Core supports horizontal scaling via libcluster for node discovery and Horde for distributed Room GenServer supervision. When a Core node fails, Horde automatically restarts Room GenServers on the surviving node within seconds (no manual intervention).

### Architecture

```
[gateway] ──gRPC:9000──▶ [core (nebu@core)]   ←── Horde CRDT cluster ───▶ [core2 (nebu@core2)]
                               │                                                    │
                          Room GenServers                                   Room GenServers
                           (Horde Registry)                               (Horde Registry)
                               │                                                    │
                               └───────────────┬──────────────────────────────────┘
                                               ▼
                                        PostgreSQL 16
```

When `core` (nebu@core) is stopped:
1. Horde CRDT reconciliation detects the node departure
2. Room GenServers that were running on `core` are restarted on `core2`
3. Horde Registry re-registers the rooms under `core2`'s node
4. Gateway's next gRPC call succeeds (Room GenServer is alive on `core2`)

### Clustering Strategies

| Environment | Strategy | Discovery |
|---|---|---|
| Docker Compose | `Cluster.Strategy.Gossip` | UDP broadcast (port 45892) within Docker network |
| Kubernetes | `Cluster.Strategy.Kubernetes.DNS` | Headless Service DNS lookup (`core-headless`) |

### Environment Variables

| Variable | Purpose | Example |
|---|---|---|
| `CLUSTER_STRATEGY` | Selects libcluster strategy (`gossip` / `kubernetes`) | `gossip` |
| `RELEASE_NODE` | Erlang node name for distribution | `nebu@core` |
| `RELEASE_DISTRIBUTION` | Erlang distribution mode (`name` = long names) | `name` |
| `CLUSTER_NODES` | Comma-separated peer list (informational; Gossip discovers dynamically) | `nebu@core` |

### Local 2-Core Stack

```bash
# Start full stack with 2 Core nodes + scaled gateways
docker compose -f docker-compose.yml -f docker-compose.scale.yml up -d

# Simulate node failure (AC1 test)
docker stop $(docker compose -f docker-compose.yml -f docker-compose.scale.yml ps --format json | jq -r 'select(.Service=="core") | .Name')

# Verify room availability after failover (rooms migrate to core2 within 10 seconds)
```

The `docker-compose.scale.yml` override adds:
- `core` service: sets `RELEASE_NODE=nebu@core` and `CLUSTER_STRATEGY=gossip`
- `core2` service: same image, `RELEASE_NODE=nebu@core2`, connects to core1 via Gossip multicast

### Kubernetes Clustering

When `core.replicaCount > 1` in `values.yaml`, the Helm chart automatically injects:
- `CLUSTER_STRATEGY=kubernetes` → activates `Cluster.Strategy.Kubernetes.DNS`
- `RELEASE_NODE=nebu@$(MY_POD_IP)` → unique long-form node name per pod
- `RELEASE_DISTRIBUTION=name` → enables Erlang long-name distribution
- `KUBERNETES_SERVICE_NAME` pointing to the headless Service (`core-headless`)

A headless Service (`core-deployment-headless`) is automatically provisioned alongside the standard ClusterIP Service when `replicaCount > 1`. The headless Service returns A records for each pod, enabling libcluster to discover peers via DNS.

### Health Endpoint — Cluster Status

`GET http://core:4000/health` now includes `cluster_nodes` in its JSON response:

```json
{
  "status": "UP",
  "node": "nebu@core",
  "cluster_nodes": ["nebu@core2"],
  "components": { ... }
}
```

`cluster_nodes` is an empty list in single-node mode (no libcluster configured).

### Erlang Cookie (Security)

Erlang distribution requires all nodes in a cluster to share the same cookie. In production deployments:
- **Docker Compose:** uses Erlang's default auto-generated cookie (acceptable for dev; nodes on the same Docker network)
- **Kubernetes/AWS:** `RELEASE_COOKIE` is injected from Secrets Manager (see AWS secrets.tf) to ensure all Core pods use the same pre-shared cookie
- **STACKIT:** operators must set a consistent `RELEASE_COOKIE` in the cloud-init bootstrap across all Core VMs

### k6 Scenario Files

| File | Tier | VUs | Duration | Send p95 threshold |
|---|---|---|---|---|
| `k6/scenarios/gold-tier.js` | Gold | 1000 | 5 min | < 500 ms |
| `k6/scenarios/silver-tier.js` | Silver | 500 | 5 min | < 800 ms |

Each scenario exercises three endpoints per VU per iteration: `POST /_matrix/client/v3/login`, `GET /_matrix/client/v3/sync`, `PUT /_matrix/client/v3/rooms/{roomId}/send/m.room.message/{txnId}`.

Custom metrics reported: `nebu_login_duration`, `nebu_sync_duration`, `nebu_send_duration` (each as a Trend with p50/p95/p99), plus per-endpoint error rates. See `k6/README.md` for full operator instructions.

### CI Syntax Gate

```bash
make test-load-syntax   # k6 inspect (both scenarios) + docker compose config --quiet
```

This gate runs as part of `make test-iac-validate` (no running stack required).
