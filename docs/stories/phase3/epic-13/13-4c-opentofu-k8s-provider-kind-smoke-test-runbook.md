---
status: done
epic: 13
story: 4c
security_review: not-needed
matrix: false
ui: false
---

# Story 13.4c: OpenTofu K8s Provider + kind Smoke Test + Runbook

Status: done

## Story

As a platform engineer,
I want `deploy/tofu/examples/k8s/main.tf` with a Kubernetes/Helm provider and a validated smoke test against a local `kind` cluster,
So that operators can provision Nebu on any Kubernetes cluster via `tofu apply`.

**Size:** S

---

## Acceptance Criteria

**AC1 — tofu plan shows helm_release:**
Given `deploy/tofu/examples/k8s/main.tf` with a configured Kubernetes provider,
When `tofu plan` runs against a local `kind` cluster (or with `tofu validate` for syntax-only check),
Then the plan shows a `helm_release` resource for Nebu — no errors

**AC2 — helm install on kind succeeds:**
Given a local `kind` cluster,
When `helm install nebu deploy/helm/nebu/ -f deploy/helm/nebu/values-dev.yaml` runs,
Then all Nebu pods reach `Running` state within 3 minutes (documented in RUNBOOK.md as the smoke test procedure)

**AC3 — tofu validate passes:**
Given `deploy/tofu/examples/k8s/main.tf`,
When `tofu validate` runs,
Then exit code 0

**AC4 — RUNBOOK.md covers K8s operations:**
Given `deploy/tofu/examples/k8s/RUNBOOK.md`,
When it is read,
Then it covers: prerequisites (kind/kubectl/helm), `helm upgrade` procedure, rollback (`helm rollback`), and HPA configuration

**AC5 — nebu-k8s module is a thin wrapper:**
Given `deploy/tofu/modules/nebu-k8s/main.tf`,
When it is inspected,
Then it contains only a `helm_release` resource block — no provider-specific infrastructure resources (those are external dependencies the operator provides)

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Infrastructure story — ATDD skipped (Helm/OpenTofu static validation).

**1. `tofu validate` for k8s example — Syntax gate**
- Given: `deploy/tofu/examples/k8s/main.tf` does not exist
- When: `make test-iac-validate` runs (extended for k8s example)
- Then: exit non-zero
- [Passes after implementation]

**2. `helm template` dry-run for kind**
- Given: `deploy/helm/nebu/values-dev.yaml` exists
- When: `helm template deploy/helm/nebu/ -f deploy/helm/nebu/values-dev.yaml | kubectl apply --dry-run=client -f -`
- Then: exit 0 (all rendered resources accepted by kubectl dry-run)
