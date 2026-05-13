# Nebu — AWS (ECS Fargate) Deployment Example

Deploys Nebu to AWS using ECS Fargate, RDS PostgreSQL, S3, ALB, and ACM.
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
- AWS CLI configured (`aws configure`)
- S3 bucket and DynamoDB table for remote state (see `deploy/README.md`)
