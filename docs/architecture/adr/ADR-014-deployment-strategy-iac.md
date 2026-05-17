# ADR-014: Deployment Strategy and Infrastructure-as-Code

## Status

Accepted

## Context

Nebu is positioned as a sovereign, self-hosted enterprise chat server. A core promise is digital sovereignty: operators choose their own infrastructure — whether public cloud, European cloud provider, or on-premises data center.

To make this promise deliverable, Nebu requires official deployment templates covering at least three operational scenarios:

1. **AWS** — the most widely used public cloud; many enterprises already run workloads there and want to integrate Nebu seamlessly.
2. **Stackit** — German GDPR-native cloud (STACKIT, Schwarz Group); the primary option for operators with elevated data protection requirements or public administration contexts.
3. **Kubernetes / Helm** — a provider-agnostic path for on-premises operation (K3s, RKE2, OpenShift) or any managed Kubernetes offering.

### Constraints

- The IaC framework must be **license-clean** — no BSL, no proprietary lock-in. Open Source, ideally under Apache 2.0 or MPL-2.0.
- Deployment templates must be maintainable by ops teams without deep framework knowledge.
- **State must not require a commercial cloud platform** — no Terraform Cloud / Pulumi Cloud as a mandatory component.
- All three variants must provision the same **core components**:
  - Nebu API Gateway (Go), Media Gateway (Go), Erlang Core
  - PostgreSQL (managed or self-hosted)
  - S3-compatible object storage for media
  - TLS termination
  - Networking (VPC / subnets / security groups or equivalent)

## Decision

### IaC Framework: OpenTofu

**OpenTofu** (MPL-2.0, Linux Foundation) is selected as the primary IaC framework.

OpenTofu is the community fork of Terraform, created in 2023 as a direct response to HashiCorp's relicensing to BSL 1.1. This shared origin aligns with Nebu's own positioning: a sovereign, license-unencumbered alternative to a solution that turned proprietary.

Practical advantages:
- Full compatibility with the Terraform provider ecosystem (AWS, Stackit, Helm, ACME, PostgreSQL, and many more)
- HCL is declarative; lower onboarding barrier for ops teams compared to full programming languages (the key disadvantage of Pulumi)
- State backend is freely selectable: S3-compatible (MinIO, Stackit Object Storage), PostgreSQL, or local filesystem — no cloud mandate
- Active community momentum; all Terraform tutorials apply directly

**Pulumi** (Apache 2.0) is **not selected** despite its clean license because:
- Commercial gravitational pull (Pulumi Cloud) contradicts Nebu's sovereignty promise
- Using programming languages as the configuration layer increases maintenance burden for ops teams
- Pulumi offers no meaningful advantage over OpenTofu for this use case

### Deployment Targets

#### AWS — ECS Fargate

Container-native deployment without cluster management. Fargate owns the compute layer; the IaC module provisions:

- VPC with 2× public subnets + 2× private subnets, IGW, NAT Gateway
- Security Groups (ALB → ECS Tasks, ECS Tasks → RDS, ECS Tasks → S3)
- ECS Cluster + Task Definitions for API GW, Media GW, Erlang Core
- Application Load Balancer with HTTPS listener
- ACM Certificate (DNS validation via Route53)
- RDS PostgreSQL (or Aurora Serverless v2) in private subnets
- S3 bucket for media with IAM Role + Policy for ECS task execution

#### Stackit — VMs with Docker Compose + Application Load Balancer

Stackit does not offer a native serverless container service comparable to ECS Fargate. Deployment runs on VMs with Docker Compose, which matches the familiar operational model for many Stackit target customers (mid-size enterprises, public administration).

Stackit provides a native **Application Load Balancer** (Layer 7) that is directly configurable via the OpenTofu provider. It handles SSL/TLS termination, content-based routing (hostname/path), and automated health checks — functionally equivalent to the AWS ALB, replacing the need for a Traefik container in the Compose stack as a reverse proxy.

The IaC module provisions:

