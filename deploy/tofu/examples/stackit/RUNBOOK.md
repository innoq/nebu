# Nebu on Stackit — Operator Runbook

This runbook covers day-1 deploy and day-2 operations for Nebu hosted on a Stackit VM using Docker Compose, bootstrapped via cloud-init.

> **SECURITY WARNING — Terraform State:** The cloud-init `user_data` rendered by `templatefile()` — including all injected secrets — is stored in Terraform state. Always use an encrypted remote state backend (Stackit Object Storage with server-side encryption) in production. Never commit `.tfstate` files to version control. See the commented `backend "s3"` block in `main.tf` for the recommended configuration.

---

## Prerequisites

- [OpenTofu](https://opentofu.org/docs/intro/install/) >= 1.6
- A Stackit service account key JSON (download: STACKIT portal → IAM → Service Accounts → Keys)
- SSH key pair (the public key goes into `terraform.tfvars`)
- Values for all required variables in `terraform.tfvars` (copy from `terraform.tfvars.example`)
- Container images (`nebu-gateway`, `nebu-core`) pushed to your registry at the correct tag

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

Expected output once healthy:

```
NAME       IMAGE                           STATUS
postgres   postgres:16-alpine              Up (healthy)
keycloak   quay.io/keycloak/keycloak:24.0  Up
core       <registry>/nebu-core:<ver>      Up (healthy)
gateway    <registry>/nebu-gateway:<ver>   Up (healthy)
```

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

> Note: if you are using a self-signed certificate or TCP passthrough (current default), add `-k` to skip TLS verification until a trusted certificate is configured.

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

## Postgres Volume Backup

Backup to a local file, then upload to Stackit Object Storage:

```bash
# SSH to the VM
ssh ubuntu@<floating_ip>

# Dump database to gzip archive
sudo docker exec postgres pg_dump -U nebu nebu | gzip > /tmp/nebu-backup-$(date +%Y%m%d-%H%M%S).sql.gz

# Upload to Stackit Object Storage (requires s3cmd or rclone configured with your Stackit credentials)
s3cmd put /tmp/nebu-backup-*.sql.gz s3://<your-bucket>/backups/
# or:
rclone copy /tmp/nebu-backup-*.sql.gz stackit:<your-bucket>/backups/
```

To restore from backup:

```bash
gunzip -c /tmp/nebu-backup-<timestamp>.sql.gz | sudo docker exec -i postgres psql -U nebu nebu
```

---

## Teardown

> **Data loss warning:** `tofu destroy` deletes all Stackit resources including the VM boot volume and all Docker volumes. Back up your database first.

```bash
tofu destroy
```

Confirm the prompt. This removes: VM, network, security group, ALB, floating IP, and SSH key pair.

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
