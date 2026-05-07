terraform {
  required_version = ">= 1.6.0"
}

locals {
  normalized_name = lower(replace("${var.name_prefix}-${var.environment}-redis", "_", "-"))
  engine_label    = var.provider_name == "aws" ? "elasticache-redis" : "memorystore-redis"
  port            = 6379

  host_placeholder = var.provider_name == "aws" ? "${local.normalized_name}.cache-placeholder.amazonaws.com" : "${local.normalized_name}.memorystore.placeholder"

  endpoint_secret_ref = (
    var.auth_secret_name == null
    ? null
    : var.provider_name == "aws" ? "aws-secretsmanager:${var.auth_secret_name}" : "gcp-secret-manager:${var.auth_secret_name}"
  )
}
