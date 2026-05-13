output "floating_ip" {
  description = "Public (floating) IP address assigned to the Nebu VM."
  value       = stackit_public_ip.nebu.ip
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
