terraform {
  required_version = ">= 1.6.0"
}

locals {
  cdn_label = var.provider_name == "aws" ? "CloudFront" : "Cloud CDN"

  # tenant_contracts is a map keyed by tenant_id with the exact
  # placeholder shape the application TenantSecretStore + per-tenant
  # CDN routing expect at deploy time. Each entry is contract-only;
  # the actual provider resources (Secrets Manager rows, CloudFront
  # distributions, Cloud Run revisions) are added in a reviewed
  # cloud-hardening slice.
  tenant_contracts = {
    for id, t in var.tenants : id => {
      tenant_id      = id
      plan           = t.plan
      region         = t.region
      provider_label = local.cdn_label

      billing_webhook = {
        path               = t.billing_webhook_path
        public_url_stub    = "https://${id}.${var.environment}.${var.name_prefix}.tenant.placeholder${t.billing_webhook_path}"
        provider_label     = var.provider_name
        rate_limit_per_min = 60
      }

      cdn = {
        enabled                = t.cdn_enabled
        provider_label         = local.cdn_label
        hostname_placeholder   = t.cdn_enabled ? (t.cdn_hostname_suffix != "" ? "${id}.${t.cdn_hostname_suffix}" : "${id}.cdn.placeholder") : ""
        viewer_protocol_policy = "redirect-to-https"
        invalidation_topic     = "tenant-${id}-cdn-invalidations"
        notes                  = "Per-tenant CDN distribution; backend triggers cache invalidation on media uploads via internal/billing usage_meter event."
      }

      secrets = {
        path_prefix = var.secret_path_prefix
        keys        = t.secret_keys
        # Resource paths the application TenantSecretStore adapter
        # constructs; see internal/adapter/awssecrets and
        # internal/adapter/gcpsecrets.
        paths = [for k in t.secret_keys : "${var.secret_path_prefix}/${id}/${k}"]
      }

      auto_scaling = {
        mc_api          = var.auto_scaling_targets.mc_api
        temporal_worker = var.auto_scaling_targets.temporal_worker
        agent_worker    = var.auto_scaling_targets.agent_worker
      }
    }
  }

  # tenant_summary is a stable, sorted list keyed by tenant_id used
  # by contract tests so the output diff is deterministic.
  tenant_summary = sort(keys(local.tenant_contracts))

  deployment_contract = {
    provider_name        = var.provider_name
    environment          = var.environment
    secret_path_prefix   = var.secret_path_prefix
    auto_scaling_targets = var.auto_scaling_targets
    tenant_count         = length(local.tenant_contracts)
    tenant_ids           = local.tenant_summary
    notes                = "Placeholder contract only; per-tenant Secrets Manager rows, CDN distributions, and auto-scaling policies are provisioned in a reviewed cloud-hardening slice."
  }
}
