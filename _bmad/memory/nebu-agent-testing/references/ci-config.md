---
name: ci-config
code: ci-config
description: Manage the GitLab CI configuration — keep .gitlab-ci.yml accurate, add new jobs for new test types, and verify the pipeline matches the local Makefile targets.
---

# CI Config Management

## What Success Looks Like

The `.gitlab-ci.yml` accurately reflects what `make` targets exist locally. New test suites added to the Makefile have corresponding CI jobs. The CI pipeline mirrors the local CI gate order.

## CI Gate Order (canonical)

The GitLab CI pipeline MUST run stages in this order, matching the local CI gate:

1. `build` — `make build-gateway && make build-core`
2. `test:unit:go` — `make test-unit-go`
3. `test:unit:elixir` — `make test-unit-elixir`
4. `test:e2e` — `make test-e2e` (requires service dependencies)
5. `test:integration` — `make test-integration` (requires service dependencies)

## When to Update

- New Makefile target added → add corresponding CI job
- Service dependency changed (new Docker service, port change) → update CI service definitions
- New test type introduced (e.g., contract tests, performance tests) → add stage and job
- CI job consistently times out → adjust `timeout` setting

## Validation

After any `.gitlab-ci.yml` change:
1. Lint the file: `docker run --rm -v "$PWD":/repo -w /repo gitlab/gitlab-runner:latest gitlab-runner lint .gitlab-ci.yml`
2. Verify stage order matches canonical order above
3. Verify all `make` targets referenced actually exist in the Makefile

## Output

Report changes made to `.gitlab-ci.yml` with brief rationale for each change.
