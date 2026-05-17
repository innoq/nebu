terraform {
  required_version = ">= 1.6.0"

  required_providers {
    stackit = {
      source  = "stackitcloud/stackit"
      version = "~> 0.95"
    }
    acme = {
      source  = "vancluever/acme"
      version = "~> 2.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
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

# Let's Encrypt ACME provider.
# Use acme_staging = true during initial testing to avoid LE production rate limits.
provider "acme" {
  server_url = var.acme_staging ? "https://acme-staging-v02.api.letsencrypt.org/directory" : "https://acme-v02.api.letsencrypt.org/directory"
}

# ── Let's Encrypt / ACME ─────────────────────────────────────────────────────
# All three resources are skipped when enable_tls = false.
# DNS-01 challenge writes TXT records into the Stackit DNS zone — requires dns_mode = "default".
# Lego env vars for Stackit: https://go-acme.github.io/lego/dns/stackit/

resource "tls_private_key" "acme_account" {
  count     = var.enable_tls ? 1 : 0
  algorithm = "RSA"
  rsa_bits  = 4096
}

resource "acme_registration" "nebu" {
  count           = var.enable_tls ? 1 : 0
  account_key_pem = tls_private_key.acme_account[0].private_key_pem
  email_address   = var.acme_email

  lifecycle {
    precondition {
      condition     = length(var.acme_email) > 0
      error_message = "acme_email must be set when enable_tls = true."
    }
    precondition {
      condition     = var.dns_mode == "default"
      error_message = "enable_tls = true requires dns_mode = 'default' — the ACME DNS-01 challenge needs a Stackit DNS zone."
    }
  }
}

resource "acme_certificate" "nebu" {
  count                     = var.enable_tls ? 1 : 0
  account_key_pem           = acme_registration.nebu[0].account_key_pem
  common_name               = var.server_name
  min_days_remaining        = 30

  dns_challenge {
    provider = "stackit"
    # Lego reads these as environment variables during the DNS-01 challenge.
    # STACKIT_SERVICE_ACCOUNT_KEY_PATH points to the same SA key used by the stackit provider.
    config = {
      STACKIT_PROJECT_ID               = var.stackit_project_id
      STACKIT_SERVICE_ACCOUNT_KEY_PATH = var.stackit_key_path
    }
  }

  # The DNS zone must exist before lego can create the TXT challenge record.
  depends_on = [stackit_dns_zone.nebu]
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

# Inbound: HTTP (port 80) for HTTP→HTTPS redirect via nginx — only when TLS is enabled.
resource "stackit_security_group_rule" "inbound_http" {
  count             = var.enable_tls ? 1 : 0
  project_id        = var.stackit_project_id
  security_group_id = stackit_security_group.nebu.security_group_id
  direction         = "ingress"
  protocol = {
    name = "tcp"
  }
  port_range = {
    min = 80
    max = 80
  }
}

# Inbound: Matrix API port from anywhere (ALB → VM).
# When TLS is disabled, the ALB targets port 8008 directly.
# When TLS is enabled, the ALB targets port 443 (nginx), so 8008 stays internal.
resource "stackit_security_group_rule" "inbound_matrix" {
  count             = var.enable_tls ? 0 : 1
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

# Inbound: Dex OIDC port (dex mode only, TLS disabled only).
# When TLS is enabled, nginx proxies /dex/ → localhost:5556, so port 5556 stays internal.
# When TLS is disabled, browser follows OIDC redirects directly to port 5556.
# Restricted to dex_allowed_cidr (default 0.0.0.0/0 for demo; restrict to your IP/VPN range in shared environments).
resource "stackit_security_group_rule" "inbound_dex" {
  count             = var.oidc_mode == "dex" && !var.enable_tls ? 1 : 0
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
  # When enable_tls = true, nginx fronts port 443 and proxies /dex/ to localhost:5556.
  # Dex's issuer becomes https://<server_name>/dex (path-based routing via nginx).
  # When enable_tls = false, Dex is reached directly on port 5556 (plain HTTP).
  dex_issuer = var.enable_tls ? "https://${var.server_name}/dex" : "http://${var.server_name}:5556/dex"

  effective_oidc_issuer = var.oidc_mode == "dex" ? local.dex_issuer : var.oidc_issuer

  # URL scheme + optional port suffix for redirect URIs injected into Dex's staticClients.
  # When TLS is enabled, nginx handles port 443 → gateway:8008 (no explicit port in URIs).
  gateway_scheme = var.enable_tls ? "https" : "http"
  gateway_port   = var.enable_tls ? "" : ":8008"

  # Certificate content — empty strings when enable_tls = false.
  # try() suppresses the index-out-of-bounds error when acme_certificate.nebu has count = 0.
  tls_cert = try(acme_certificate.nebu[0].certificate_pem, "")
  tls_key  = try(acme_certificate.nebu[0].private_key_pem, "")
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
  # cloud-init bootstrap: installs Docker (+ nginx when enable_tls = true), writes /opt/nebu/ layout, starts nebu.service.
  # All secrets are injected from OpenTofu variables — no hardcoded values in the template.
  #
  # Traffic flow when enable_tls = false, oidc_mode = "dex":
  #   Browser → ALB:443 (TCP passthrough) → gateway:8008 (host port, bound directly)
  #   Gateway OIDC discovery (hairpin): gateway → http://<server_name>:5556/dex (host IP → Dex container)
  #   Browser OIDC redirect: browser → http://<server_name>:5556/dex/auth (SG port 5556 open)
  # Traffic flow when enable_tls = true:
  #   Browser → ALB:443 (TCP passthrough) → VM:443 (nginx, TLS termination) → localhost:8008 (gateway)
  #   Browser → ALB:80  (TCP passthrough) → VM:80  (nginx, 301 → https://<server_name>)
  #   When oidc_mode = "dex": nginx also proxies /dex/ → localhost:5556 (Dex, plain HTTP on loopback)
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
    # TLS — cert and key are empty strings when enable_tls = false (nginx block is skipped in template)
    enable_tls     = var.enable_tls
    tls_cert       = local.tls_cert
    tls_key        = local.tls_key
    gateway_scheme = local.gateway_scheme
    gateway_port   = local.gateway_port
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

locals {
  # ALB listeners and target pools vary by TLS mode.
  # enable_tls = false: single HTTPS listener → gateway:8008 (TCP passthrough).
  # enable_tls = true:  port 443 (TCP passthrough → nginx:443) + port 80 (TCP → nginx:80 for redirect).
  _alb_vm_targets = [
    {
      display_name = stackit_server.nebu.name
      ip           = stackit_network_interface.nebu.ipv4
    },
  ]
  _alb_health_check = {
    # Stackit ALB health checks are TCP-based only.
    healthy_threshold   = 3
    interval            = "10s"
    interval_jitter     = "3s"
    timeout             = "5s"
    unhealthy_threshold = 3
  }

  alb_listeners = concat(
    [
      {
        display_name = "https-443"
        port         = 443
        protocol     = "PROTOCOL_TCP"
        target_pool  = "matrix-api"
      },
    ],
    var.enable_tls ? [
      {
        display_name = "http-80"
        port         = 80
        protocol     = "PROTOCOL_TCP"
        target_pool  = "http-redirect"
      },
    ] : []
  )

  alb_target_pools = concat(
    [
      {
        name = "matrix-api"
        # When TLS is enabled, nginx listens on port 443 and terminates TLS.
        # When TLS is disabled, the ALB connects directly to the gateway on port 8008.
        target_port         = var.enable_tls ? 443 : 8008
        targets             = local._alb_vm_targets
        active_health_check = local._alb_health_check
      },
    ],
    var.enable_tls ? [
      {
        name                = "http-redirect"
        target_port         = 80
        targets             = local._alb_vm_targets
        active_health_check = local._alb_health_check
      },
    ] : []
  )
}

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

  listeners    = local.alb_listeners
  target_pools = local.alb_target_pools

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
