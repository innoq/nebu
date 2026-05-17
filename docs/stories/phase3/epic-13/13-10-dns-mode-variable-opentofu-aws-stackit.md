---
status: review
epic: 13
story: 10
security_review: not-needed
matrix: false
ui: false
atdd: not-applicable
---

# Story 13.10: DNS Mode — `dns_mode` variable for OpenTofu deployments (AWS + Stackit)

Status: review

## Story

As a system operator,
I want to control DNS record creation via a `dns_mode` variable in the OpenTofu deployments,
so that I can have OpenTofu automatically create DNS records in the cloud provider's DNS service (`dns_mode = "default"`) or skip DNS creation entirely and register the ALB hostname/IP myself in an external DNS server (`dns_mode = "external"`).

---

## Background

DNS is currently not handled in the IaC deployment. Neither `deploy/tofu/examples/aws/` nor `deploy/tofu/examples/stackit/` create DNS records. The ALB's public DNS name (AWS) or floating IP (Stackit) is output after apply, but the operator must manually register it in their DNS server.

This story adds a `dns_mode` variable to both deployment examples:

| `dns_mode` | AWS behaviour | Stackit behaviour |
|---|---|---|
| `"default"` | Creates a Route 53 A-record (ALIAS to ALB) for `<domain_name>` | Creates a Stackit DNS zone entry pointing `<server_name>` to the floating IP |
| `"external"` | No DNS resource creation. Outputs ALB DNS name for manual registration. | No DNS resource creation. Outputs floating IP for manual registration. |

**Default must be `"external"`** — to prevent accidental DNS changes on existing deployments.

**Dex subdomain support:** When `dns_mode = "default"` and `dex_subdomain_enabled = true`, an additional DNS record `dex.<server_name>` pointing to the same ALB/floating-IP is created. This lays the groundwork for future host-based Dex routing (see story 13-9 dev notes: "TODO (future): implement host-based routing via nginx (dex.<server_name>:443 → dex)").

---

## Acceptance Criteria

**AC1 — `dns_mode` variable exists in both deployment examples:**
Given `deploy/tofu/examples/aws/variables.tf` and `deploy/tofu/examples/stackit/variables.tf`,
When they are inspected,
Then:
- `variable "dns_mode"` is present with `type = string`, `default = "external"`, and a `validation` block enforcing `contains(["default", "external"], var.dns_mode)`
- The description clearly explains the two modes

**AC2 — `dex_subdomain_enabled` variable exists in Stackit example:**
Given `deploy/tofu/examples/stackit/variables.tf`,
When it is inspected,
Then:
- `variable "dex_subdomain_enabled"` is present with `type = bool`, `default = false`
- The description explains it creates `dex.<server_name>` when `dns_mode = "default"` and `oidc_mode = "dex"`

**AC3 — AWS Route 53 resources created when `dns_mode = "default"`:**
Given `deploy/tofu/examples/aws/main.tf` with `dns_mode = "default"`,
When it is inspected,
Then:
- A `data "aws_route53_zone"` data source looks up the hosted zone for `var.domain_name`
- A `resource "aws_route53_record" "nebu"` creates an ALIAS A-record pointing `var.domain_name` to `module.nebu_aws.alb_dns_name` (using `module.nebu_aws.alb_zone_id` for the ALIAS zone ID)
- Both are guarded by `count = var.dns_mode == "default" ? 1 : 0`

**AC4 — AWS Route 53 resources absent when `dns_mode = "external"`:**
Given `deploy/tofu/examples/aws/main.tf` with `dns_mode = "external"` (default),
When the file is inspected and `tofu validate` runs,
Then the `data.aws_route53_zone.nebu` and `aws_route53_record.nebu` resources are guarded with `count = 0`, so no Route 53 resources appear in the plan output, and `tofu validate` exits with code 0

