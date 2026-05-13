---
status: done
epic: 13
story: 3a
security_review: not-needed
matrix: false
ui: false
---

# Story 13.3a: Stackit VM Provisioning + Networking (OpenTofu)

Status: ready-for-dev

## Story

As a system operator,
I want an OpenTofu configuration under `deploy/tofu/examples/stackit/` that provisions a Stackit VM with Floating IP and security groups,
So that the compute and network infrastructure for EU-hosted deployment is ready for Docker Compose bootstrap.

**Size:** S

---

## Acceptance Criteria

**AC1 — tofu plan shows VM + Floating IP + Security Group:**
Given `deploy/tofu/examples/stackit/main.tf` is configured with a Stackit API token variable,
When `tofu plan` runs,
Then the plan shows a VM (Ubuntu 24.04 LTS, 4 vCPU / 8 GB), a Floating IP, and a Security Group with ports 443 and 22 open inbound — no errors

**AC2 — Module outputs exposed:**
Given `deploy/tofu/examples/stackit/outputs.tf`,
When `tofu output` runs after apply,
Then `floating_ip` and `vm_id` are printed

**AC3 — tofu validate and fmt pass:**
Given `deploy/tofu/examples/stackit/`,
When `tofu validate` and `tofu fmt -check -recursive deploy/tofu/` run,
Then both exit code 0

**AC4 — SSH key variable:**
Given `deploy/tofu/examples/stackit/variables.tf`,
When it is inspected,
Then a `ssh_public_key` variable (type: string, sensitive: false) is defined for key injection

**AC5 — Stackit ALB listener on port 443:**
Given the Stackit Application Load Balancer resource per ADR-014,
When it is inspected,
Then an HTTPS listener on port 443 is configured with health check targeting `/_matrix/client/v3/versions`

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Infrastructure story — ATDD skipped (OpenTofu static validation).

**1. `make test-iac-validate` — Static validation**
- Given: `deploy/tofu/examples/stackit/` does not exist
- When: `make test-iac-validate` runs
- Then: exit non-zero (stackit example directory missing)
- [Passes after implementation]
