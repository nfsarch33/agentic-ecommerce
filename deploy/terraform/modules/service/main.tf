terraform {
  required_version = ">= 1.6.0"
}

locals {
  normalized_name = lower(replace("${var.name_prefix}-${var.environment}-${var.service_name}", "_", "-"))
  image_ref       = "${var.image}:${var.image_tag}"

  runtime_label = var.runtime_target == "ecs-fargate" ? "AWS ECS Fargate" : "GCP Cloud Run"

  log_destination = var.provider_name == "aws" ? "/ecs/${local.normalized_name}" : "cloud-run/${local.normalized_name}"

  url_placeholder = (
    var.runtime_target == "ecs-fargate"
    ? "https://${local.normalized_name}.alb.placeholder"
    : "https://${local.normalized_name}.run.app"
  )

  deployment_contract = {
    service_name         = var.service_name
    runtime_target       = var.runtime_target
    runtime_label        = local.runtime_label
    image_ref            = local.image_ref
    container_port       = var.container_port
    cpu                  = var.cpu
    memory_mb            = var.memory_mb
    min_instances        = var.min_instances
    max_instances        = var.max_instances
    health_check_path    = var.health_check_path
    allow_public_ingress = var.allow_public_ingress
    network_id           = var.network_id
    private_subnet_ids   = var.private_subnet_ids
    security_group_id    = var.security_group_id
    cloud_run_connector  = var.cloud_run_vpc_connector
    service_account_name = var.service_account_name
    non_secret_env_names = sort(keys(var.env_vars))
    secret_env_names     = sort(keys(var.secret_env_vars))
    log_destination      = local.log_destination
    service_url_preview  = local.url_placeholder
  }
}
