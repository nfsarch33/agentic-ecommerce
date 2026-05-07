terraform {
  required_version = ">= 1.6.0"
}

locals {
  normalized_name = lower(replace("${var.name_prefix}-${var.environment}-postgres", "_", "-"))
  engine_label    = var.provider_name == "aws" ? "rds-postgresql" : "cloudsql-postgresql"
  port            = 5432

  host_placeholder = var.provider_name == "aws" ? "${local.normalized_name}.cluster-placeholder.rds.amazonaws.com" : "${local.normalized_name}.private.cloudsql.placeholder"

  connection_secret_ref = var.provider_name == "aws" ? "aws-secretsmanager:${var.password_secret_name}" : "gcp-secret-manager:${var.password_secret_name}"
}
