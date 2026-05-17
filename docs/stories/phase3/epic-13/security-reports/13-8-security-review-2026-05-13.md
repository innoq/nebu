# Security Review — 13-8: Managed Database Services (Aurora Serverless v2 + Stackit PostgresFlex) — 2026-05-13

**Agent:** Kassandra
**Diff base:** `git diff --staged` — story 13-8
**Classification:** CLEAN (no CRITICAL, no HIGH)
**Verdict:** APPROVED — proceed to commit with the MEDIUM/LOW items tracked as follow-ups.
**Config:** `blocking_severity=CRITICAL` (default), `model=claude-sonnet-4-6`

## Executive Summary

This diff migrates the AWS database layer from a Multi-AZ `aws_db_instance` to an Aurora
Serverless v2 cluster (`aws_rds_cluster` + `aws_rds_cluster_instance` with
`instance_class = "db.serverless"`), and replaces the self-hosted `postgres:16-alpine`
docker-compose service on the Stackit VM with a managed `stackit_postgresflex_instance`.
The change improves the baseline security posture in several meaningful ways:

- **Credential isolation:** Stackit moves from one shared `db_password` (used for both
  `keycloak` and `nebu_app` SQL users via `init-db.sql`) to two separately auto-generated
  PostgresFlex users (`nebu_app`, `keycloak_app`). Compromise of one no longer compromises
  the other.
- **Secret-on-disk minimization:** `KC_DB_PASSWORD` moved from inline docker-compose YAML
  to `.env` (mode `0600`), and `docker-compose.yml` permissions tightened from `0640` to
  `0600`. The `init-db.sql` file (which contained DB passwords on first boot) is gone.
- **Managed backup/patching:** Aurora Serverless v2 and PostgresFlex both provide automated
  daily backups + PITR without operator scripting.
- **Network exposure:** `stackit_postgresflex_instance.acl = [var.network_cidr]` restricts
  ingress to the VM's private CIDR only; Aurora is inherently VPC-only (no
  `publicly_accessible` attribute exists on `aws_rds_cluster`) and remains gated by
  `aws_security_group.rds` which only accepts 5432 from the ECS SG.

No CRITICAL or HIGH findings. Two MEDIUM items (URL-injection risk via auto-generated
passwords, hardcoded `deletion_protection = false`) and three LOW items
(weak `db_password` default + min-length, missing IAM DB auth, missing CMK on Aurora
encryption, missing `var.network_cidr` validation) are advisory hardening opportunities.

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | MEDIUM | `deploy/tofu/examples/stackit/cloud-init.tftpl:34-35` | URL injection via unencoded auto-generated password | URL-encode `pg_password` in template, or use a `PG*` env-var form instead of a DSN string |
| 2 | MEDIUM | `deploy/tofu/modules/nebu-aws/database.tf:52` | `deletion_protection = false` hardcoded — production cluster can be destroyed by `tofu destroy` or accidental state change | Make a variable: `var.deletion_protection` (default `true`, override to `false` only in dev tfvars) |
| 3 | LOW | `deploy/tofu/modules/nebu-aws/variables.tf:50-55` | `db_password` default `"changeme"` + 8-char minimum is too weak for an Aurora master password | Remove default (force operator to supply); raise minimum to 16 characters, recommend `manage_master_user_password = true` instead |
| 4 | LOW | `deploy/tofu/modules/nebu-aws/database.tf:23` | Aurora cluster lacks `iam_database_authentication_enabled = true` (defense-in-depth) | Enable IAM DB auth for break-glass operator access without password rotation |
| 5 | LOW | `deploy/tofu/modules/nebu-aws/database.tf:23` | `storage_encrypted = true` uses default `aws/rds` KMS key, not a customer-managed CMK | Add a `kms_key_id` referencing a CMK with key-rotation enabled — required for many compliance baselines (PCI DSS, SOC 2) |
| 6 | LOW | `deploy/tofu/examples/stackit/variables.tf:35-39` | `var.network_cidr` accepts `"0.0.0.0/0"` without validation; if an operator sets that, PostgresFlex `acl` becomes world-open | Add a `validation` block rejecting `"0.0.0.0/0"` and prefixes ≤ /8 |

---

## Detail

### Finding #1 — URL injection via unencoded auto-generated PostgresFlex password [MEDIUM]

**Location:** `deploy/tofu/examples/stackit/cloud-init.tftpl:34-35`

