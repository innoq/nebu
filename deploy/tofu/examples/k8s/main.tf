terraform {
  required_version = ">= 1.6.0"

  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.0"
    }
    helm = {
      source  = "hashicorp/helm"
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

provider "kubernetes" {
  config_path    = var.kubeconfig_path
  config_context = var.kube_context
}

provider "helm" {
  kubernetes {
    config_path    = var.kubeconfig_path
    config_context = var.kube_context
  }
}

module "nebu_k8s" {
  source = "../../modules/nebu-k8s"

  chart_path        = var.chart_path
  namespace         = var.namespace
  gateway_image_tag = var.gateway_image_tag
  core_image_tag    = var.core_image_tag
  ingress_enabled   = var.ingress_enabled
  # values_files paths are resolved relative to the module file (path.module).
  # Use an absolute path here to avoid CWD-dependent resolution.
  values_files = ["${path.module}/../../../helm/nebu/values-dev.yaml"]
}
