# ADR-012: Upstream Repo Contains Example-Only Credentials — No Official Deployments

## Status

Accepted — 2026-05-04

## Context

The upstream Nebu repository (innoq/nebu-chat) is a public reference implementation.
CI pipelines contain credentials for test infrastructure (PostgreSQL, Dex OIDC, gRPC PSK).

Two approaches exist for managing these CI credentials:

**Option A — GitLab CI/CD masked variables:**
Credentials are removed from YAML and stored in project settings (masked, not visible in logs).
Requires manual variable setup per project; contributors without project access cannot trigger CI;
forks lose the variables and cannot run integration tests out of the box.

**Option B — Example-only credentials in YAML, no-deployment policy:**
Credentials remain in YAML but are explicitly scoped to CI-only infrastructure (postgres sidecar,
dex sidecar) that is never reachable from outside the CI pod. The upstream repo declares a
no-deployment policy: it is a reference implementation, not an operational system.
Operators who deploy Nebu must fork the repository and supply their own credentials.

## Decision

**Option B.** The upstream repo is a reference implementation, not a production deployment.

All credentials in `.gitlab-ci.yml`, `dev/dex/config.yaml`, and `docker-compose.yml` are
example-only values. They are scoped to ephemeral CI service containers and local development
stacks that are not reachable from outside the process boundary.

Policy:
- No production or staging deployments are made from the upstream `innoq/nebu-chat` repository.
- Operators deploying Nebu must fork the repository and replace all credentials with their own.
- CI uses `POSTGRES_HOST_AUTH_METHOD: trust` for the postgres sidecar — no static password needed.
- The OIDC client secret, gRPC PSK, and other CI values carry a `ci-example-*` naming convention
  to signal their example-only status.

This policy is documented in the README (Epic 8, Story 8-2) and in the CONTRIBUTING guide.

## Consequences

**Positive:**
- CI configuration is self-contained and works in forks without additional setup.
- No operational overhead for secret rotation in the upstream repo.
- New contributors can run integration tests immediately after cloning.
- Clear separation: upstream = reference, downstream = operator responsibility.

**Negative:**
- Example credentials in the repository are visible to anyone with read access.
  Mitigated by: CI postgres is trust-auth (no real password); all other credentials
  only function inside an ephemeral CI pod.
- If an operator accidentally deploys from the upstream repo without changing credentials,
  their deployment uses example values. Mitigated by: deployment documentation (Story 8-2)
  explicitly calls this out.

_Replaces the deferred finding from code review of story 8-11 (2026-05-04)._
