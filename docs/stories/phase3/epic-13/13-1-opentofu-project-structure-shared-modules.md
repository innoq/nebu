---
status: done
epic: 13
story: 1
security_review: not-needed
matrix: false
ui: false
---

# Story 13.1: OpenTofu Project Structure + Shared Modules

Status: done

## Story

As a system operator,
I want an OpenTofu project under `deploy/` with shared modules for platform-specific infrastructure,
So that all three target platforms (AWS, Stackit, Kubernetes) share a consistent project layout and reusable shared primitives.

**Size:** M

---

## Acceptance Criteria

**AC1 — Directory structure exists:**
Given the repository is cloned,
When `ls deploy/` is inspected,
Then the following directories exist: `deploy/tofu/modules/nebu-core/`, `deploy/tofu/modules/nebu-aws/`, `deploy/tofu/modules/nebu-stackit/`, `deploy/tofu/modules/nebu-k8s/`, `deploy/tofu/examples/aws/`, `deploy/tofu/examples/stackit/`, `deploy/tofu/examples/k8s/`, `deploy/helm/`

**AC2 — tofu validate passes:**
Given `deploy/tofu/examples/aws/main.tf` exists with a minimal AWS provider reference,
When `tofu validate` runs inside `deploy/tofu/examples/aws/`,
Then validation succeeds with 0 errors

**AC3 — tofu fmt passes:**
Given the OpenTofu project,
When `tofu fmt -check -recursive deploy/tofu/` runs,
Then exit code is 0 (all files formatted correctly)

**AC4 — nebu-core module contains shared variables:**
Given `deploy/tofu/modules/nebu-core/variables.tf`,
When it is inspected,
Then it defines at minimum: `nebu_version` (string), `domain_name` (string with validation), `admin_email` (string), `postgres_db_name` (string), `image_registry` (string)

**AC5 — README documents prerequisites and quick start:**
Given `deploy/README.md` exists,
When it is inspected,
Then it documents: prerequisites (tofu, helm, AWS CLI, Stackit CLI), backend configuration options, per-platform quick start (3 commands each), secrets management strategy reference to ADR-014

**AC6 — CI IaC validation job added:**
Given the `.gitlab-ci.yml` pipeline,
When it runs on a branch with changes under `deploy/`,
Then a new `validate-iac` job runs `tofu fmt -check -recursive deploy/tofu/` and `tofu validate` for each example directory, exiting non-zero on failure

**AC7 — Makefile target for local IaC validation:**
Given `make test-iac-validate` runs locally,
When it completes,
Then `tofu fmt -check` and `tofu validate` run for each example directory and exit 0

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Infrastructure story — ATDD skipped (no application logic, only OpenTofu static validation).
Verification is via CI/make targets.

**1. `make test-iac-validate` — Static validation target**
- Given: `deploy/tofu/` directory does NOT exist yet
- When: `make test-iac-validate` runs
- Then: exit code non-zero (failing, because no .tf files exist)
- [Expected to pass after implementation]

**2. GitLab CI `validate-iac` job — CI gate**
- Given: a push with changes under `deploy/` triggers the pipeline
- When: the `validate-iac` job runs
- Then: `tofu validate` and `tofu fmt -check` exit 0

Note: These tests fail before implementation (no `deploy/` directory, no Makefile target, no CI job) and pass after implementation.
