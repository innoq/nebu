---
status: review
epic: 13
story: 11
security_review: not-needed
matrix: false
ui: false
atdd: not-applicable
---

# Story 13.11: Versioning and Release Pipeline

Status: review

## Story

As a system operator,
I want a `make release TAG=v1.0.0` target and a GitLab CI release pipeline that builds versioned Docker images and pushes them to the GitLab Container Registry,
so that I can deploy a specific, immutable release version to any target platform (AWS, Stackit, K8s) by referencing `nebu_version = "1.0.0"` in my OpenTofu variables or `gateway.image.tag=1.0.0` in Helm.

---

## Background

Currently, the build system has no concept of release versioning:

- `make build-gateway` tags the image as `nebu-gateway:dev` (local-only tag, not the compose image)
- `make redeploy` uses `docker compose build` which tags as `latest` locally — no registry push
- The GitLab CI `build-gateway-image` + `build-core-image` jobs build and push `registry.gitlab.com/.../<service>:<CI_COMMIT_SHA>` — useful for integration tests but not for operators deploying by version
- The OpenTofu examples already have `variable "nebu_version"` (AWS + Stackit) and `variable "gateway_image_tag"` / `variable "core_image_tag"` (K8s) referencing versioned tags — but no pipeline produces those tags in the registry

Image naming convention established in the existing CI (from `build-gateway-image` / `build-core-image`):
- Gateway: `${CI_REGISTRY_IMAGE}/gateway:<tag>`
- Core: `${CI_REGISTRY_IMAGE}/core:<tag>`

The AWS + Stackit OpenTofu modules reference images as `<image_registry>/nebu-gateway:<nebu_version>` and `<image_registry>/nebu-core:<nebu_version>` (see `deploy/tofu/modules/nebu-aws/compute.tf` and `deploy/tofu/examples/stackit/main.tf`).

This story introduces a **parallel release track** with `nebu-gateway` / `nebu-core` as the canonical image names for versioned release images, alongside the existing `gateway:<SHA>` / `core:<SHA>` images used for integration tests. The OpenTofu examples already reference `nebu-gateway` / `nebu-core` (see `deploy/tofu/examples/stackit/variables.tf` lines 168–176 and `modules/nebu-aws/compute.tf`), so the release pipeline aligns with what operators actually use.

The SHA-tagged integration-test images (`gateway:<SHA>`, `core:<SHA>`) are kept unchanged to avoid disrupting `integration-test-k8s`. Renaming those integration-test images to `nebu-gateway`/`nebu-core` is deferred to a follow-up story.

---

## Acceptance Criteria

**AC1 — `make release` target reads TAG and builds versioned images:**
Given `Makefile`,
When `TAG=v1.0.0 make release` is executed,
Then:
- The variable `TAG` is validated to start with `v` and match SemVer format (e.g. `v1.0.0`, `v1.2.3`)
- `make release` without `TAG` set fails immediately with a clear error: `"TAG is required. Usage: TAG=v1.0.0 make release"`
- The gateway Docker image is built and tagged as `${CI_REGISTRY_IMAGE}/nebu-gateway:1.0.0` (version without `v` prefix — SemVer convention for image tags)
- The core Docker image is built and tagged as `${CI_REGISTRY_IMAGE}/nebu-core:1.0.0`
- Both builds pass `GIT_COMMIT`, `BUILD_TIME`, and `RELEASE_VERSION` as Docker build args (consistent with the `redeploy` target and the Dockerfiles in `gateway/Dockerfile` and `core/Dockerfile`)
- `CI_REGISTRY_IMAGE` defaults to `registry.gitlab.com/philippb/open-chat` when not set

**AC2 — `make release-push` pushes the versioned images:**
Given both images are built by `make release TAG=v1.0.0`,
When `TAG=v1.0.0 make release-push` is executed,
Then:
- Both `nebu-gateway:1.0.0` and `nebu-core:1.0.0` are pushed to the registry
- `make release` and `make release-push` can be chained as `TAG=v1.0.0 make release release-push`

