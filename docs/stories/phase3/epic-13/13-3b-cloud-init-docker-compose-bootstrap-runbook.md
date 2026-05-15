---
status: done
epic: 13
story: 3b
security_review: optional
matrix: false
ui: false
---

# Story 13.3b: cloud-init Docker Compose Bootstrap + Runbook

Status: done

## Story

As a system operator,
I want a cloud-init script injected by OpenTofu that installs Docker, bootstraps `.secrets/`, and starts Nebu via `docker compose up -d` on first boot,
So that Nebu is operational on the Stackit VM without manual steps after `tofu apply`.

**Size:** S

---

## Acceptance Criteria

**AC1 — cloud-init installs Docker and starts Nebu:**
Given the cloud-init script embedded (via `templatefile()`) in `deploy/tofu/examples/stackit/main.tf`,
When the VM boots for the first time,
Then Docker and Docker Compose are installed, `.secrets/` contents are injected from OpenTofu `templatefile` variables (not hardcoded), and `docker compose up -d` starts automatically via a systemd unit

**AC2 — Services healthy after boot:**
Given `tofu apply` completes and the VM has finished its first boot,
When `docker compose ps` is run on the VM,
Then all Nebu services (gateway, core, postgres, keycloak) show as running and healthy

**AC3 — TLS endpoint reachable:**
Given the deployed VM with TLS configured via Stackit ALB,
When `curl https://<floating-ip>/_matrix/client/v3/versions` is called,
Then a valid Matrix versions response is returned (documented in RUNBOOK.md as verification command)

**AC4 — Secrets not hardcoded:**
Given the cloud-init template and any `.tfvars.example` files,
When they are inspected,
Then no actual secret values appear — only variable references (`${db_password}`) or placeholder comments

**AC5 — RUNBOOK.md covers Stackit day-2 operations:**
Given `deploy/tofu/examples/stackit/RUNBOOK.md`,
When it is read,
Then it covers: first deploy, update strategy (`docker compose pull && docker compose up -d --force-recreate`), Postgres volume backup (pg_dump to Stackit Object Storage), and teardown

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Infrastructure story — ATDD skipped (cloud-init + Docker Compose, no application logic).

**1. `make test-iac-validate` — Static validation**
- Given: `deploy/tofu/examples/stackit/main.tf` with `templatefile()` call for cloud-init
- When: `tofu validate` runs
- Then: exit 0 (templatefile syntax is valid)

**2. Secrets gate — no hardcoded values**
- Given: cloud-init template file `deploy/tofu/examples/stackit/cloud-init.tftpl`
- When: inspected for literal passwords or secrets
- Then: only `${variable}` references appear — no hardcoded values

Note: `security_review: optional` because cloud-init handles secret injection; reviewer should check for secret leakage in template variables.
