variable "stackit_project_id" {
  description = "STACKIT project UUID to deploy into."
  type        = string
}

variable "stackit_key_path" {
  description = "Path to Stackit service account key JSON file for provider authentication. Mark as sensitive — do not commit."
  type        = string
  sensitive   = true
}

variable "ssh_public_key" {
  description = "SSH public key injected into the VM for operator access."
  type        = string
}

variable "environment" {
  description = "Deployment environment label (e.g. dev, staging, prod). Used in resource names."
  type        = string
  default     = "dev"
}

variable "region" {
  description = "STACKIT region to deploy into (e.g. eu01)."
  type        = string
  default     = "eu01"
}

variable "availability_zone" {
  description = "STACKIT availability zone for the VM (e.g. eu01-1)."
  type        = string
  default     = "eu01-1"
}

variable "network_cidr" {
  description = "IPv4 CIDR block for the Nebu network."
  type        = string
  default     = "10.0.0.0/24"
}

variable "vm_plan_id" {
  description = "STACKIT compute machine type / plan ID. Run `stackit compute server machine-types list` to find available plan IDs for your region."
  type        = string
  default     = "g2i.2"
}

variable "alb_plan_id" {
  description = "STACKIT load balancer plan ID. Run `stackit load-balancer plans list` to find available plans. p10 is the smallest billable plan."
  type        = string
  default     = "p10"
}

variable "ubuntu_image_id" {
  description = "STACKIT image ID for Ubuntu 24.04 LTS in the target region. Look up the current ID in the STACKIT portal under Compute > Images."
  type        = string
  # No default — image IDs are region-specific and change with minor releases.
  # Example (eu01, 2024): "59838a89-51b1-4892-b57f-b3caf598ee2f"
}

variable "stackit_tls_certificate_arn" {
  description = "Stackit-managed TLS certificate ARN (name) for HTTPS termination at the ALB. Must be set to a valid certificate before `tofu apply` in production. Empty string is accepted for `tofu validate` only."
  type        = string
  default     = ""
  # Note: obtain a certificate via the STACKIT portal under Load Balancing > Certificates,
  # then set this to the certificate name returned by the API.
}
