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

variable "gcp_project_id" {
  description = "Example GCP project id placeholder. Override with a real project outside version control."
  type        = string
  default     = "example-project"
}

variable "gcp_region" {
  description = "Example GCP region. Override in real accounts; this value is not tied to private infrastructure."
  type        = string
  default     = "us-central1"
}

variable "vpc_connector_cidr" {
  description = "Example Serverless VPC Access connector CIDR."
  type        = string
  default     = "203.0.113.0/28"
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
  description = "Minimum Cloud Run instance count for mc-api."
  type        = number
  default     = 1
}

variable "mc_api_max_instances" {
  description = "Maximum Cloud Run instance count for mc-api."
  type        = number
  default     = 20
}

variable "temporal_worker_min_instances" {
  description = "Minimum Cloud Run instance count for temporal-worker."
  type        = number
  default     = 1
}

variable "temporal_worker_max_instances" {
  description = "Maximum Cloud Run instance count for temporal-worker."
  type        = number
  default     = 10
}

variable "allowed_origin" {
  description = "Public frontend origin allowed by mc-api CORS."
  type        = string
  default     = "https://frontend.example.com"
}

variable "api_token_secret_name" {
  description = "GCP Secret Manager name for ECOMMERCE_API_TOKEN."
  type        = string
  default     = "agentic-ecommerce-api-token"
}

variable "jwt_secret_name" {
  description = "GCP Secret Manager name for ECOMMERCE_JWT_SECRET."
  type        = string
  default     = "agentic-ecommerce-jwt-secret"
}

variable "admin_username_secret_name" {
  description = "GCP Secret Manager name for ECOMMERCE_ADMIN_USERNAME."
  type        = string
  default     = "agentic-ecommerce-admin-username"
}

variable "admin_password_secret_name" {
  description = "GCP Secret Manager name for ECOMMERCE_ADMIN_PASSWORD."
  type        = string
  default     = "agentic-ecommerce-admin-password"
}

variable "postgres_password_secret_name" {
  description = "GCP Secret Manager name for the database password or full DB_URL."
  type        = string
  default     = "agentic-ecommerce-postgres"
}

variable "redis_auth_secret_name" {
  description = "GCP Secret Manager name for Redis auth or endpoint metadata."
  type        = string
  default     = "agentic-ecommerce-redis"
}

variable "wc_consumer_key_secret_name" {
  description = "GCP Secret Manager name for WooCommerce consumer key."
  type        = string
  default     = "agentic-ecommerce-wc-consumer-key"
}

variable "wc_store_url_secret_name" {
  description = "GCP Secret Manager name for WooCommerce store URL."
  type        = string
  default     = "agentic-ecommerce-wc-store-url"
}

variable "wc_consumer_secret_secret_name" {
  description = "GCP Secret Manager name for WooCommerce consumer secret."
  type        = string
  default     = "agentic-ecommerce-wc-consumer-secret"
}

variable "wc_webhook_secret_name" {
  description = "GCP Secret Manager name for WooCommerce webhook HMAC secret."
  type        = string
  default     = "agentic-ecommerce-wc-webhook-secret"
}

variable "fleet_ai_bridge_url_secret_name" {
  description = "GCP Secret Manager name for the fleet AI bridge URL."
  type        = string
  default     = "agentic-ecommerce-fleet-ai-bridge-url"
}

variable "embedding_bridge_url_secret_name" {
  description = "GCP Secret Manager name for the fleet embedding bridge URL."
  type        = string
  default     = "agentic-ecommerce-embedding-bridge-url"
}

variable "media_bucket_name" {
  description = "Optional GCS media bucket name placeholder. Leave empty to derive one from project and environment."
  type        = string
  default     = ""
}

variable "media_object_prefix" {
  description = "Object key prefix reserved for media assets."
  type        = string
  default     = "media/"
}

variable "media_public_base_url" {
  description = "Optional Cloud CDN or GCS public URL placeholder for media assets."
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
