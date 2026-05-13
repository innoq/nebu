terraform {
  required_version = ">= 1.6.0"

  required_providers {
    stackit = {
      source  = "stackitcloud/stackit"
      version = "~> 0.95"
    }
  }

  # Backend: Stackit Object Storage (S3-compatible). Configure before use.
  # Uncomment and fill in values — do NOT commit credentials.
  #
  # backend "s3" {
  #   bucket                      = "my-tofu-state"
  #   key                         = "nebu/stackit/terraform.tfstate"
  #   endpoint                    = "https://object.storage.eu01.onstackit.cloud"
  #   region                      = "eu01"
  #   skip_credentials_validation = true
  #   skip_region_validation      = true
  #   skip_metadata_api_check     = true
  # }
}

provider "stackit" {
  default_region           = var.region
  service_account_key_path = var.stackit_key_path
  # beta feature: enable_beta_resources = true required for PROTOCOL_HTTPS ALB listener
  enable_beta_resources = true
}

# ── Network ─────────────────────────────────────────────────────────────────

resource "stackit_network" "nebu" {
  project_id       = var.stackit_project_id
  name             = "nebu-${var.environment}"
  ipv4_prefix      = var.network_cidr
  ipv4_nameservers = ["8.8.8.8", "8.8.4.4"]
  routed           = true
}

# ── Security Group ───────────────────────────────────────────────────────────

resource "stackit_security_group" "nebu" {
  project_id = var.stackit_project_id
  name       = "nebu-${var.environment}-sg"
  stateful   = true
}

# Inbound: HTTPS from anywhere
resource "stackit_security_group_rule" "inbound_https" {
  project_id        = var.stackit_project_id
  security_group_id = stackit_security_group.nebu.security_group_id
  direction         = "ingress"
  protocol = {
    name = "tcp"
  }
  port_range = {
    min = 443
    max = 443
  }
}

# Inbound: Matrix API port from anywhere (ALB → VM)
resource "stackit_security_group_rule" "inbound_matrix" {
  project_id        = var.stackit_project_id
  security_group_id = stackit_security_group.nebu.security_group_id
  direction         = "ingress"
  protocol = {
    name = "tcp"
  }
  port_range = {
    min = 8008
    max = 8008
  }
}

# Inbound: Dex OIDC port (dex mode only — browser follows OIDC redirects to port 5556).
# Restricted to dex_allowed_cidr (default 0.0.0.0/0 for demo; restrict to your IP/VPN range in shared environments).
resource "stackit_security_group_rule" "inbound_dex" {
  count             = var.oidc_mode == "dex" ? 1 : 0
  project_id        = var.stackit_project_id
  security_group_id = stackit_security_group.nebu.security_group_id
  direction         = "ingress"
  ip_range          = var.dex_allowed_cidr
  protocol = {
    name = "tcp"
  }
  port_range = {
    min = 5556
    max = 5556
  }
}

# Inbound: SSH from anywhere (restrict to a bastion CIDR in production)
resource "stackit_security_group_rule" "inbound_ssh" {
  project_id        = var.stackit_project_id
  security_group_id = stackit_security_group.nebu.security_group_id
  direction         = "ingress"
  protocol = {
    name = "tcp"
  }
  port_range = {
    min = 22
    max = 22
  }
}

# Outbound: allow all
resource "stackit_security_group_rule" "outbound_all" {
  project_id        = var.stackit_project_id
  security_group_id = stackit_security_group.nebu.security_group_id
  direction         = "egress"
}

# ── Network Interface ────────────────────────────────────────────────────────

resource "stackit_network_interface" "nebu" {
  project_id         = var.stackit_project_id
  network_id         = stackit_network.nebu.network_id
  security_group_ids = [stackit_security_group.nebu.security_group_id]
}

# ── SSH Key Pair ─────────────────────────────────────────────────────────────

resource "stackit_key_pair" "nebu" {
  # Note: stackit_key_pair is a global (account-level) resource; project_id is not supported.
  name       = "nebu-${var.environment}"
  public_key = var.ssh_public_key
}

# ── OIDC Profile ─────────────────────────────────────────────────────────────

