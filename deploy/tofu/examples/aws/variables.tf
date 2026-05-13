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

variable "db_instance_class" {
  description = "RDS instance class (e.g. 'db.t3.medium', 'db.r6g.large')."
  type        = string
  default     = "db.t3.medium"
}

variable "db_password" {
  description = "Initial master password for the RDS PostgreSQL instance. Sensitive — do not commit."
  type        = string
  sensitive   = true
  default     = "changeme"
}

variable "skip_final_snapshot" {
  description = "When true, no final DB snapshot is created on deletion. Set to false for production."
  type        = bool
  default     = true
}

variable "nebu_secrets_arn" {
  description = "ARN of the AWS Secrets Manager secret containing Nebu runtime env vars. Leave empty ('') to skip — validate passes without real credentials."
  type        = string
  default     = ""
}

variable "enable_performance_insights" {
  description = "Enable RDS Performance Insights. Disable for db.t3.micro or unsupported instance classes."
  type        = bool
  default     = true
}
