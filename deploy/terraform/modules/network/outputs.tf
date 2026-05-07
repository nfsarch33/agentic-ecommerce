output "provider_name" {
  description = "Cloud provider selected for the network contract."
  value       = var.provider_name
}

output "network_id" {
  description = "Placeholder VPC or VPC network identifier consumed by service/database modules."
  value       = local.network_id
}

output "cidr_block" {
  description = "AWS VPC CIDR placeholder when provider_name is aws."
  value       = var.cidr_block
}

output "public_subnet_ids" {
  description = "Placeholder public subnet identifiers for AWS load balancers."
  value       = local.public_subnet_ids
}

output "private_subnet_ids" {
  description = "Placeholder private subnet identifiers for private compute and data services."
  value       = local.private_subnet_ids
}

output "security_group_id" {
  description = "Placeholder AWS security group identifier for ECS tasks."
  value       = var.provider_name == "aws" ? "${local.normalized_name}-service-sg" : null
}

output "vpc_connector_name" {
  description = "Placeholder GCP Serverless VPC Access connector name for Cloud Run egress."
  value       = local.vpc_connector_name
}

output "allowed_ingress_cidrs" {
  description = "CIDR ranges expected at the public ingress boundary."
  value       = var.allowed_ingress_cidrs
}
