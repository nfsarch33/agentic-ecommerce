terraform {
  required_version = ">= 1.6.0"
}

locals {
  provider_name = "aws"

  common_backend_env = {
    ECOMMERCE_ALLOWED_ORIGIN                  = var.allowed_origin
    ECOMMERCE_JWT_ISSUER                      = "agentic-ecommerce"
    ECOMMERCE_JWT_AUDIENCE                    = "mc-api"
    ECOMMERCE_JWT_ACCESS_TTL                  = "15m"
    ECOMMERCE_REFRESH_TTL                     = "24h"
    ECOMMERCE_ADMIN_ROLE                      = "admin"
    ECOMMERCE_RATE_LIMIT_CAPACITY             = "120"
    ECOMMERCE_RATE_LIMIT_REFILL               = "1m"
    ECOMMERCE_EVENTBUS_DRIVER                 = "redis"
    ECOMMERCE_EVENTBUS_CHANNEL_SYNC           = "ec.sync.events"
    ECOMMERCE_EVENTBUS_CHANNEL_DLQ            = "ec.sync.deadletter"
    ECOMMERCE_EMBEDDING_MODEL                 = "minimax-embedding-01"
    ECOMMERCE_EMBEDDING_DIMENSIONS            = "1536"
    ECOMMERCE_RAG_CHUNK_SIZE                  = "1000"
    ECOMMERCE_MEDIA_STORAGE_DRIVER            = module.media_store.storage_driver
    ECOMMERCE_MEDIA_STORE                     = module.media_store.storage_driver
    ECOMMERCE_MEDIA_BASE_PATH                 = module.media_store.object_prefix
    ECOMMERCE_MEDIA_BUCKET                    = module.media_store.bucket_name
    ECOMMERCE_MEDIA_PUBLIC_BASE_URL           = module.media_store.public_base_url_placeholder
    ECOMMERCE_MEDIA_REGION                    = var.aws_region
    ECOMMERCE_MEDIA_MAX_SIZE_BYTES            = tostring(var.media_max_size_bytes)
    ECOMMERCE_MEDIA_ALLOWED_MIME_TYPES        = join(",", var.media_allowed_mime_types)
    ECOMMERCE_COMPLIANCE_MAX_IMAGE_SIZE_BYTES = tostring(var.media_max_size_bytes)
    ECOMMERCE_COMPLIANCE_ALLOWED_MIME_TYPES   = join(",", var.media_allowed_mime_types)
  }

  common_backend_secrets = {
    ECOMMERCE_DB_URL               = module.postgres.connection_secret_ref
    ECOMMERCE_REDIS_ADDR           = module.redis.endpoint_secret_ref
    ECOMMERCE_JWT_SECRET           = "aws-secretsmanager:${var.jwt_secret_name}"
    ECOMMERCE_ADMIN_USERNAME       = "aws-secretsmanager:${var.admin_username_secret_name}"
    ECOMMERCE_ADMIN_PASSWORD       = "aws-secretsmanager:${var.admin_password_secret_name}"
    ECOMMERCE_API_TOKEN            = "aws-secretsmanager:${var.api_token_secret_name}"
    ECOMMERCE_AI_BRIDGE_URL        = "aws-secretsmanager:${var.fleet_ai_bridge_url_secret_name}"
    ECOMMERCE_EMBEDDING_BRIDGE_URL = "aws-secretsmanager:${var.embedding_bridge_url_secret_name}"
    ECOMMERCE_WC_CONSUMER_KEY      = "aws-secretsmanager:${var.wc_consumer_key_secret_name}"
    ECOMMERCE_WC_CONSUMER_SECRET   = "aws-secretsmanager:${var.wc_consumer_secret_secret_name}"
  }
}

module "network" {
  source = "../modules/network"

  provider_name         = local.provider_name
  name_prefix           = var.project_name
  environment           = var.environment
  cidr_block            = var.vpc_cidr
  public_subnet_cidrs   = var.public_subnet_cidrs
  private_subnet_cidrs  = var.private_subnet_cidrs
  allowed_ingress_cidrs = var.allowed_ingress_cidrs
}

module "postgres" {
  source = "../modules/postgres"

