---
status: ready-for-dev
epic: 13
story: 4b
security_review: optional
matrix: false
ui: false
---

# Story 13.4b: Helm Chart Ingress, PVC, Secrets, HPA

Status: ready-for-dev

## Story

As a platform engineer,
I want Helm chart templates for Ingress, PersistentVolumeClaim, Kubernetes Secrets, and HorizontalPodAutoscaler,
So that production-grade Nebu deployments support TLS ingress, persistent storage, secret management, and autoscaling.

**Size:** S

---

## Acceptance Criteria

**AC1 — Ingress hostname configurable:**
Given the Ingress template `deploy/helm/nebu/templates/ingress.yaml`,
When `helm template` renders it with `ingress.enabled: true`,
Then the Ingress hostname and TLS secret name are configurable via `values.yaml` — no hardcoded values appear

**AC2 — Secret template references external secrets:**
Given the Secret template `deploy/helm/nebu/templates/secrets.yaml`,
When it is inspected,
Then DB credentials and internal secret are referenced by Kubernetes Secret name (not hardcoded in `values.yaml`) — the template uses `{{ .Values.existingSecret.name }}` or equivalent external reference pattern

**AC3 — HPA template passes helm lint:**
Given the HorizontalPodAutoscaler template for gateway,
When it is enabled via `autoscaling.gateway.enabled: true` in `values.yaml`,
Then `helm lint deploy/helm/nebu/` still passes with 0 warnings

**AC4 — PVC conditional on postgres.external:**
Given the PersistentVolumeClaim template,
When `postgres.external: false` is set in `values.yaml`,
Then a PVC for Postgres is rendered; when `postgres.external: true`, no PVC is rendered (conditional via `{{- if not .Values.postgres.external }}`)

**AC5 — helm template renders all templates without errors:**
Given all templates including Ingress, PVC, Secret, and HPA,
When `helm template deploy/helm/nebu/ -f deploy/helm/nebu/values-dev.yaml` runs,
Then exit code 0 and valid YAML is rendered for all enabled resources

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Infrastructure story — ATDD skipped (Helm static validation).

**1. `helm lint` with HPA enabled — Lint gate**
- Given: `values.yaml` sets `autoscaling.gateway.enabled: true`
- When: `helm lint deploy/helm/nebu/` runs
- Then: exit 0, 0 warnings

**2. PVC conditional rendering**
- Given: `helm template ... --set postgres.external=true`
- When: output is inspected
- Then: no PVC resource appears in the rendered YAML

Note: `security_review: optional` — Secret template references should be reviewed to ensure no plaintext credentials leak into chart values.