**AC3 — GitLab CI `release` stage triggers on Git tags:**
Given `.gitlab-ci.yml`,
When a Git tag matching `v[0-9]+.[0-9]+.[0-9]+` is pushed (e.g. `git tag v1.0.0 && git push origin v1.0.0`),
Then a new CI pipeline stage `release` runs and:
- `release-gateway-image` job: builds and pushes `${CI_REGISTRY_IMAGE}/nebu-gateway:<semver>` (tag without `v` prefix) using Kaniko — same approach as existing `build-gateway-image` / `build-core-image` jobs
- `release-core-image` job: builds and pushes `${CI_REGISTRY_IMAGE}/nebu-core:<semver>` using Kaniko
- Both jobs do NOT run on branch pipelines — they are exclusively triggered by `$CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+$/`
- Both jobs pass `GIT_COMMIT=${CI_COMMIT_SHA}`, `BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)`, `RELEASE_VERSION=<semver>` as Kaniko `--build-arg` flags

**AC4 — Release images carry correct build metadata:**
Given a release image `nebu-gateway:1.0.0` built via `make release TAG=v1.0.0` or the CI release pipeline,
When the container is started,
Then the build metadata injected via ldflags/env vars matches:
- `RELEASE_VERSION=1.0.0` (without `v` prefix)
- `GIT_COMMIT=<sha>` (short SHA from `git rev-parse --short HEAD`)
- `BUILD_TIME=<iso8601>` (UTC timestamp)

**AC5 — OpenTofu deploy examples document the versioned image naming convention:**
Given `deploy/tofu/examples/aws/terraform.tfvars.example` and `deploy/tofu/examples/stackit/terraform.tfvars.example`,
When they are inspected,
Then:
- `image_registry` comment explains that the release pipeline pushes images as `<image_registry>/nebu-gateway:<version>` and `<image_registry>/nebu-core:<version>`
- `nebu_version` comment references the Git tag convention (e.g. `"1.0.0"` for Git tag `v1.0.0`)
- The default value in `terraform.tfvars.example` (currently `"0.3.0"`) is updated to a realistic example like `"1.0.0"`

**AC6 — Helm `values.yaml` documents the versioned image naming convention:**
Given `deploy/helm/nebu/values.yaml`,
When it is inspected,
Then:
- `image.registry` comment references the GitLab Container Registry path where release images are pushed
- `gateway.image.tag` and `core.image.tag` comments document that versioned tags (e.g. `"1.0.0"`) are produced by the release pipeline

**AC7 — `make release` and `make release-push` are documented in the `Makefile` help comments:**
Given `Makefile`,
When `grep "^##" Makefile` or `make help` is run (if a help target exists),
Then both `release` and `release-push` targets appear with clear descriptions.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

This story is infrastructure/CI — no automated unit tests are applicable (atdd: not-applicable).
Verification is structural (linting + manual invocation):

1. **`make release` guard test** — Shell
   - When: `make release` without `TAG` variable set
   - Then: exits non-zero with message `"TAG is required. Usage: TAG=v1.0.0 make release"`

2. **`make release` SemVer validation** — Shell
   - When: `TAG=notasemver make release`
   - Then: exits non-zero with message explaining valid format

3. **CI release pipeline trigger** — GitLab CI YAML lint
   - When: `.gitlab-ci.yml` is validated with `gitlab-ci-lint` or inspected
   - Then: `release-gateway-image` and `release-core-image` jobs have `rules: if: '$CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+$/'` and do NOT appear in branch pipelines

---

## Tasks / Subtasks

- [x] **T1 — Makefile: add `release` and `release-push` targets** (AC1, AC2, AC4, AC7)
  - [x] T1.1: Define `IMAGE_VERSION` computed from `TAG` by stripping leading `v` (e.g. `v1.0.0` → `1.0.0`)
  - [x] T1.2: Validate `TAG` is set and matches `v[0-9]+.[0-9]+.[0-9]+` — fail fast with usage message if not
  - [x] T1.3: Default `CI_REGISTRY_IMAGE` to `registry.gitlab.com/philippb/open-chat`
  - [x] T1.4: Build gateway image with correct `--build-arg` flags (`GIT_COMMIT`, `BUILD_TIME`, `RELEASE_VERSION`)
  - [x] T1.5: Build core image with correct `--build-arg` flags
  - [x] T1.6: Tag both images as `${CI_REGISTRY_IMAGE}/nebu-gateway:${IMAGE_VERSION}` and `${CI_REGISTRY_IMAGE}/nebu-core:${IMAGE_VERSION}`
  - [x] T1.7: `release-push` target: `docker push` both tagged images
  - [x] T1.8: Add `## release:` and `## release-push:` docstring comments
  - [x] T1.9: Add `release` and `release-push` to `.PHONY`

