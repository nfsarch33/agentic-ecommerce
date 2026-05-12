terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

variable "primary_endpoint" {
  description = "Primary GKE cluster load balancer IP/hostname"
  type        = string
}

variable "dr_endpoint" {
  description = "DR EKS cluster load balancer IP/hostname"
  type        = string
}

variable "domain_name" {
  description = "The domain name for the application"
  type        = string
  default     = "api.agentic-ecommerce.example.com"
}

variable "hosted_zone_id" {
  description = "Route53 hosted zone ID"
  type        = string
}

resource "aws_route53_health_check" "primary" {
  fqdn              = var.primary_endpoint
  port              = 443
  type              = "HTTPS"
  resource_path     = "/health/ready"
  failure_threshold = 3
  request_interval  = 30

  tags = {
    Name        = "agentic-ecommerce-primary-health"
    environment = "production"
    role        = "primary"
  }
}

resource "aws_route53_health_check" "dr" {
  fqdn              = var.dr_endpoint
  port              = 443
  type              = "HTTPS"
  resource_path     = "/health/ready"
  failure_threshold = 3
  request_interval  = 30

  tags = {
    Name        = "agentic-ecommerce-dr-health"
    environment = "dr"
    role        = "disaster-recovery"
  }
}

resource "aws_route53_record" "primary" {
  zone_id = var.hosted_zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = var.primary_endpoint
    zone_id                = var.hosted_zone_id
    evaluate_target_health = true
  }

  set_identifier  = "primary"
  health_check_id = aws_route53_health_check.primary.id

  failover_routing_policy {
    type = "PRIMARY"
  }
}

resource "aws_route53_record" "dr" {
  zone_id = var.hosted_zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = var.dr_endpoint
    zone_id                = var.hosted_zone_id
    evaluate_target_health = true
  }

  set_identifier  = "dr"
  health_check_id = aws_route53_health_check.dr.id

  failover_routing_policy {
    type = "SECONDARY"
  }
}

output "primary_health_check_id" {
  description = "Route53 health check ID for the primary GKE endpoint"
  value       = aws_route53_health_check.primary.id
}

output "dr_health_check_id" {
  description = "Route53 health check ID for the DR EKS endpoint"
  value       = aws_route53_health_check.dr.id
}
