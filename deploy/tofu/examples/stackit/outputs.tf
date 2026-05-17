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

output "tls_certificate_expiry" {
  description = "Let's Encrypt certificate expiry date. null when enable_tls = false. Run `tofu apply` before this date (min_days_remaining = 30 triggers automatic renewal)."
  value       = try(acme_certificate.nebu[0].certificate_not_after, null)
}
