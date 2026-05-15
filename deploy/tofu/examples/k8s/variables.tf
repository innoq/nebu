variable "kubeconfig_path" {
  description = "Path to the kubeconfig file used by the kubernetes and helm providers."
  type        = string
  default     = "~/.kube/config"
}

variable "kube_context" {
  description = "Kubernetes context to use from the kubeconfig file. Leave empty to use the active context."
  type        = string
  default     = ""
}

variable "gateway_image_tag" {
  description = "Container image tag for the Nebu gateway component (e.g. '0.3.0' or 'dev')."
  type        = string
}

variable "core_image_tag" {
  description = "Container image tag for the Nebu core component (e.g. '0.3.0' or 'dev')."
  type        = string
}

variable "namespace" {
  description = "Kubernetes namespace to deploy Nebu into."
  type        = string
  default     = "nebu"
}

variable "chart_path" {
  description = "Path to the Nebu Helm chart directory (relative to the k8s example root)."
  type        = string
  default     = "../../../helm/nebu"
}

variable "ingress_enabled" {
  description = "Enable the Ingress resource for external access."
  type        = bool
  default     = false
}

