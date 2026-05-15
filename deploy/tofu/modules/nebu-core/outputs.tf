# nebu-core: Outputs — re-exported variables for consumption by platform modules.

output "nebu_version" {
  description = "Nebu container image tag."
  value       = var.nebu_version
}

output "domain_name" {
  description = "Public domain name for the Nebu instance."
  value       = var.domain_name
}

output "admin_email" {
  description = "Email address of the initial instance administrator."
  value       = var.admin_email
}

output "postgres_db_name" {
  description = "PostgreSQL database name."
  value       = var.postgres_db_name
}

output "image_registry" {
  description = "Container image registry prefix."
  value       = var.image_registry
}
