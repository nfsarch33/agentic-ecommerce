output "cluster_name" {
  description = "Placeholder Kubernetes cluster name."
  value       = local.cluster_name
}

output "cluster_endpoint" {
  description = "Non-routable placeholder cluster API endpoint."
  value       = local.cluster_endpoint_placeholder
}

output "kubeconfig_command" {
  description = "CLI command to configure kubeconfig for the target cluster."
  value       = local.kubeconfig_command
}

output "is_autopilot" {
  description = "Whether GKE Autopilot mode is enabled."
  value       = var.enable_autopilot && local.is_gcp
}

output "node_pool_config" {
  description = "Node pool configuration (null for GKE Autopilot)."
  value       = local.node_pool_config
}

output "cluster_contract" {
  description = "Normalized cluster deployment contract for downstream consumers."
  value       = local.cluster_contract
}