**AC5 — Stackit DNS zone + record created when `dns_mode = "default"`:**
Given `deploy/tofu/examples/stackit/main.tf` with `dns_mode = "default"`,
When it is inspected,
Then:
- A `resource "stackit_dns_zone" "nebu"` creates a DNS zone for the domain extracted from `var.server_name`
- A `resource "stackit_dns_record_set" "nebu"` creates an A-record pointing `var.server_name` to `stackit_public_ip.nebu.ip`
- Both are guarded by `count = var.dns_mode == "default" ? 1 : 0`

**AC6 — Stackit `dex.<server_name>` DNS record created when `dns_mode = "default"` and `dex_subdomain_enabled = true`:**
Given `deploy/tofu/examples/stackit/main.tf` with `dns_mode = "default"` and `dex_subdomain_enabled = true`,
When it is inspected,
Then:
- A `resource "stackit_dns_record_set" "dex"` creates an A-record for `"dex.${var.server_name}"` pointing to `stackit_public_ip.nebu.ip`
- It is guarded by `count = var.dns_mode == "default" && var.dex_subdomain_enabled ? 1 : 0`

**AC7 — `dns_name` output added to both examples:**
Given `deploy/tofu/examples/aws/main.tf` (or a new `outputs.tf`) and `deploy/tofu/examples/stackit/outputs.tf`,
When `dns_mode = "external"`,
Then:
- An output `dns_name` is present in both examples
- For AWS: value is `module.nebu_aws.alb_dns_name` (what to put in an external CNAME)
- For Stackit: value is `stackit_public_ip.nebu.ip` (what to put in an external A-record)
- The output description instructs the operator to register this value in their external DNS

**AC8 — `terraform.tfvars.example` documents `dns_mode` in both examples:**
Given both `terraform.tfvars.example` files,
When they are inspected,
Then:
- `dns_mode` is documented with a comment explaining the two modes
- Default `"external"` is shown with a comment about manual DNS registration
- `"default"` option is shown as a comment with a note about what DNS resources are created
- For Stackit: `dex_subdomain_enabled` is documented (commented-out example, default false)

**AC9 — `tofu validate` passes on both examples after changes:**
Given `deploy/tofu/examples/aws/` and `deploy/tofu/examples/stackit/`,
When `make test-iac-validate` runs,
Then exit code is 0 for both (via `tofu fmt -check && tofu validate`)

**AC10 — RUNBOOK.md updated in both examples:**
Given both `RUNBOOK.md` files,
When inspected,
Then a "DNS Configuration" section describes both modes and the manual registration steps for `dns_mode = "external"`

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Infrastructure-only story — ATDD is not applicable. The acceptance gates are OpenTofu static validation and file-content inspection checks.

**1. `tofu validate` — AWS example with `dns_mode = "external"` (default, no Route 53)**
- Given: updated `deploy/tofu/examples/aws/` with `dns_mode` variable (default `"external"`)
- When: `docker run --rm -v $(pwd)/deploy/tofu/examples/aws:/workspace -w /workspace ghcr.io/opentofu/opentofu:1.9 sh -c "tofu init -backend=false && tofu validate"`
- Then: exit code 0, no schema errors

**2. `tofu validate` — Stackit example with `dns_mode = "external"` (default, no Stackit DNS)**
- Given: updated `deploy/tofu/examples/stackit/` with `dns_mode` variable (default `"external"`)
- When: same `tofu validate` command
- Then: exit code 0

**3. File inspection — Route 53 resources guarded by `count = 0`**
- Given: `deploy/tofu/examples/aws/main.tf`
- When: file is inspected for `count = var.dns_mode == "default" ? 1 : 0` guards on `data.aws_route53_zone.nebu` and `aws_route53_record.nebu`, and `tofu validate` runs
- Then: both resources have the count guard present (file inspection confirms), and `tofu validate` passes with exit code 0

**4. File inspection — Stackit DNS resources guarded by count**
- Given: updated `deploy/tofu/examples/stackit/main.tf`
- When: `grep -c 'count.*dns_mode.*"default"' deploy/tofu/examples/stackit/main.tf`
- Then: output >= 2 (zone + record resources both guarded)

