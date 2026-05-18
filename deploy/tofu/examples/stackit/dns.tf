# ── DNS ───────────────────────────────────────────────────────────────────────
# Created only when dns_mode = "default".
# Requires Stackit DNS service to be enabled in your project.
# If the zone already exists, import it first:
#   tofu import 'stackit_dns_zone.nebu[0]' <zone_id>

resource "stackit_dns_zone" "nebu" {
  count         = var.dns_mode == "default" ? 1 : 0
  project_id    = var.stackit_project_id
  name          = var.server_name
  dns_name      = var.server_name # No trailing dot on zone dns_name per provider docs.
  type          = "primary"
  description   = "Nebu instance DNS zone — managed by OpenTofu."
  contact_email = var.dns_contact_email != "" ? var.dns_contact_email : null
}

resource "stackit_dns_record_set" "nebu" {
  count      = var.dns_mode == "default" ? 1 : 0
  project_id = var.stackit_project_id
  zone_id    = stackit_dns_zone.nebu[0].zone_id
  name       = "${var.server_name}." # Record set names require trailing dot (FQDN format).
  type       = "A"
  records    = [stackit_public_ip.nebu.ip]
  ttl        = 300
}

# Optional: dex.<server_name> A-record for future host-based Dex routing.
# Only created when dns_mode = "default" and dex_subdomain_enabled = true.
resource "stackit_dns_record_set" "dex" {
  count      = var.dns_mode == "default" && var.dex_subdomain_enabled ? 1 : 0
  project_id = var.stackit_project_id
  zone_id    = stackit_dns_zone.nebu[0].zone_id
  name       = "dex.${var.server_name}."
  type       = "A"
  records    = [stackit_public_ip.nebu.ip]
  ttl        = 300
}
