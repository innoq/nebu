---
status: review
epic: 13
story: 9
security_review: required
matrix: false
ui: false
atdd: not-applicable
---

# Story 13.9: OIDC Deployment Profile — Remove Keycloak, add oidc_mode variable

Status: review

## Story

As a system operator,
I want the Stackit deployment to choose between a bundled Dex IdP sidecar (`oidc_mode = "dex"`) or an externally-provided OIDC provider (`oidc_mode = "external"`),
so that I can use a lightweight, zero-database identity provider for test environments and skip the bundled IdP entirely for production deployments that use an external OIDC provider.

---

## Background

The current Stackit deployment (`deploy/tofu/examples/stackit/`) unconditionally deploys Keycloak as a sidecar Docker Compose service **and** creates two dedicated Keycloak PostgresFlex resources (`stackit_postgresflex_user.keycloak`, `stackit_postgresflex_database.keycloak`). This design has two problems:

1. **Operators without a Keycloak requirement pay for a PostgresFlex user + database they never use.**
2. **Keycloak is heavyweight** — it requires a database, significant startup RAM (~512 MB), and non-trivial configuration. For a test or demo environment, [Dex IdP](https://dexidp.io/) is a far lighter alternative: it uses a static YAML config file (no database), boots in < 2 seconds, and is the IdP used in Nebu's own `make dev` stack.

This story replaces Keycloak with a two-profile model:

| `oidc_mode` | Behaviour |
|---|---|
| `"dex"` | Dex sidecar is deployed via Docker Compose; config file is written to `/opt/nebu/dex/config.yaml`. `oidc_issuer` is auto-derived as `https://<server_name>/dex`. No Keycloak. No Keycloak DB. |
| `"external"` | No bundled IdP deployed. Operator supplies `oidc_issuer` and `oidc_client_secret` via `terraform.tfvars`. No Keycloak. No Dex. No Keycloak DB. |

**Keycloak resources (`stackit_postgresflex_user.keycloak`, `stackit_postgresflex_database.keycloak`) are removed in both profiles.** Dex needs no database.

---

## Acceptance Criteria

**AC1 — `oidc_mode` variable exists in `variables.tf`:**
Given `deploy/tofu/examples/stackit/variables.tf`,
When it is inspected,
Then:
- `variable "oidc_mode"` is present with `type = string`, `default = "external"`, and a `validation` block enforcing `contains(["dex", "external"], var.oidc_mode)`
- The description explains the two profiles clearly

**AC2 — Keycloak PostgresFlex resources are fully removed from `main.tf`:**
Given `deploy/tofu/examples/stackit/main.tf`,
When it is inspected,
Then:
- `stackit_postgresflex_user.keycloak` is gone
- `stackit_postgresflex_database.keycloak` is gone
- No references to `keycloak` remain in any resource definition

**AC3 — Keycloak container block removed from `cloud-init.tftpl`:**
Given `deploy/tofu/examples/stackit/cloud-init.tftpl`,
When the rendered docker-compose section is inspected,
Then:
- The `keycloak:` service block is completely removed
- `KC_DB_PASSWORD=${kc_password}` is removed from `.env`
- `kc_user` and `kc_password` template variables no longer exist

**AC4 — Dex sidecar injected when `oidc_mode = "dex"`:**
Given `deploy/tofu/examples/stackit/cloud-init.tftpl` rendered with `oidc_mode = "dex"`,
When the rendered docker-compose section is inspected,
Then:
- A `dex:` service block is present using image `dexidp/dex:v2.45.1`
- `/opt/nebu/dex/config.yaml` is written via `write_files` with correct Dex static-client configuration for `nebu-gateway`
- `oidc_issuer` in `.env` is set to `http://${server_name}:5556/dex`

**AC5 — No IdP service when `oidc_mode = "external"`:**
Given `deploy/tofu/examples/stackit/cloud-init.tftpl` rendered with `oidc_mode = "external"`,
When the rendered docker-compose section is inspected,
Then:
- No `dex:` service block is present
- No `keycloak:` service block is present
- `oidc_issuer` in `.env` is set to the operator-provided `${oidc_issuer}` value

**AC6 — `main.tf` `templatefile()` call updated — `kc_user`/`kc_password` removed, `oidc_mode` added:**
Given `deploy/tofu/examples/stackit/main.tf` `stackit_server.nebu` resource,
When the `templatefile()` call is inspected,
Then:
- `kc_user` and `kc_password` are not passed
- `oidc_mode = var.oidc_mode` is passed
- `oidc_issuer` logic: when `oidc_mode = "dex"`, `oidc_issuer` is auto-set to `"http://${var.server_name}:5556/dex"` (computed in `locals {}` or inline); when `oidc_mode = "external"`, `oidc_issuer = var.oidc_issuer` is used

**AC7 — `terraform.tfvars.example` documents both profiles:**
Given `deploy/tofu/examples/stackit/terraform.tfvars.example`,
When it is inspected,
Then:
- `oidc_mode` is documented with two commented examples — one for `"dex"` and one for `"external"`
- The `oidc_issuer` entry is marked as "required only when `oidc_mode = external`"
- `oidc_client_secret` entry is updated similarly

**AC8 — RUNBOOK.md updated for both profiles:**
Given `deploy/tofu/examples/stackit/RUNBOOK.md`,
When the "OIDC Profiles" section (new) is read,
Then it documents:
- How to choose between `dex` and `external`
- For `dex`: expected `docker compose ps` output (shows `dex` service), where the Dex config file lives, and how to confirm Dex is the issuer
- For `external`: what `oidc_issuer` and `oidc_client_secret` to provide and what Bootstrap Wizard step configures the external IdP

**AC9 — `tofu validate` passes on the Stackit example:**
Given `deploy/tofu/examples/stackit/`,
When `tofu validate` runs (with `tofu init -backend=false` first),
Then exit code is 0

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Infrastructure-only story — ATDD is not applicable. There is no application logic, Matrix API, or observable UI behavior. The acceptance gates are OpenTofu static validation and the file-content inspection checks below.

**1. `tofu validate` — Stackit example with `oidc_mode = "dex"` (static schema check)**
- Given: updated `deploy/tofu/examples/stackit/` with the new `oidc_mode` variable
- When: `docker run --rm -v $(pwd)/deploy/tofu/examples/stackit:/workspace -w /workspace ghcr.io/opentofu/opentofu:1.9 sh -c "tofu init -backend=false && tofu validate"`
- Then: exit code 0, no schema errors

**2. `tofu validate` — Stackit example with `oidc_mode = "external"` (no extra template vars)**
- Given: `terraform.tfvars.override` setting `oidc_mode = "external"` (or just rely on default)
- When: same `tofu validate` command
- Then: exit code 0

**3. File inspection — no Keycloak in `main.tf`**
- Given: updated `main.tf`
- When: `grep -c "keycloak" deploy/tofu/examples/stackit/main.tf`
- Then: output is `0`

**4. File inspection — no `kc_user`/`kc_password` in `cloud-init.tftpl`**
- Given: updated `cloud-init.tftpl`
- When: `grep -E "kc_user|kc_password|KC_DB_PASSWORD|keycloak" deploy/tofu/examples/stackit/cloud-init.tftpl`
- Then: output is empty (zero matches)

**5. File inspection — Dex block present in template when conditionally included**
- Given: the `cloud-init.tftpl` template source (before rendering)
- When: `grep -c "dexidp/dex\|dex:" deploy/tofu/examples/stackit/cloud-init.tftpl`
- Then: output > 0 (Dex service block is in the template, guarded by conditional)

**6. File inspection — `oidc_mode` variable with validation in `variables.tf`**
- Given: updated `variables.tf`
- When: `grep -A 5 "variable \"oidc_mode\"" deploy/tofu/examples/stackit/variables.tf`
- Then: output contains `validation` and `contains(["dex", "external"]`

**7. Security gate — no hardcoded secrets or `kc_*` vars in `.env` block**
- Given: updated `cloud-init.tftpl`
- When: inspect the `.env` `write_files` block
- Then: only `${variable_name}` template references; no literals; no `kc_*` variables

---

## Dev Notes — Implementation Guide

### File Change Map

| File | Action | Notes |
|---|---|---|
| `deploy/tofu/examples/stackit/variables.tf` | UPDATE | Add `oidc_mode` variable; adjust `oidc_issuer` description (optional when `oidc_mode = "dex"`) |
| `deploy/tofu/examples/stackit/main.tf` | UPDATE | Remove Keycloak user/DB resources; add `locals {}` for computed `oidc_issuer`; update `templatefile()` call |
| `deploy/tofu/examples/stackit/cloud-init.tftpl` | UPDATE | Remove Keycloak service block; remove `kc_user`/`kc_password` from `.env`; add conditional Dex block |
| `deploy/tofu/examples/stackit/terraform.tfvars.example` | UPDATE | Document both profiles; mark `oidc_issuer` as optional for `dex` mode |
| `deploy/tofu/examples/stackit/RUNBOOK.md` | UPDATE | Add "OIDC Profiles" section |
| `deploy/tofu/examples/stackit/outputs.tf` | NO CHANGE | No Keycloak outputs exist; no new outputs needed |

### 1. `variables.tf` Changes

**Add `oidc_mode` variable** (add after `oidc_client_secret`):

```hcl
variable "oidc_mode" {
  description = "OIDC deployment profile. 'dex': deploy Dex as a sidecar (static config, no database — for test/demo environments). 'external': no bundled IdP; operator must provide oidc_issuer and oidc_client_secret (for production with a managed OIDC provider)."
  type        = string
  default     = "external"

  validation {
    condition     = contains(["dex", "external"], var.oidc_mode)
    error_message = "oidc_mode must be 'dex' or 'external'."
  }
}
```

**Update `oidc_issuer` variable description** — clarify it is required only for `oidc_mode = "external"`:

```hcl
variable "oidc_issuer" {
  description = "OIDC issuer URL. Required when oidc_mode = 'external' (e.g. 'https://auth.example.com/realms/nebu'). When oidc_mode = 'dex', this value is ignored — the issuer is automatically set to 'https://<server_name>/dex'."
  type        = string
  default     = ""  # Empty default enables `tofu validate` without providing a value
}
```

**Update `oidc_client_secret` description** and relax validation for `dex` mode:

```hcl
variable "oidc_client_secret" {
  description = "OIDC client secret for the nebu-gateway application. Required when oidc_mode = 'external'. When oidc_mode = 'dex', a static secret is embedded in the Dex config — set this to any non-empty string (the static Dex secret is used instead)."
  type        = string
  sensitive   = true

  validation {
    condition     = length(var.oidc_client_secret) >= 16
    error_message = "oidc_client_secret must be at least 16 characters."
  }
}
```

### 2. `main.tf` Changes

**Remove Keycloak resources** — delete these two resource blocks entirely:
- `resource "stackit_postgresflex_user" "keycloak"` (lines 200–205 in current file)
- `resource "stackit_postgresflex_database" "keycloak"` (lines 214–219 in current file)

**Add `locals {}` block** for computed `oidc_issuer` (place it BEFORE the `stackit_server.nebu` resource):

```hcl
locals {
  # When oidc_mode = "dex", the issuer is the Dex sidecar running under the server's public domain.
  # When oidc_mode = "external", the operator provides the issuer explicitly.
  effective_oidc_issuer = var.oidc_mode == "dex" ? "https://${var.server_name}/dex" : var.oidc_issuer
}
```

**Update `templatefile()` call** in `stackit_server.nebu.user_data`:

```hcl
user_data = base64encode(templatefile("${path.module}/cloud-init.tftpl", {
  internal_secret    = var.internal_secret
  oidc_client_secret = var.oidc_client_secret
  oidc_issuer        = local.effective_oidc_issuer
  oidc_mode          = var.oidc_mode
  server_name        = var.server_name
  image_registry     = var.image_registry
  nebu_version       = var.nebu_version
  # PostgresFlex connection details for the Nebu application user
  pg_host     = stackit_postgresflex_user.nebu.host
  pg_port     = stackit_postgresflex_user.nebu.port
  pg_user     = stackit_postgresflex_user.nebu.username
  pg_password = stackit_postgresflex_user.nebu.password
  # kc_user and kc_password are removed — Keycloak is no longer deployed
}))
```

Note: remove the `kc_user` and `kc_password` lines from the current `templatefile()` call (lines 161–162 in current `main.tf`).

### 3. `cloud-init.tftpl` Changes

The template uses OpenTofu's `templatefile()` function, which supports Go template syntax. Conditional blocks use `%{ if condition }...%{ else }...%{ endif }` directives.

#### 3a. Remove from `.env` `write_files` block:

Remove this line from the `.env` `content`:
```
KC_DB_PASSWORD=${kc_password}
```

The updated `.env` block content should be:
```
NEBU_DB_URL=postgresql://${pg_user}:${pg_password}@${pg_host}:${pg_port}/nebu
NEBU_DB_URL_MIGRATE=postgresql://${pg_user}:${pg_password}@${pg_host}:${pg_port}/nebu
NEBU_OIDC_ISSUER=${oidc_issuer}
NEBU_OIDC_CLIENT_SECRET=${oidc_client_secret}
NEBU_SERVER_NAME=${server_name}
NEBU_IMAGE_REGISTRY=${image_registry}
NEBU_VERSION=${nebu_version}
```

#### 3b. Add Dex config `write_files` entry (conditionally):

Place this `write_files` entry immediately after the `.env` entry and before the `docker-compose.yml` entry:

```yaml
%{ if oidc_mode == "dex" }
  # ── /opt/nebu/dex/config.yaml ──────────────────────────────────────────────
  # Dex static configuration — no database required.
  # Static client: nebu-gateway with the oidc_client_secret from tfvars.
  # Static password: operator@example.com / changeme (replace before going live).
  - path: /opt/nebu/dex/config.yaml
    owner: root:root
    permissions: "0600"
    content: |
      issuer: https://${server_name}/dex

      storage:
        type: memory

      web:
        http: 0.0.0.0:5556

      oauth2:
        skipApprovalScreen: true

      oauth2:
        responseTypes: [code]
        skipApprovalScreen: true
        grantTypes:
          - authorization_code
          - refresh_token

      enablePasswordDB: true

      staticClients:
        - id: nebu-gateway
          secret: ${oidc_client_secret}
          redirectURIs:
            - https://${server_name}/_matrix/client/v3/login/sso/redirect/oidc
          name: Nebu Gateway

      staticPasswords:
        - email: "operator@example.com"
          hash: "$2a$10$E4ye88CWSgoigClMVojmGOu.gHlKe7L7RRf07QWZ60aZmZ7Rfak/6"
          username: "operator"
          userID: "operator-00000000-0000-0000-0000-000000000001"
          groups:
            - instance_admin
%{ endif }
```

> **Note on bcrypt hash:** The hash above (`$2a$10$E4ye88CWS...`) is the same hash used in `dev/dex/config.yaml` — it corresponds to a known dev password. Operators MUST replace this with a new bcrypt hash before using this profile in any shared environment. Generate a new hash with: `htpasswd -ynBC 10 "" | tr -d ':\n'` or `python3 -c "import bcrypt; print(bcrypt.hashpw(b'YourPassword', bcrypt.gensalt()).decode())"`. Document this requirement in RUNBOOK.

#### 3c. Replace the `keycloak:` service block in docker-compose with conditional Dex block:

Remove the entire `keycloak:` service from the `docker-compose.yml` `write_files` content:

```yaml
        keycloak:
          image: quay.io/keycloak/keycloak:24.0
          restart: unless-stopped
          command: ["start-optimized"]
          environment:
            KC_DB: postgres
            KC_DB_URL: jdbc:postgresql://${pg_host}:${pg_port}/keycloak
            KC_DB_USERNAME: ${kc_user}
            KC_DB_PASSWORD: $${KC_DB_PASSWORD}
            KC_HOSTNAME: $${NEBU_SERVER_NAME}
          env_file:
            - .env
```

Replace with this conditional Dex block (using Go template `%{ if }` directives inside YAML):

```yaml
%{ if oidc_mode == "dex" }
        dex:
          image: dexidp/dex:v2.45.1
          restart: unless-stopped
          command: ["dex", "serve", "/etc/dex/config.yaml"]
          volumes:
            - /opt/nebu/dex/config.yaml:/etc/dex/config.yaml:ro
          ports:
            - "5556:5556"
          healthcheck:
            test: ["CMD", "wget", "-qO-", "http://localhost:5556/dex/.well-known/openid-configuration"]
            interval: 10s
            timeout: 5s
            retries: 3
            start_period: 10s
%{ endif }
```

> **YAML indentation warning:** The `%{ if }` / `%{ endif }` directives in `templatefile()` must be placed at the correct indentation level in the source template. The content between the directives is emitted as-is. Align the `dex:` key at the same 8-space indentation as `core:` and `gateway:` in the services block.

#### 3d. YAML indentation caution for `templatefile()`

OpenTofu `templatefile()` uses Go's `text/template`. The `%{` directives are stripped from the output but the **surrounding whitespace is preserved**. When wrapping a YAML block in `%{ if }...%{ endif }`, ensure:

- The `%{ if oidc_mode == "dex" }` line itself is on its own line at column 0 (or flush with surrounding whitespace — choose consistently)
- The YAML content inside the conditional has the correct indentation for the docker-compose services map (8 spaces for service-level keys under `services:`)

A tested pattern that works cleanly with `templatefile()`:

```
      services:
%{ if oidc_mode == "dex" ~}
        dex:
          image: dexidp/dex:v2.45.1
          ...
%{ endif ~}
        core:
          ...
```

The `~` after `%{...}` trims the newline produced by the directive itself — preventing blank lines in the rendered output.

### 4. `terraform.tfvars.example` Changes

Add the OIDC profile section near the `oidc_issuer` block:

```hcl
# ── OIDC Profile ────────────────────────────────────────────────────────────
# Choose 'dex' to deploy a bundled Dex IdP sidecar (test/demo environments).
# Choose 'external' to use an external OIDC provider you manage (production).
oidc_mode = "external"  # or: "dex"

# Required only when oidc_mode = "external".
# When oidc_mode = "dex", this value is ignored (issuer = https://<server_name>/dex).
oidc_issuer = "https://auth.example.com/realms/nebu"

# Required: OIDC client secret.
# When oidc_mode = "dex": used as the static Dex client secret — any 16+ char value.
# When oidc_mode = "external": must match the secret configured in your OIDC provider.
oidc_client_secret = "<REPLACE_WITH_OIDC_CLIENT_SECRET>"
```

Remove the existing Keycloak-specific comments from `oidc_client_secret` (currently says "Keycloak / Dex").

### 5. RUNBOOK.md Changes

Add a new "OIDC Profiles" section after "Prerequisites":

```markdown
## OIDC Profiles

Nebu on Stackit supports two OIDC deployment modes, configured via `oidc_mode` in `terraform.tfvars`.

### `oidc_mode = "dex"` — Bundled Dex (test/demo)

Deploys [Dex IdP](https://dexidp.io/) as a Docker Compose sidecar alongside Nebu. Dex uses a static YAML configuration — no database is required.

**When to use:** Development, demo, or integration test environments where you do not have an external OIDC provider.

**What gets deployed:**

```bash
sudo docker compose ps
# NAME    IMAGE                       STATUS
# dex     dexidp/dex:v2.45.1 Up (healthy)
# core    <registry>/nebu-core:...    Up (healthy)
# gateway <registry>/nebu-gateway:... Up (healthy)
```

The Dex config file is at `/opt/nebu/dex/config.yaml`. It contains a static OIDC client (`nebu-gateway`) and a static user (`operator@example.com` — in the `instance_admin` group).

> **Security:** The default static password hash in the template is the same hash used in `dev/dex/config.yaml` (a known dev password). Replace the `hash` in `/opt/nebu/dex/config.yaml` with a new bcrypt hash before using this profile in any shared environment. Generate a new hash with: `htpasswd -ynBC 10 "" | tr -d ':\n'`

The OIDC issuer is automatically set to `https://<server_name>/dex`.

### `oidc_mode = "external"` — External OIDC Provider (production)

No bundled IdP is deployed. You must provide:
- `oidc_issuer` — the OIDC issuer URL of your external provider (Keycloak, Authentik, Azure AD, etc.)
- `oidc_client_secret` — the client secret issued to `nebu-gateway` by your provider

**When to use:** Production deployments where you already manage an OIDC provider or use a cloud IdP service.

Configure the external IdP client using the Nebu Bootstrap Wizard (step 3 — OIDC configuration), then copy the issued client secret into `terraform.tfvars` as `oidc_client_secret`.
```

Also update the "Checking VM Boot Status" section to remove the Keycloak line and conditionally mention Dex:

```markdown
Expected output (oidc_mode = "dex"):
NAME       IMAGE                           STATUS
dex        dexidp/dex:v2.45.1     Up (healthy)
core       <registry>/nebu-core:<ver>      Up (healthy)
gateway    <registry>/nebu-gateway:<ver>   Up (healthy)

Expected output (oidc_mode = "external"):
NAME       IMAGE                           STATUS
core       <registry>/nebu-core:<ver>      Up (healthy)
gateway    <registry>/nebu-gateway:<ver>   Up (healthy)
```

### 6. Dex Image Version

**Use `dexidp/dex:v2.45.1`** — this is the exact version already used in the Nebu `make dev` stack (root `docker-compose.yml`, confirmed via `grep`). The dev stack uses Docker Hub (`dexidp/dex`) — not `ghcr.io/dexidp/dex`. Use the same image reference for consistency.

Confirmed version:
```
# docker-compose.yml (repo root) — line: image: dexidp/dex:v2.45.1
```

**Never use `:latest` — always use a pinned tag.**

### 7. Security Considerations (why `security_review: required`)

1. **`oidc_client_secret` passed into Dex config file as plaintext** — The file is written to `/opt/nebu/dex/config.yaml` with permissions `0600`. It must not be world-readable.
2. **Static password hash in Dex config** — The default `changeme` hash must be documented as "REPLACE BEFORE PRODUCTION" in both the template comment and RUNBOOK. The hash must use bcrypt (already the case with `$2a$10$...` prefix).
3. **`oidc_client_secret` validation** — The existing 16-character minimum validation on `oidc_client_secret` must be preserved. Do not weaken it for `dex` mode.
4. **`kc_password` in state** — After this story, `stackit_postgresflex_user.keycloak` is removed. The Keycloak user and database are destroyed on next `tofu apply`. Operators should confirm the keycloak DB is no longer needed before destroying it (no keycloak data migration needed if Keycloak was never fully set up — this is the expected state since the story precedes a real Keycloak rollout).
5. **No Keycloak in `.env`** — After the change, `KC_DB_PASSWORD` must not appear in `.env`. The secret review must grep for it: `grep KC_DB_PASSWORD deploy/tofu/examples/stackit/cloud-init.tftpl` → zero matches.

### 8. Impact Analysis — What Must NOT Break

| Invariant | Verification |
|---|---|
| `nebu.service` systemd unit unchanged | No modifications to the `[Service]` section of the unit file in `cloud-init.tftpl` |
| `internal_secret` injection via `/opt/nebu/.secrets/internal_secret` unchanged | Keep the `write_files` block for `internal_secret` as-is |
| PostgresFlex `nebu` user + database unchanged | `stackit_postgresflex_user.nebu` and `stackit_postgresflex_database.nebu` remain; only `keycloak` variants are removed |
| `core` and `gateway` services unchanged | No modifications to core/gateway service blocks beyond removing `depends_on` from keycloak (which was never there — only postgres had it) |
| `oidc_issuer` in `.env` still references a valid issuer | When `oidc_mode = "dex"`: `local.effective_oidc_issuer = "https://${var.server_name}/dex"` must be computed before it reaches the template |
| `tofu validate` still exits 0 | Run after every change; see Section 9 below |

### 9. `tofu validate` Workflow

Since no local OpenTofu installation is assumed (per CLAUDE.md), use the Docker-based validation:

```bash
# Stackit example
docker run --rm \
  -v $(pwd)/deploy/tofu/examples/stackit:/workspace \
  -w /workspace \
  ghcr.io/opentofu/opentofu:1.9 \
  sh -c "tofu init -backend=false && tofu validate"
```

The `init -backend=false` flag skips backend initialization (state bucket credentials not needed for validate).

If the Docker image tag `1.9` is not available, use `latest` or `1.8`.

### 10. Pattern Consistency

Follow the patterns already established in the Stackit IaC files:

- Resource names: `"nebu-${var.environment}-<type>"`
- Section separators: `# ── Section Name ──────────────────────────────────────`
- Variable descriptions: full sentences, trailing period
- `sensitive = true` on all credential variables
- `templatefile()` directives use `%{...~}` to trim trailing newlines (prevents empty lines in rendered YAML)
- All `write_files` content entries have `owner: root:root` and restrictive `permissions`

---

## Tasks / Subtasks

- [x] Add `oidc_mode` variable to `variables.tf`; update `oidc_issuer` and `oidc_client_secret` descriptions (AC1)
  - [x] Validation block: `contains(["dex", "external"], var.oidc_mode)`
  - [x] `oidc_issuer` default `""` with updated description (optional for `dex` mode)
- [x] Remove Keycloak PostgresFlex resources from `main.tf` (AC2)
  - [x] Delete `stackit_postgresflex_user.keycloak`
  - [x] Delete `stackit_postgresflex_database.keycloak`
  - [x] Add `locals {}` block for `effective_oidc_issuer`
  - [x] Update `templatefile()` call: add `oidc_mode`; replace hardcoded `oidc_issuer` with `local.effective_oidc_issuer`; remove `kc_user`/`kc_password` (AC6)
- [x] Update `cloud-init.tftpl` (AC3, AC4, AC5)
  - [x] Remove `KC_DB_PASSWORD=${kc_password}` from `.env` block
  - [x] Add conditional `write_files` entry for `/opt/nebu/dex/config.yaml`
  - [x] Remove `keycloak:` service block from docker-compose
  - [x] Add conditional `dex:` service block (guarded by `%{ if oidc_mode == "dex" ~}`)
- [x] Update `terraform.tfvars.example` (AC7)
  - [x] Add `oidc_mode` examples for both profiles
  - [x] Clarify conditional nature of `oidc_issuer` and `oidc_client_secret`
- [x] Update `RUNBOOK.md` (AC8)
  - [x] Add "OIDC Profiles" section
  - [x] Update expected `docker compose ps` output for both profiles
- [x] Run `tofu validate` (AC9)
  - [x] Fix any validation errors before marking story done

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

_No blockers encountered._

### Completion Notes List

- Removed `stackit_postgresflex_user.keycloak` and `stackit_postgresflex_database.keycloak` from `main.tf` — both resources deleted entirely.
- Added `locals { effective_oidc_issuer }` block in `main.tf` that computes the issuer from `server_name` when `oidc_mode = "dex"`, or passes `var.oidc_issuer` for `oidc_mode = "external"`.
- Updated `templatefile()` call: removed `kc_user`/`kc_password`, added `oidc_mode`, replaced `var.oidc_issuer` with `local.effective_oidc_issuer`.
- `variables.tf`: added `oidc_mode` variable with `contains(["dex","external"])` validation and `default = "external"`; updated `oidc_issuer` to have `default = ""` with clarified description; updated `oidc_client_secret` description to reflect dex vs external semantics.
- `cloud-init.tftpl`: removed `KC_DB_PASSWORD=${kc_password}` from `.env`; added conditional `%{ if oidc_mode == "dex" ~}` Dex config `write_files` entry (`/opt/nebu/dex/config.yaml`, permissions `0600`); removed `keycloak:` service block; added conditional `dex:` service block with `dexidp/dex:v2.45.1`; added `mkdir -p /opt/nebu/dex` in runcmd.
- `terraform.tfvars.example`: added OIDC Profile section documenting both modes; reorganised `oidc_issuer` and `oidc_client_secret` to be adjacent with clear conditionality comments.
- `RUNBOOK.md`: added "OIDC Profiles" section describing dex and external modes; updated "Checking VM Boot Status" expected output for both profiles.
- `make test-iac-validate` passes (exit code 0): `tofu fmt -check`, `tofu validate` for all three examples (aws, stackit, k8s), Helm lint, k6 inspect.

**Review cycle 1 fixes (2026-05-13):**
- CRITICAL-1: Added nginx reverse-proxy sidecar (`nginx:1.27-alpine`) in `cloud-init.tftpl` — conditionally included only when `oidc_mode == "dex"`. nginx listens on host port 8008 and routes `/dex/` to `dex:5556` (docker network) and all other paths to `gateway:8008` (docker network). Gateway's host `ports:` binding is conditionally removed in dex mode (gateway only accessible via docker network). ALB target port remains 8008 — no ALB config change needed.
- CRITICAL-1: Added `write_files` entry for `/opt/nebu/nginx/dex.conf` with nginx location blocks for path-based routing.
- CRITICAL-1: Added `mkdir -p /opt/nebu/nginx` in runcmd (conditional on `oidc_mode == "dex"`).
- CRITICAL-1: Updated `main.tf` with comment documenting ALB → nginx → gateway/dex traffic flow for dex mode.
- CRITICAL-1: Updated `RUNBOOK.md` "OIDC Profiles" section to document nginx routing, `docker compose ps` output with nginx service, and `curl` verification commands (now working via nginx proxy).
- MAJOR-1: Added `dex_static_password_hash` variable to `variables.tf` (sensitive, default null, bcrypt validation). Replaced hardcoded hash in `cloud-init.tftpl` with `${dex_static_password_hash}`. Added precondition on `stackit_server.nebu` resource: `oidc_mode != "dex" || dex_static_password_hash != null`. Updated `templatefile()` call in `main.tf` to pass `dex_static_password_hash`. Updated `terraform.tfvars.example` with generation command. Replaced plain-password comment with `# Static user: operator@example.com — see RUNBOOK.md 'OIDC Profiles' to rotate the password hash.`
- MAJOR-2: Added precondition on `stackit_server.nebu`: `oidc_mode == "dex" || length(oidc_issuer) > 0` with error "oidc_issuer must be set when oidc_mode = 'external'."
- MAJOR-3: Updated `oidc_client_secret` variable description in `variables.tf` to the prescribed text.
- MINOR-2: Quoted `${oidc_client_secret}`, `${server_name}`, and `${oidc_static_password_hash}` in Dex YAML config inside `cloud-init.tftpl`.
- MINOR-3: Used `%{~ if }` / `%{ endif ~}` pattern consistently throughout `cloud-init.tftpl` (right-trim on `endif`, left-trim on `if` where structurally safe; avoided joined-line issues in YAML list contexts).
- MINOR-5: Wrapped `mkdir -p /opt/nebu/dex` and `mkdir -p /opt/nebu/nginx` in `%{ if oidc_mode == "dex" ~}` conditional in runcmd section.

**Review cycle 2 fixes (2026-05-13):**
- MAJOR-1 (hairpin TLS): Changed `effective_oidc_issuer` for dex mode from `https://${var.server_name}/dex` to `http://dex:5556/dex` in `main.tf` locals. Changed Dex `issuer` in `cloud-init.tftpl` dex config from `https://${server_name}/dex` to `http://${server_name}:5556/dex`. The gateway fetches OIDC discovery over the Docker-internal network (no TLS hairpin through ALB). Browser OIDC redirects go to `http://<server_name>:5556/dex/auth`. Added conditional SG rule `inbound_dex` (port 5556, count = `oidc_mode == "dex" ? 1 : 0`) in `main.tf`. Updated RUNBOOK.md OIDC Profiles section to document HTTP (not HTTPS) for Dex. Updated `oidc_issuer` variable description to reflect `http://<server_name>:5556/dex`.
- MAJOR-2 (nginx healthcheck): Added `healthcheck` block to the `nginx` service in `cloud-init.tftpl` docker-compose section (wget on `/_matrix/client/v3/versions`, 10s interval, 5s timeout, 3 retries, 10s start_period).
- MAJOR-3 (missing /admin/callback redirect URI): Added `https://${server_name}/admin/callback` to `staticClients[nebu-gateway].redirectURIs` in `cloud-init.tftpl` Dex config.
- MINOR-1 (RUNBOOK docker compose ps missing nginx): Added `nginx nginx:1.27-alpine Up (healthy)` row to the dex-mode expected output table.
- MINOR-2 (RUNBOOK TLS note): Replaced misleading `-k` suggestion with accurate explanation that dex port 5556 is plain HTTP; Matrix API on port 8008 routes through nginx (plain HTTP on VM); TLS termination at ALB or gateway.
- MINOR-4 (trim direction on line 42): Changed `%{~ if oidc_mode == "dex" }` to `%{ if oidc_mode == "dex" ~}`.
- MINOR-5 (negative comparison): Changed `%{ if oidc_mode != "dex" ~}` to `%{ if oidc_mode == "external" ~}`.
- MINOR-6 (oidc_client_secret YAML chars): Added validation block to `oidc_client_secret` in `variables.tf` rejecting `"` and `\` characters.
- MINOR-7 (duplicate comment): Removed duplicate `# dex_static_password_hash = "$2a$12$..."` example line from `terraform.tfvars.example`; kept only `<REPLACE_WITH_BCRYPT_HASH>` placeholder.
- `make test-iac-validate` passes (exit code 0).

**Review cycle 3 fixes (2026-05-13):**
- Removed nginx entirely from `cloud-init.tftpl`: deleted nginx `write_files` block (`/opt/nebu/nginx/dex.conf`), deleted nginx service from docker-compose, removed `mkdir -p /opt/nebu/nginx` from runcmd. nginx is no longer deployed in dex mode.
- Fixed `effective_oidc_issuer` in `main.tf` locals: changed from `"http://dex:5556/dex"` (Docker-internal) to `"http://${var.server_name}:5556/dex"` (public VM address). Both `effective_oidc_issuer` and Dex `issuer:` in cloud-init now use the same `http://<server_name>:5556/dex` URL, ensuring go-oidc issuer comparison passes.
- Fixed redirect URIs in Dex `staticClients`: changed from `https://${server_name}/_matrix/...` and `https://${server_name}/admin/callback` to `http://${server_name}:8008/_matrix/client/v3/login/sso/redirect/oidc` and `http://${server_name}:8008/admin/callback`. Gateway speaks plain HTTP on port 8008 — no TLS in dex mode.
- Gateway now binds port `8008:8008` unconditionally (both modes). The conditional `%{ if oidc_mode == "external" ~}` ports block is removed; ports are now always present on the gateway service.
- Added `ports: "5556:5556"` to dex service (already had it from cycle 1 — retained).
- SG rule `inbound_dex` (port 5556, conditional on dex mode) kept from cycle 2.
- Updated `main.tf` traffic flow comment to describe hairpin routing (no nginx).
- Updated `main.tf` locals comment to document hairpin behaviour and add TODO for future nginx host-based routing.
- Updated RUNBOOK.md: removed all nginx operational references; updated issuer URL to `http://<server_name>:5556/dex`; updated `docker compose ps` expected output for dex mode (dex+core+gateway, no nginx row); updated smoke test curl commands to use `http://<server_name>:5556/dex/...` and `http://<server_name>:8008/...`; removed misleading `-k` TLS note; added TODO note for future nginx host-based routing.
- Updated `terraform.tfvars.example`: fixed comment for `oidc_issuer` (was `https://<server_name>/dex`, now `http://<server_name>:5556/dex`); uncommented `dex_static_password_hash` example line (MINOR-2 fix).
- `make test-iac-validate` passes (exit code 0).

### File List

- `deploy/tofu/examples/stackit/variables.tf`
- `deploy/tofu/examples/stackit/main.tf`
- `deploy/tofu/examples/stackit/cloud-init.tftpl`
- `deploy/tofu/examples/stackit/terraform.tfvars.example`
- `deploy/tofu/examples/stackit/RUNBOOK.md`

## Change Log

| Date | Change |
|---|---|
| 2026-05-13 | Implemented story 13-9: replaced Keycloak with two-profile OIDC model (`dex`/`external`); removed Keycloak PostgresFlex resources; added conditional Dex sidecar in cloud-init; `tofu validate` passes. |
| 2026-05-13 | Review cycle 1 fixes: CRITICAL-1 nginx sidecar (path routing for dex mode); MAJOR-1 dex_static_password_hash variable (removes hardcoded bcrypt hash); MAJOR-2 cross-variable oidc_issuer validation precondition; MAJOR-3 oidc_client_secret description; MINOR-2 quote template vars in Dex YAML; MINOR-3 consistent template trimming; MINOR-5 conditional mkdir for dex dirs. `make test-iac-validate` passes (exit 0). |
| 2026-05-13 | Review cycle 2 fixes: MAJOR-1 hairpin TLS (dex issuer to http://dex:5556/dex, SG port 5556); MAJOR-2 nginx healthcheck; MAJOR-3 /admin/callback redirect URI in Dex config; MINOR-1 RUNBOOK nginx row; MINOR-2 TLS note accuracy; MINOR-4 template trim direction; MINOR-5 negative→positive comparison; MINOR-6 oidc_client_secret YAML char validation; MINOR-7 duplicate comment. `make test-iac-validate` passes (exit 0). |
| 2026-05-13 | Review cycle 3 fixes: removed nginx entirely (no nginx service, no nginx config, no nginx mkdir); fixed effective_oidc_issuer to http://${var.server_name}:5556/dex (public address, matches Dex issuer); fixed redirect URIs to http://port 8008; gateway now binds 8008:8008 unconditionally; updated RUNBOOK (no nginx, correct issuer, docker compose ps output, smoke test commands); updated terraform.tfvars.example (issuer comment, uncommented dex_static_password_hash). `make test-iac-validate` passes (exit 0). |