**5. File inspection — `dns_mode` variable with validation in both `variables.tf` files**
- Given: updated `variables.tf` in both examples
- When: `grep -A 5 'variable "dns_mode"' deploy/tofu/examples/aws/variables.tf`
- Then: output contains `validation` and `contains(["default", "external"]`
- Repeat for Stackit

**6. File inspection — `dns_name` output present in both examples**
- Given: updated output files in both examples
- When: `grep "dns_name" deploy/tofu/examples/aws/main.tf` and `grep "dns_name" deploy/tofu/examples/stackit/outputs.tf`
- Then: both contain the output definition

**7. `make test-iac-validate` passes (full CI gate)**
- Given: all changes applied
- When: `make test-iac-validate`
- Then: exit code 0 (tofu fmt -check + validate for aws + stackit + k8s; helm lint; k6 inspect)

---

## Dev Notes — Implementation Guide

### Architecture Context

- AWS example (`deploy/tofu/examples/aws/`) uses the `nebu-aws` module for all AWS resources. The module already outputs `alb_dns_name` and `alb_zone_id` from `modules/nebu-aws/outputs.tf` (lines 48–56). Route 53 resources should be created in `main.tf` of the example (not inside the module) — example-level resources keep the module reusable.
- Stackit example (`deploy/tofu/examples/stackit/`) is flat (no module) — all resources live directly in `main.tf`. DNS resources go into `main.tf` following the same pattern as the existing `stackit_loadbalancer` resource.
- The AWS example currently has NO `outputs.tf` file. The `dns_name` output should be added to `main.tf` directly, or a new `outputs.tf` must be created. Given pattern from Stackit (`outputs.tf` exists), prefer creating `outputs.tf` for consistency.

### File Change Map

| File | Action | Notes |
|---|---|---|
| `deploy/tofu/examples/aws/variables.tf` | UPDATE | Add `dns_mode` variable |
| `deploy/tofu/examples/aws/main.tf` | UPDATE | Add `data.aws_route53_zone` + `aws_route53_record` (count-guarded) |
| `deploy/tofu/examples/aws/outputs.tf` | NEW | Create: `dns_name` output (value = `module.nebu_aws.alb_dns_name`) |
| `deploy/tofu/examples/aws/terraform.tfvars.example` | UPDATE | Add `dns_mode` documentation |
| `deploy/tofu/examples/aws/RUNBOOK.md` | UPDATE | Add "DNS Configuration" section |
| `deploy/tofu/examples/stackit/variables.tf` | UPDATE | Add `dns_mode` + `dex_subdomain_enabled` variables |
| `deploy/tofu/examples/stackit/main.tf` | UPDATE | Add `stackit_dns_zone` + `stackit_dns_record_set.nebu` + `stackit_dns_record_set.dex` (count-guarded) |
| `deploy/tofu/examples/stackit/outputs.tf` | UPDATE | Add `dns_name` output (value = `stackit_public_ip.nebu.ip`) |
| `deploy/tofu/examples/stackit/terraform.tfvars.example` | UPDATE | Add `dns_mode` + `dex_subdomain_enabled` documentation |
| `deploy/tofu/examples/stackit/RUNBOOK.md` | UPDATE | Add "DNS Configuration" section |

### 1. Variables (both examples)

**`dns_mode` variable — add to both `variables.tf` files:**

```hcl
variable "dns_mode" {
  description = "DNS record creation mode. 'default': OpenTofu creates DNS records in the cloud provider's DNS service (Route 53 for AWS, Stackit DNS for Stackit). 'external': No DNS resources are created; the operator registers the ALB hostname/IP in their own DNS server. The 'dns_name' output shows what to register. Default is 'external' to prevent accidental DNS changes on existing deployments."
  type        = string
  default     = "external"

  validation {
    condition     = contains(["default", "external"], var.dns_mode)
    error_message = "dns_mode must be 'default' or 'external'."
  }
}
```

