variable "provider_name" {
  description = "Cloud provider contract this tenant module is intended for. Supported values: aws, gcp."
  type        = string

  validation {
    condition     = contains(["aws", "gcp"], var.provider_name)
    error_message = "provider_name must be either aws or gcp."
  }
}

variable "name_prefix" {
  description = "Short, public-safe prefix used when deriving placeholder names. Mirrors objectstore module."
  type        = string
}

variable "environment" {
  description = "Deployment environment name (dev, staging, prod)."
  type        = string
  default     = "dev"
}

variable "tenants" {
  description = <<-EOT
    Map of tenant_id -> tenant configuration. Each tenant ships a
    plan, region, billing webhook, CDN preferences, and the secret
    keys the marketplace+billing planes expect.
  EOT
  type = map(object({
    plan                 = optional(string, "free")
    region               = optional(string, "us-east-1")
    billing_webhook_path = optional(string, "/webhooks/stripe")
    cdn_enabled          = optional(bool, true)
    cdn_hostname_suffix  = optional(string, "")
    secret_keys = optional(list(string), [
      "stripe-api-key",
      "stripe-webhook-secret",
      "license-hmac",
      "url-hmac",
      "registration-hmac",
      "marketplace-webhook",
    ])
  }))
  default = {}
}

variable "secret_path_prefix" {
  description = "Prefix for the per-tenant secret resource path. Default mirrors the application TenantSecretStore port."
  type        = string
  default     = "agentic-ecommerce"
}

variable "auto_scaling_targets" {
  description = "Per-service auto-scaling targets that the tenant fan-out should account for. Defaults match v2.7.0 marketplace targets."
  type = object({
    mc_api          = object({ target_cpu_percent = number, min_replicas = number, max_replicas = number })
    temporal_worker = object({ queue_metric = string, min_replicas = number, max_replicas = number })
    agent_worker    = object({ queue_metric = string, min_replicas = number, max_replicas = number })
  })
  default = {
    mc_api = {
      target_cpu_percent = 70
      min_replicas       = 1
      max_replicas       = 20
    }
    temporal_worker = {
      queue_metric = "temporal_queue_depth"
      min_replicas = 1
      max_replicas = 10
    }
    agent_worker = {
      queue_metric = "agent_runs_pending"
      min_replicas = 1
      max_replicas = 15
    }
  }
}
