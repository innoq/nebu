terraform {
  required_version = ">= 1.6.0"

  required_providers {
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.0"
    }
  }

  # Backend configuration: configure before use.
  # See deploy/README.md for backend options.
  # For Kubernetes-native workflows, any S3-compatible or PostgreSQL backend works.
  # WARNING: Do NOT use a local backend for team use — state will not be shared.
  # Use an S3-compatible backend (AWS S3, STACKIT Object Storage, MinIO) or a PostgreSQL backend.
  backend "s3" {
    # Configure via -backend-config flags or a backend.hcl file:
    #   bucket   = "my-nebu-tofu-state"
    #   key      = "nebu/k8s/terraform.tfstate"
    #   endpoint = "https://..."   # S3-compatible endpoint (AWS, STACKIT, MinIO)
    #   region   = "eu-central-1"
  }
}

provider "helm" {
  kubernetes {
    config_path    = var.kubeconfig_path
    config_context = var.kube_context
  }
}

provider "kubernetes" {
  config_path    = var.kubeconfig_path
  config_context = var.kube_context
}

module "nebu_core" {
  source = "../../modules/nebu-core"

  nebu_version     = var.nebu_version
  domain_name      = var.domain_name
  admin_email      = var.admin_email
  postgres_db_name = var.postgres_db_name
  image_registry   = var.image_registry
}

# ── Kubernetes/Helm resources are added in Story 13-4 ───────────────────────
# Provisioned here: helm_release for nebu chart, cert-manager (optional),
# ingress-nginx (optional), ConfigMap + Secret references.
# See ADR-014: Kubernetes / Helm.