- Stackit VPC + subnets + security groups
- 1–3 VMs via the `stackit` provider (official OpenTofu/Terraform provider, maintained by Stackit)
- `cloud-init` script: Docker + Docker Compose, systemd unit for `nebu.service`, pulling `docker-compose.yml` and `.env` from configured sources
- **Stackit Application Load Balancer** with HTTPS listener, SSL/TLS termination, and path-based routing (`/` → API GW, `/_matrix/media` → Media GW)
- Health checks at ALB level against API GW and Media GW
- Stackit PostgreSQL Flex DBaaS (managed) for production
- Stackit Object Storage (S3-compatible) for media
- Floating IP + Stackit DNS record
- TLS certificate on the ALB (Let's Encrypt via `acme` provider or bring-your-own certificate)

#### Kubernetes / Helm

Provider-agnostic deployment path for on-premises Kubernetes clusters (K3s, RKE2, OpenShift) or any managed Kubernetes offering (EKS, AKS, GKE, SKE).

The IaC module (`nebu-k8s`) is a thin OpenTofu wrapper around a `helm_release` resource block. The actual deployment artifact is a **Helm Chart** (`deploy/helm/nebu/`) that is usable independently of OpenTofu.

The module / chart provisions:

- Helm release for Nebu (API GW, Media GW, Erlang Core as separate Deployments)
- Optional: cert-manager (ClusterIssuer Let's Encrypt or custom CA)
- Optional: ingress-nginx as Ingress Controller
- ConfigMap + Secret references for database connection, OIDC credentials, object storage keys
- Horizontal Pod Autoscaler for API GW and Media GW

PostgreSQL and object storage are **not** provisioned by the K8s module — they are cluster-external dependencies the operator supplies (managed DBaaS or a separate Helm chart such as CloudNativePG).

## Module Structure

```
deploy/
  tofu/
    modules/
      nebu-core/          # Shared variables, validations, locals
      nebu-aws/           # ECS Fargate + RDS + S3 + ACM
      nebu-stackit/       # VMs + Docker Compose + DBaaS + Object Storage
      nebu-k8s/           # Helm Release wrapper
    examples/
      aws/
        main.tf
        terraform.tfvars.example
        README.md
      stackit/
        main.tf
        terraform.tfvars.example
        README.md
      k8s/
        main.tf
        values.yaml.example
        README.md
  helm/
    nebu/
      Chart.yaml
      values.yaml
      templates/
        api-gateway/
        media-gateway/
        erlang-core/
        _helpers.tpl
```

The `nebu-core` module contains no provider resources — only:
- Shared input variables (`nebu_version`, `domain_name`, `admin_email`, `postgres_db_name`, `image_registry`)
- Validation rules (e.g. semantic version format)
- Outputs consumed by all other modules

## Affected Components

| Component           | AWS           | Stackit                  | K8s / Helm            |
|---------------------|---------------|--------------------------|-----------------------|
| API Gateway (Go)    | ECS Fargate   | Docker Compose           | Kubernetes Deployment |
| Media Gateway (Go)  | ECS Fargate   | Docker Compose           | Kubernetes Deployment |
| Erlang Core         | ECS Fargate   | Docker Compose           | Kubernetes Deployment |
| PostgreSQL          | RDS           | DBaaS or VM              | External (expected)   |
| Object Storage      | S3            | Stackit Object Storage   | External (expected)   |
| TLS                 | ACM + ALB     | Stackit ALB + acme       | cert-manager          |
| Networking          | VPC + ALB     | VPC + Stackit ALB        | Ingress Controller    |
| State Backend       | S3 + DynamoDB | Stackit OS + table       | S3-compatible         |

## Rejected Alternatives

### Terraform (HashiCorp)

Relicensed to BSL 1.1 in August 2023. Proprietary restrictions on competitive use. Contradicts Nebu's sovereignty principle. Not eligible.

### Pulumi

Technically solid, Apache 2.0 core. However:
- Pulumi Cloud is prominent in product marketing as the primary state backend
- Programming languages as the configuration layer increase maintenance burden
- No meaningful narrative fit with Nebu's open-source positioning

### Ansible

Suitable for VM configuration (Stackit scenario), but not a complete IaC framework for cloud resources. Could be used supplementarily for cloud-init / VM bootstrap, not as a replacement for OpenTofu.

### Helm-only (without OpenTofu)

A standalone Helm Chart would be sufficient for the K8s scenario. The OpenTofu wrapper, however, gives operators who already manage other resources with OpenTofu a seamless integration path. The Helm Chart remains standalone-usable.

## Consequences

### Positive

- Clear deployment options cover the three primary target groups
- OpenTofu MPL-2.0 is compatible with Nebu's Apache 2.0 license without conflict
- State backend sovereignty: operators can use MinIO or Stackit Object Storage as an OpenTofu backend — no cloud provider mandate
- Stackit as a first-class option reinforces the digital sovereignty narrative and opens the market for GDPR-sensitive operators in the DACH region
- The Helm Chart is usable independently of OpenTofu — important for GitOps workflows (ArgoCD, Flux)

### Negative / Risks

- Three separate deployment variants increase initial documentation effort
- The Stackit provider is younger than the AWS provider; ALB support in the OpenTofu provider should be validated for completeness before production use (particularly HTTPS listener configuration and health check options)
- VM-based Stackit deployment is less resilient than ECS Fargate on hardware failure — multiple VMs behind the Stackit ALB are required for HA
- The Helm Chart and OpenTofu modules must be kept in sync when the Nebu architecture changes

### Open Questions (follow-up ADRs)

- **ADR-015**: OpenTofu state backend recommendation per deployment target (S3 + DynamoDB for AWS, Stackit OS + PostgreSQL for Stackit, Helm-native for K8s)
- **ADR-016**: Secrets management across deployment variants (AWS Secrets Manager, Stackit Key Management, External Secrets Operator for K8s)
- Open question: Should an official Nebu Helm Chart repository (OCI registry or GitHub Pages) be provided, or only deployment via the Git repository?

## Testing Strategy

IaC modules cannot be fully tested automatically against real cloud providers in every CI pipeline run — costs are too high, runtimes too long. The testing strategy is therefore structured in three tiers that differ in effort, speed, and cost.

**Priority:** AWS and Stackit are the primary test targets. Kubernetes/Helm is treated as supplementary and can be validated locally.

### Tier 1 — Static Validation (always, free)

Runs in CI on every push and pull request. No cloud access required; runtime < 1 minute.

| Tool     | Command                      | Checks                                              |
|----------|------------------------------|-----------------------------------------------------|
| OpenTofu | `tofu validate`              | HCL syntax, types, module references                |
| OpenTofu | `tofu fmt -check`            | Formatting consistency                              |
| tflint   | `tflint --recursive`         | Best practices, deprecated provider arguments       |
| trivy    | `trivy config .`             | Open security groups, unencrypted buckets, IAM wildcards |
| helm     | `helm lint deploy/helm/nebu` | Chart syntax, values.yaml completeness              |
| helm     | `helm template deploy/helm/nebu` | Template rendering, nil references             |

Tier 1 is the minimum requirement and must pass green before every merge.

### Tier 2 — Cloud Integration Tests (planned, cost-incurring)

Tests against real AWS and Stackit environments — ephemeral: resources are destroyed immediately after the test. Framework: **[Terratest](https://github.com/gruntwork-io/terratest)** (Go, Apache 2.0).

Terratest executes `tofu apply`, checks outputs and reachability, and calls `tofu destroy` in a `defer` block — even on test failure.

**Execution:** Nightly CI job or manual pre-release run. Not on every PR — runtime ~10–15 minutes, cost ~$0.50–1.00 per run.

#### AWS Test Scope

```go
func TestNebuAWS(t *testing.T) {
    opts := &terraform.Options{
        TerraformDir: "../../examples/aws",
        Vars: map[string]interface{}{
            "nebu_version": "dev",
            "domain_name":  "test.nebu.example.com",
        },
    }
    defer terraform.Destroy(t, opts)
    terraform.InitAndApply(t, opts)

    albDns := terraform.Output(t, opts, "alb_dns_name")
    http_helper.HttpGetWithRetry(t,
        "https://"+albDns+"/health", nil, 200, "", 30, 10*time.Second)
}
```

Verified: VPC and subnets created correctly, ECS service running (desired == running), ALB responds on `/health`, RDS endpoint present in outputs, S3 bucket exists with correct tags.

#### Stackit Test Scope

Verified: VM provisioned and reachable via SSH, Docker Compose service running (`docker ps` via SSH exec), Stackit ALB responds on `/health`, DBaaS PostgreSQL endpoint present in outputs, object storage bucket exists.

### Tier 3 — Smoke Test (Nebu actually runs)

Requires a working Nebu container image. Runs directly after a successful Tier 2 apply as a final verification.

Verifies minimal Matrix functionality:

```bash
# 1. Matrix version endpoint reachable
curl -sf https://$NEBU_HOST/_matrix/client/versions | jq .versions

# 2. OIDC login → access token
# 3. Create room → Room ID
# 4. Send message → Event ID
```

For early IaC phases where no Nebu image exists yet, an Nginx stub is deployed that responds to `/health` with HTTP 200. Tier 3 is activated once a Nebu dev image is available in the container registry.

### Kubernetes / Helm (supplementary, local)

As K8s/Helm is a lower-priority deployment target, local validation is sufficient initially:

```bash
# Rendering without a cluster
helm template deploy/helm/nebu | kubectl apply --dry-run=client -f -

# Lint in CI via chart-testing
ct lint --chart-dirs deploy/helm
```

A full integration test via `kind` (Kubernetes in Docker) in CI is optional and deferred until the chart reaches beta status.

### Summary

| Tier                    | Scope               | When                    | Cost              |
|-------------------------|---------------------|-------------------------|-------------------|
| 1 — Static              | All modules + chart | Every push / PR         | Free              |
| 2 — Integration AWS     | `nebu-aws`          | Nightly / pre-release   | ~$0.50–1 per run  |
| 2 — Integration Stackit | `nebu-stackit`      | Nightly / pre-release   | Low               |
| 3 — Smoke Test          | AWS + Stackit       | After Tier 2 apply      | Included in apply |
| K8s local               | `nebu-k8s` + chart  | Manual / pre-release    | Free              |

_Decision date: 2026-05-13_
