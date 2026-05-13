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
