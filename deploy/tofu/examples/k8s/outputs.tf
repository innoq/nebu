output "helm_release_name" {
  description = "The name of the deployed Nebu Helm release."
  value       = module.nebu_k8s.release_name
}

output "namespace" {
  description = "The Kubernetes namespace where Nebu is deployed."
  value       = module.nebu_k8s.namespace
}
