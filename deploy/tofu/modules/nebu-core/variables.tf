# nebu-core: Shared input variables consumed by all platform modules.
# This module contains NO provider resources — only variables, validations, and outputs.

variable "nebu_version" {
  description = "Nebu container image tag to deploy (e.g. '0.3.0' or 'latest')."
  type        = string

  validation {
    condition     = can(regex("^(latest|[0-9]+\\.[0-9]+\\.[0-9]+([-+][a-zA-Z0-9._-]+)?)$", var.nebu_version))
    error_message = "nebu_version must be 'latest' or a semantic version string such as '0.3.0' or '1.0.0-rc.1'."
  }
}

variable "domain_name" {
  description = "Public domain name for the Nebu instance (e.g. 'chat.example.com')."
  type        = string

  validation {
    condition     = length(trimspace(var.domain_name)) > 0
    error_message = "domain_name must not be empty."
  }
}

variable "admin_email" {
  description = "Email address of the initial instance administrator. Used for TLS certificate issuance via ACME."
  type        = string
}

variable "postgres_db_name" {
  description = "PostgreSQL database name for Nebu."
  type        = string
  default     = "nebu"
}

variable "image_registry" {
  description = "Container image registry prefix (e.g. 'registry.gitlab.com/myorg/open-chat')."
  type        = string

  validation {
    condition     = length(trimspace(var.image_registry)) > 0
    error_message = "image_registry must not be empty."
  }
}