```hcl
NEBU_DB_URL=postgresql://${pg_user}:${pg_password}@${pg_host}:${pg_port}/nebu
NEBU_DB_URL_MIGRATE=postgresql://${pg_user}:${pg_password}@${pg_host}:${pg_port}/nebu
```

**Description:** `pg_password` is supplied by `stackit_postgresflex_user.nebu.password` —
an auto-generated value the operator does not control. The template embeds it directly into
a PostgreSQL connection URI. The lines above (24–29) include a documentation comment
acknowledging the constraint and listing the URL-reserved characters (`@ : / ? #`), but the
comment is the only mitigation. There is no programmatic guard.

If Stackit ever issues (or rotates to) a password containing reserved URL characters, the
DSN parses incorrectly. Two concrete failure modes:

- **`@` in password:** `postgresql://user:foo@bar@host:5432/nebu` — the rightmost `@` is the
  delimiter, so the parser sees `user:foo` as user/password and `bar@host:5432` as host.
  If `bar` resolves via DNS (NXDOMAIN fallback, search-domain expansion, or attacker-
  controlled wildcard), the gateway connects to the wrong host with valid credentials —
  this is a textbook DSN-injection auth bypass. Narrow exploit path, but it exists.
- **`#` in password:** the URL fragment delimiter truncates the password silently —
  authentication fails, migrations break, the stack stops. Availability impact.

Why this is MEDIUM and not LOW:
- The MINOR-5 deferral comment turns a programmatic concern into a runbook footnote.
  The "rotate the credential" mitigation is reactive — by the time you discover the
  problem, your stack is already wedged or, worse, talking to the wrong host.
- The Stackit provider currently does not document the auto-generated password's
  alphabet; we cannot assume reserved characters never occur.

Why it is not HIGH:
- The risk requires an unfortunate password roll (probability < 1%) AND a usable DNS
  pivot (probability much lower in a private network where the gateway only resolves via
  internal DNS).

**Remediation (in priority order):**

1. **Preferred:** Replace the DSN form with PostgreSQL environment variables which do not
   require URL encoding:
   ```
   PGHOST=${pg_host}
   PGPORT=${pg_port}
   PGUSER=${pg_user}
   PGPASSWORD=${pg_password}
   PGDATABASE=nebu
   ```
   The Go gateway's `pgx` driver and the Elixir Postgrex driver both honor `PG*` env
   vars; the application code that currently parses `NEBU_DB_URL` would need to fall
   back to these. Cleanest path, no encoding worries.
2. **Acceptable:** URL-encode `pg_password` inside the template:
   ```hcl
   NEBU_DB_URL=postgresql://${pg_user}:${urlencode(pg_password)}@${pg_host}:${pg_port}/nebu
   ```
   OpenTofu provides `urlencode()` as a built-in function.
3. **Minimum:** Add a `precondition` block in the `stackit_postgresflex_user.nebu`
   resource that fails the apply if the generated password contains any of `@:/?#[]%`,
   forcing the operator to roll the credential before the VM ever boots with a broken DSN.

---

### Finding #2 — `deletion_protection = false` is hardcoded on the Aurora cluster [MEDIUM]

**Location:** `deploy/tofu/modules/nebu-aws/database.tf:52`

```hcl
deletion_protection       = false
```

**Description:** The Aurora cluster ships with `deletion_protection = false` regardless of
environment. An accidental `tofu destroy`, a `targeted` destroy from a CI misconfiguration,
or a `terraform state rm` followed by a re-apply will wipe production data. The
`var.skip_final_snapshot` lever already exists in the module, but `deletion_protection`
is the actual guardrail — `skip_final_snapshot` only controls whether a snapshot is
taken on the way down.

Why this is MEDIUM and not LOW:
- This is the security-of-state-changes axis (CWE-1394, "Unprotected Configuration Data").
  The existing pattern (`skip_final_snapshot` defaulted true) is documented for dev. There
  is no equivalent escape valve to lift `deletion_protection` only for prod, so prod
  ships with the dev-grade default.
- The previous `aws_db_instance.this` had the same default. Story 13-2 already approved
  it. So this is not a regression. But it is the right moment to fix it — the line is
  being touched.

**Remediation:**

```hcl
variable "deletion_protection" {
  description = "If true, the Aurora cluster cannot be deleted by 'tofu destroy'. Set to true for production; false for ephemeral environments."
  type        = bool
  default     = true
}

# in aws_rds_cluster.this:
deletion_protection = var.deletion_protection
```

