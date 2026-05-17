# nebu-k8s: Thin OpenTofu wrapper around a helm_release resource.
# Manages the Nebu Helm release on a Kubernetes cluster.
# Providers (kubernetes + helm) must be configured by the caller.

resource "helm_release" "nebu" {
  name             = var.release_name
  chart            = var.chart_path
  namespace        = var.namespace
  create_namespace = true

  # wait=true blocks tofu apply until all pods are Ready (timeout: var.helm_timeout seconds).
  wait    = true
  timeout = var.helm_timeout

  # values_files entries must be non-empty absolute paths. Relative paths are resolved
  # from the working directory of the tofu apply invocation, not the module directory.
  # Use path.module-relative absolute paths in the calling root module to avoid CWD issues.
  values = [for f in var.values_files : file(f)]

  set {
    name  = "gateway.image.tag"
    value = var.gateway_image_tag
  }

  set {
    name  = "core.image.tag"
    value = var.core_image_tag
  }

  set {
    name  = "ingress.enabled"
    value = var.ingress_enabled
  }
}