**`dex_subdomain_enabled` — add to Stackit `variables.tf` only (after `dns_mode`):**

```hcl
variable "dex_subdomain_enabled" {
  description = "When true and dns_mode = 'default', creates an additional DNS record for 'dex.<server_name>' pointing to the same floating IP. Intended for future host-based routing where Dex is accessible via a subdomain instead of a port number. Only meaningful when dns_mode = 'default'."
  type        = bool
  default     = false
}
```

Note: `dex_subdomain_enabled` is Stackit-only in this story. AWS ALIAS records for the dex subdomain can be added in a future story if needed.

### 2. AWS — Route 53 Resources

The AWS `main.tf` already imports and uses the `nebu-aws` module. Add Route 53 resources at the bottom of `main.tf`, after the module block. The `aws_route53_record` ALIAS record type requires the hosted zone ID of the ALB, which is already available as `module.nebu_aws.alb_zone_id`.

**Route 53 data source — look up the hosted zone by domain name:**

```hcl
# ── DNS (Route 53) ────────────────────────────────────────────────────────────
# Created only when dns_mode = "default".
# Requires a Route 53 hosted zone for var.domain_name to exist in the AWS account.
# If no hosted zone exists, create one via the AWS console or add aws_route53_zone resource.

data "aws_route53_zone" "nebu" {
  count = var.dns_mode == "default" ? 1 : 0

  name         = var.domain_name
  private_zone = false
}

resource "aws_route53_record" "nebu" {
  count = var.dns_mode == "default" ? 1 : 0

  zone_id = data.aws_route53_zone.nebu[0].zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = module.nebu_aws.alb_dns_name
    zone_id                = module.nebu_aws.alb_zone_id
    evaluate_target_health = true
  }
}
```

**Important:** The `data "aws_route53_zone"` lookup requires the hosted zone to already exist in the AWS account. This is documented in the RUNBOOK. Operators who manage DNS outside Route 53 use `dns_mode = "external"`.

### 3. AWS — Outputs

Create a new file `deploy/tofu/examples/aws/outputs.tf`:

```hcl
# AWS deployment example outputs.

output "dns_name" {
  description = "ALB DNS name to register in your external DNS server when dns_mode = 'external'. Create a CNAME record pointing your domain to this value. When dns_mode = 'default', Route 53 is managing this automatically."
  value       = module.nebu_aws.alb_dns_name
}
```

### 4. Stackit — DNS Resources

The Stackit provider (`stackitcloud/stackit ~> 0.95`, locked at `0.95.0`) includes `stackit_dns_zone` and `stackit_dns_record_set` resources. The `external_address` of the ALB (floating IP) is already available as `stackit_public_ip.nebu.ip`.

**Zone and record resources — add to Stackit `main.tf` after the `stackit_loadbalancer` block:**

```hcl
# ── DNS ──────────────────────────────────────────────────────────────────────
# Created only when dns_mode = "default".
# Requires Stackit DNS service to be enabled in your project.
# The zone is created for the domain extracted from server_name.
# If the zone already exists, import it first: tofu import stackit_dns_zone.nebu <zone_id>

resource "stackit_dns_zone" "nebu" {
  count      = var.dns_mode == "default" ? 1 : 0
  project_id = var.stackit_project_id
  name       = var.server_name
  dns_name   = "${var.server_name}."
  type       = "primary"
  description = "Nebu instance DNS zone — managed by OpenTofu."
}

resource "stackit_dns_record_set" "nebu" {
  count      = var.dns_mode == "default" ? 1 : 0
  project_id = var.stackit_project_id
  zone_id    = stackit_dns_zone.nebu[0].zone_id
  name       = "${var.server_name}."
  type       = "A"
  records    = [stackit_public_ip.nebu.ip]
  ttl        = 300
}

# Optional dex subdomain record (only when dex_subdomain_enabled = true).
# Enables host-based routing to dex.<server_name> (future nginx routing story).
resource "stackit_dns_record_set" "dex" {
  count      = var.dns_mode == "default" && var.dex_subdomain_enabled ? 1 : 0
  project_id = var.stackit_project_id
  zone_id    = stackit_dns_zone.nebu[0].zone_id
  name       = "dex.${var.server_name}."
  type       = "A"
  records    = [stackit_public_ip.nebu.ip]
  ttl        = 300
}
```

