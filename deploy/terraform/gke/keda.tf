variable "keda_enabled" {
  type        = bool
  default     = false
  description = "Install KEDA via Helm for event-driven autoscaling."
}

variable "keda_version" {
  type        = string
  default     = "2.16.1"
  description = "KEDA Helm chart version."
}

resource "helm_release" "keda" {
  count = var.keda_enabled ? 1 : 0

  name       = "keda"
  repository = "https://kedacore.github.io/charts"
  chart      = "keda"
  version    = var.keda_version
  namespace  = "keda"

  create_namespace = true

  set {
    name  = "resources.operator.requests.cpu"
    value = "100m"
  }

  set {
    name  = "resources.operator.requests.memory"
    value = "128Mi"
  }

  depends_on = [google_container_cluster.autopilot]
}
