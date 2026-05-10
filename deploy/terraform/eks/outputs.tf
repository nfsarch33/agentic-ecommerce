output "cluster_endpoint" {
  description = "EKS cluster API endpoint"
  value       = aws_eks_cluster.main.endpoint
}

output "cluster_name" {
  description = "EKS cluster name"
  value       = aws_eks_cluster.main.name
}

output "cluster_oidc_issuer" {
  description = "OIDC issuer URL for the EKS cluster (used for IRSA)"
  value       = aws_eks_cluster.main.identity[0].oidc[0].issuer
}

output "cluster_certificate_authority" {
  description = "Base64-encoded cluster CA certificate"
  value       = aws_eks_cluster.main.certificate_authority[0].data
  sensitive   = true
}

output "node_group_arn" {
  description = "ARN of the default managed node group"
  value       = aws_eks_node_group.default.arn
}

output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.main.id
}
