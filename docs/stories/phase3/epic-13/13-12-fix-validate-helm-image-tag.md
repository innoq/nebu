---
status: new
epic: 13
story: 12
security_review: not-needed
matrix: false
ui: false
---

# Story 13.12: Fix validate-helm CI — gateway.image.tag must be set

Status: new

## Problem

The `validate-helm` CI job in `.gitlab-ci.yml` fails with:

```
Error: execution error at (nebu/templates/gateway-deployment.yaml:26:63):
gateway.image.tag must be set (use --set gateway.image.tag=X.Y.Z)
```

This is because `values.yaml` intentionally leaves `gateway.image.tag` and `core.image.tag` empty (`""`) with a `required` error in `gateway-deployment.yaml` to prevent accidental deployment of unversioned images. The `helm lint` and `helm template` commands in the CI job do not pass `--set` flags, so the `required` helper triggers.

The Makefile target `test-iac-validate` already passes `--set gateway.image.tag=validate --set core.image.tag=validate` correctly (see Makefile line 397–399). The CI job is missing the same flags.

## Goal

Fix the `validate-helm` CI job so that `helm lint` and `helm template` succeed without cloud credentials or a running cluster.

## Acceptance Criteria

1. `helm lint deploy/helm/nebu/` passes with 0 warnings
2. `helm template nebu deploy/helm/nebu/` renders valid YAML (including gateway-deployment.yaml)
3. The fix matches the Makefile's approach: `--set gateway.image.tag=validate --set core.image.tag=validate`

## Implementation

In `.gitlab-ci.yml`, update the `validate-helm` job script block:

```yaml
# Before (broken):
script:
  - helm lint deploy/helm/nebu/
  - helm template nebu deploy/helm/nebu/ > /dev/null

# After (fixed):
script:
  - helm lint deploy/helm/nebu/ --set gateway.image.tag=validate --set core.image.tag=validate
  - helm template nebu deploy/helm/nebu/ --set gateway.image.tag=validate --set core.image.tag=validate > /dev/null
```

## Acceptance Tests

1. `helm lint deploy/helm/nebu/ --set gateway.image.tag=validate --set core.image.tag=validate` — returns exit code 0
2. `helm template nebu deploy/helm/nebu/ --set gateway.image.tag=validate --set core.image.tag=validate > /dev/null` — renders without error
