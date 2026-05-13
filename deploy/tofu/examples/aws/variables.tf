variable "aws_region" {
  description = "AWS region to deploy into (e.g. 'eu-central-1')."
  type        = string
  default     = "eu-central-1"
}

variable "nebu_version" {
  description = "Nebu container image tag to deploy."
  type        = string
}

variable "domain_name" {
  description = "Public domain name for the Nebu instance."
  type        = string
}

variable "admin_email" {
  description = "Email for the initial instance administrator and TLS certificate issuance."
  type        = string
}

variable "postgres_db_name" {
  description = "PostgreSQL database name."
  type        = string
  default     = "nebu"
}

variable "image_registry" {
  description = "Container image registry prefix."
  type        = string
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC."
  type        = string
  default     = "10.0.0.0/16"
}

variable "environment" {
  description = "Deployment environment name (e.g. 'prod', 'staging'). Used for resource tagging."
  type        = string
  default     = "prod"
}

variable "db_password" {
  description = "Initial master password for the Aurora PostgreSQL cluster. Sensitive — do not commit."
  type        = string
  sensitive   = true
  default     = "changeme"
}

variable "skip_final_snapshot" {
  description = "When true, no final DB snapshot is created on deletion. Set to false for production."
  type        = bool
  default     = true
}

variable "aurora_min_capacity" {
  description = "Minimum Aurora Serverless v2 capacity in ACUs. Set to 0 for dev (scale-to-zero). Set to 0.5 for production to avoid cold-start latency."
  type        = number
  default     = 0
}

variable "aurora_max_capacity" {
  description = "Maximum Aurora Serverless v2 capacity in ACUs. Default 4 is sufficient for Nebu MVP load. Increase for high-traffic production."
  type        = number
  default     = 4
}

variable "acm_certificate_arn" {
  description = "ARN of the ACM certificate for the ALB HTTPS listener. Must be in the same AWS region."
  type        = string
  default     = ""
}

variable "ecs_desired_count" {
  description = "Desired number of running ECS tasks for gateway and core services."
  type        = number
  default     = 1
}

variable "dns_mode" {
  description = "DNS record creation mode. 'default': OpenTofu creates DNS records in the cloud provider's DNS service (Route 53 for AWS, Stackit DNS for Stackit). 'external': No DNS resources are created; the operator registers the ALB hostname/IP in their own DNS server. The 'dns_name' output shows what to register. Default is 'external' to prevent accidental DNS changes on existing deployments."
  type        = string
  default     = "external"

  validation {
    condition     = contains(["default", "external"], var.dns_mode)
    error_message = "dns_mode must be 'default' or 'external'."
  }
}
