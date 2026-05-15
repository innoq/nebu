---
status: done
epic: 13
story: 4a
security_review: not-needed
matrix: false
ui: false
---

# Story 13.4a: Helm Chart Core Templates (Deployment, Service, ConfigMap)

Status: done

## Story

As a platform engineer,
I want the core Helm chart templates — Deployment, Service, and ConfigMap — under `deploy/helm/nebu/`,
So that the foundational Kubernetes resources for Nebu gateway and core can be rendered and linted without errors.

**Size:** S

---

## Acceptance Criteria

**AC1 — helm lint passes:**
Given `deploy/helm/nebu/Chart.yaml`, `values.yaml`, and `values-dev.yaml` exist,
When `helm lint deploy/helm/nebu/` runs,
Then exit code is 0 with 0 warnings

**AC2 — helm template renders valid YAML:**
Given the Helm chart templates,
When `helm template deploy/helm/nebu/` runs,
Then valid YAML is rendered without errors, including:
  - Separate Deployments for gateway and core
  - A ClusterIP Service for gateway (port 8008)
  - A ConfigMap for non-secret `NEBU_*` environment variables

**AC3 — values.yaml exposes key configuration:**
Given `deploy/helm/nebu/values.yaml`,
When it is inspected,
Then the following are configurable: image tags (gateway, core), replica counts (gateway and core independently), `NEBU_OIDC_ISSUER`, `NEBU_SERVER_NAME`, and resource limits (CPU/Memory requests and limits)

**AC4 — _helpers.tpl defines common labels:**
Given `deploy/helm/nebu/templates/_helpers.tpl`,
When `helm template` renders any resource,
Then all resources carry labels: `app.kubernetes.io/name`, `app.kubernetes.io/instance`, `app.kubernetes.io/version`, `helm.sh/chart`

**AC5 — values-dev.yaml suitable for local kind:**
Given `deploy/helm/nebu/values-dev.yaml`,
When it is inspected,
Then resource limits are reduced (e.g., 100m CPU, 128Mi memory) and image pull policy is `IfNotPresent`

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Infrastructure story — ATDD skipped (Helm chart static validation).

**1. `helm lint deploy/helm/nebu/` — Lint gate (fails before files exist)**
- Given: `deploy/helm/nebu/Chart.yaml` does NOT exist
- When: `helm lint deploy/helm/nebu/` runs
- Then: exit non-zero (chart not found)
- [Passes after implementation]

**2. `helm template deploy/helm/nebu/` — Render gate**
- Given: chart exists with templates
- When: `helm template deploy/helm/nebu/` runs
- Then: valid YAML output, exit 0

**3. `make test-iac-validate` — Makefile target (extended in this story)**
- Given: story 13.1's `make test-iac-validate` exists
- When: extended to also run `helm lint deploy/helm/nebu/`
- Then: all checks exit 0

---

### Review Findings

- [x] [Review][Patch] MINOR-1: `image.tag: ""` lacks `required` validator — Fixed: both gateway-deployment.yaml and core-deployment.yaml now use `required` with a descriptive error message
- [x] [Review][Patch] MINOR-2: Shared `image.tag` for gateway and core — Fixed: split into `gateway.image.tag` and `core.image.tag` in values.yaml; both Deployments reference their respective key; values-dev.yaml updated accordingly
- [x] [Review][Patch] MAJOR-1: No `core-service.yaml` — Fixed: created `deploy/helm/nebu/templates/core-service.yaml` ClusterIP Service exposing port 9000 (gRPC) and 4000 (HTTP) with name `{release}-core`
- [x] [Review][Patch] MAJOR-2: `NEBU_CORE_GRPC_ADDR` missing from ConfigMap — Fixed: added to configmap.yaml derived from release name as `{release}-core:9000`; `config.coreGrpcAddr: ""` placeholder added to values.yaml with explanatory comment
- [x] [Review][Defer] INFO: `validate-helm` CI job has no `needs:` dependency on `validate-iac` — runs in parallel even if tofu fmt fails; cosmetic pipeline efficiency issue — deferred, pre-existing pattern in repo
- [x] [Review][Defer] INFO: Core liveness/readiness uses `tcpSocket` on 4000 — TCP probes can report green during a crash loop if the port remains bound; HTTP health endpoint for core not in scope for this story — deferred, no core health endpoint exists yet
- [x] [Review][Defer] INFO: No explicit `strategy.type: RollingUpdate` — K8s default is correct for MVP; explicit config is a production hardening item — deferred, out of scope
