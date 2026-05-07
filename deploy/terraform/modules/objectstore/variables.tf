variable "provider_name" {
  description = "Cloud provider contract this object store is intended for. Supported values: aws, gcp."
  type        = string

  validation {
    condition     = contains(["aws", "gcp"], var.provider_name)
    error_message = "provider_name must be either aws or gcp."
  }
}

variable "name_prefix" {
  description = "Short, public-safe prefix used when deriving placeholder bucket names."
  type        = string
}

variable "environment" {
  description = "Deployment environment name such as dev, staging, or prod."
  type        = string
  default     = "dev"
}

variable "bucket_name" {
  description = "Optional explicit bucket name placeholder. Leave empty to derive one from project and environment."
  type        = string
  default     = ""
}

variable "object_prefix" {
  description = "Object key prefix reserved for media assets."
  type        = string
  default     = "media/"
}

variable "public_base_url" {
  description = "Optional CDN or public object URL placeholder. Leave empty to emit provider-native examples."
  type        = string
  default     = ""
}

variable "cdn_stub_enabled" {
  description = "Whether to emit a provider-specific CDN placeholder contract for media reads."
  type        = bool
  default     = true
}

variable "cdn_hostname" {
  description = "Optional CDN hostname placeholder. Leave empty to derive a public-safe placeholder."
  type        = string
  default     = ""
}

variable "cdn_default_ttl_seconds" {
  description = "Default CDN cache TTL for immutable media assets."
  type        = number
  default     = 3600
}

variable "cdn_max_ttl_seconds" {
  description = "Maximum CDN cache TTL for immutable media assets."
  type        = number
  default     = 86400
}

variable "cdn_allowed_methods" {
  description = "HTTP methods allowed at the media CDN edge."
  type        = list(string)
  default     = ["GET", "HEAD", "OPTIONS"]
}

variable "cdn_viewer_protocol_policy" {
  description = "Viewer protocol policy placeholder. Use redirect-to-https or https-only for real deployments."
  type        = string
  default     = "redirect-to-https"
}

variable "versioning_enabled" {
  description = "Whether the eventual bucket should keep object versions."
  type        = bool
  default     = true
}

variable "noncurrent_retention_days" {
  description = "Example lifecycle retention for noncurrent object versions."
  type        = number
  default     = 30
}

variable "force_destroy_allowed" {
  description = "Safety flag for future provider resources. Keep false outside disposable test environments."
  type        = bool
  default     = false
}

variable "runtime_service_account_name" {
  description = "Runtime service account that should receive least-privilege object read/write access."
  type        = string
  default     = null
}