Wire `aurora_min_capacity = 0` (dev) implicitly through the same tfvars file that sets
`deletion_protection = false`. Production tfvars omit both overrides and get safe defaults.

---

### Finding #3 — Weak `db_password` default and minimum length [LOW]

**Location:** `deploy/tofu/modules/nebu-aws/variables.tf:50-55`

```hcl
default = "changeme"
validation {
  condition     = length(var.db_password) >= 8
  error_message = "db_password must be at least 8 characters."
}
```

**Description:** Aurora's master password is the keys-to-the-kingdom credential for the
cluster. The current configuration:

1. Defaults to `"changeme"` — a string an operator could accidentally apply with
   (especially given `tofu validate`/`plan` pass with this value).
2. Allows passwords as short as 8 characters — well below industry baseline (16+).

**Remediation:**
- Drop the default. Force operators to supply via tfvars or `TF_VAR_db_password`.
- Raise the minimum to 16 characters.
- Better yet, switch to AWS-managed master credentials:
  ```hcl
  manage_master_user_password = true
  master_user_secret_kms_key_id = aws_kms_key.aurora.id  # optional
  ```
  This removes the password from Terraform state entirely; AWS generates and rotates it
  in Secrets Manager.

This is LOW because the existing `secrets.tf` Secrets Manager workflow gives operators a
canonical rotation path, and `sensitive = true` keeps the value out of plan output. But
the default invites the wrong behavior.

---

### Finding #4 — Aurora lacks IAM database authentication [LOW]

**Location:** `deploy/tofu/modules/nebu-aws/database.tf` (`aws_rds_cluster.this`)

**Description:** `iam_database_authentication_enabled` is not set; the cluster defaults
to `false`. With IAM auth enabled, the ECS task role can authenticate to Aurora using
short-lived IAM tokens — eliminating the password from the runtime path entirely and
giving operators a break-glass channel without needing to read the password from
Secrets Manager.

**Remediation:**
```hcl
resource "aws_rds_cluster" "this" {
  # ...
  iam_database_authentication_enabled = true
}
```

Defense-in-depth — does not block this story; tracked for the production hardening pass.

---

### Finding #5 — Aurora `storage_encrypted` uses default AWS-managed KMS key, not a CMK [LOW]

**Location:** `deploy/tofu/modules/nebu-aws/database.tf:23`

```hcl
storage_encrypted = true
```

**Description:** Without `kms_key_id`, Aurora encrypts at rest with the AWS-managed
`aws/rds` key. The encryption is real, but:

- Operators cannot disable the key, so they cannot prove cryptographic erasure to
  auditors.
- The key is shared across the account's RDS resources — blast radius for any AWS
  internal key incident is larger.
- PCI DSS 3.5/3.6, SOC 2 CC6.7, and many enterprise customer security questionnaires
  explicitly require customer-managed keys.

**Remediation:**
```hcl
resource "aws_kms_key" "aurora" {
  description             = "CMK for Nebu Aurora cluster encryption"
  enable_key_rotation     = true
  deletion_window_in_days = 30
  tags                    = merge(var.common_tags, { Name = "nebu-${var.environment}-aurora-kms" })
}

resource "aws_rds_cluster" "this" {
  # ...
  kms_key_id = aws_kms_key.aurora.arn
}
```

LOW because the data-at-rest threat is theoretical at this layer and this is a
compliance hardening item, not a vulnerability.

---

### Finding #6 — `var.network_cidr` accepts `0.0.0.0/0` without validation [LOW]

**Location:** `deploy/tofu/examples/stackit/variables.tf:35-39` and consumed in
`deploy/tofu/examples/stackit/main.tf` as `acl = [var.network_cidr]`

**Description:** The PostgresFlex `acl` directly mirrors `var.network_cidr`. The
default (`"10.0.0.0/24"`) is safe, but the variable has no validation block. An
operator who copy-pastes from another tfvars file and ends up with `network_cidr =
"0.0.0.0/0"` will silently open PostgresFlex to every Stackit tenant the platform
permits through that ACL.

The Dev Notes (story file, line 443) explicitly call out: *"ACL must be set to
`[var.network_cidr]` — never `["0.0.0.0/0"]`."* This makes the absence of a guard
more glaring.