- [x] **T2 — GitLab CI: add `release` stage with Kaniko jobs** (AC3, AC4)
  - [x] T2.1: Add `release` stage to `stages:` list (after `build`)
  - [x] T2.2: Add `release-gateway-image` job: Kaniko build, destination `${CI_REGISTRY_IMAGE}/nebu-gateway:${CI_COMMIT_TAG_NO_V}` — strip leading `v` from tag
  - [x] T2.3: Add `release-core-image` job: Kaniko build, destination `${CI_REGISTRY_IMAGE}/nebu-core:${CI_COMMIT_TAG_NO_V}`
  - [x] T2.4: Both jobs: `rules: - if: '$CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+$/'` — only on SemVer tag pushes
  - [x] T2.5: Both jobs: pass `GIT_COMMIT`, `BUILD_TIME`, `RELEASE_VERSION` as `--build-arg` to Kaniko
  - [x] T2.6: Both jobs: set `interruptible: false` — release jobs must not be cancelled
  - [x] T2.7: Compute `CI_COMMIT_TAG_NO_V` in a `before_script` variable or via shell substitution (`${CI_COMMIT_TAG#v}`)

- [x] **T3 — Update deploy documentation** (AC5, AC6)
  - [x] T3.1: Update `deploy/tofu/examples/aws/terraform.tfvars.example`: update `nebu_version` example + comment on image naming
  - [x] T3.2: Update `deploy/tofu/examples/stackit/terraform.tfvars.example`: update `nebu_version` example + comment
  - [x] T3.3: Update `deploy/helm/nebu/values.yaml`: add comments to `image.registry`, `gateway.image.tag`, `core.image.tag`

---

## Dev Notes

### Critical Context: Existing Build Infrastructure

**CI image tagging convention (DO NOT CHANGE — used by `integration-test-k8s`):**
```yaml
# build-gateway-image (line 233-254 in .gitlab-ci.yml):
--destination "${CI_REGISTRY_IMAGE}/gateway:${CI_COMMIT_SHA}"

# build-core-image:
--destination "${CI_REGISTRY_IMAGE}/core:${CI_COMMIT_SHA}"
```
These SHA-tagged images (`/gateway:sha`, `/core:sha`) feed directly into `integration-test-k8s` via:
```yaml
services:
  - name: $CI_REGISTRY_IMAGE/core:${CI_COMMIT_SHA}
  - name: $CI_REGISTRY_IMAGE/gateway:${CI_COMMIT_SHA}
```
**Do NOT change these existing jobs.** The release jobs produce DIFFERENT image names (`nebu-gateway:1.0.0`, `nebu-core:1.0.0`) — parallel track.

**Dockerfiles already support build args** — both `gateway/Dockerfile` and `core/Dockerfile` accept `ARG GIT_COMMIT`, `ARG BUILD_TIME`, `ARG RELEASE_VERSION` and inject them at build time. The `redeploy` target in Makefile (line 61-65) shows the correct invocation pattern.

**`make redeploy` existing pattern** (use as template for `make release`):
```makefile
redeploy:
	GIT_COMMIT=$$(git rev-parse --short HEAD) \
	BUILD_TIME=$$(date -u +%Y-%m-%dT%H:%M:%SZ) \
	RELEASE_VERSION=$$(git describe --tags --always 2>/dev/null || echo dev) \
	docker compose build --no-cache gateway core
```
For `make release`, use `docker build` directly (not `docker compose build`) so you can specify the exact image name and tag independently of docker compose.

### Makefile Implementation Pattern

The `make release` target must use `docker build` (not `docker compose build`) to control image names precisely:

```makefile
# How to strip v prefix and validate TAG:
IMAGE_VERSION = $(patsubst v%,%,$(TAG))
# Validation: check TAG starts with 'v' and has N.N.N form
```

Use `$(error ...)` for guard checks:
```makefile
release:
ifndef TAG
	$(error TAG is required. Usage: TAG=v1.0.0 make release)
endif
```

