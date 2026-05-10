variable "project_id" {
  type        = string
  description = "GCP project ID for the GKE cluster and associated resources."
}

variable "region" {
  type        = string
  default     = "australia-southeast1"
  description = "GCP region for the GKE cluster."
}

variable "cluster_name" {
  type        = string
  default     = "agentic-ecommerce"
  description = "Name of the GKE Autopilot cluster."
}

variable "network" {
  type        = string
  default     = ""
  description = "Pre-existing VPC network self_link. Leave empty to create a new VPC."
}

variable "subnetwork" {
  type        = string
  default     = ""
  description = "Pre-existing subnetwork self_link. Leave empty to create a new subnetwork."
}

variable "pods_cidr_range" {
  type        = string
  default     = "10.4.0.0/14"
  description = "Secondary CIDR for GKE pods."
}

variable "services_cidr_range" {
  type        = string
  default     = "10.8.0.0/20"
  description = "Secondary CIDR for GKE services."
}

variable "db_tier" {
  type        = string
  default     = "db-custom-2-7680"
  description = "Cloud SQL machine tier."
}

variable "db_version" {
  type        = string
  default     = "POSTGRES_16"
  description = "Cloud SQL Postgres version."
}

variable "db_name" {
  type        = string
  default     = "ecommerce"
  description = "Cloud SQL default database name."
}

variable "redis_memory_size_gb" {
  type        = number
  default     = 1
  description = "Memorystore for Redis instance size in GB."
}

variable "redis_version" {
  type        = string
  default     = "REDIS_7_0"
  description = "Memorystore Redis version."
}

variable "environment" {
  type        = string
  default     = "production"
  description = "Deployment environment label (production, staging, dev)."
}

variable "labels" {
  type        = map(string)
  default     = {}
  description = "Additional labels to apply to all resources."
}
