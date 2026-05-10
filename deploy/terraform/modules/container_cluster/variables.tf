variable "provider_name" {
  description = "Cloud provider for the container cluster. Supported values: aws, gcp."
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

variable "cluster_version" {
  description = "Kubernetes version for the managed cluster."
  type        = string
  default     = "1.30"
}

variable "node_count" {
  description = "Desired node count for the default node pool. Ignored for GKE Autopilot."
  type        = number
  default     = 3
}

variable "node_min_count" {
  description = "Minimum node count for autoscaling."
  type        = number
  default     = 1
}

variable "node_max_count" {
  description = "Maximum node count for autoscaling."
  type        = number
  default     = 6
}

variable "machine_type" {
  description = "Provider-specific machine type placeholder (e.g. e2-standard-4, t3.medium)."
  type        = string
  default     = "placeholder-standard"
}

variable "enable_autopilot" {
  description = "GKE only: enable Autopilot mode (ignores node pool settings when true)."
  type        = bool
  default     = true
}

variable "network_id" {
  description = "Placeholder VPC or VPC network identifier from the network module."
  type        = string
}

variable "private_subnet_ids" {
  description = "Private subnet identifiers for node placement."
  type        = list(string)
  default     = []
}

variable "public_subnet_ids" {
  description = "Public subnet identifiers for load balancers (EKS)."
  type        = list(string)
  default     = []
}

variable "pods_cidr_range" {
  description = "GKE secondary CIDR for pods."
  type        = string
  default     = null
}

variable "services_cidr_range" {
  description = "GKE secondary CIDR for services."
  type        = string
  default     = null
}