SemVer validation: use a shell check in the recipe body:
```makefile
	@echo "$(TAG)" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$$' || \
		(echo "ERROR: TAG must match vN.N.N (e.g. v1.0.0)" && exit 1)
```

Default `CI_REGISTRY_IMAGE` in Makefile preamble (consistent with existing variable pattern):
```makefile
CI_REGISTRY_IMAGE ?= registry.gitlab.com/philippb/open-chat
```

Full image names for release:
- `$(CI_REGISTRY_IMAGE)/nebu-gateway:$(IMAGE_VERSION)`
- `$(CI_REGISTRY_IMAGE)/nebu-core:$(IMAGE_VERSION)`

Gateway build context is `./gateway` (Dockerfile at `gateway/Dockerfile`).
Core build context is `./core` (Dockerfile at `core/Dockerfile`).

### GitLab CI: Kaniko Tag Stripping

In GitLab CI, strip the `v` prefix from `$CI_COMMIT_TAG` using shell parameter expansion in `before_script`:
```bash
export RELEASE_VERSION="${CI_COMMIT_TAG#v}"
```
Then use `$RELEASE_VERSION` for the Kaniko destination and `--build-arg`.

The Kaniko jobs should mirror the `build-gateway-image` / `build-core-image` structure with:
- Same `*build_ci_image` YAML anchor for Kaniko setup and registry auth
- Same `--context "${CI_PROJECT_DIR}/gateway"` / `--context "${CI_PROJECT_DIR}/core"` pattern
- `--cache=false` for release builds (no layer cache leakage into production images) or `--cache=true` with a distinct cache key

### OpenTofu Variable Naming Discrepancy

The existing deploy examples use two naming patterns:
- AWS + Stackit: single `nebu_version` variable, module computes full image name as `<image_registry>/nebu-gateway:<nebu_version>`
- K8s: separate `gateway_image_tag` and `core_image_tag` variables

For this story, update comments in `terraform.tfvars.example` (not the HCL variable definitions themselves) to clarify that `nebu_version = "1.0.0"` corresponds to the Git tag `v1.0.0`.

The K8s `terraform.tfvars.example` also needs to be updated so `gateway_image_tag` and `core_image_tag` show `"1.0.0"` instead of `"dev"` as the example — but the default in `variables.tf` has no default (required variable), so just update the example comment.

### Files to Create / Modify

| File | Action | Description |
|---|---|---|
| `Makefile` | UPDATE | Add `CI_REGISTRY_IMAGE ?=` default, `release` + `release-push` targets |
| `.gitlab-ci.yml` | UPDATE | Add `release` stage, `release-gateway-image` + `release-core-image` jobs |
| `deploy/tofu/examples/aws/terraform.tfvars.example` | UPDATE | Update `nebu_version` example + add comment on image naming |
| `deploy/tofu/examples/stackit/terraform.tfvars.example` | UPDATE | Update `nebu_version` example + add comment on image naming |
| `deploy/helm/nebu/values.yaml` | UPDATE | Add comments to `image.registry`, `gateway.image.tag`, `core.image.tag` |

> **Note:** `deploy/tofu/examples/k8s/terraform.tfvars.example` is NOT in scope for T3 (AC5 only covers aws + stackit). The K8s example retains `gateway_image_tag = "dev"` / `core_image_tag = "dev"` for local kind dev workflows with a comment explaining the release convention.

### CI Stage Order

The new `release` stage must be added AFTER `build` (which produces the SHA-tagged images used by integration tests). The `release` stage only fires on tag pushes and does not affect branch pipelines.

Current stages order:
```yaml
stages:
  - build-ci-images
  - lint
  - unit
  - scan
  - verify
  - validate-iac
  - build
  - integration
```

New order — add `release` after `integration`:
```yaml
stages:
  - build-ci-images
  - lint
  - unit
  - scan
  - verify
  - validate-iac
  - build
  - integration
  - release
```

### Security: No Credentials in Make Targets

`make release` runs locally and assumes the operator is already authenticated with the registry (`docker login registry.gitlab.com`). Do not add registry credentials to the Makefile.

### Anti-Patterns to Avoid