**Remediation:**
```hcl
variable "network_cidr" {
  description = "IPv4 CIDR block for the Nebu network."
  type        = string
  default     = "10.0.0.0/24"

  validation {
    condition = (
      can(cidrnetmask(var.network_cidr)) &&
      var.network_cidr != "0.0.0.0/0" &&
      tonumber(split("/", var.network_cidr)[1]) >= 16
    )
    error_message = "network_cidr must be a valid private CIDR; 0.0.0.0/0 is rejected, and prefixes must be /16 or smaller blocks (≥ /16) to prevent over-broad PostgresFlex ACL."
  }
}
```

LOW because the default is safe; this is a guardrail against operator error.

---

## What the diff got right (positive findings, no remediation needed)

1. **Two separately auto-generated users for Stackit:** `nebu_app` and `keycloak_app` no
   longer share `db_password`. The previous `init-db.sql` baked one operator-supplied
   password into both SQL users; the new design replaces that with two
   provider-generated, individually rotatable credentials. This is a real privilege-
   isolation improvement.
2. **`init-db.sql` removed entirely:** The bootstrap path no longer touches a file with
   plaintext SQL containing `CREATE USER ... WITH PASSWORD '${db_password}'`. One fewer
   secret-bearing artifact on the VM.
3. **`docker-compose.yml` permissions tightened from `0640` → `0600`:** The compose
   file now references `KC_DB_PASSWORD` from `.env` rather than embedding a `$$`-escaped
   variable directly. Mode `0600` matches `.env` and `init-db.sql`'s former mode.
4. **`postgres_host` output, no `pg_password` output:** The sensitive password is
   intentionally not exposed in `tofu output`. The RUNBOOK warns explicitly that the
   password is in state and operators must use an encrypted state backend. Good.
5. **Aurora cluster: inherently VPC-only:** `aws_rds_cluster` has no
   `publicly_accessible` attribute (unlike `aws_db_instance`); the cluster is reachable
   only from within the VPC. The existing `aws_security_group.rds` further restricts
   ingress to the ECS SG on port 5432. No regression.
6. **`backup_retention_period = 7` preserved on Aurora cluster** and PostgresFlex
   has `backup_schedule = "0 2 * * *"` for daily backups. Backup posture maintained.
7. **`storage_encrypted = true`** on the Aurora cluster — encryption at rest is on.
   (Finding #5 is about which key, not whether encryption exists.)
8. **`final_snapshot_identifier` uses `timestamp()` with `lifecycle.ignore_changes`** —
   avoids a perpetual diff while still producing a unique snapshot name at destroy time.
   Correct pattern.
9. **`master_password` is sourced from `var.db_password` (sensitive), not hardcoded.**
   The Dev Notes asked "check which is used" — confirmed: `master_password = var.db_password`,
   NOT `manage_master_user_password = true`. Finding #3 covers the hardening option.

---

## Summary

- **CRITICAL:** 0 — no commit-blocking findings
- **HIGH:**     0 — no commit-blocking findings
- **MEDIUM:**   2 — advisory, address before epic-end retro or as separate hardening story
- **LOW:**      4 — advisory hardening items

**Verdict:** APPROVED. Story 13-8 may proceed to commit. The two MEDIUM findings
(URL injection via auto-generated password; hardcoded `deletion_protection = false`)
should be tracked as follow-up tasks before the epic-end SEC Gate 2 review, ideally
as a small follow-up story in this epic. The four LOW items are appropriate to bundle
into a Phase-3 production-hardening story.

---

## Memory Notes

Patterns worth carrying forward into MEMORY.md after this review:

- **DSN composition with externally-generated passwords** is a recurring infra-layer risk.
  Any time a provider-generated credential ends up inside a URI (PostgreSQL DSN, AMQP URL,
  MongoDB connection string, etc.), require URL-encoding or switch to env-var form.
- **`deletion_protection` defaults**: every database-bearing resource we provision should
  default to `deletion_protection = true` with an explicit dev override, not the inverse.
  This applies to RDS, Aurora, DynamoDB tables, EFS, EBS snapshots — review existing
  resources in `deploy/tofu/` for the same anti-pattern.
- **PostgresFlex ACL = single-variable**: any time a SG-equivalent input is `acl =
  [var.foo]` (singleton list passthrough), enforce a validation block on the source
  variable rejecting `0.0.0.0/0` and overly broad prefixes.
