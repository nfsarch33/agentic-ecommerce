variable "provider_name" {
  description = "Cloud provider contract this Redis service is intended for. Supported values: aws, gcp."
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

variable "engine_version" {
  description = "Redis engine version for ElastiCache or Memorystore."
  type        = string
  default     = "7.0"
}

variable "node_type" {
  description = "Provider-specific cache size placeholder."
  type        = string
  default     = "placeholder-small"
}

variable "memory_size_gb" {
  description = "GCP Memorystore memory size placeholder. AWS examples can leave the default."
  type        = number
  default     = 1
}

variable "private_network_id" {
  description = "Placeholder VPC or VPC network identifier from the network module."
  type        = string
}

variable "auth_secret_name" {
  description = "Secret Manager or Secrets Manager name containing the Redis auth token when enabled."
  type        = string
  default     = null
}

variable "transit_encryption_enabled" {
  description = "Whether the real Redis deployment should require TLS in transit."
  type        = bool
  default     = true
}
