---
status: new
epic: 13
story: 13
security_review: not-needed
matrix: false
ui: false
---

# Story 13.13: Include media-gateway in release pipeline and Helm deployment

Status: new

## Story

As a system operator,
I want the media gateway Docker image (`nebu-media`) to be built and pushed by the release pipeline and deployed via the Helm chart,
so that I can deploy a fully functional Nebu instance (including media upload/download) to any target platform by referencing a single version tag.

---

## Background

Story 13.11 introduced a versioned release pipeline (`make release TAG=vN.N.N`) and a GitLab CI `release` stage that builds and pushes versioned Docker images to the GitLab Container Registry. However, the release pipeline only produces images for **gateway** and **core**:

- `make release` builds `${CI_REGISTRY_IMAGE}/nebu-gateway:<version>` and `${CI_REGISTRY_IMAGE}/nebu-core:<version>`
- `make release-push` pushes only those two images
- GitLab CI has `release-gateway-image` and `release-core-image` Kaniko jobs — no media equivalent

Meanwhile, the media gateway is a first-class service in the system:

- `docker-compose.yml` defines a `media` service (port 8009) that depends on postgres, minio, and createbuckets
- `media/Dockerfile` exists with a multi-stage build (builder + Alpine final image, exposes port 8009)
- The Matrix Media API (`POST /_matrix/media/v3/upload`, `GET /_matrix/media/v3/download/{server}/{id}`) requires the media gateway to be running
- Helm chart templates exist for gateway and core but **not** for media-gateway (the `media-gateway/` directory under `deploy/helm/nebu/templates/` is empty)

Without a media gateway release image and Helm deployment, operators cannot deploy a complete Nebu instance — users can join rooms and sync messages, but cannot upload or download media.

---

## Acceptance Criteria

**AC1 — `make release` builds the media gateway image:**
Given `Makefile`,
When `TAG=v1.0.0 make release` is executed,
Then:
- A third Docker image is built and tagged as `${CI_REGISTRY_IMAGE}/nebu-media:$(IMAGE_VERSION)` (e.g. `registry.gitlab.com/philippb/open-chat/nebu-media:1.0.0`)
- The build uses the same `--build-arg` flags as gateway/core (`GIT_COMMIT`, `BUILD_TIME`, `RELEASE_VERSION`)
- The build context is `./media` (where `media/Dockerfile` lives)

**AC2 — `make release-push` pushes the media gateway image:**
Given all three images are built by `make release TAG=v1.0.0`,
When `TAG=v1.0.0 make release-push` is executed,
Then:
- All three images are pushed: `nebu-gateway:1.0.0`, `nebu-core:1.0.0`, `nebu-media:1.0.0`
- If any single push fails, the pipeline reports the failure but does not silently succeed

**AC3 — GitLab CI builds and pushes the media image on tag pushes:**
Given `.gitlab-ci.yml`,
When a SemVer tag is pushed (e.g. `v1.0.0`),
Then:
- A new job `release-media-image` runs in the `release` stage alongside `release-gateway-image` and `release-core-image`
- The job uses Kaniko to build `media/Dockerfile` and pushes `${CI_REGISTRY_IMAGE}/nebu-media:${RELEASE_VERSION}`
- The job shares the same structure as the existing release jobs: `<<: *build_ci_image`, `interruptible: false`, `--cache=false`, SemVer-only rules

**AC4 — Helm chart includes media-gateway deployment and service:**
Given `deploy/helm/nebu/`,
When the chart is rendered with `helm template`,
Then:
- A `media-gateway/deployment.yaml` renders a Deployment for the media gateway using image `${image.registry}/nebu-media:${media.image.tag}`
- A `media-gateway/service.yaml` renders a Service exposing port 8009
- Both templates follow the same patterns as gateway/core (label selectors, fullname helper, security context, nodeSelector, affinity, tolerations)
- The Helm values structure includes `media:` section with `image.tag`, `replicaCount`, and `resources`

**AC5 — Helm values.yaml documents media image naming:**
Given `deploy/helm/nebu/values.yaml`,
When it is inspected,
Then:
- A `media:` section exists with `image.registry`, `image.pullPolicy`, `image.tag`, `replicaCount`, and `resources`
- Comments document that `nebu-media:<version>` is produced by the release pipeline

**AC6 — docker-compose.ci.yml includes media service for integration tests:**
Given `docker-compose.ci.yml`,
When it is inspected,
Then:
- The `media` service image is overridden to use `${CI_REGISTRY_IMAGE}/media:${CI_COMMIT_SHA}` (matching the pattern for gateway and core)
- This ensures integration tests run against the CI-built media image, not the locally built one

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **`make release` builds three images** — Shell
    - When: `TAG=v0.0.1 make release` (dry-run or actual)
    - Then: `docker images | grep nebu` shows `nebu-gateway`, `nebu-core`, and `nebu-media` with the same version tag

2. **`make release-push` pushes three images** — Shell
    - When: `TAG=v0.0.1 make release-push` (dry-run: check `docker push` would be called for all three)
    - Then: All three image names appear in the command output

3. **CI release job exists for media** — YAML inspection
    - When: `.gitlab-ci.yml` is inspected
    - Then: `release-media-image` job exists with `stage: release`, SemVer-only rules, and `<<: *build_ci_image` anchor

4. **Helm chart renders media templates** — Helm template
    - When: `helm template nebu deploy/helm/nebu/ --set gateway.image.tag=v --set core.image.tag=v --set media.image.tag=v`
    - Then: `kubectl apply --dry-run=client` succeeds for both `media-gateway/deployment.yaml` and `media-gateway/service.yaml`

