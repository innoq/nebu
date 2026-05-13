# Nebu — Kubernetes / Helm Deployment Example

Deploys Nebu to any Kubernetes cluster via a Helm Release managed by OpenTofu.
See [ADR-014](../../docs/architecture/adr/ADR-014-deployment-strategy-iac.md) for the full architecture.

## Quick Start

```bash
# 1. Configure variables and Helm values
cp terraform.tfvars.example terraform.tfvars
cp values.yaml.example values.yaml
$EDITOR terraform.tfvars values.yaml

# 2. Initialise OpenTofu
tofu init

# 3. Review the plan
tofu plan

# 4. Apply
tofu apply
```

## Prerequisites

- [OpenTofu](https://opentofu.org/) >= 1.6.0
- [Helm](https://helm.sh/) >= 3.0
- `kubectl` configured and connected to the target cluster
- External PostgreSQL instance (Managed DBaaS or CloudNativePG)
