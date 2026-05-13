---
status: done
epic: 13
story: 2a
security_review: not-needed
matrix: false
ui: false
---

# Story 13.2a: AWS Networking Module (VPC, Subnets, Security Groups)

Status: done

## Story

As a system operator,
I want an OpenTofu networking module under `deploy/tofu/modules/nebu-aws/` that provisions the AWS network foundation,
So that all subsequent AWS resources (RDS, ECS, ALB) can be built on a validated, reusable network layer.

**Size:** S

---

## Acceptance Criteria

**AC1 — tofu plan shows expected AWS resources:**
Given `deploy/tofu/modules/nebu-aws/network.tf` exists,
When `tofu plan` runs inside `deploy/tofu/examples/aws/`,
Then the plan shows VPC, 2 public subnets, 2 private subnets, NAT Gateway, and security groups for ALB, ECS, and RDS — no errors

**AC2 — Module outputs exposed:**
Given `deploy/tofu/modules/nebu-aws/outputs.tf`,
When `tofu output` runs after apply,
Then VPC ID and all Subnet IDs are printed as module outputs

**AC3 — tofu validate and fmt pass:**
Given `deploy/tofu/examples/aws/`,
When `tofu validate` and `tofu fmt -check -recursive deploy/tofu/` run,
Then both exit with code 0

**AC4 — Security groups configured correctly:**
Given the security group definitions,
When they are inspected,
Then: ALB SG allows inbound 443/80 from 0.0.0.0/0; ECS SG allows inbound from ALB SG only; RDS SG allows inbound port 5432 from ECS SG only

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Infrastructure story — ATDD skipped (OpenTofu static validation).

**1. `make test-iac-validate` — Static validation (from story 13.1)**
- Given: `deploy/tofu/modules/nebu-aws/network.tf` does NOT exist
- When: `make test-iac-validate` runs
- Then: exit non-zero (tofu validate fails on missing module reference)
- [Passes after implementation]

**2. `tofu fmt -check` — Formatting gate**
- Given: any .tf files written
- When: `tofu fmt -check -recursive deploy/tofu/` runs
- Then: exit 0 (properly formatted)
