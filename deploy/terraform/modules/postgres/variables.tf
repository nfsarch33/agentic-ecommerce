variable "provider_name" {
  description = "Cloud provider contract this PostgreSQL service is intended for. Supported values: aws, gcp."
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

variable "database_name" {
  description = "Application database name."
  type        = string
  default     = "ecommerce"
}

variable "admin_username" {
  description = "Database administrator username. Do not pass passwords through Terraform variables."
  type        = string
  default     = "ecommerce"
}

variable "engine_version" {
  description = "PostgreSQL major version for RDS or Cloud SQL."
  type        = string
  default     = "16"
}

variable "instance_class" {
  description = "Provider-specific instance size placeholder, such as db.t4g.micro or db-custom-1-3840."
  type        = string
  default     = "placeholder-small"
}

variable "allocated_storage_gb" {
  description = "Initial storage size placeholder for managed PostgreSQL."
  type        = number
  default     = 20
}

variable "high_availability" {
  description = "Whether the real deployment should use Multi-AZ or regional high availability."
  type        = bool
  default     = false
}

variable "deletion_protection" {
  description = "Whether the real deployment should protect the database from accidental deletion."
  type        = bool
  default     = true
}

variable "private_network_id" {
  description = "Placeholder VPC or VPC network identifier from the network module."
  type        = string
}

variable "password_secret_name" {
  description = "Secret Manager or Secrets Manager name containing the database password."
  type        = string
}
