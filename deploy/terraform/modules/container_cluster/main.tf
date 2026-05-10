terraform {
  required_version = ">= 1.6.0"
}

locals {
  normalized_name = lower(replace("${var.name_prefix}-${var.environment}", "_", "-"))
  is_aws          = var.provider_name == "aws"
  is_gcp          = var.provider_name == "gcp"

  cluster_name = "${local.normalized_name}-cluster"

  cluster_endpoint_placeholder = (
    local.is_aws
    ? "https://${local.cluster_name}.eks.placeholder.amazonaws.com"
    : "https://${local.cluster_name}.gke.placeholder.googleapis.com"
  )

  kubeconfig_command = (
    local.is_aws
    ? "aws eks update-kubeconfig --name ${local.cluster_name} --region <REGION>"
    : "gcloud container clusters get-credentials ${local.cluster_name} --region <REGION> --project <PROJECT_ID>"
  )

  node_pool_config = var.enable_autopilot && local.is_gcp ? null : {
    machine_type = var.machine_type
    desired_size = var.node_count
    min_size     = var.node_min_count
    max_size     = var.node_max_count
  }

  cluster_contract = {
    provider_name   = var.provider_name
    cluster_name    = local.cluster_name
    cluster_version = var.cluster_version
    endpoint        = local.cluster_endpoint_placeholder
    kubeconfig_cmd  = local.kubeconfig_command
    autopilot       = var.enable_autopilot && local.is_gcp
    node_pool       = local.node_pool_config
    network_id      = var.network_id
    private_subnets = var.private_subnet_ids
    public_subnets  = var.public_subnet_ids
  }
}
