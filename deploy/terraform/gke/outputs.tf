output "cluster_endpoint" {
  value       = google_container_cluster.autopilot.endpoint
  sensitive   = true
  description = "GKE Autopilot cluster API endpoint."
}

output "cluster_ca_certificate" {
  value       = google_container_cluster.autopilot.master_auth[0].cluster_ca_certificate
  sensitive   = true
  description = "Base64-encoded cluster CA certificate."
}

output "cluster_name" {
  value       = google_container_cluster.autopilot.name
  description = "GKE cluster name."
}

output "database_connection_name" {
  value       = google_sql_database_instance.postgres.connection_name
  description = "Cloud SQL connection name for the Cloud SQL Auth Proxy."
}

output "database_private_ip" {
  value       = google_sql_database_instance.postgres.private_ip_address
  sensitive   = true
  description = "Cloud SQL private IP address."
}

output "redis_host" {
  value       = google_redis_instance.cache.host
  sensitive   = true
  description = "Memorystore Redis host IP."
}

output "redis_port" {
  value       = google_redis_instance.cache.port
  description = "Memorystore Redis port."
}

output "vpc_network" {
  value       = local.network_name
  description = "VPC network self_link used by the cluster."
}