**Note on Stackit DNS zone `dns_name` field:** The `dns_name` field requires a trailing dot (FQDN format: `"chat.example.com."`). Same for record `name` fields. Failure to add the trailing dot will cause provider validation errors.

**Note on existing zones:** If the operator already has a DNS zone for the domain, `tofu apply` will fail with a duplicate zone error. Document this in RUNBOOK: either import the existing zone (`tofu import stackit_dns_zone.nebu <zone_id>`) or use `dns_mode = "external"`.

### 5. Stackit — Outputs Update

Add `dns_name` output to `deploy/tofu/examples/stackit/outputs.tf` (append after existing outputs):

```hcl
output "dns_name" {
  description = "Floating IP address to register in your external DNS server when dns_mode = 'external'. Create an A-record pointing your domain to this IP address. When dns_mode = 'default', Stackit DNS is managing this automatically."
  value       = stackit_public_ip.nebu.ip
}
```

### 6. `terraform.tfvars.example` Changes

**AWS — add after `ecs_desired_count`:**

```hcl
# ── DNS ──────────────────────────────────────────────────────────────────────
# dns_mode = "default" creates a Route 53 A-record (ALIAS) pointing domain_name to the ALB.
# Requires a Route 53 hosted zone for domain_name to exist in this AWS account.
# dns_mode = "external" (default) skips DNS creation — register the 'dns_name' output
# value as a CNAME in your DNS provider of choice.
dns_mode = "external"  # or: "default"
```

**Stackit — add after the OIDC profile block (after `nebu_version`):**

```hcl
# ── DNS ──────────────────────────────────────────────────────────────────────
# dns_mode = "default" creates Stackit DNS zone + A-record for server_name → floating IP.
# Requires Stackit DNS service to be enabled in your project.
# If the DNS zone for server_name already exists, import it before apply:
#   tofu import stackit_dns_zone.nebu <zone_id>
# dns_mode = "external" (default) skips DNS creation — register the 'dns_name' output
# (floating IP) as an A-record in your DNS provider.
dns_mode = "external"  # or: "default"

# When dns_mode = "default", creates dex.<server_name> A-record → same floating IP.
# Only useful when oidc_mode = "dex" and planning host-based routing for Dex.
# dex_subdomain_enabled = false
```

### 7. RUNBOOK.md Changes

**Both examples — add a "DNS Configuration" section** before or after the existing sections:

```markdown
## DNS Configuration

Nebu supports two DNS modes, configured via `dns_mode` in `terraform.tfvars`.

### `dns_mode = "external"` (default) — Manual DNS Registration

OpenTofu does not create DNS records. After `tofu apply`, retrieve the ALB endpoint:

```bash
tofu output dns_name
```

Register this value in your DNS provider:
- **AWS:** Create a CNAME record pointing `<domain_name>` → the ALB DNS hostname shown in `dns_name`.
- **Stackit:** Create an A-record pointing `<server_name>` → the floating IP shown in `dns_name`.

### `dns_mode = "default"` — Managed DNS

OpenTofu creates DNS records automatically in the cloud provider's DNS service.

**AWS Route 53:**
- Requires a Route 53 hosted zone for `domain_name` to exist in your AWS account.
- Creates an ALIAS A-record (no CNAME needed — ALIAS records support the zone apex).
- If the zone does not exist: create it manually in the AWS console, then re-run `tofu apply`.

**Stackit DNS:**
- Creates a DNS zone + A-record for `server_name` → floating IP.
- If a zone for `server_name` already exists: import it before apply:
  ```bash
  tofu import stackit_dns_zone.nebu <zone_id>
  ```
- `dex_subdomain_enabled = true` additionally creates `dex.<server_name>` → floating IP (useful for future host-based Dex routing).
```

