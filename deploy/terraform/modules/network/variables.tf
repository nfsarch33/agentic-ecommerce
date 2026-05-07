variable "provider_name" {
  description = "Cloud provider contract this network is intended for. Supported values: aws, gcp."
  type        = string

  validation {
    condition     = contains(["aws", "gcp"], var.provider_name)
    error_message = "provider_name must be either aws or gcp."
  }
}

variable "name_prefix" {
  description = "Short, public-safe prefix used when deriving placeholder resource names."
  type        = string
}

variable "environment" {
  description = "Deployment environment name such as dev, staging, or prod."
  type        = string
  default     = "dev"
}

variable "cidr_block" {
  description = "AWS VPC CIDR placeholder. Leave null for GCP examples that use an existing VPC."
  type        = string
  default     = null
}

variable "public_subnet_cidrs" {
  description = "AWS public subnet CIDRs for load balancers or NAT gateways."
  type        = list(string)
  default     = []
}

variable "private_subnet_cidrs" {
  description = "AWS private subnet CIDRs for ECS tasks, RDS, and ElastiCache."
  type        = list(string)
  default     = []
}

variable "vpc_connector_cidr" {
  description = "GCP Serverless VPC Access connector CIDR placeholder for Cloud Run egress."
  type        = string
  default     = null
}

variable "allowed_ingress_cidrs" {
  description = "CIDR ranges allowed at the public load-balancer boundary. Keep narrow for real deployments."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}
