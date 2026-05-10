# Redis Module

Cloud-agnostic managed Redis abstraction (Memorystore / ElastiCache).

## Supported Providers

| Provider | Service | Notes |
|----------|---------|-------|
| `gcp` | Memorystore for Redis | Standard HA tier |
| `aws` | ElastiCache Redis | Transit encryption optional |

## Usage

```hcl
module "redis" {
  source = "../modules/redis"

  provider_name      = "aws"
  name_prefix        = "ec"
  environment        = "prod"
  engine_version     = "7.0"
  node_type          = "cache.t4g.micro"
  private_network_id = module.network.network_id
}
```

## Inputs

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `provider_name` | string | — | `aws` or `gcp` |
| `name_prefix` | string | — | Resource name prefix |
| `engine_version` | string | `7.0` | Redis version |
| `node_type` | string | `placeholder-small` | Cache instance size |
| `memory_size_gb` | number | `1` | GCP Memorystore size |
| `private_network_id` | string | — | VPC from network module |
| `transit_encryption_enabled` | bool | `true` | Require TLS in transit |

## Outputs

| Name | Description |
|------|-------------|
| `host_placeholder` | Non-routable Redis host |
| `port` | Redis port (6379) |
| `redis_addr_placeholder` | host:port for ECOMMERCE_REDIS_ADDR |
