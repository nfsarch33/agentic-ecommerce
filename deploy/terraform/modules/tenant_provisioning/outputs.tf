output "tenant_contracts" {
  description = "Per-tenant placeholder contracts (billing webhook, CDN, secret paths, auto-scaling) keyed by tenant_id."
  value       = local.tenant_contracts
}

output "tenant_ids" {
  description = "Sorted list of tenant_ids declared in this module instance."
  value       = local.tenant_summary
}

output "deployment_contract" {
  description = "Provider-neutral tenant fan-out contract for dry-run review."
  value       = local.deployment_contract
}
