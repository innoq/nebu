output "release_name" {
  description = "The name of the deployed Helm release."
  value       = helm_release.nebu.name
}

output "namespace" {
  description = "The Kubernetes namespace of the Helm release."
  value       = helm_release.nebu.namespace
}

output "status" {
  description = "The current status of the Helm release."
  value       = helm_release.nebu.status
}
