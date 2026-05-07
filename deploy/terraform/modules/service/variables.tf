variable "provider_name" {
  description = "Cloud provider contract this service is intended for. Supported values: aws, gcp."
  type        = string

  validation {
    condition     = contains(["aws", "gcp"], var.provider_name)
    error_message = "provider_name must be either aws or gcp."
  }
}

variable "runtime_target" {
  description = "Container runtime pattern. Supported values: ecs-fargate, cloud-run."
  type        = string

  validation {
    condition     = contains(["ecs-fargate", "cloud-run"], var.runtime_target)
    error_message = "runtime_target must be ecs-fargate or cloud-run."
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

variable "service_name" {
  description = "Logical service name, such as mc-api, wc-sync, frontend, or agent-worker."
  type        = string
}

variable "image" {
  description = "Container image repository without tag."
  type        = string
}

variable "image_tag" {
  description = "Immutable image tag, preferably a short git SHA."
  type        = string
}

variable "container_port" {
  description = "Container port exposed by HTTP services."
  type        = number
  default     = 8080
}

variable "cpu" {
  description = "Runtime CPU placeholder. ECS examples use CPU units; Cloud Run examples use vCPU count."
  type        = number
  default     = 512
}

variable "memory_mb" {
  description = "Runtime memory limit in MiB."
  type        = number
  default     = 1024
}

variable "min_instances" {
  description = "Minimum task or instance count."
  type        = number
  default     = 1
}

variable "max_instances" {
  description = "Maximum task or instance count."
  type        = number
  default     = 3
}

variable "health_check_path" {
  description = "HTTP health check path for load balancer or Cloud Run startup/liveness checks."
  type        = string
  default     = "/healthz"
}

variable "env_vars" {
  description = "Non-secret environment variables safe to store in Terraform state."
  type        = map(string)
  default     = {}
}

variable "secret_env_vars" {
  description = "Environment variable names mapped to Secret Manager or Secrets Manager references."
  type        = map(string)
  default     = {}
}

variable "network_id" {
  description = "Placeholder VPC or VPC network identifier."
  type        = string
  default     = null
}

variable "private_subnet_ids" {
  description = "Placeholder AWS private subnet identifiers for ECS Fargate services."
  type        = list(string)
  default     = []
}

variable "security_group_id" {
  description = "Placeholder AWS security group identifier for ECS tasks."
  type        = string
  default     = null
}

variable "cloud_run_vpc_connector" {
  description = "Placeholder GCP Serverless VPC Access connector name for Cloud Run egress."
  type        = string
  default     = null
}

variable "service_account_name" {
  description = "Runtime service account name for least-privilege cloud access."
  type        = string
  default     = null
}

variable "allow_public_ingress" {
  description = "Whether this service is intended to receive public ingress through a managed load balancer or HTTPS endpoint."
  type        = bool
  default     = false
}
