# nebu-aws: Input variables for the AWS-specific infrastructure module.

variable "vpc_cidr" {
  description = "CIDR block for the VPC. Must be /22 or larger (prefix <= 22) to accommodate the fixed subnet offsets (0,1,10,11) used by this module."
  type        = string
  default     = "10.0.0.0/16"

  validation {
    condition     = can(cidrnetmask(var.vpc_cidr))
    error_message = "vpc_cidr must be a valid CIDR block (e.g. '10.0.0.0/16')."
  }

  validation {
    condition     = tonumber(split("/", var.vpc_cidr)[1]) <= 22
    error_message = "vpc_cidr prefix length must be /22 or larger (e.g. /16, /20, /22) to fit the four fixed /24 subnets used by this module."
  }
}

variable "environment" {
  description = "Deployment environment name (e.g. 'dev', 'staging', 'prod'). Incorporated into resource names."
  type        = string
  default     = "dev"
}

variable "availability_zones" {
  description = "List of availability zones to use. If empty, the first two AZs of the region are used automatically."
  type        = list(string)
  default     = []

  validation {
    condition     = length(var.availability_zones) == 0 || length(var.availability_zones) >= 2
    error_message = "availability_zones must be empty (use data source) or contain at least 2 AZs."
  }
}

variable "common_tags" {
  description = "Tags applied to every AWS resource created by this module."
  type        = map(string)
  default     = {}
}

# ── Database variables ────────────────────────────────────────────────────────

variable "db_instance_class" {
  description = "RDS instance class (e.g. 'db.t3.medium', 'db.r6g.large')."
  type        = string
  default     = "db.t3.medium"
}

variable "db_password" {
  description = "Initial master password for the RDS PostgreSQL instance. Must be replaced before apply in any non-dev environment. Sensitive — do not commit."
  type        = string
  sensitive   = true
  # WARNING: 'changeme' is only a placeholder for tofu validate/plan in dev.
  # Always supply a strong password via a tfvars file or environment variable at apply time.
  default = "changeme"

  validation {
    condition     = length(var.db_password) >= 8
    error_message = "db_password must be at least 8 characters."
  }
}

variable "skip_final_snapshot" {
  description = "When true, no final DB snapshot is created before the instance is deleted. Set to false for production."
  type        = bool
  default     = true
}

variable "enable_performance_insights" {
  description = "Enable RDS Performance Insights. Supported on db.t3.medium and larger. Disable for db.t3.micro or unsupported instance classes."
  type        = bool
  default     = true
}

# ── Compute variables ─────────────────────────────────────────────────────────

variable "image_registry" {
  description = "Container image registry prefix (e.g. 'registry.gitlab.com/myorg/open-chat'). Used in ECS task definitions."
  type        = string
  default     = ""
}

variable "nebu_version" {
  description = "Nebu container image tag to deploy (e.g. '0.3.0' or 'latest'). Used in ECS task definitions."
  type        = string
  default     = "latest"
}

variable "nebu_secrets_arn" {
  description = "ARN of the AWS Secrets Manager secret containing Nebu runtime env vars (NEBU_DB_URL, NEBU_OIDC_ISSUER, etc.). Leave empty ('') to skip Secrets Manager references — validate will pass without real credentials."
  type        = string
  default     = ""
}

variable "aws_region" {
  description = "AWS region for CloudWatch Logs configuration in ECS task definitions."
  type        = string
  default     = "eu-central-1"
}
