---
status: ready-for-dev
epic: 13
story: 2c
security_review: required
matrix: false
ui: false
---

# Story 13.2c: AWS ECS Task Definitions (gateway+core) + ALB + Secrets Manager + Runbook

Status: ready-for-dev

## Story

As a system operator,
I want complete ECS Task Definitions, an Application Load Balancer, and AWS Secrets Manager secrets provisioned via OpenTofu,
So that Nebu is fully deployable on AWS and reachable via HTTPS with secrets managed securely.

**Size:** S

---

## Acceptance Criteria

**AC1 — ECS Task Definitions reference Secrets Manager:**
Given the ECS Task Definitions for gateway and core,
When they are inspected,
Then all `NEBU_*` environment variables are sourced from AWS Secrets Manager (not hardcoded), CPU/Memory are correctly configured, and the health check points to `/_matrix/client/v3/versions` on port 8008

**AC2 — ALB configured with HTTPS redirect:**
Given the ALB configuration,
When it is inspected,
Then an HTTPS listener on port 443 forwards to the gateway Target Group on port 8008, and an HTTP listener on port 80 redirects to HTTPS

**AC3 — Secrets Manager resources provisioned:**
Given the AWS Secrets Manager secrets,
When they are inspected,
Then secrets for DB password, internal secret, and OIDC client secret are provisioned as `aws_secretsmanager_secret` resources and referenced by the ECS task definitions (never hardcoded in .tf files or tfvars)

**AC4 — tofu validate passes:**
Given `deploy/tofu/examples/aws/`,
When `tofu validate` runs,
Then exit code 0

**AC5 — Smoke test command documented:**
Given a deployed AWS stack,
When `curl https://<alb-dns>/_matrix/client/v3/versions` is called (documented in RUNBOOK.md),
Then the RUNBOOK documents the expected response: a valid Matrix versions JSON

**AC6 — RUNBOOK.md covers day-2 operations:**
Given `deploy/tofu/examples/aws/RUNBOOK.md`,
When it is read,
Then it covers: initial deploy (3 commands), rolling update (ECS force-new-deployment), secret rotation procedure, and teardown (`tofu destroy`)

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Infrastructure story — ATDD skipped (OpenTofu static validation + security review gate).

**1. `make test-iac-validate` — Static validation**
- Given: incomplete AWS task definitions and ALB config
- When: `make test-iac-validate` runs
- Then: exit non-zero
- [Passes after implementation]

**2. Security gate — no hardcoded secrets**
- Given: any `.tf` or `.tfvars.example` files in `deploy/tofu/examples/aws/`
- When: `trivy config deploy/tofu/examples/aws/` runs
- Then: no HIGH/CRITICAL findings for hardcoded secrets or open security groups

Note: This story has `security_review: required` because it provisions AWS Secrets Manager and IAM roles with ECS task execution permissions.
