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

module "nebu_aws" {
  source = "../../modules/nebu-aws"

  vpc_cidr    = var.vpc_cidr
  environment = var.environment

  # Database — Aurora Serverless v2
  db_password         = var.db_password
  skip_final_snapshot = var.skip_final_snapshot
  aurora_min_capacity = var.aurora_min_capacity
  aurora_max_capacity = var.aurora_max_capacity

  # Compute — task definitions reference nebu-core outputs for image/version
  aws_region          = var.aws_region
  image_registry      = module.nebu_core.image_registry
  nebu_version        = module.nebu_core.nebu_version
  acm_certificate_arn = var.acm_certificate_arn
  ecs_desired_count   = var.ecs_desired_count

  common_tags = {
    Project     = "nebu"
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}
