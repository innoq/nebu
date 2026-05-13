# Nebu on Stackit — Operator Runbook

This runbook covers day-1 deploy and day-2 operations for Nebu hosted on a Stackit VM using Docker Compose, bootstrapped via cloud-init.

> **SECURITY WARNING — Terraform State:** The cloud-init `user_data` rendered by `templatefile()` — including all injected secrets — is stored in Terraform state. The PostgresFlex database password (`stackit_postgresflex_user.nebu.password`) is also stored in state. Always use an encrypted remote state backend (Stackit Object Storage with server-side encryption) in production. Never commit `.tfstate` files to version control. See the commented `backend "s3"` block in `main.tf` for the recommended configuration. To retrieve the database password after apply: `tofu output -json | jq` — note the password is intentionally not an output; read it from state via `tofu state show stackit_postgresflex_user.nebu`.

---

## Prerequisites

- [OpenTofu](https://opentofu.org/docs/intro/install/) >= 1.6
- A Stackit service account key JSON (download: STACKIT portal → IAM → Service Accounts → Keys)
- SSH key pair (the public key goes into `terraform.tfvars`)
- Values for all required variables in `terraform.tfvars` (copy from `terraform.tfvars.example`)
- Container images (`nebu-gateway`, `nebu-core`) pushed to your registry at the correct tag

---

## OIDC Profiles

Nebu on Stackit supports two OIDC deployment modes, configured via `oidc_mode` in `terraform.tfvars`.

### `oidc_mode = "dex"` — Bundled Dex (test/demo)

Deploys [Dex IdP](https://dexidp.io/) as a Docker Compose sidecar alongside Nebu. Dex uses a static YAML configuration — no database is required.

**When to use:** Development, demo, or integration test environments where you do not have an external OIDC provider.

**Traffic routing (dex mode):**

The gateway binds host port 8008 directly (same as external mode). The ALB targets port 8008 — no configuration change required between modes.

Dex is exposed on host port 5556 (plain HTTP). The SG rule `inbound_dex` is created conditionally in dex mode to allow browsers to follow OIDC redirects directly to port 5556.

The gateway performs OIDC discovery via a Docker hairpin: it calls `http://<server_name>:5556/dex/.well-known/openid-configuration` using the VM's public IP. Standard Linux SNAT/masquerade handles this routing correctly.

> **Note:** Host-based routing via nginx (`dex.<server_name>`) is a planned improvement for when TLS is configured. This will be implemented in a follow-up story.

**What gets deployed:**

```bash
sudo docker compose ps
# NAME     IMAGE                        STATUS
# dex      dexidp/dex:v2.45.1           Up (healthy)
# core     <registry>/nebu-core:...     Up (healthy)
# gateway  <registry>/nebu-gateway:...  Up (healthy)
```

The Dex config file is at `/opt/nebu/dex/config.yaml`. It contains a static OIDC client (`nebu-gateway`) and a static user (`operator@example.com` — in the `instance_admin` group).

> **Admin access:** The static Dex user is placed in the `instance_admin` group claim. For admin access to work, the Nebu Bootstrap Wizard must be configured with `admin_group_claim = "groups"` and the claim value `instance_admin`. Complete the Bootstrap Wizard after first deploy to enable admin login.

> **Security:** The `dex_static_password_hash` variable is required when using this mode. Generate a unique bcrypt hash with: `htpasswd -bnBC 12 '' 'yourpassword' | tr -d ':' | sed 's/$2y/$2a/'`. Never share or reuse password hashes across environments.

The OIDC issuer is automatically set to `http://<server_name>:5556/dex`. You do not need to set `oidc_issuer` in `terraform.tfvars` when using this mode.

> **Note on HTTP (not HTTPS) for Dex:** Dex runs on port 5556 with plain HTTP. **Dex mode is intended for test/demo environments only — not for production deployments where HTTPS for the IdP is required.**

To confirm Dex is reachable (port 5556, plain HTTP):

```bash
curl http://<server_name>:5556/dex/.well-known/openid-configuration
# Expected: {"issuer":"http://<server_name>:5556/dex", ...}
```

To confirm the gateway is reachable on port 8008:

```bash
curl http://<server_name>:8008/_matrix/client/v3/versions
# Expected: Matrix version list from gateway
```

### `oidc_mode = "external"` — External OIDC Provider (production)

No bundled IdP is deployed. You must provide:

- `oidc_issuer` — the OIDC issuer URL of your external provider (Keycloak, Authentik, Azure AD, etc.)
- `oidc_client_secret` — the client secret issued to `nebu-gateway` by your provider

**When to use:** Production deployments where you already manage an OIDC provider or use a cloud IdP service.

Configure the external IdP client using the Nebu Bootstrap Wizard (step 3 — OIDC configuration), then copy the issued client secret into `terraform.tfvars` as `oidc_client_secret`.

---

## First Deploy

```bash
# 1. Initialise providers
tofu init

# 2. Preview changes
tofu plan -out=tfplan

# 3. Apply — provisions VM, networking, ALB, injects cloud-init
tofu apply tfplan
```

`tofu apply` outputs the floating IP:

```
floating_ip = "185.x.x.x"
```

---

## Checking VM Boot Status

After `tofu apply`, the VM boots and cloud-init runs in the background (typically 2–5 minutes).

```bash
# SSH to the VM (use your private key matching the public key in tfvars)
ssh ubuntu@<floating_ip>

# Check cloud-init progress
sudo cloud-init status --wait

# Once cloud-init reports 'done', check Nebu services
cd /opt/nebu
sudo docker compose ps
```

Expected output once healthy (`oidc_mode = "dex"`):

```
NAME       IMAGE                           STATUS
dex        dexidp/dex:v2.45.1              Up (healthy)
core       <registry>/nebu-core:<ver>      Up (healthy)
gateway    <registry>/nebu-gateway:<ver>   Up (healthy)
```

Expected output once healthy (`oidc_mode = "external"`):

```
NAME       IMAGE                           STATUS
core       <registry>/nebu-core:<ver>      Up (healthy)
gateway    <registry>/nebu-gateway:<ver>   Up (healthy)
```

> Note: the `postgres` container is no longer present — the database is now managed by Stackit PostgresFlex. Nebu services connect to the PostgresFlex instance directly via the private network.

If cloud-init fails, check the log:

```bash
sudo cat /var/log/cloud-init-output.log
```

---

## Smoke Test

Verify the Matrix API is reachable through the ALB:

```bash
curl https://<floating_ip>/_matrix/client/v3/versions
```

Expected response:

```json
{"versions":["v1.1","v1.2","v1.3","v1.4","v1.5"]}
```

> Note: the ALB currently uses TCP passthrough (`PROTOCOL_TCP`). TLS termination is handled by the gateway. In `oidc_mode = "dex"`, the Dex OIDC endpoint (`http://<server_name>:5556/dex/`) is plain HTTP — no TLS. The Matrix API on port 8008 is served directly by the gateway container.

---

## Update Strategy

To deploy a new Nebu version without reprovisioning the VM:

```bash
# SSH to the VM
ssh ubuntu@<floating_ip>

# Pull new images and recreate containers (zero config change, data preserved)
cd /opt/nebu
sudo docker compose pull
sudo docker compose up -d --force-recreate
```

To deploy updated config (e.g. new environment variable):

1. Update `terraform.tfvars` with the new values.
2. Run `tofu apply` — the `user_data` change will trigger VM replacement (data loss; see backup first).
   - Alternatively: SSH to the VM, edit `/opt/nebu/.env` directly, then `sudo docker compose up -d`.

---

## Database Backup (PostgresFlex)

Backups are handled automatically by the Stackit PostgresFlex platform. Daily backups run at 02:00 UTC (as configured in `backup_schedule`). Point-in-time recovery (PITR) is available via the STACKIT portal.

For manual dumps or cross-environment restores, connect to the PostgresFlex instance from the VM using the credentials stored in Terraform state:

```bash
# SSH to the VM
ssh ubuntu@<floating_ip>

# Retrieve connection details from Terraform state (run on your local machine)
tofu state show stackit_postgresflex_user.nebu

# Dump via pg_dump (install postgresql-client on the VM if needed)
pg_dump "postgresql://<pg_user>:<pg_password>@<pg_host>:<pg_port>/nebu" | \
  gzip > /tmp/nebu-backup-$(date +%Y%m%d-%H%M%S).sql.gz

# Upload to Stackit Object Storage
rclone copy /tmp/nebu-backup-*.sql.gz stackit:<your-bucket>/backups/
```

---

## Teardown

> **Data loss warning:** `tofu destroy` deletes all Stackit resources including the VM boot volume and all Docker volumes. Back up your database first.

```bash
tofu destroy
```

Confirm the prompt. This removes: VM, network, security group, ALB, floating IP, and SSH key pair.

---

## DNS Configuration

Nebu supports two DNS modes, configured via `dns_mode` in `terraform.tfvars`.

### `dns_mode = "external"` (default) — Manual DNS Registration

OpenTofu does not create DNS records. After `tofu apply`, retrieve the floating IP:

```bash
tofu output dns_name
```

Register this value in your DNS provider:
- **Stackit:** Create an A-record pointing `<server_name>` to the floating IP shown in `dns_name`.

### `dns_mode = "default"` — Managed DNS (Stackit DNS)

OpenTofu creates DNS records automatically in Stackit DNS.

**Stackit DNS:**
- Creates a DNS zone + A-record for `server_name` pointing to the floating IP.
- Requires Stackit DNS service to be enabled in your project.
- If a zone for `server_name` already exists: import it before apply:
  ```bash
  tofu import stackit_dns_zone.nebu <zone_id>
  ```
- `dex_subdomain_enabled = true` additionally creates `dex.<server_name>` pointing to the floating IP (useful for future host-based Dex routing).

---

## Troubleshooting

| Symptom | Check |
|---|---|
| cloud-init hangs | `sudo tail -f /var/log/cloud-init-output.log` |
| Docker not installed | `sudo apt-get install -y docker-ce docker-compose-plugin` (manual fallback) |
| nebu.service failed | `sudo systemctl status nebu.service` + `sudo journalctl -u nebu.service -n 50` |
| Gateway unhealthy | `sudo docker compose logs gateway` — check OIDC issuer and DB connectivity |
| `/_matrix/client/v3/versions` returns 502 | ALB target not healthy; check `sudo docker compose ps` — gateway must be `Up (healthy)` |
| TLS certificate error | See `variables.tf` — `stackit_tls_certificate_arn` must be set for HTTPS termination at ALB |
