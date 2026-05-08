variable "project_name" {
  description = "Project name used for placeholder resource names."
  type        = string
  default     = "agentic-ecommerce"
}

variable "environment" {
  description = "Deployment environment name."
  type        = string
  default     = "dev"
}

variable "aws_region" {
  description = "Example AWS region. Override in real accounts; this value is not tied to private infrastructure."
  type        = string
  default     = "us-east-1"
}

variable "vpc_cidr" {
  description = "Example VPC CIDR for the AWS ECS Fargate path."
  type        = string
  default     = "198.51.100.0/24"
}

variable "public_subnet_cidrs" {
  description = "Example public subnet CIDRs for ALB ingress."
  type        = list(string)
  default     = ["198.51.100.0/26", "198.51.100.64/26"]
}

variable "private_subnet_cidrs" {
  description = "Example private subnet CIDRs for ECS tasks and data stores."
  type        = list(string)
  default     = ["198.51.100.128/26", "198.51.100.192/26"]
}

variable "allowed_ingress_cidrs" {
  description = "Example ALB ingress CIDRs. Narrow this before real deployment."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "backend_image" {
  description = "Backend image repository without tag."
  type        = string
  default     = "ghcr.io/nfsarch33/agentic-ecommerce"
}

variable "frontend_image" {
  description = "Frontend image repository without tag."
  type        = string
  default     = "ghcr.io/nfsarch33/agentic-ecommerce-web"
}

variable "image_tag" {
  description = "Immutable image tag, preferably the git SHA used by CI."
  type        = string
  default     = "sha-placeholder"
}

variable "temporal_image" {
  description = "Temporal server image repository without tag."
  type        = string
  default     = "temporalio/temporal"
}

variable "temporal_image_tag" {
  description = "Temporal server image tag placeholder. Pin before real deployment."
  type        = string
  default     = "1.26.2"
}

variable "temporal_namespace" {
  description = "Temporal namespace used by backend workflow clients and workers."
  type        = string
  default     = "default"
}

variable "temporal_task_queue" {
  description = "Temporal task queue used by ecommerce workflows."
  type        = string
  default     = "ec-workflows"
}

variable "mc_api_min_instances" {
  description = "Minimum ECS task count for mc-api."
  type        = number
  default     = 1
}

variable "mc_api_max_instances" {
  description = "Maximum ECS task count for mc-api. v2.7.0 raises the marketplace ceiling to 20 to accommodate multi-tenant load."
  type        = number
  default     = 20
}

variable "temporal_worker_min_instances" {
  description = "Minimum ECS task count for temporal-worker."
  type        = number
  default     = 1
}

variable "temporal_worker_max_instances" {
  description = "Maximum ECS task count for temporal-worker. v2.7.0 raises to 10 to absorb queue spikes from marketplace plugin events."
  type        = number
  default     = 10
}

variable "agent_worker_min_instances" {
  description = "Minimum ECS task count for agent-worker."
  type        = number
  default     = 1
}

variable "agent_worker_max_instances" {
  description = "Maximum ECS task count for agent-worker. v2.7.0 raises to 15 for multi-tenant agent run queue absorption."
  type        = number
  default     = 15
}

variable "tenants" {
  description = <<-EOT
    Map of tenant_id -> per-tenant configuration consumed by the
    tenant_fanout module (billing webhook path, CDN preferences,
    secret keys, region, plan).
  EOT
  type = map(object({
    plan                 = optional(string, "free")
    region               = optional(string, "us-east-1")
    billing_webhook_path = optional(string, "/webhooks/stripe")
    cdn_enabled          = optional(bool, true)
    cdn_hostname_suffix  = optional(string, "")
    secret_keys          = optional(list(string), [])
  }))
  default = {}
}

variable "tenant_secret_path_prefix" {
  description = "Prefix for AWS Secrets Manager paths the application TenantSecretStore adapter uses."
  type        = string
  default     = "agentic-ecommerce"
}

variable "allowed_origin" {
  description = "Public frontend origin allowed by mc-api CORS."
  type        = string
  default     = "https://frontend.example.com"
}

variable "api_token_secret_name" {
  description = "AWS Secrets Manager name for ECOMMERCE_API_TOKEN."
  type        = string
  default     = "example/agentic-ecommerce/api-token"
}

variable "jwt_secret_name" {
  description = "AWS Secrets Manager name for ECOMMERCE_JWT_SECRET."
  type        = string
  default     = "example/agentic-ecommerce/jwt-secret"
}

variable "admin_username_secret_name" {
  description = "AWS Secrets Manager name for ECOMMERCE_ADMIN_USERNAME."
  type        = string
  default     = "example/agentic-ecommerce/admin-username"
}

variable "admin_password_secret_name" {
  description = "AWS Secrets Manager name for ECOMMERCE_ADMIN_PASSWORD."
  type        = string
  default     = "example/agentic-ecommerce/admin-password"
}

variable "postgres_password_secret_name" {
  description = "AWS Secrets Manager name for the database password or full DB_URL."
  type        = string
  default     = "example/agentic-ecommerce/postgres"
}

variable "redis_auth_secret_name" {
  description = "AWS Secrets Manager name for Redis auth or endpoint metadata."
  type        = string
  default     = "example/agentic-ecommerce/redis"
}

variable "wc_consumer_key_secret_name" {
  description = "AWS Secrets Manager name for WooCommerce consumer key."
  type        = string
  default     = "example/agentic-ecommerce/wc-consumer-key"
}

variable "wc_store_url_secret_name" {
  description = "AWS Secrets Manager name for WooCommerce store URL."
  type        = string
  default     = "example/agentic-ecommerce/wc-store-url"
}

variable "wc_consumer_secret_secret_name" {
  description = "AWS Secrets Manager name for WooCommerce consumer secret."
  type        = string
  default     = "example/agentic-ecommerce/wc-consumer-secret"
}

variable "wc_webhook_secret_name" {
  description = "AWS Secrets Manager name for WooCommerce webhook HMAC secret."
  type        = string
  default     = "example/agentic-ecommerce/wc-webhook-secret"
}

variable "fleet_ai_bridge_url_secret_name" {
  description = "AWS Secrets Manager name for the fleet AI bridge URL."
  type        = string
  default     = "example/agentic-ecommerce/fleet-ai-bridge-url"
}

variable "embedding_bridge_url_secret_name" {
  description = "AWS Secrets Manager name for the fleet embedding bridge URL."
  type        = string
  default     = "example/agentic-ecommerce/embedding-bridge-url"
}

variable "media_bucket_name" {
  description = "Optional S3 media bucket name placeholder. Leave empty to derive one from project and environment."
  type        = string
  default     = ""
}

variable "media_object_prefix" {
  description = "Object key prefix reserved for media assets."
  type        = string
  default     = "media/"
}

variable "media_public_base_url" {
  description = "Optional CloudFront or S3 public URL placeholder for media assets."
  type        = string
  default     = ""
}

variable "media_max_size_bytes" {
  description = "Maximum media object size accepted by backend validation."
  type        = number
  default     = 5242880
}

variable "media_allowed_mime_types" {
  description = "Allowed media MIME types for backend validation."
  type        = list(string)
  default     = ["image/jpeg", "image/png", "image/webp"]
}

variable "sync_dry_run" {
  description = "Keep WooCommerce sync dry-run by default for cloud dry-runs."
  type        = bool
  default     = true
}
