terraform {
  required_version = ">= 1.6.0"

  required_providers {
    stackit = {
      source  = "stackitcloud/stackit"
      version = "~> 0.38"
    }
  }

  # Backend configuration: configure before use.
  # See deploy/README.md for backend options (Stackit Object Storage, local).
  # STACKIT Object Storage is S3-compatible — use the S3 backend with a STACKIT endpoint.
  backend "s3" {
    # Configure via -backend-config flags or terraform.tfvars:
    #   bucket   = "my-tofu-state"
    #   key      = "nebu/stackit/terraform.tfstate"
    #   endpoint = "https://object.storage.eu01.onstackit.cloud"
    #   region   = "eu01"
    #   skip_credentials_validation = true
    #   skip_region_validation      = true
    #   skip_metadata_api_check     = true
  }
}

provider "stackit" {
  region = var.stackit_region
}

module "nebu_core" {
  source = "../../modules/nebu-core"

  nebu_version     = var.nebu_version
  domain_name      = var.domain_name
  admin_email      = var.admin_email
  postgres_db_name = var.postgres_db_name
  image_registry   = var.image_registry
}

# ── STACKIT-specific resources are added in Story 13-3 ──────────────────────
# Provisioned here: STACKIT VPC, subnets, security groups, VMs, cloud-init,
# Stackit Application Load Balancer, DBaaS PostgreSQL, Object Storage.
# See ADR-014: Stackit — VMs + Docker Compose + Application Load Balancer.
