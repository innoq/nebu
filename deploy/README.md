# Nebu — Deployment Guide

This directory contains the Infrastructure-as-Code (IaC) templates for deploying Nebu to production.
Three deployment targets are supported: **AWS (ECS Fargate)**, **STACKIT (VMs + Docker Compose)**, and **Kubernetes / Helm**.

See [ADR-014](../docs/architecture/adr/ADR-014-deployment-strategy-iac.md) for the full decision rationale.

---

## Directory Structure

```
deploy/
  tofu/
    modules/
      nebu-core/       # Shared variables, validations, outputs (no provider resources)
      nebu-aws/        # AWS: ECS Fargate + RDS + S3 + ACM (Story 13-2)
      nebu-stackit/    # STACKIT: VMs + Docker Compose + ALB + DBaaS (Story 13-3)
      nebu-k8s/        # Kubernetes: Helm Release wrapper (Story 13-4)
    examples/
      aws/             # AWS quick-start example
      stackit/         # STACKIT quick-start example
      k8s/             # Kubernetes quick-start example
  helm/
    nebu/              # Standalone Helm Chart (usable without OpenTofu)
```

---

## Prerequisites

| Tool | Version | Required for |
|------|---------|-------------|
| [OpenTofu](https://opentofu.org/) | >= 1.6.0 | All OpenTofu deployments |
| [Helm](https://helm.sh/) | >= 3.0 | Kubernetes deployment |
| [AWS CLI](https://aws.amazon.com/cli/) | >= 2.0 | AWS deployment |
| [STACKIT CLI](https://github.com/stackitcloud/stackit-cli) | latest | STACKIT deployment |
| `kubectl` | >= 1.28 | Kubernetes deployment |

---

## Backend Configuration

OpenTofu stores state remotely. Configure the appropriate backend before running `tofu init`.

### AWS (S3 + DynamoDB)

Create a `backend.hcl` in the example directory:

```hcl
bucket         = "my-nebu-tofu-state"
key            = "nebu/aws/terraform.tfstate"
region         = "eu-central-1"
dynamodb_table = "nebu-tofu-locks"
encrypt        = true
```

Then initialise:

```bash
tofu init -backend-config=backend.hcl
```

### STACKIT (Object Storage)

STACKIT Object Storage is S3-compatible. Use the S3 backend with a STACKIT endpoint:

```hcl
bucket                      = "my-nebu-tofu-state"
key                         = "nebu/stackit/terraform.tfstate"
endpoint                    = "https://object.storage.eu01.onstackit.cloud"
region                      = "eu01"
skip_credentials_validation = true
skip_region_validation      = true
skip_metadata_api_check     = true
```

### Local (Development only)

```hcl
# No configuration needed — uses terraform.tfstate in the working directory.
```

Use the local backend only for development. Never commit `terraform.tfstate`.

---

## Per-Platform Quick Start

### AWS

```bash
cd deploy/tofu/examples/aws
cp terraform.tfvars.example terraform.tfvars && $EDITOR terraform.tfvars && cp backend.hcl.example backend.hcl && $EDITOR backend.hcl
tofu init -backend-config=backend.hcl && tofu plan && tofu apply
```

### STACKIT

```bash
cd deploy/tofu/examples/stackit
cp terraform.tfvars.example terraform.tfvars && $EDITOR terraform.tfvars && cp backend.hcl.example backend.hcl && $EDITOR backend.hcl
tofu init -backend-config=backend.hcl && tofu plan && tofu apply
```

### Kubernetes / Helm

```bash
cd deploy/tofu/examples/k8s
cp terraform.tfvars.example terraform.tfvars && $EDITOR terraform.tfvars && cp values.yaml.example values.yaml && $EDITOR values.yaml
tofu init && tofu plan && tofu apply
```

---

## Secrets Management

Secrets (DB passwords, OIDC client secrets, TLS private keys) are **never stored in the OpenTofu state file in plaintext**.

| Platform | Recommended approach |
|----------|---------------------|
| AWS | AWS Secrets Manager + ECS task execution role |
| STACKIT | Environment variables injected via cloud-init; STACKIT Key Management planned (ADR-016) |
| Kubernetes | Kubernetes Secrets + External Secrets Operator (ADR-016) |

See [ADR-014 Consequences](../docs/architecture/adr/ADR-014-deployment-strategy-iac.md#consequences) and the forthcoming ADR-016 (Secrets Management) for the full strategy.

---

## Local IaC Validation

Run `tofu fmt -check` and `tofu validate` for all example directories without cloud credentials:

```bash
make test-iac-validate
```

This is equivalent to the `validate-iac` CI job.