### 8. Pattern Consistency Requirements

Follow these patterns established in existing IaC files:

- Section separator comments: `# ── DNS ────────────────────────────────────────────────────────────────────`
- Variable descriptions: full sentences, trailing period
- `count` pattern for conditional resources: `count = var.dns_mode == "default" ? 1 : 0` (not `for_each`)
- Resource names: `"nebu"` (consistent with `stackit_loadbalancer.nebu`, `aws_lb.this`, etc.)
- Always use `tofu fmt` before committing — `make test-iac-validate` runs `tofu fmt -check`
- No `sensitive = true` on `dns_mode` or `dex_subdomain_enabled` (not secrets)

### 9. What Must NOT Break

| Invariant | Verification |
|---|---|
| Existing resources in both examples unchanged | No modifications to `network.tf`, `alb.tf`, `compute.tf`, `database.tf`, or existing Stackit resources |
| `module.nebu_aws.alb_dns_name` and `alb_zone_id` are already exported | Confirmed in `modules/nebu-aws/outputs.tf` lines 48–56 — no module changes needed |
| Stackit `floating_ip` output unchanged | Only `dns_name` output is added; `floating_ip` output in `outputs.tf` is kept as-is |
| `make test-iac-validate` still exits 0 | Run after every change: `tofu fmt -check + validate` for all three examples |
| Default `dns_mode = "external"` is safe on existing deployments | The `count = 0` guard means no DNS resources are planned when using the default |

### 10. Provider Schema Notes

**AWS provider (`hashicorp/aws ~> 5.0`, locked at `5.100.0`):**
- `aws_route53_zone` data source: `name` (string, required), `private_zone` (bool)
- `aws_route53_record`: `zone_id`, `name`, `type = "A"`, `alias {}` block with `name`, `zone_id`, `evaluate_target_health`
- ALIAS records (`alias {}` block) do NOT require `ttl` — it is set by AWS automatically for ALIAS records

**Stackit provider (`stackitcloud/stackit ~> 0.95`, locked at `0.95.0`):**
- `stackit_dns_zone`: `project_id`, `name` (display name), `dns_name` (FQDN with trailing dot), `type = "primary"`, optional `description`
- `stackit_dns_record_set`: `project_id`, `zone_id`, `name` (FQDN with trailing dot), `type = "A"`, `records = [string]`, `ttl` (int, seconds)

**Important trailing dot:** Stackit DNS requires FQDN format with trailing dot for `dns_name` on zones and `name` on record sets. Example: `"chat.example.com."` not `"chat.example.com"`. Omitting the trailing dot causes provider API validation errors.

### 11. `tofu validate` Workflow

Since no local OpenTofu installation is assumed (per CLAUDE.md), use Docker:

```bash
# AWS example
docker run --rm \
  -v $(pwd)/deploy/tofu/examples/aws:/workspace \
  -w /workspace \
  ghcr.io/opentofu/opentofu:1.9 \
  sh -c "tofu init -backend=false && tofu validate"

# Stackit example
docker run --rm \
  -v $(pwd)/deploy/tofu/examples/stackit:/workspace \
  -w /workspace \
  ghcr.io/opentofu/opentofu:1.9 \
  sh -c "tofu init -backend=false && tofu validate"
```

Or use the Makefile target which runs both (and k8s + helm lint):

```bash
make test-iac-validate
```

### 12. Previous Story Learnings (13-9, 13-3a)

From story 13-9 (OIDC mode variable — same pattern as this story):
- **Validation blocks are required** — `contains(["value1", "value2"], var.varname)` pattern
- **`count` pattern for conditional resources** is correct (not `for_each`) — consistent with `inbound_dex` SG rule in `main.tf`
- **Cross-variable preconditions** using `lifecycle { precondition {} }` were needed in 13-9 for `oidc_mode`/`oidc_issuer` interaction. Consider whether `dns_mode = "default"` without a valid hosted zone deserves a similar precondition — but since the zone lookup is a data source (fails at plan time anyway), a precondition is NOT needed here
- **`tofu fmt` must pass** — `make test-iac-validate` runs `tofu fmt -check`, any non-canonical formatting fails the gate

