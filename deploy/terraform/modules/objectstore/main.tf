terraform {
  required_version = ">= 1.6.0"
}

locals {
  normalized_name = lower(replace("${var.name_prefix}-${var.environment}-media", "_", "-"))
  bucket_name     = var.bucket_name != "" ? var.bucket_name : local.normalized_name
  driver          = var.provider_name == "aws" ? "s3" : "gcs"
  cdn_label       = var.provider_name == "aws" ? "CloudFront" : "Cloud CDN"
  cdn_hostname    = var.cdn_hostname != "" ? var.cdn_hostname : var.provider_name == "aws" ? "${local.bucket_name}.cloudfront.placeholder" : "${local.bucket_name}.cdn.placeholder"

  public_base_url_placeholder = (
    var.public_base_url != ""
    ? var.public_base_url
    : var.cdn_stub_enabled ? "https://${local.cdn_hostname}" : var.provider_name == "aws" ? "https://${local.bucket_name}.s3.amazonaws.com" : "https://storage.googleapis.com/${local.bucket_name}"
  )

  cdn_contract = {
    enabled                = var.cdn_stub_enabled
    provider_label         = local.cdn_label
    hostname_placeholder   = local.cdn_hostname
    origin_bucket          = local.bucket_name
    origin_access_required = true
    allowed_methods        = var.cdn_allowed_methods
    viewer_protocol_policy = var.cdn_viewer_protocol_policy
    default_ttl_seconds    = var.cdn_default_ttl_seconds
    max_ttl_seconds        = var.cdn_max_ttl_seconds
    notes                  = "Placeholder contract only; keep bucket public access blocked and route public reads through the CDN origin identity when provider resources are added."
  }

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
    cdn                          = local.cdn_contract
    notes                        = "Placeholder contract only; add provider resources, IAM, encryption, lifecycle, bucket public-access block, and CDN origin access in a reviewed cloud-hardening slice."
  }
}
