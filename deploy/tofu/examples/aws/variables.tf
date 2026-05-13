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
