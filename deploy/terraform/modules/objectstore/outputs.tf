output "storage_driver" {
  description = "Storage driver value expected by backend runtime configuration."
  value       = local.driver
}

output "bucket_name" {
  description = "Placeholder S3 or GCS bucket name."
  value       = local.bucket_name
}

output "object_prefix" {
  description = "Object key prefix reserved for media assets."
  value       = var.object_prefix
}

output "public_base_url_placeholder" {
  description = "Credential-free public URL placeholder for CDN or direct object URLs."
  value       = local.public_base_url_placeholder
}

output "cdn_contract" {
  description = "Provider-specific CDN placeholder contract for media assets."
  value       = local.cdn_contract
}

output "deployment_contract" {
  description = "Provider-neutral object-store deployment contract for dry-run review."
  value       = local.deployment_contract
}
