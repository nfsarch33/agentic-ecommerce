output "service_name" {
  description = "Placeholder service name."
  value       = local.normalized_name
}

output "runtime_label" {
  description = "Human-readable runtime target."
  value       = local.runtime_label
}

output "image_ref" {
  description = "Immutable image reference for this service."
  value       = local.image_ref
}

output "url_placeholder" {
  description = "Placeholder URL showing the expected public endpoint shape."
  value       = local.url_placeholder
}

output "endpoint_placeholder" {
  description = "Placeholder endpoint for HTTP, gRPC, or worker-style services."
  value       = local.endpoint_placeholder
}

output "log_destination" {
  description = "CloudWatch Logs group or Cloud Logging service label placeholder."
  value       = local.log_destination
}

output "deployment_contract" {
  description = "Normalized service deployment contract for provider-specific implementations."
  value       = local.deployment_contract
}
