terraform {
  required_version = ">= 1.6.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  # Backend configuration: configure before use.
  # See deploy/README.md for backend options (S3, local filesystem).
  backend "s3" {
    # Configure via -backend-config flags or terraform.tfvars:
    #   bucket         = "my-tofu-state"
    #   key            = "nebu/aws/terraform.tfstate"
    #   region         = "eu-central-1"
    #   dynamodb_table = "tofu-locks"
  }
}

provider "aws" {
  region = var.aws_region
}

module "nebu_core" {
  source = "../../modules/nebu-core"

  nebu_version     = var.nebu_version
  domain_name      = var.domain_name
  admin_email      = var.admin_email
  postgres_db_name = var.postgres_db_name
  image_registry   = var.image_registry
}

# ── AWS-specific resources are added in Story 13-2 ──────────────────────────
# Provisioned here: VPC, subnets, security groups, ECS Fargate cluster,
# task definitions, ALB, ACM certificate, RDS PostgreSQL, S3 media bucket.
# See ADR-014: AWS — ECS Fargate.
