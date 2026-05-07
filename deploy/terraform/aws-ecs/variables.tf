variable "project_name" {
  description = "Project name used for placeholder resource names."
  type        = string
  default     = "agentic-ecommerce"
}

variable "environment" {
  description = "Deployment environment name."
  type        = string
  default     = "dev"
}

variable "aws_region" {
  description = "Example AWS region. Override in real accounts; this value is not tied to private infrastructure."
  type        = string
  default     = "us-east-1"
}

variable "vpc_cidr" {
  description = "Example VPC CIDR for the AWS ECS Fargate path."
  type        = string
  default     = "198.51.100.0/24"
}

variable "public_subnet_cidrs" {
  description = "Example public subnet CIDRs for ALB ingress."
  type        = list(string)
  default     = ["198.51.100.0/26", "198.51.100.64/26"]
}

variable "private_subnet_cidrs" {
  description = "Example private subnet CIDRs for ECS tasks and data stores."
  type        = list(string)
  default     = ["198.51.100.128/26", "198.51.100.192/26"]
}

variable "allowed_ingress_cidrs" {
  description = "Example ALB ingress CIDRs. Narrow this before real deployment."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "backend_image" {
  description = "Backend image repository without tag."
  type        = string
  default     = "ghcr.io/nfsarch33/agentic-ecommerce"
}

variable "frontend_image" {
  description = "Frontend image repository without tag."
  type        = string
  default     = "ghcr.io/nfsarch33/agentic-ecommerce-web"
}

variable "image_tag" {
  description = "Immutable image tag, preferably the git SHA used by CI."
  type        = string
  default     = "sha-placeholder"
}

variable "allowed_origin" {
  description = "Public frontend origin allowed by mc-api CORS."
  type        = string
  default     = "https://frontend.example.com"
}

variable "api_token_secret_name" {
  description = "AWS Secrets Manager name for ECOMMERCE_API_TOKEN."
  type        = string
  default     = "example/agentic-ecommerce/api-token"
}

variable "postgres_password_secret_name" {
  description = "AWS Secrets Manager name for the database password or full DB_URL."
  type        = string
  default     = "example/agentic-ecommerce/postgres"
}

variable "redis_auth_secret_name" {
  description = "AWS Secrets Manager name for Redis auth or endpoint metadata."
  type        = string
  default     = "example/agentic-ecommerce/redis"
}

variable "wc_consumer_key_secret_name" {
  description = "AWS Secrets Manager name for WooCommerce consumer key."
  type        = string
  default     = "example/agentic-ecommerce/wc-consumer-key"
}

variable "wc_consumer_secret_secret_name" {
  description = "AWS Secrets Manager name for WooCommerce consumer secret."
  type        = string
  default     = "example/agentic-ecommerce/wc-consumer-secret"
}

variable "fleet_ai_bridge_url_secret_name" {
  description = "AWS Secrets Manager name for the fleet AI bridge URL."
  type        = string
  default     = "example/agentic-ecommerce/fleet-ai-bridge-url"
}

variable "sync_dry_run" {
  description = "Keep WooCommerce sync dry-run by default for cloud dry-runs."
  type        = bool
  default     = true
}
