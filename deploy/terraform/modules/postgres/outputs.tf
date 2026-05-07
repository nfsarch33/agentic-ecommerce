output "engine_label" {
  description = "Managed PostgreSQL service family selected by provider."
  value       = local.engine_label
}

output "instance_name" {
  description = "Placeholder RDS or Cloud SQL instance name."
  value       = local.normalized_name
}

output "host_placeholder" {
  description = "Non-routable placeholder host showing where application DB_URL should point after provisioning."
  value       = local.host_placeholder
}

output "port" {
  description = "PostgreSQL port."
  value       = local.port
}

output "database_name" {
  description = "Application database name."
  value       = var.database_name
}

output "admin_username" {
  description = "Database administrator username."
  value       = var.admin_username
}

output "connection_secret_ref" {
  description = "Secret reference that should hold the full ECOMMERCE_DB_URL at deploy time."
  value       = local.connection_secret_ref
}

output "migration_command" {
  description = "Repo-local SQL migration command to run from a one-off task or trusted release runner after infrastructure is ready."
  value       = "make migrate-up DB_URL=<resolved ECOMMERCE_DB_URL>"
}
