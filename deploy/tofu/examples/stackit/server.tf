# ── OIDC / TLS locals ────────────────────────────────────────────────────────
#
# When enable_tls = true, certbot provisions a Let's Encrypt cert on first boot
# via HTTP-01 challenge. nginx proxies /dex/ → localhost:5556 when oidc_mode = "dex".
# Dex's issuer becomes https://<server_name>/dex (path-based routing via nginx).
# When enable_tls = false, Dex is reached directly on port 5556 (plain HTTP).

locals {
  dex_issuer            = var.enable_tls ? "https://${var.server_name}/dex" : "http://${var.server_name}:5556/dex"
  effective_oidc_issuer = var.oidc_mode == "dex" ? local.dex_issuer : var.oidc_issuer

  # URL scheme + optional port suffix for redirect URIs injected into Dex's staticClients.
  gateway_scheme = var.enable_tls ? "https" : "http"
  gateway_port   = var.enable_tls ? "" : ":8008"

  # Logs — hostname extracted from Stackit Logs ingest_url (empty when enable_logs = false).
  logs_ingest_host = try(regex("https://([^/]+)", stackit_logs_instance.nebu[0].ingest_url)[0], "")
}

# ── SSH Key Pair ──────────────────────────────────────────────────────────────

resource "stackit_key_pair" "nebu" {
  # Note: stackit_key_pair is a global (account-level) resource; project_id is not supported.
  name       = "nebu-${var.environment}"
  public_key = var.ssh_public_key
}

# ── VM ────────────────────────────────────────────────────────────────────────
# Ubuntu 24.04 LTS — image ID for eu01 region.
# Update this ID if deploying to a different region (check STACKIT image catalog).
# Machine type g2i.2 = 4 vCPU / 8 GB RAM.
#
# SECURITY NOTE: cloud-init template including secrets is stored in Terraform state.
# Use encrypted state backend (Stackit Object Storage with server-side encryption) in production.
# The stackit_postgresflex_user.nebu.password is stored in state — ensure state encryption is enabled.
# Never commit .tfstate files. See the backend "s3" block in main.tf for the recommended configuration.
#
# Traffic flow when enable_tls = false, oidc_mode = "dex":
#   Browser → ALB:443 (TCP passthrough) → gateway:8008 (host port, bound directly)
#   Gateway OIDC discovery (hairpin): gateway → http://<server_name>:5556/dex (host IP → Dex container)
#   Browser OIDC redirect: browser → http://<server_name>:5556/dex/auth (SG port 5556 open)
# Traffic flow when enable_tls = true:
#   Browser → ALB:443 (TCP passthrough) → VM:443 (nginx, TLS termination) → localhost:8008 (gateway)
#   Browser → ALB:80  (TCP passthrough) → VM:80  (nginx, 301 → https://<server_name>)
#   When oidc_mode = "dex": nginx also proxies /dex/ → localhost:5556 (Dex, plain HTTP on loopback)

resource "stackit_server" "nebu" {
  project_id        = var.stackit_project_id
  name              = "nebu-${var.environment}"
  machine_type      = var.vm_plan_id
  availability_zone = var.availability_zone

  boot_volume = {
    size        = 64
    source_type = "image"
    # Ubuntu 24.04 LTS (eu01) — verify current ID in STACKIT portal before apply
    source_id = var.ubuntu_image_id
  }

  keypair_name = stackit_key_pair.nebu.name

  network_interfaces = [
    stackit_network_interface.nebu.network_interface_id,
  ]

  user_data = templatefile("${path.module}/cloud-init.tftpl", {
    internal_secret          = var.internal_secret
    oidc_client_secret       = var.oidc_client_secret
    oidc_issuer              = local.effective_oidc_issuer
    oidc_mode                = var.oidc_mode
    dex_static_password_hash = var.oidc_mode == "dex" ? var.dex_static_password_hash : ""
    server_name              = var.server_name
    image_registry           = var.image_registry
    nebu_version             = var.nebu_version
    # PostgresFlex connection details — runtime (nebu_app) and migration (nebu_migrate)
    pg_host            = stackit_postgresflex_user.nebu.host
    pg_port            = stackit_postgresflex_user.nebu.port
    pg_user            = stackit_postgresflex_user.nebu.username
    pg_password        = stackit_postgresflex_user.nebu.password
    pg_migrate_user    = stackit_postgresflex_user.nebu_migrate.username
    pg_migrate_password = stackit_postgresflex_user.nebu_migrate.password
    # TLS — certbot on the VM handles cert issuance/renewal via HTTP-01 challenge
    enable_tls     = var.enable_tls
    acme_email     = var.acme_email
    acme_staging   = var.acme_staging
    gateway_scheme = local.gateway_scheme
    gateway_port   = local.gateway_port
    # Logging — Fluent Bit ships nebu.service journal logs to Stackit Logs (Loki)
    pii_encryption_key = var.pii_encryption_key
    enable_logs        = var.enable_logs
    logs_ingest_host   = local.logs_ingest_host
    logs_bearer_token  = try(stackit_logs_access_token.nebu[0].access_token, "")
    environment        = var.environment
  })

  lifecycle {
    precondition {
      condition     = var.oidc_mode != "dex" || var.dex_static_password_hash != null
      error_message = "dex_static_password_hash is required when oidc_mode = 'dex'."
    }

    precondition {
      condition     = var.oidc_mode == "dex" || length(var.oidc_issuer) > 0
      error_message = "oidc_issuer must be set when oidc_mode = 'external'."
    }
  }
}
