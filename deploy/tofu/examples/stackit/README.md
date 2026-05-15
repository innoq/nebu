# Nebu — STACKIT Deployment Example

Deploys Nebu to STACKIT using VMs, Docker Compose, Stackit Application Load Balancer, and DBaaS PostgreSQL.
See [ADR-014](../../docs/architecture/adr/ADR-014-deployment-strategy-iac.md) for the full architecture.

## Quick Start

```bash
# 1. Configure backend and variables
cp terraform.tfvars.example terraform.tfvars
$EDITOR terraform.tfvars

# 2. Initialise OpenTofu
tofu init -backend-config=backend.hcl

# 3. Review the plan
tofu plan

# 4. Apply
tofu apply
```

## Prerequisites

- [OpenTofu](https://opentofu.org/) >= 1.6.0
- STACKIT CLI configured (`stackit auth login`)
- STACKIT Object Storage bucket for remote state (see `deploy/README.md`)
