---
status: review
epic: 13
story: 8
security_review: required
matrix: false
ui: false
atdd: not-applicable
---

# Story 13.8: Managed Database Services — AWS Aurora Serverless v2 + Stackit Managed PostgreSQL (PostgresFlex)

Status: review

## Story

As a system operator,
I want Nebu's AWS deployment to use Aurora Serverless v2 instead of a fixed RDS instance, and the Stackit deployment to use Stackit PostgresFlex (managed PostgreSQL) instead of a self-hosted Docker container,
So that both deployments have lower baseline cost, operational overhead is reduced through managed backup/patching, and Aurora Serverless v2 charges only for actual usage (scales to zero ACUs) rather than billing a running instance 24/7.

**Size:** M

---

## Background & Motivation

### AWS — Why Aurora Serverless v2 over RDS Multi-AZ

The current `deploy/tofu/modules/nebu-aws/database.tf` uses `aws_db_instance` with `multi_az = true` and `instance_class = "db.t3.medium"`. This always-on instance bills ~$50–100/month regardless of load.

Aurora Serverless v2 (`engine_mode = "provisioned"` + `serverlessv2_scaling_configuration`) scales in 0.5 ACU increments and can scale to `min_capacity = 0` ACUs (auto-pause after idle period), meaning near-zero cost when Nebu is inactive. In production, `min_capacity = 0.5` avoids cold-start latency while keeping cost proportional to actual load.

**Key difference from Aurora Serverless v1:** Serverless v2 uses `engine_mode = "provisioned"` (NOT `"serverless"`). It requires both an `aws_rds_cluster` resource AND at least one `aws_rds_cluster_instance` with `instance_class = "db.serverless"`.

### Stackit — Why PostgresFlex over bundled Docker PostgreSQL

The current `deploy/tofu/examples/stackit/cloud-init.tftpl` starts a `postgres:16-alpine` container inside docker-compose on the VM alongside Keycloak, core, and gateway. This bundles the database with the application VM:

- Single point of failure (VM crash = data loss unless volume backup ran)
- No automated backups via Stackit platform
- Snapshot/PITR requires manual pg_dump cron
- Keycloak and Nebu data share a single Postgres process

`stackit_postgresflex_instance` provisions a dedicated managed PostgreSQL instance (PostgresFlex) in the Stackit platform with automated daily backups, PITR, and HA replication. The VM then connects via the instance's private endpoint — the postgres container is removed from docker-compose.

**Important:** The Stackit provider resource is `stackit_postgresflex_instance` (NOT `stackit_postgresql_instance`). The user resource is `stackit_postgresflex_user`. The provider version `~> 0.95` (already in the lock file) supports these resources.

---

## Acceptance Criteria

**AC1 — AWS: `aws_rds_cluster` replaces `aws_db_instance` in `database.tf`:**
Given `deploy/tofu/modules/nebu-aws/database.tf`,
When the file is inspected,
Then:
- `aws_db_instance.this` is removed
- `aws_rds_cluster.this` is present with `engine = "aurora-postgresql"`, `engine_mode = "provisioned"`, `engine_version = "16.6"`, and a `serverlessv2_scaling_configuration` block
- `aws_rds_cluster_instance.this` is present with `instance_class = "db.serverless"`
- `aws_db_subnet_group.this` is retained (Aurora clusters also use DB subnet groups)

**AC2 — AWS: Serverless v2 scaling configuration is present:**
Given `aws_rds_cluster.this`,
When the `serverlessv2_scaling_configuration` block is inspected,
Then:
- `min_capacity = 0` (scales to zero when idle, for dev/staging)
- `max_capacity = 4` (sufficient for expected Nebu load — 2 vCPU equivalent)
- `seconds_until_auto_pause = 3600` (auto-pause after 1 hour idle)
- A comment explains that operators should set `min_capacity = 0.5` for production to avoid cold starts

**AC3 — AWS: `db_endpoint` output updated to Aurora cluster endpoint:**
Given `deploy/tofu/modules/nebu-aws/outputs.tf`,
When the `db_endpoint` output is inspected,
Then it references `aws_rds_cluster.this.endpoint` instead of the removed `aws_db_instance.this.endpoint`