From story 13-3a (Stackit provider learnings):
- `stackit_key_pair` is a global resource (no `project_id`) — but DNS resources DO use `project_id`
- `enable_beta_resources = true` is already set in the provider block for ALB — not needed for DNS resources
- MAJOR finding was caught where `project_id` was incorrectly added to a global resource — DNS resources are project-scoped, so `project_id` IS required

From story 13-2a (AWS networking):
- AWS security group rules use `count` for conditional creation — same pattern applies here for Route 53 records
- `tofu validate` passes even with empty string defaults for things like `acm_certificate_arn` — same applies to `dns_mode = "external"` default

---

## Tasks / Subtasks

- [x] Add `dns_mode` variable to `deploy/tofu/examples/aws/variables.tf` (AC1)
  - [x] `type = string`, `default = "external"`, validation block with `contains(["default", "external"], var.dns_mode)`
- [x] Add Route 53 resources to `deploy/tofu/examples/aws/main.tf` (AC3, AC4)
  - [x] `data "aws_route53_zone" "nebu"` with `count = var.dns_mode == "default" ? 1 : 0`
  - [x] `resource "aws_route53_record" "nebu"` ALIAS A-record with `count` guard
  - [x] ALIAS block uses `module.nebu_aws.alb_dns_name` + `module.nebu_aws.alb_zone_id`
- [x] Create `deploy/tofu/examples/aws/outputs.tf` with `dns_name` output (AC7)
- [x] Update `deploy/tofu/examples/aws/terraform.tfvars.example` with `dns_mode` documentation (AC8)
- [x] Update `deploy/tofu/examples/aws/RUNBOOK.md` with "DNS Configuration" section (AC10)
- [x] Add `dns_mode` variable to `deploy/tofu/examples/stackit/variables.tf` (AC1)
  - [x] Same structure as AWS variable
- [x] Add `dex_subdomain_enabled` variable to `deploy/tofu/examples/stackit/variables.tf` (AC2)
  - [x] `type = bool`, `default = false`
- [x] Add DNS resources to `deploy/tofu/examples/stackit/main.tf` (AC5, AC6)
  - [x] `resource "stackit_dns_zone" "nebu"` with `count` guard
  - [x] `resource "stackit_dns_record_set" "nebu"` with `count` guard
  - [x] `resource "stackit_dns_record_set" "dex"` with combined `count` guard
  - [x] All `dns_name`/`name` fields use trailing dot FQDN format
- [x] Add `dns_name` output to `deploy/tofu/examples/stackit/outputs.tf` (AC7)
- [x] Update `deploy/tofu/examples/stackit/terraform.tfvars.example` (AC8)
  - [x] `dns_mode` documentation
  - [x] `dex_subdomain_enabled` commented example
- [x] Update `deploy/tofu/examples/stackit/RUNBOOK.md` with "DNS Configuration" section (AC10)
- [x] Run `make test-iac-validate` — verify exit code 0 for all examples (AC9)

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