locals {
  # When oidc_mode = "dex", the issuer is the Dex sidecar exposed on port 5556 of the VM's public IP.
  # The gateway resolves Dex via a Docker hairpin (host IP → port 5556 → Dex container).
  # Standard Linux SNAT/masquerade handles this — no additional routing configuration needed.
  # Dex's issuer in config.yaml is also set to http://<server_name>:5556/dex so iss validation passes.
  # Port 5556 is opened in the SG (inbound_dex rule) so browsers can follow OIDC redirects directly.
  # Future: when TLS is configured, switch to host-based nginx routing (dex.<server_name>) instead.
  # When oidc_mode = "external", the operator provides the issuer explicitly.
  effective_oidc_issuer = var.oidc_mode == "dex" ? "http://${var.server_name}:5556/dex" : var.oidc_issuer
}

# ── VM ───────────────────────────────────────────────────────────────────────
# Ubuntu 24.04 LTS — image ID for eu01 region.
# Update this ID if deploying to a different region (check STACKIT image catalog).
# Machine type g2i.2 = 4 vCPU / 8 GB RAM.

resource "stackit_server" "nebu" {
  project_id = var.stackit_project_id
  name       = "nebu-${var.environment}"
  # Run `stackit compute server machine-types list` to find available plan IDs for your region
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

  # SECURITY NOTE: cloud-init template including secrets is stored in Terraform state.
  # Use encrypted state backend (Stackit Object Storage with server-side encryption) in production.
  # The stackit_postgresflex_user.nebu.password is stored in state — ensure state encryption is enabled.
  # Never commit .tfstate files. See the backend "s3" block above for the recommended configuration.
  #
  # cloud-init bootstrap: installs Docker, writes /opt/nebu/ layout, starts nebu.service.
  # All secrets are injected from OpenTofu variables — no hardcoded values in the template.
  #
  # Traffic flow when oidc_mode = "dex":
  #   Browser → ALB:443 (TCP passthrough) → gateway:8008 (host port, bound directly)
  #   Gateway OIDC discovery (hairpin): gateway → http://<server_name>:5556/dex (host IP → Dex container)
  #     Standard Linux SNAT/masquerade handles the hairpin — no extra routing needed.
  #   Browser OIDC redirect: browser → http://<server_name>:5556/dex/auth (SG port 5556 open)
  # Traffic flow when oidc_mode = "external":
  #   ALB:443 (TCP passthrough) → gateway:8008 (host)
  #   Gateway OIDC discovery: gateway → <oidc_issuer> (external HTTPS provider)
  # No ALB target port change is needed — both modes use port 8008 on the host.
  # TODO (future): when TLS is configured, implement host-based routing via nginx
  #   (dex.<server_name>:443 → dex, <server_name>:443 → gateway) in a follow-up story.
  user_data = base64encode(templatefile("${path.module}/cloud-init.tftpl", {
    internal_secret          = var.internal_secret
    oidc_client_secret       = var.oidc_client_secret
    oidc_issuer              = local.effective_oidc_issuer
    oidc_mode                = var.oidc_mode
    dex_static_password_hash = var.oidc_mode == "dex" ? var.dex_static_password_hash : ""
    server_name              = var.server_name
    image_registry           = var.image_registry
    nebu_version             = var.nebu_version
    # PostgresFlex connection details for the Nebu application user
    pg_host     = stackit_postgresflex_user.nebu.host
    pg_port     = stackit_postgresflex_user.nebu.port
    pg_user     = stackit_postgresflex_user.nebu.username
    pg_password = stackit_postgresflex_user.nebu.password
    # kc_user and kc_password are removed — Keycloak is no longer deployed
  }))

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

# ── PostgresFlex Managed Database ────────────────────────────────────────────
# Replaces the self-hosted postgres:16-alpine container in cloud-init.
# Provides automated daily backups, PITR, and HA replication via the Stackit platform.

resource "stackit_postgresflex_instance" "nebu" {
  project_id      = var.stackit_project_id
  name            = "nebu-${var.environment}-postgres"
  version         = "16"
  replicas        = var.postgres_replicas
  backup_schedule = "0 2 * * *" # Daily at 02:00 UTC

  # ACL: restrict to the VM's private network CIDR only.
  # The VM connects via its private network interface — no public exposure.
  acl = [var.network_cidr]

  flavor = {
    cpu = var.postgres_cpu
    ram = var.postgres_ram
  }

  storage = {
    class = "premium-perf6-stackit"
    size  = var.postgres_storage_size
  }
}

resource "stackit_postgresflex_user" "nebu" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  username    = "nebu_app"
  roles       = ["login"]
}

