# ── Network ───────────────────────────────────────────────────────────────────

resource "stackit_network" "nebu" {
  project_id       = var.stackit_project_id
  name             = "nebu-${var.environment}"
  ipv4_prefix      = var.network_cidr
  ipv4_nameservers = ["8.8.8.8", "8.8.4.4"]
  routed           = true
}

# ── Security Group ────────────────────────────────────────────────────────────

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

# ── Network Interface ─────────────────────────────────────────────────────────

resource "stackit_network_interface" "nebu" {
  project_id         = var.stackit_project_id
  network_id         = stackit_network.nebu.network_id
  security_group_ids = [stackit_security_group.nebu.security_group_id]
}

# ── Floating IP ───────────────────────────────────────────────────────────────

resource "stackit_public_ip" "nebu" {
  project_id           = var.stackit_project_id
  network_interface_id = stackit_network_interface.nebu.network_interface_id
  # Note: if the VM is recreated, the Floating IP must be manually re-associated with the new
  # network interface in the STACKIT portal or via CLI: `stackit beta network-interface public-ip attach`.
}
