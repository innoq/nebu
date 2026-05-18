output "floating_ip" {
  description = "Public (floating) IP address assigned to the Nebu VM."
  value       = stackit_public_ip.nebu.ip
}

output "postgres_instance_id" {
  description = "STACKIT PostgresFlex instance ID."
  value       = stackit_postgresflex_instance.nebu.instance_id
}

output "postgres_host" {
  description = "PostgresFlex private host (only reachable from within the Stackit private network)."
  value       = stackit_postgresflex_user.nebu.host
}

output "vm_id" {
  description = "STACKIT server ID of the Nebu VM."
  value       = stackit_server.nebu.server_id
}

output "load_balancer_id" {
  description = "Internal resource ID of the Stackit Application Load Balancer."
  value       = stackit_loadbalancer.nebu.id
}

output "load_balancer_private_address" {
  description = "Transient private IP address of the Stackit ALB (used for internal routing). The public entry point is floating_ip."
  value       = stackit_loadbalancer.nebu.private_address
}

output "dns_name" {
  description = "Floating IP address to register in your external DNS server when dns_mode = 'external'. Create an A-record pointing your domain to this IP address. When dns_mode = 'default', Stackit DNS is managing this automatically."
  value       = stackit_public_ip.nebu.ip
}

output "tls_info" {
  description = "TLS status. When enable_tls = true, certbot manages cert renewal automatically via its systemd timer. Check cert expiry on the VM: sudo certbot certificates"
  value       = var.enable_tls ? "certbot-managed (HTTP-01, auto-renews via systemd timer)" : "disabled"
}

output "nebu_migrate_password" {
  description = "Auto-generated password for the nebu_migrate PostgresFlex user (DDL migration runner). Needed to update NEBU_DB_URL_MIGRATE on an existing VM after tofu apply."
  value       = stackit_postgresflex_user.nebu_migrate.password
  sensitive   = true
}

output "logs_ingest_url" {
  description = "Stackit Logs Loki push endpoint. After apply: Portal → Logs → instance → Access tokens → Create (Read+Write role). Then SSH into the VM and run: /opt/nebu/configure-fluent-bit.sh <token>"
  value       = var.enable_logs ? stackit_logs_instance.nebu[0].ingest_url : "disabled"
}
