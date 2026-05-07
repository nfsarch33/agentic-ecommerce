output "engine_label" {
  description = "Managed Redis service family selected by provider."
  value       = local.engine_label
}

output "instance_name" {
  description = "Placeholder ElastiCache or Memorystore instance name."
  value       = local.normalized_name
}

output "host_placeholder" {
  description = "Non-routable placeholder host showing where ECOMMERCE_REDIS_ADDR should point after provisioning."
  value       = local.host_placeholder
}

output "port" {
  description = "Redis port."
  value       = local.port
}

output "endpoint_secret_ref" {
  description = "Secret reference for Redis auth or endpoint metadata when the deployment uses secret injection."
  value       = local.endpoint_secret_ref
}

output "redis_addr_placeholder" {
  description = "Placeholder host:port form expected by ECOMMERCE_REDIS_ADDR."
  value       = "${local.host_placeholder}:${local.port}"
}
