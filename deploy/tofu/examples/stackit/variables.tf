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

# ── PostgresFlex sizing variables ─────────────────────────────────────────────

variable "postgres_replicas" {
  description = "Number of PostgresFlex replicas. Use 1 for dev/testing. Use 3 for production HA."
  type        = number
  default     = 1
}

variable "postgres_cpu" {
  description = "CPU cores for the PostgresFlex instance flavor."
  type        = number
  default     = 1
}

variable "postgres_ram" {
  description = "RAM in GB for the PostgresFlex instance flavor."
  type        = number
  default     = 4
}

variable "postgres_storage_size" {
  description = "Storage size in GB for the PostgresFlex instance."
  type        = number
  default     = 20
}

# ── cloud-init / bootstrap variables ─────────────────────────────────────────

variable "internal_secret" {
  description = "Shared PSK used for gateway ↔ core node registration (see ADR-008). Injected into .secrets/internal_secret at first boot."
  type        = string
  sensitive   = true

  validation {
    condition     = length(var.internal_secret) >= 16
    error_message = "internal_secret must be at least 16 characters. Use 'openssl rand -hex 32' to generate."
  }
}

variable "oidc_client_secret" {
  description = "Required. When oidc_mode = 'dex': embedded into Dex staticClients config (same value used by both gateway and Dex). When oidc_mode = 'external': must match the client secret registered in your external OIDC provider. Minimum 16 characters."
  type        = string
  sensitive   = true

  validation {
    condition     = length(var.oidc_client_secret) >= 16
    error_message = "oidc_client_secret must be at least 16 characters."
  }

  validation {
    condition     = !strcontains(var.oidc_client_secret, "\"") && !strcontains(var.oidc_client_secret, "\\")
    error_message = "oidc_client_secret must not contain double-quote or backslash characters (YAML interpolation constraint)."
  }
}

variable "oidc_issuer" {
  description = "OIDC issuer URL. Required when oidc_mode = 'external' (e.g. 'https://auth.example.com/realms/nebu'). When oidc_mode = 'dex', this value is ignored — the issuer is automatically set to 'http://<server_name>:5556/dex'. The gateway reaches Dex via Docker hairpin NAT through the VM's public IP (standard Linux SNAT/masquerade)."
  type        = string
  default     = "" # Empty default enables `tofu validate` without providing a value
}

variable "oidc_mode" {
  description = "OIDC deployment profile. 'dex': deploy Dex as a sidecar (static config, no database — for test/demo environments). 'external': no bundled IdP; operator must provide oidc_issuer and oidc_client_secret (for production with a managed OIDC provider)."
  type        = string
  default     = "external"

  validation {
    condition     = contains(["dex", "external"], var.oidc_mode)
    error_message = "oidc_mode must be 'dex' or 'external'."
  }
}

variable "dex_allowed_cidr" {
  description = "CIDR block allowed to reach Dex on port 5556 (only used when oidc_mode = 'dex'). Restrict to your test network or developer IP range. Default '0.0.0.0/0' is intentionally permissive for demo setups — always restrict in shared environments."
  type        = string
  default     = "0.0.0.0/0"

  validation {
    condition     = can(cidrnetmask(var.dex_allowed_cidr))
    error_message = "dex_allowed_cidr must be a valid CIDR block (e.g. '10.0.0.0/8' or '203.0.113.1/32')."
  }
}

variable "dex_static_password_hash" {
  description = "bcrypt hash for the Dex static user (operator@example.com). Required when oidc_mode = 'dex'. Generate with: htpasswd -bnBC 12 '' 'yourpassword' | tr -d ':' | sed 's/$2y/$2a/'. This value is written to dex/config.yaml on the VM (mode 0600)."
  type        = string
  sensitive   = true
  default     = null

  validation {
    condition     = var.dex_static_password_hash == null || startswith(var.dex_static_password_hash, "$2a$") || startswith(var.dex_static_password_hash, "$2b$") || startswith(var.dex_static_password_hash, "$2y$")
    error_message = "dex_static_password_hash must be a bcrypt hash starting with '$2a$', '$2b$', or '$2y$' (all are equivalent bcrypt variants)."
  }
}

variable "server_name" {
  description = "Public server name / domain for the Nebu instance (e.g. 'chat.example.com'). Used as NEBU_SERVER_NAME inside containers."
  type        = string
}

variable "nebu_version" {
  description = "Nebu container image tag to deploy. Must be a specific semver tag (e.g. '1.0.0'). Never use 'latest' in production."
  type        = string
  default     = ""

  validation {
    condition     = var.nebu_version != "latest"
    error_message = "Use a specific version tag in production. Set nebu_version to a semver tag like '1.0.0'."
  }
}

variable "image_registry" {
  description = "Container image registry prefix (e.g. 'registry.gitlab.com/myorg/open-chat'). Images pulled as <image_registry>/nebu-gateway:<nebu_version> and <image_registry>/nebu-core:<nebu_version>."
  type        = string
}
