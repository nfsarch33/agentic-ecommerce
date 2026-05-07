terraform {
  required_version = ">= 1.6.0"
}

locals {
  normalized_name = lower(replace("${var.name_prefix}-${var.environment}", "_", "-"))
  is_aws          = var.provider_name == "aws"
  is_gcp          = var.provider_name == "gcp"

  network_id = local.is_aws ? "${local.normalized_name}-vpc" : "${local.normalized_name}-vpc-network"

  private_subnet_ids = [
    for index, _ in var.private_subnet_cidrs : "${local.normalized_name}-private-${index + 1}"
  ]

  public_subnet_ids = [
    for index, _ in var.public_subnet_cidrs : "${local.normalized_name}-public-${index + 1}"
  ]

  vpc_connector_name = local.is_gcp ? "${local.normalized_name}-connector" : null
}