resource "stackit_postgresflex_database" "nebu" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  name        = "nebu"
  owner       = stackit_postgresflex_user.nebu.username
}

# ── Floating IP ──────────────────────────────────────────────────────────────

resource "stackit_public_ip" "nebu" {
  project_id           = var.stackit_project_id
  network_interface_id = stackit_network_interface.nebu.network_interface_id
  # Note: if the VM is recreated, the Floating IP must be manually re-associated with the new
  # network interface in the STACKIT portal or via CLI: `stackit beta network-interface public-ip attach`.
}

# ── Application Load Balancer ─────────────────────────────────────────────────
# Stackit native Layer 7 ALB (per ADR-014).
# Handles TLS termination and health checks at ALB level.
# beta feature: enable_beta_resources = true required in provider block

resource "stackit_loadbalancer" "nebu" {
  project_id       = var.stackit_project_id
  name             = "nebu-${var.environment}-alb"
  plan_id          = var.alb_plan_id
  external_address = stackit_public_ip.nebu.ip

  networks = [
    {
      network_id = stackit_network.nebu.network_id
      role       = "ROLE_LISTENERS_AND_TARGETS"
    },
  ]

  listeners = [
    {
      display_name = "https-443"
      port         = 443
      # Current: PROTOCOL_TCP (passthrough — TLS terminated by the gateway on port 8008).
      # Upgrade path: when stackit provider >= 0.96 exposes PROTOCOL_HTTPS in its stable schema,
      # change protocol to "PROTOCOL_HTTPS" and add certificate_reference.name = var.stackit_tls_certificate_arn.
      # That also requires enable_beta_resources = true in the provider block (already set above).
      protocol    = "PROTOCOL_TCP"
      target_pool = "matrix-api"
    },
  ]

  target_pools = [
    {
      name        = "matrix-api"
      target_port = 8008
      targets = [
        {
          display_name = stackit_server.nebu.name
          ip           = stackit_network_interface.nebu.ipv4
        },
      ]
      active_health_check = {
        # Note: Stackit ALB health checks are TCP-based only (no HTTP path checks).
        # The health check verifies TCP connectivity on port 8008.
        # For application-level health checking, monitor via /metrics or application logs.
        healthy_threshold   = 3
        interval            = "10s"
        interval_jitter     = "3s"
        timeout             = "5s"
        unhealthy_threshold = 3
      }
    },
  ]

  options = {
    private_network_only = false
  }
}

# ── DNS ───────────────────────────────────────────────────────────────────────
# Created only when dns_mode = "default".
# Requires Stackit DNS service to be enabled in your project.
# The zone is created for the domain extracted from server_name.
# If the zone already exists, import it first: tofu import stackit_dns_zone.nebu <zone_id>

resource "stackit_dns_zone" "nebu" {
  count         = var.dns_mode == "default" ? 1 : 0
  project_id    = var.stackit_project_id
  name          = var.server_name
  dns_name      = var.server_name # No trailing dot on zone dns_name per provider docs.
  type          = "primary"
  description   = "Nebu instance DNS zone — managed by OpenTofu."
  contact_email = var.dns_contact_email != "" ? var.dns_contact_email : null # Optional. Omitted (null) when empty; recommended to set in production.
}

resource "stackit_dns_record_set" "nebu" {
  count      = var.dns_mode == "default" ? 1 : 0
  project_id = var.stackit_project_id
  zone_id    = stackit_dns_zone.nebu[0].zone_id
  name       = "${var.server_name}." # Record set names require trailing dot (FQDN format), unlike the zone dns_name field.
  type       = "A"
  records    = [stackit_public_ip.nebu.ip]
  ttl        = 300
}

# Optional dex subdomain record (only when dex_subdomain_enabled = true).
# Enables host-based routing to dex.<server_name> (future nginx routing story).
resource "stackit_dns_record_set" "dex" {
  count      = var.dns_mode == "default" && var.dex_subdomain_enabled ? 1 : 0
  project_id = var.stackit_project_id
  zone_id    = stackit_dns_zone.nebu[0].zone_id
  name       = "dex.${var.server_name}."
  type       = "A"
  records    = [stackit_public_ip.nebu.ip]
  ttl        = 300
}