  provider_name        = local.provider_name
  name_prefix          = var.project_name
  environment          = var.environment
  private_network_id   = module.network.network_id
  password_secret_name = var.postgres_password_secret_name
  instance_class       = "db.t4g.micro"
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
  node_type                  = "cache.t4g.micro"
  transit_encryption_enabled = true
}

module "media_store" {
  source = "../modules/objectstore"

  provider_name                = local.provider_name
  name_prefix                  = var.project_name
  environment                  = var.environment
  bucket_name                  = var.media_bucket_name
  object_prefix                = var.media_object_prefix
  public_base_url              = var.media_public_base_url
  runtime_service_account_name = "${var.project_name}-${var.environment}-ecs-task"
}

module "mc_api_service" {
  source = "../modules/service"

  provider_name        = local.provider_name
  runtime_target       = "ecs-fargate"
  name_prefix          = var.project_name
  environment          = var.environment
  service_name         = "mc-api"
  image                = var.backend_image
  image_tag            = var.image_tag
  container_port       = 8080
  cpu                  = 512
  memory_mb            = 1024
  min_instances        = 1
  max_instances        = 3
  health_check_path    = "/readyz"
  allow_public_ingress = true
  network_id           = module.network.network_id
  private_subnet_ids   = module.network.private_subnet_ids
  security_group_id    = module.network.security_group_id
  service_account_name = "${var.project_name}-${var.environment}-ecs-task"
  env_vars             = merge(local.common_backend_env, { ECOMMERCE_HTTP_ADDR = "0.0.0.0:8080" })
  secret_env_vars      = local.common_backend_secrets
}

module "wc_sync_service" {
  source = "../modules/service"

  provider_name        = local.provider_name
  runtime_target       = "ecs-fargate"
  name_prefix          = var.project_name
  environment          = var.environment
  service_name         = "wc-sync"
  image                = var.backend_image
  image_tag            = "${var.image_tag}-wc-sync"
  cpu                  = 256
  memory_mb            = 512
  min_instances        = 0
  max_instances        = 1
  health_check_path    = "/healthz"
  allow_public_ingress = false
  network_id           = module.network.network_id
  private_subnet_ids   = module.network.private_subnet_ids
  security_group_id    = module.network.security_group_id
  service_account_name = "${var.project_name}-${var.environment}-ecs-task"
  env_vars = {
    ECOMMERCE_SYNC_MODE    = "once"
    ECOMMERCE_SYNC_DRY_RUN = tostring(var.sync_dry_run)
  }
  secret_env_vars = local.common_backend_secrets
}

module "agent_worker_service" {
  source = "../modules/service"

  provider_name        = local.provider_name
  runtime_target       = "ecs-fargate"
  name_prefix          = var.project_name
  environment          = var.environment
  service_name         = "agent-worker"
  image                = var.backend_image
  image_tag            = "${var.image_tag}-agent-worker"
  container_port       = 8081
  cpu                  = 512
  memory_mb            = 1024
  min_instances        = 1
  max_instances        = 2
  health_check_path    = "/healthz"
  allow_public_ingress = false
  network_id           = module.network.network_id
  private_subnet_ids   = module.network.private_subnet_ids
  security_group_id    = module.network.security_group_id
  service_account_name = "${var.project_name}-${var.environment}-ecs-task"
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

  provider_name        = local.provider_name
  runtime_target       = "ecs-fargate"
  name_prefix          = var.project_name
  environment          = var.environment
  service_name         = "frontend"
  image                = var.frontend_image
  image_tag            = var.image_tag
  container_port       = 3000
  cpu                  = 512
  memory_mb            = 1024
  min_instances        = 1
  max_instances        = 3
  health_check_path    = "/"
  allow_public_ingress = true
  network_id           = module.network.network_id
  private_subnet_ids   = module.network.private_subnet_ids
  security_group_id    = module.network.security_group_id
  service_account_name = "${var.project_name}-${var.environment}-ecs-task"
  env_vars = {
    NODE_ENV        = "production"
    MC_API_BASE_URL = module.mc_api_service.url_placeholder
  }
  secret_env_vars = {
    FLEET_AI_BRIDGE_URL = "aws-secretsmanager:${var.fleet_ai_bridge_url_secret_name}"
  }
}
