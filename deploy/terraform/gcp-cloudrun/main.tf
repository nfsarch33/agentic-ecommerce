terraform {
  required_version = ">= 1.6.0"
}

locals {
  provider_name = "gcp"

  runtime_service_account = "${var.project_name}-${var.environment}-run@${var.gcp_project_id}.iam.gserviceaccount.com"

  common_backend_env = {
    ECOMMERCE_ALLOWED_ORIGIN        = var.allowed_origin
    ECOMMERCE_JWT_ISSUER           = "agentic-ecommerce"
    ECOMMERCE_JWT_AUDIENCE         = "mc-api"
    ECOMMERCE_JWT_ACCESS_TTL       = "15m"
    ECOMMERCE_REFRESH_TTL          = "24h"
    ECOMMERCE_ADMIN_ROLE           = "admin"
    ECOMMERCE_RATE_LIMIT_CAPACITY  = "120"
    ECOMMERCE_RATE_LIMIT_REFILL    = "1m"
    ECOMMERCE_EVENTBUS_DRIVER       = "redis"
    ECOMMERCE_EVENTBUS_CHANNEL_SYNC = "ec.sync.events"
    ECOMMERCE_EVENTBUS_CHANNEL_DLQ  = "ec.sync.deadletter"
    ECOMMERCE_MEDIA_STORE           = "object"
  }

  common_backend_secrets = {
    ECOMMERCE_DB_URL             = module.postgres.connection_secret_ref
    ECOMMERCE_REDIS_ADDR         = module.redis.endpoint_secret_ref
    ECOMMERCE_JWT_SECRET         = "gcp-secret-manager:${var.jwt_secret_name}"
    ECOMMERCE_ADMIN_USERNAME     = "gcp-secret-manager:${var.admin_username_secret_name}"
    ECOMMERCE_ADMIN_PASSWORD     = "gcp-secret-manager:${var.admin_password_secret_name}"
    ECOMMERCE_API_TOKEN          = "gcp-secret-manager:${var.api_token_secret_name}"
    ECOMMERCE_AI_BRIDGE_URL      = "gcp-secret-manager:${var.fleet_ai_bridge_url_secret_name}"
    ECOMMERCE_WC_CONSUMER_KEY    = "gcp-secret-manager:${var.wc_consumer_key_secret_name}"
    ECOMMERCE_WC_CONSUMER_SECRET = "gcp-secret-manager:${var.wc_consumer_secret_secret_name}"
  }
}

module "network" {
  source = "../modules/network"

  provider_name         = local.provider_name
  name_prefix           = var.project_name
  environment           = var.environment
  vpc_connector_cidr    = var.vpc_connector_cidr
  allowed_ingress_cidrs = ["0.0.0.0/0"]
}

module "postgres" {
  source = "../modules/postgres"

  provider_name        = local.provider_name
  name_prefix          = var.project_name
  environment          = var.environment
  private_network_id   = module.network.network_id
  password_secret_name = var.postgres_password_secret_name
  instance_class       = "db-custom-1-3840"
  high_availability    = false
  deletion_protection  = true
  allocated_storage_gb = 20
}

module "redis" {
  source = "../modules/redis"

  provider_name              = local.provider_name
  name_prefix                = var.project_name
  environment                = var.environment
  private_network_id         = module.network.network_id
  auth_secret_name           = var.redis_auth_secret_name
  node_type                  = "basic"
  memory_size_gb             = 1
  transit_encryption_enabled = true
}

module "mc_api_service" {
  source = "../modules/service"

  provider_name           = local.provider_name
  runtime_target          = "cloud-run"
  name_prefix             = var.project_name
  environment             = var.environment
  service_name            = "mc-api"
  image                   = var.backend_image
  image_tag               = var.image_tag
  container_port          = 8080
  cpu                     = 1
  memory_mb               = 1024
  min_instances           = 1
  max_instances           = 10
  health_check_path       = "/readyz"
  allow_public_ingress    = true
  network_id              = module.network.network_id
  cloud_run_vpc_connector = module.network.vpc_connector_name
  service_account_name    = local.runtime_service_account
  env_vars                = merge(local.common_backend_env, { ECOMMERCE_HTTP_ADDR = "0.0.0.0:8080" })
  secret_env_vars         = local.common_backend_secrets
}

module "wc_sync_service" {
  source = "../modules/service"

  provider_name           = local.provider_name
  runtime_target          = "cloud-run"
  name_prefix             = var.project_name
  environment             = var.environment
  service_name            = "wc-sync-job"
  image                   = var.backend_image
  image_tag               = "${var.image_tag}-wc-sync"
  cpu                     = 1
  memory_mb               = 512
  min_instances           = 0
  max_instances           = 1
  health_check_path       = "/healthz"
  allow_public_ingress    = false
  network_id              = module.network.network_id
  cloud_run_vpc_connector = module.network.vpc_connector_name
  service_account_name    = local.runtime_service_account
  env_vars = {
    ECOMMERCE_SYNC_MODE    = "once"
    ECOMMERCE_SYNC_DRY_RUN = tostring(var.sync_dry_run)
  }
  secret_env_vars = local.common_backend_secrets
}

module "agent_worker_service" {
  source = "../modules/service"

  provider_name           = local.provider_name
  runtime_target          = "cloud-run"
  name_prefix             = var.project_name
  environment             = var.environment
  service_name            = "agent-worker"
  image                   = var.backend_image
  image_tag               = "${var.image_tag}-agent-worker"
  container_port          = 8081
  cpu                     = 1
  memory_mb               = 1024
  min_instances           = 1
  max_instances           = 3
  health_check_path       = "/healthz"
  allow_public_ingress    = false
  network_id              = module.network.network_id
  cloud_run_vpc_connector = module.network.vpc_connector_name
  service_account_name    = local.runtime_service_account
  env_vars = merge(local.common_backend_env, {
    ECOMMERCE_AGENT_WORKER_ENABLED      = "true"
    ECOMMERCE_AGENT_WORKER_RUN_ONCE     = "false"
    ECOMMERCE_AGENT_WORKER_CONCURRENCY  = "1"
    ECOMMERCE_AGENT_WORKER_INTERVAL     = "5m"
    ECOMMERCE_AGENT_WORKER_METRICS_ADDR = "0.0.0.0:8081"
  })
  secret_env_vars = local.common_backend_secrets
}

module "frontend_service" {
  source = "../modules/service"

  provider_name           = local.provider_name
  runtime_target          = "cloud-run"
  name_prefix             = var.project_name
  environment             = var.environment
  service_name            = "frontend"
  image                   = var.frontend_image
  image_tag               = var.image_tag
  container_port          = 3000
  cpu                     = 1
  memory_mb               = 1024
  min_instances           = 1
  max_instances           = 10
  health_check_path       = "/"
  allow_public_ingress    = true
  network_id              = module.network.network_id
  cloud_run_vpc_connector = module.network.vpc_connector_name
  service_account_name    = local.runtime_service_account
  env_vars = {
    NODE_ENV        = "production"
    MC_API_BASE_URL = module.mc_api_service.url_placeholder
  }
  secret_env_vars = {
    FLEET_AI_BRIDGE_URL = "gcp-secret-manager:${var.fleet_ai_bridge_url_secret_name}"
  }
}