1. **Do NOT modify `build-gateway-image` or `build-core-image` CI jobs** — they serve integration tests via `$CI_COMMIT_SHA` tags and must remain unchanged.
2. **Do NOT use `docker compose build` in `make release`** — it produces `nebu-gateway:dev` or `nebu-gateway:latest` which the operator cannot easily retag. Use `docker build` directly.
3. **Do NOT push `latest` tag** — the release pipeline must only push the specific version tag. `latest` in production is an anti-pattern (enforced by the existing `nebu_version != "latest"` validation in both AWS and Stackit `variables.tf`).
4. **Do NOT modify the `nebu_version` variable definition or validation in HCL files** — only update comments in `terraform.tfvars.example` files.

### References

- Existing Kaniko pattern: `.gitlab-ci.yml` lines 53-115 (`*build_ci_image` anchor, `build-ci-go`, `build-ci-elixir`, `build-ci-dex`)
- Existing application Kaniko jobs: `.gitlab-ci.yml` lines 228-254 (`build-gateway-image`, `build-core-image`)
- Existing `redeploy` target with build args: `Makefile` lines 57-65
- Gateway Dockerfile with build args: `gateway/Dockerfile` lines 11-15
- Core Dockerfile with build args: `core/Dockerfile` lines 13-20
- AWS `nebu_version` variable: `deploy/tofu/examples/aws/variables.tf` lines 7-11
- Stackit `nebu_version` variable with "never use latest" validation: `deploy/tofu/examples/stackit/variables.tf` lines 168-176
- Stackit image reference pattern: `deploy/tofu/examples/stackit/main.tf` line 197 (`nebu_version = var.nebu_version`)
- K8s image tag variables: `deploy/tofu/examples/k8s/variables.tf` lines 13-22
- Helm values image tags: `deploy/helm/nebu/values.yaml` lines 5-35

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

_None_

### Completion Notes List

- Implemented `make release TAG=vN.N.N` and `make release-push TAG=vN.N.N` targets using `docker build` directly (not `docker compose build`) with TAG guard, SemVer validation, and correct `--build-arg` flags.
- Added `CI_REGISTRY_IMAGE ?= registry.gitlab.com/philippb/open-chat` default and `IMAGE_VERSION` computed variable to Makefile preamble.
- Added `release` and `release-push` to `.PHONY` with `##` docstring comments.
- Added `release` stage to `.gitlab-ci.yml` stages list (after `integration`).
- Added `release-gateway-image` and `release-core-image` Kaniko jobs — exclusive to SemVer tag pushes (`$CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+$/`), `interruptible: false`, `--cache=false`, RELEASE_VERSION stripped via `${CI_COMMIT_TAG#v}`.
- Updated `workflow.rules` to allow tag pipelines so the release stage fires on `git push origin vN.N.N`.
- Existing `build-gateway-image` / `build-core-image` jobs left untouched (integration test dependency).
- Updated AWS + Stackit deploy example files with canonical image naming comments and `"1.0.0"` as the example version (AC5 scope). K8s example NOT modified (out of AC5 scope).
- Review cycle 2: Added `ifeq ($(strip $(TAG)),)` guard alongside `ifndef TAG` in Makefile release target (MINOR-1). Added RC/beta exclusion comment to workflow.rules SemVer line (MINOR-2). Added `needs: []` to both release jobs for DAG-style independence (MINOR-6). Reverted K8s tfvars back to `"dev"` with clarifying comment — was scope creep (MINOR-7). Removed K8s tfvars from story Files table, added scope note (MINOR-8).

### File List

- `Makefile`
- `.gitlab-ci.yml`
- `deploy/tofu/examples/aws/terraform.tfvars.example`
- `deploy/tofu/examples/stackit/terraform.tfvars.example`
- `deploy/tofu/examples/k8s/terraform.tfvars.example`
- `deploy/helm/nebu/values.yaml`
- `docs/stories/phase3/epic-13/13-11-versioning-and-release-pipeline.md`

## Change Log

- 2026-05-14: Story implemented — Makefile release targets, GitLab CI release stage, deploy example documentation updates (claude-sonnet-4-6)
- 2026-05-14: Review cycle 2 — MINOR-1 ifeq empty TAG guard, MINOR-2 RC/beta comment, MINOR-6 needs:[] DAG jobs, MINOR-7 K8s tfvars reverted to "dev", MINOR-8 story table/scope note (claude-sonnet-4-6)