5. **CI overlay includes media** — YAML inspection
    - When: `docker-compose.ci.yml` is inspected
    - Then: `media:` service with `image: ${CI_REGISTRY_IMAGE}/media:${CI_COMMIT_SHA}` is present

---

## Tasks / Subtasks

- [x] **T1 — Makefile: add media to `release` and `release-push`** (AC1, AC2)
   - [x] T1.1: Add a third `docker build` block for media gateway in `make release` target
   - [x] T1.2: Add a third `docker push` line in `make release-push` target
   - [x] T1.3: Use same build arg pattern (`GIT_COMMIT`, `BUILD_TIME`, `RELEASE_VERSION`)

- [x] **T2 — GitLab CI: add `release-media-image` job** (AC3)
   - [x] T2.1: Mirror `release-core-image` structure with `media/Dockerfile` context
   - [x] T2.2: Destination `${CI_REGISTRY_IMAGE}/nebu-media:${RELEASE_VERSION}`
   - [x] T2.3: Same `rules` (SemVer tag only), `interruptible: false`, `needs: []`

- [x] **T3 — Helm chart: media-gateway templates** (AC4, AC5)
   - [x] T3.1: Create `media-gateway/deployment.yaml` following gateway pattern
   - [x] T3.2: Create `media-gateway/service.yaml` exposing port 8009
   - [x] T3.3: Add `media:` section to `values.yaml` with image, replicaCount, resources
   - [x] T3.4: Add comments documenting release image naming convention

- [x] **T4 — docker-compose.ci.yml: add media overlay** (AC6)
   - [x] T4.1: Add `media:` service with image override to `${CI_REGISTRY_IMAGE}/media:${CI_COMMIT_SHA}`

---

## Dev Notes

### Critical Context

**Existing `make release` pattern (DO NOT REFACTOR):**
```makefile
release:
	docker build \
		--build-arg GIT_COMMIT=... \
		--build-arg BUILD_TIME=... \
		--build-arg RELEASE_VERSION=... \
		-t $(CI_REGISTRY_IMAGE)/nebu-gateway:$(IMAGE_VERSION) \
		./gateway
	docker build \
		--build-arg GIT_COMMIT=... \
		--build-arg BUILD_TIME=... \
		--build-arg RELEASE_VERSION=... \
		-t $(CI_REGISTRY_IMAGE)/nebu-core:$(IMAGE_VERSION) \
		./core
```

Add a third block for media. The pattern is repetitive by design — keep it simple, do not abstract into a loop.

**Existing `make release-push` pattern:**
```makefile
release-push:
	docker push $(CI_REGISTRY_IMAGE)/nebu-gateway:$(IMAGE_VERSION)
	docker push $(CI_REGISTRY_IMAGE)/nebu-core:$(IMAGE_VERSION)
```

Add a third `docker push` line.

**Media Dockerfile** (`media/Dockerfile`) already exists with the correct structure:
- Multi-stage build (golang builder → Alpine final)
- Exposes port 8009
- ENTRYPOINT `/media`
- Accepts no special build args currently — but we should pass the same `GIT_COMMIT`, `BUILD_TIME`, `RELEASE_VERSION` for consistency

**Helm chart structure:**
- `deploy/helm/nebu/templates/media-gateway/` directory exists but is empty
- Follow `gateway-deployment.yaml` and `gateway-service.yaml` patterns exactly
- Media gateway listens on port 8009 (see `docker-compose.yml` line 238)
- No HPA needed for media in MVP (single replica is fine)

**docker-compose.ci.yml** currently only overlays `gateway` and `core` images. Add `media` to the list.

### Anti-Patterns to Avoid

1. **Do NOT change the image naming convention** — use `nebu-media` (consistent with `nebu-gateway`, `nebu-core`)
2. **Do NOT add HPA for media** — MVP only needs a basic deployment + service
3. **Do NOT add media to docker-compose.yml main services** — it already exists there; the CI overlay is the only change needed
4. **Do NOT modify existing release jobs** — `release-gateway-image` and `release-core-image` stay as-is

### References

- Existing `make release`: `Makefile` lines 80-100
- Existing `make release-push`: `Makefile` lines 104-112
- Media Dockerfile: `media/Dockerfile`
- Gateway Helm deployment: `deploy/helm/nebu/templates/gateway-deployment.yaml`
- Gateway Helm service: `deploy/helm/nebu/templates/gateway-service.yaml`
- Media service in docker-compose: `docker-compose.yml` lines 200-245
- CI overlay: `docker-compose.ci.yml`
- GitLab CI release jobs: `.gitlab-ci.yml` lines 480-512

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

_None_

### Completion Notes List

- Added `nebu-media` image to `make release` and `make release-push` targets in Makefile
- Added `release-media-image` Kaniko job to GitLab CI (`.gitlab-ci.yml`)
- Created Helm templates: `media-gateway/deployment.yaml`, `media-gateway/service.yaml`
- Added `media:` section to `deploy/helm/nebu/values.yaml` with image, replicaCount, resources
- Updated `docker-compose.ci.yml` to overlay media image for integration tests
- Updated `make redeploy` to include `media` service
- Updated `validate-helm` CI job to include `--set media.image.tag=validate`

### File List

- `Makefile`
- `.gitlab-ci.yml`
- `docker-compose.ci.yml`
- `deploy/helm/nebu/values.yaml`
- `deploy/helm/nebu/templates/media-gateway/deployment.yaml` (new)
- `deploy/helm/nebu/templates/media-gateway/service.yaml` (new)

## Change Log

- 2026-05-14: Story created (claude-sonnet-4-6)
- 2026-05-14: Story implemented — media gateway added to release pipeline, Helm chart, and CI overlay (claude-sonnet-4-6)