- Added `dns_mode` variable (type=string, default="external", validation block) to both AWS and Stackit `variables.tf`.
- Added `dex_subdomain_enabled` variable (type=bool, default=false) to Stackit `variables.tf` only.
- Added Route 53 data source + ALIAS A-record to AWS `main.tf`, both guarded by `count = var.dns_mode == "default" ? 1 : 0`.
- Created new `deploy/tofu/examples/aws/outputs.tf` with `dns_name` output (value = `module.nebu_aws.alb_dns_name`).
- Added `stackit_dns_zone.nebu`, `stackit_dns_record_set.nebu`, and `stackit_dns_record_set.dex` to Stackit `main.tf`, all count-guarded. Trailing dot FQDN format used for all `dns_name`/`name` fields per Stackit provider requirements.
- Added `dns_name` output to Stackit `outputs.tf` (value = `stackit_public_ip.nebu.ip`).
- Updated both `terraform.tfvars.example` files with DNS section documentation.
- Added "DNS Configuration" section to both `RUNBOOK.md` files describing external and default modes.
- `make test-iac-validate` passed with exit code 0: tofu fmt -check, tofu validate (aws + stackit + k8s), helm lint, k6 inspect all green.
- Review cycle 1 fixes applied (all 7 MINOR items):
  - M1/M6: AWS `main.tf` Route53 data source comment updated — explicit exact zone-name requirement, removed misleading `aws_route53_zone` resource suggestion.
  - M1: AWS RUNBOOK `dns_mode = "default"` section clarified exact zone name match requirement.
  - M2: Stackit `main.tf` — removed trailing dot from `stackit_dns_zone.nebu.dns_name` (zone field does not use trailing dot per provider docs).
  - M3: Added inline comment on `stackit_dns_record_set.nebu` `name` field explaining trailing dot requirement for record sets.
  - M4: Added `dns_contact_email` variable to Stackit `variables.tf` + `contact_email` attribute on `stackit_dns_zone.nebu` + entry in `terraform.tfvars.example`.
  - M5: AWS RUNBOOK `external` section — added CNAME apex limitation note; AWS `terraform.tfvars.example` DNS comment updated accordingly.
  - M7: Added `validation` block on `dex_subdomain_enabled` enforcing `dns_mode = "default"` requirement.
- `make test-iac-validate` re-run after review fixes — exit code 0.
- Review cycle 2 fixes applied (3 MINOR items):
  - M8: Stackit `main.tf` — `contact_email` now uses `var.dns_contact_email != "" ? var.dns_contact_email : null` so the field is omitted (null) when empty, matching the variable description.
  - M9: AWS `outputs.tf` `dns_name` description updated — added apex CNAME caveat: "Note: CNAME is not supported at the zone apex — use an ALIAS/ANAME record for apex domains."
  - M10: AC4 and Acceptance Test #3 reworded — gate is now "count = 0 guards present in main.tf (file inspection) + tofu validate passes", instead of the unverifiable "no Route 53 API calls are planned".

### File List

- `deploy/tofu/examples/aws/variables.tf`
- `deploy/tofu/examples/aws/main.tf`
- `deploy/tofu/examples/aws/outputs.tf` (NEW)
- `deploy/tofu/examples/aws/terraform.tfvars.example`
- `deploy/tofu/examples/aws/RUNBOOK.md`
- `deploy/tofu/examples/stackit/variables.tf`
- `deploy/tofu/examples/stackit/main.tf`
- `deploy/tofu/examples/stackit/outputs.tf`
- `deploy/tofu/examples/stackit/terraform.tfvars.example`
- `deploy/tofu/examples/stackit/RUNBOOK.md`

## Change Log

| Date | Change |
|---|---|
| 2026-05-14 | Story created |
| 2026-05-14 | Implementation complete: dns_mode variable + Route 53 resources (AWS), dns_mode + dex_subdomain_enabled + Stackit DNS resources (Stackit), outputs, tfvars.example docs, RUNBOOK sections. make test-iac-validate exit 0. |
| 2026-05-14 | Review cycle 1: applied all 7 MINOR fixes (M1–M7): AWS Route53 exact-zone comment, RUNBOOK zone-name requirement, Stackit dns_name trailing dot removed, record-set comment added, dns_contact_email variable + attr, CNAME apex warning, dex_subdomain_enabled validation. make test-iac-validate exit 0. |
| 2026-05-14 | Review cycle 2: applied 3 MINOR fixes (M8–M10): Stackit contact_email null-coalesce, AWS outputs.tf apex CNAME caveat, AC4 + Acceptance Test #3 rewording for verifiable gate. make test-iac-validate exit 0. |
