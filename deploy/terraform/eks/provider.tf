terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  backend "s3" {
    bucket = "agentic-ecommerce-terraform-state"
    key    = "eks/terraform.tfstate"
    region = "ap-southeast-2"
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      app         = "agentic-ecommerce"
      environment = var.environment
      managed_by  = "terraform"
      role        = "disaster-recovery"
    }
  }
}