**AC4 — AWS: Variables cleaned up — `db_instance_class` and `enable_performance_insights` removed:**
Given `deploy/tofu/modules/nebu-aws/variables.tf`,
When it is inspected,
Then:
- `var.db_instance_class` is removed (Aurora Serverless v2 uses `db.serverless`, not a configurable class)
- `var.enable_performance_insights` is removed (Performance Insights is enabled by default on Aurora; not a relevant input)
- Two new variables added: `var.aurora_min_capacity` (default `0`) and `var.aurora_max_capacity` (default `4`), both of type `number`

**AC5 — AWS: `examples/aws/main.tf` module call updated:**
Given `deploy/tofu/examples/aws/main.tf`,
When the `module "nebu_aws"` block is inspected,
Then:
- `db_instance_class` and `enable_performance_insights` arguments are removed
- `aurora_min_capacity` and `aurora_max_capacity` arguments are present (wired from variables or defaults)

**AC6 — AWS: `examples/aws/variables.tf` updated accordingly:**
Given `deploy/tofu/examples/aws/variables.tf`,
When it is inspected,
Then `db_instance_class` and `enable_performance_insights` variables are removed and `aurora_min_capacity` / `aurora_max_capacity` variables are added with appropriate descriptions and defaults.

**AC7 — Stackit: `stackit_postgresflex_instance` added to `examples/stackit/main.tf`:**
Given `deploy/tofu/examples/stackit/main.tf`,
When the file is inspected,
Then:
- `stackit_postgresflex_instance.nebu` is present with `project_id`, `name`, `version = "16"`, `replicas`, `backup_schedule`, `acl` (locked to the VM's private network CIDR), `flavor`, and `storage` blocks
- `stackit_postgresflex_user.nebu` is present, referencing the instance and with roles `["login"]`
- `stackit_postgresflex_database.nebu` is present with `name = "nebu"` and the user as owner
- `stackit_postgresflex_database.keycloak` is present with `name = "keycloak"` (Keycloak still runs in docker-compose and needs its own DB in the managed instance)

**AC8 — Stackit: postgres container removed from cloud-init:**
Given `deploy/tofu/examples/stackit/cloud-init.tftpl`,
When the rendered docker-compose section is inspected,
Then:
- The `postgres` service block is gone
- `postgres_data` volume is gone
- `init-db.sql` write_files block and the runcmd `rm -f /opt/nebu/init-db.sql` line are gone
- `NEBU_DB_URL` in `.env` now points to the PostgresFlex host/port/credentials (injected as template variables from OpenTofu)
- `keycloak` service `KC_DB_URL` points to the PostgresFlex host (not `postgres:5432`)
- `core` and `gateway` `depends_on: postgres` conditions are removed (replaced with a readiness comment)

**AC9 — Stackit: New variables for PostgresFlex connection added:**
Given `deploy/tofu/examples/stackit/variables.tf`,
When it is inspected,
Then the following variables are added:
- `var.postgres_replicas` (number, default `1`, description includes note that `3` is recommended for production HA)
- `var.postgres_cpu` (number, default `1`)
- `var.postgres_ram` (number, default `4`, in GB)
- `var.postgres_storage_size` (number, default `20`, in GB)
And `var.db_password` is REMOVED (it was used only for the self-hosted postgres container — the managed instance auto-generates credentials via `stackit_postgresflex_user`)

**AC10 — Stackit: Outputs include PostgresFlex connection details:**
Given `deploy/tofu/examples/stackit/outputs.tf`,
When it is inspected,
Then outputs `postgres_instance_id` and `postgres_host` are added (the password is sensitive and intentionally not outputted — operators read it from `stackit_postgresflex_user.nebu.password` in state or via the STACKIT portal).

**AC11 — Stackit: `terraform.tfvars.example` updated:**
Given `deploy/tofu/examples/stackit/terraform.tfvars.example`,
When it is inspected,
Then:
- `db_password` variable is removed
- PostgresFlex sizing variables (`postgres_replicas`, `postgres_cpu`, `postgres_ram`, `postgres_storage_size`) are added with example values and production guidance comments

**AC12 — `tofu validate` passes on both examples:**
Given `deploy/tofu/examples/aws/` and `deploy/tofu/examples/stackit/`,
When `tofu validate` runs in each directory,
Then exit code 0 (no schema errors)

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Infrastructure-only story — ATDD is not applicable. There is no application logic, Matrix API, or observable UI behavior. The acceptance gates are OpenTofu static validation and the file inspection checks below.

**1. Static validation — AWS module**
- Given: `deploy/tofu/modules/nebu-aws/database.tf` still contains `aws_db_instance`
- When: `tofu validate` runs in `deploy/tofu/examples/aws/`
- Then: exits 0 today (existing state); after removal of `aws_db_instance` and addition of `aws_rds_cluster` — must also exit 0
- Verification command: `docker run --rm -v $(pwd)/deploy/tofu/examples/aws:/workspace -w /workspace ghcr.io/opentofu/opentofu:1.9 validate`

**2. Static validation — Stackit example**
- Given: `deploy/tofu/examples/stackit/main.tf` contains `stackit_postgresflex_instance`
- When: `tofu validate` runs in `deploy/tofu/examples/stackit/`
- Then: exit 0
- Verification command: `docker run --rm -v $(pwd)/deploy/tofu/examples/stackit:/workspace -w /workspace ghcr.io/opentofu/opentofu:1.9 init -backend=false && tofu validate`

**3. Security gate — no plaintext credentials in template**
- Given: updated `cloud-init.tftpl`
- When: grep for literal password strings or connection strings with embedded passwords
- Then: only `${variable_name}` template references found — no hardcoded values

**4. Regression check — no postgres container in cloud-init**
- Given: updated `cloud-init.tftpl`
- When: grep for `image: postgres` or `postgres_data:`
- Then: zero matches

---

## Dev Notes — Implementation Guide

### File Change Map

| File | Action | Notes |
|---|---|---|
| `deploy/tofu/modules/nebu-aws/database.tf` | UPDATE | Replace `aws_db_instance` with `aws_rds_cluster` + `aws_rds_cluster_instance` |
| `deploy/tofu/modules/nebu-aws/outputs.tf` | UPDATE | `db_endpoint` → `aws_rds_cluster.this.endpoint` |
| `deploy/tofu/modules/nebu-aws/variables.tf` | UPDATE | Remove `db_instance_class`, `enable_performance_insights`; add `aurora_min_capacity`, `aurora_max_capacity` |
| `deploy/tofu/modules/nebu-aws/network.tf` | NO CHANGE | `aws_security_group.rds` name still correct for Aurora cluster SG |
| `deploy/tofu/examples/aws/main.tf` | UPDATE | Remove removed vars from module call; add new Aurora vars |
| `deploy/tofu/examples/aws/variables.tf` | UPDATE | Remove old DB vars; add Aurora scaling vars |
| `deploy/tofu/examples/stackit/main.tf` | UPDATE | Add PostgresFlex resources; remove `db_password` from `user_data` template call |
| `deploy/tofu/examples/stackit/variables.tf` | UPDATE | Remove `db_password`; add PostgresFlex sizing vars |
| `deploy/tofu/examples/stackit/outputs.tf` | UPDATE | Add `postgres_instance_id`, `postgres_host` |
| `deploy/tofu/examples/stackit/cloud-init.tftpl` | UPDATE | Remove postgres service + volume; update DB URL to managed instance |
| `deploy/tofu/examples/stackit/terraform.tfvars.example` | UPDATE | Remove `db_password`; add PostgresFlex vars |

### AWS Aurora Serverless v2 — Exact Resource Shape

The canonical provider schema (from context7 docs, AWS provider `~> 5.0`):

```hcl
# deploy/tofu/modules/nebu-aws/database.tf

resource "aws_db_subnet_group" "this" {
  # KEEP AS-IS — Aurora uses DB subnet groups too
  name       = "nebu-${var.environment}-db-subnet-group"
  subnet_ids = aws_subnet.private[*].id
  tags       = merge(var.common_tags, { Name = "nebu-${var.environment}-db-subnet-group" })
}

resource "aws_rds_cluster" "this" {
  cluster_identifier = "nebu-${var.environment}-aurora"

  engine         = "aurora-postgresql"
  engine_mode    = "provisioned"   # Serverless v2 uses "provisioned" — NOT "serverless"
  engine_version = "16.6"

  database_name   = "nebu"
  master_username = "nebu"
  master_password = var.db_password

  storage_encrypted = true

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  # Serverless v2 scaling — min=0 allows scale-to-zero (auto-pause) for dev.
  # For production, set min_capacity = 0.5 to avoid cold-start latency.
  serverlessv2_scaling_configuration {
    min_capacity             = var.aurora_min_capacity
    max_capacity             = var.aurora_max_capacity
    seconds_until_auto_pause = 3600
  }

  backup_retention_period = 7
  preferred_backup_window = "03:00-04:00"
  preferred_maintenance_window = "sun:04:00-sun:05:00"

  skip_final_snapshot       = var.skip_final_snapshot
  final_snapshot_identifier = "nebu-${var.environment}-final-snapshot"
  deletion_protection       = false

  tags = merge(var.common_tags, { Name = "nebu-${var.environment}-aurora-cluster" })
}

resource "aws_rds_cluster_instance" "this" {
  identifier         = "nebu-${var.environment}-aurora-instance-1"
  cluster_identifier = aws_rds_cluster.this.id
  instance_class     = "db.serverless"   # Required for Serverless v2
  engine             = aws_rds_cluster.this.engine
  engine_version     = aws_rds_cluster.this.engine_version

  db_subnet_group_name = aws_db_subnet_group.this.name

  tags = merge(var.common_tags, { Name = "nebu-${var.environment}-aurora-instance-1" })
}
```

**Critical distinction:** Aurora Serverless v2 cluster endpoint is `aws_rds_cluster.this.endpoint` (writer endpoint), NOT `aws_rds_cluster.this.reader_endpoint`. The gateway and core must connect to the writer endpoint for read-write access.

**`outputs.tf` change:**
```hcl
output "db_endpoint" {
  description = "Writer endpoint for the Aurora PostgreSQL cluster (host:port)."
  value       = "${aws_rds_cluster.this.endpoint}:${aws_rds_cluster.this.port}"
}
```

**New variables in `variables.tf`:**
```hcl
variable "aurora_min_capacity" {
  description = "Minimum Aurora Serverless v2 capacity in ACUs (0.5 ACU = ~1 vCPU/2 GB RAM). Set to 0 for dev (scale-to-zero). Set to 0.5 for production to avoid cold-start latency."
  type        = number
  default     = 0

  validation {
    condition     = var.aurora_min_capacity >= 0 && var.aurora_min_capacity <= 256
    error_message = "aurora_min_capacity must be between 0 and 256 ACUs."
  }
}

variable "aurora_max_capacity" {
  description = "Maximum Aurora Serverless v2 capacity in ACUs. 1 ACU ≈ 2 GB RAM. Default 4 = sufficient for expected Nebu MVP load. Increase for high-traffic production."
  type        = number
  default     = 4

  validation {
    condition     = var.aurora_max_capacity >= 0.5 && var.aurora_max_capacity <= 256
    error_message = "aurora_max_capacity must be between 0.5 and 256 ACUs."
  }
}
```

### Stackit PostgresFlex — Exact Resource Shape

The Stackit provider uses `stackit_postgresflex_instance` (confirmed via context7 docs). The resource is available in provider `~> 0.95` (already locked).

```hcl
# In deploy/tofu/examples/stackit/main.tf

# ── PostgresFlex Managed Database ────────────────────────────────────────────

resource "stackit_postgresflex_instance" "nebu" {
  project_id      = var.stackit_project_id
  name            = "nebu-${var.environment}-postgres"
  version         = "16"
  replicas        = var.postgres_replicas
  backup_schedule = "0 2 * * *"  # Daily at 02:00 UTC

  # ACL: restrict to the VM's private network CIDR only.
  # The VM connects via its private network interface — no public exposure.
  acl = [var.network_cidr]

  flavor = {
    cpu = var.postgres_cpu
    ram = var.postgres_ram
  }

  storage = {
    class = "premium-perf6-stackit"
    size  = var.postgres_storage_size
  }
}

resource "stackit_postgresflex_user" "nebu" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  username    = "nebu_app"
  roles       = ["login"]
}

resource "stackit_postgresflex_database" "nebu" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  name        = "nebu"
  owner       = stackit_postgresflex_user.nebu.username
}

resource "stackit_postgresflex_database" "keycloak" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  name        = "keycloak"
  owner       = stackit_postgresflex_user.nebu.username
}
```

**Connection details from `stackit_postgresflex_user`:** The resource exposes `host`, `port`, `password`, and `username` as computed/sensitive attributes after creation. Use these to build the DB URL injected into cloud-init.

**Outputs to add in `outputs.tf`:**
```hcl
output "postgres_instance_id" {
  description = "STACKIT PostgresFlex instance ID."
  value       = stackit_postgresflex_instance.nebu.instance_id
}

output "postgres_host" {
  description = "PostgresFlex private host (only reachable from within the Stackit private network)."
  value       = stackit_postgresflex_user.nebu.host
}
```

Note: `stackit_postgresflex_user.nebu.password` is sensitive — do NOT output it. Operators retrieve it from Terraform state or via the STACKIT portal.

### Stackit cloud-init Changes

**`templatefile()` call in `stackit_server.nebu.user_data` must be updated:**

Remove `db_password` from the template variables. Add PostgresFlex connection details:

```hcl
user_data = base64encode(templatefile("${path.module}/cloud-init.tftpl", {
  # db_password removed — managed by PostgresFlex, not cloud-init
  internal_secret    = var.internal_secret
  oidc_client_secret = var.oidc_client_secret
  oidc_issuer        = var.oidc_issuer
  server_name        = var.server_name
  image_registry     = var.image_registry
  nebu_version       = var.nebu_version
  # PostgresFlex connection details (computed after instance creation)
  pg_host     = stackit_postgresflex_user.nebu.host
  pg_port     = stackit_postgresflex_user.nebu.port
  pg_user     = stackit_postgresflex_user.nebu.username
  pg_password = stackit_postgresflex_user.nebu.password
}))
```

**`cloud-init.tftpl` changes:**

1. Remove the entire `postgres` service from docker-compose (including `volumes:`, `healthcheck:`, and `env_file`)
2. Remove the `postgres_data:` volume
3. Remove the `init-db.sql` `write_files` block
4. Update `.env` to use PostgresFlex credentials:
   ```
   NEBU_DB_URL=postgresql://${pg_user}:${pg_password}@${pg_host}:${pg_port}/nebu
   NEBU_DB_URL_MIGRATE=postgresql://${pg_user}:${pg_password}@${pg_host}:${pg_port}/nebu
   ```
   Remove `POSTGRES_PASSWORD` env var (no longer needed)
5. Update `keycloak` service:
   ```yaml
   KC_DB_URL: jdbc:postgresql://${pg_host}:${pg_port}/keycloak
   KC_DB_USERNAME: ${pg_user}
   KC_DB_PASSWORD: ${pg_password}
   ```
   Remove `KC_DB_PASSWORD: $${POSTGRES_PASSWORD}` pattern
6. Remove `depends_on: postgres` from `core` and `gateway` services (the managed DB is always available)
7. Remove `runcmd` line: `rm -f /opt/nebu/init-db.sql`

### Stackit Variables to Add/Remove

**Remove from `variables.tf`:**
- `variable "db_password"` — completely removed; managed DB auto-generates credentials

**Add to `variables.tf`:**
```hcl
variable "postgres_replicas" {
  description = "Number of PostgresFlex replicas. Use 1 for dev/testing. Use 3 for production HA."
  type        = number
  default     = 1
}

variable "postgres_cpu" {
  description = "CPU cores for the PostgresFlex instance flavor."
  type        = number
  default     = 1
}

variable "postgres_ram" {
  description = "RAM in GB for the PostgresFlex instance flavor."
  type        = number
  default     = 4
}

variable "postgres_storage_size" {
  description = "Storage size in GB for the PostgresFlex instance."
  type        = number
  default     = 20
}
```

### Security Considerations (Required Review)

This story touches database credentials and connection strings — `security_review: required` applies.

Key security invariants the reviewer must verify:

1. **No credentials in cloud-init template output:** `pg_password` appears only inside the `.env` file block (mode `0600`), never in environment variables passed directly to a service or in log output.
2. **`pg_password` not emitted as a Tofu output:** Sensitive password must use `sensitive = true` if ever added to outputs — current plan does NOT output it.
3. **ACL on PostgresFlex:** `acl` must be set to `[var.network_cidr]` — never `["0.0.0.0/0"]`.
4. **Aurora cluster not publicly accessible:** `aws_rds_cluster` has no `publicly_accessible` attribute (unlike `aws_db_instance`) — Aurora clusters are inherently VPC-only. Verify the cluster is in private subnets only.
5. **State file encryption:** The Stackit state backend (`backend "s3"`) must use server-side encryption. The `stackit_postgresflex_user.nebu.password` is stored in the Terraform state — this must be documented in RUNBOOK or README.
6. **`master_password` in Aurora cluster:** `var.db_password` (for Aurora) remains sensitive — do not log or output it.

### Impact Analysis — What Must NOT Break

**AWS module callers (`examples/aws/main.tf`):**
- The `db_endpoint` output changes format slightly: previously `host:port` from `aws_db_instance.this.endpoint` (which includes port in the endpoint string already), now explicitly `"${aws_rds_cluster.this.endpoint}:${aws_rds_cluster.this.port}"`. Verify the format matches what `secrets.tf` sets for `db_url` — the placeholder `"PLACEHOLDER_postgres://nebu:ROTATE_ME@hostname:5432/nebu"` suggests port 5432. Aurora PostgreSQL uses port 5432.
- The `aws_security_group.rds` name stays the same — no rename needed, rename would destroy/recreate it.

**Stackit cloud-init dependent behaviors that must NOT break:**
- Keycloak container must still start and connect to its database (now via PostgresFlex)
- `gateway` and `core` services must receive a valid `NEBU_DB_URL` pointing to the PostgresFlex endpoint
- `nebu.service` systemd unit remains unchanged
- `internal_secret` injection via `/opt/nebu/.secrets/internal_secret` remains unchanged

### `tofu validate` Workflow

Since no local OpenTofu installation is assumed (per CLAUDE.md), use the Docker-based validation:

```bash
# AWS
docker run --rm \
  -v $(pwd)/deploy/tofu/examples/aws:/workspace \
  -w /workspace \
  ghcr.io/opentofu/opentofu:1.9 \
  sh -c "tofu init -backend=false && tofu validate"

# Stackit
docker run --rm \
  -v $(pwd)/deploy/tofu/examples/stackit:/workspace \
  -w /workspace \
  ghcr.io/opentofu/opentofu:1.9 \
  sh -c "tofu init -backend=false && tofu validate"
```

The `init -backend=false` flag skips backend initialization (state bucket credentials not needed for validate).

If the Docker image tag `1.9` is not available, use `latest` or `1.8` — the `tofu validate` command is stable across these versions.

### Pattern Consistency

Follow the patterns established in existing IaC files:

- All resource names use `"nebu-${var.environment}-<type>"` pattern (e.g., `"nebu-${var.environment}-aurora-cluster"`)
- Tags always use `merge(var.common_tags, { Name = "..." })` (AWS)
- Descriptions on variables always use full sentences with a trailing period
- Comments above resources follow the `# ── Section Name ──────` separator style
- `sensitive = true` on all credential variables (password, secrets)
- `lifecycle { ignore_changes = [secret_string] }` on Secrets Manager versions (existing pattern in `secrets.tf` — do NOT apply to Aurora password directly)

---

## Checklist

- [ ] `deploy/tofu/modules/nebu-aws/database.tf` — `aws_db_instance` replaced with `aws_rds_cluster` + `aws_rds_cluster_instance`
- [ ] `deploy/tofu/modules/nebu-aws/outputs.tf` — `db_endpoint` references Aurora cluster endpoint
- [ ] `deploy/tofu/modules/nebu-aws/variables.tf` — `db_instance_class` and `enable_performance_insights` removed; `aurora_min_capacity` and `aurora_max_capacity` added
- [ ] `deploy/tofu/examples/aws/main.tf` — module call updated
- [ ] `deploy/tofu/examples/aws/variables.tf` — variables aligned with module changes
- [ ] `deploy/tofu/examples/stackit/main.tf` — `stackit_postgresflex_instance`, `stackit_postgresflex_user`, two `stackit_postgresflex_database` resources added; `user_data` template call updated
- [ ] `deploy/tofu/examples/stackit/variables.tf` — `db_password` removed; PostgresFlex sizing vars added
- [ ] `deploy/tofu/examples/stackit/outputs.tf` — `postgres_instance_id` and `postgres_host` added
- [ ] `deploy/tofu/examples/stackit/cloud-init.tftpl` — postgres service + volume removed; DB URLs use `${pg_host}/${pg_port}/${pg_user}/${pg_password}`; Keycloak DB URL updated
- [ ] `deploy/tofu/examples/stackit/terraform.tfvars.example` — `db_password` removed; PostgresFlex vars added
- [ ] `tofu validate` exits 0 in both `examples/aws/` and `examples/stackit/`
- [ ] Security: no `pg_password` in plain output; ACL not `0.0.0.0/0`; state encryption note in docs
