terraform {
  required_version = ">= 1.6.0"
}

locals {
  normalized_name = lower(replace("${var.name_prefix}-${var.environment}-media", "_", "-"))
  bucket_name     = var.bucket_name != "" ? var.bucket_name : local.normalized_name
  driver          = var.provider_name == "aws" ? "s3" : "gcs"

  public_base_url_placeholder = (
    var.public_base_url != ""
    ? var.public_base_url
    : var.provider_name == "aws" ? "https://${local.bucket_name}.s3.amazonaws.com" : "https://storage.googleapis.com/${local.bucket_name}"
  )

  deployment_contract = {
    provider_name                = var.provider_name
    storage_driver               = local.driver
    bucket_name                  = local.bucket_name
    object_prefix                = var.object_prefix
    public_base_url_placeholder  = local.public_base_url_placeholder
    versioning_enabled           = var.versioning_enabled
    noncurrent_retention_days    = var.noncurrent_retention_days
    force_destroy_allowed        = var.force_destroy_allowed
    runtime_service_account_name = var.runtime_service_account_name
    notes                        = "Placeholder contract only; add provider resources, IAM, encryption, lifecycle, and CDN in a reviewed cloud-hardening slice."
  }
}
