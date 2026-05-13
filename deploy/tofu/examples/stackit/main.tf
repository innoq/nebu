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
  user_data = base64encode(templatefile("${path.module}/cloud-init.tftpl", {
    # db_password removed — managed by PostgresFlex, credentials injected via pg_* vars below
    internal_secret    = var.internal_secret
    oidc_client_secret = var.oidc_client_secret
    oidc_issuer        = var.oidc_issuer
    server_name        = var.server_name
    image_registry     = var.image_registry
    nebu_version       = var.nebu_version
    # PostgresFlex connection details for the Nebu application user
    pg_host     = stackit_postgresflex_user.nebu.host
    pg_port     = stackit_postgresflex_user.nebu.port
    pg_user     = stackit_postgresflex_user.nebu.username
    pg_password = stackit_postgresflex_user.nebu.password
    # Keycloak-dedicated DB credentials (separate user, owns the keycloak database)
    kc_user     = stackit_postgresflex_user.keycloak.username
    kc_password = stackit_postgresflex_user.keycloak.password
  }))
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

# Dedicated Keycloak DB user — separate credentials from the application user.
resource "stackit_postgresflex_user" "keycloak" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  username    = "keycloak_app"
  roles       = ["login"]
}

resource "stackit_postgresflex_database" "nebu" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  name        = "nebu"
  owner       = stackit_postgresflex_user.nebu.username
}

resource "stackit_postgresflex_database" "keycloak" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  name        = "keycloak"
  owner       = stackit_postgresflex_user.keycloak.username
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
