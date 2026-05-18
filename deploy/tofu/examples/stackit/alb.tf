# ── Application Load Balancer ─────────────────────────────────────────────────
# Stackit native ALB (per ADR-014). TCP passthrough — TLS is terminated on the VM by nginx.
# beta feature: enable_beta_resources = true required in provider block (main.tf).
#
# enable_tls = false: port 443 → gateway:8008 (direct, no TLS)
# enable_tls = true:  port 443 → nginx:443 (TLS termination) + port 80 → nginx:80 (HTTP redirect)

locals {
  _alb_vm_targets = [
    {
      display_name = stackit_server.nebu.name
      ip           = stackit_network_interface.nebu.ipv4
    },
  ]

  # Stackit ALB health checks are TCP-based only.
  _alb_health_check = {
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
        name                = "matrix-api"
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
