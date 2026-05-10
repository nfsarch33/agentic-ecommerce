# Network Module

Cloud-agnostic VPC/VCN abstraction for the Agentic E-Commerce stack.

## Supported Providers

| Provider | Service | Notes |
|----------|---------|-------|
| `gcp` | VPC Network | VPC connector for Cloud Run egress |
| `aws` | VPC | Public/private subnets with CIDR allocation |

## Usage

```hcl
module "network" {
  source = "../modules/network"

  provider_name       = "aws"
  name_prefix         = "ec"
  environment         = "prod"
  cidr_block          = "10.0.0.0/16"
  private_subnet_cidrs = ["10.0.1.0/24", "10.0.2.0/24"]
  public_subnet_cidrs  = ["10.0.101.0/24", "10.0.102.0/24"]
}
```

## Inputs

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `provider_name` | string | — | `aws` or `gcp` |
| `name_prefix` | string | — | Resource name prefix |
| `environment` | string | `dev` | Environment label |
| `cidr_block` | string | `null` | AWS VPC CIDR |
| `private_subnet_cidrs` | list(string) | `[]` | Private subnet CIDRs |
| `public_subnet_cidrs` | list(string) | `[]` | Public subnet CIDRs |

## Outputs

| Name | Description |
|------|-------------|
| `network_id` | VPC/network identifier |
| `private_subnet_ids` | Private subnet IDs |
| `public_subnet_ids` | Public subnet IDs |
| `security_group_id` | AWS security group (null for GCP) |
