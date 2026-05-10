# Postgres Module

Cloud-agnostic managed PostgreSQL abstraction (Cloud SQL / RDS).

## Supported Providers

| Provider | Service | Notes |
|----------|---------|-------|
| `gcp` | Cloud SQL for PostgreSQL | Private IP via VPC peering |
| `aws` | RDS PostgreSQL | Multi-AZ optional |

## Usage

```hcl
module "postgres" {
  source = "../modules/postgres"

  provider_name        = "gcp"
  name_prefix          = "ec"
  environment          = "prod"
  engine_version       = "16"
  instance_class       = "db-custom-2-7680"
  private_network_id   = module.network.network_id
  password_secret_name = "ecommerce-db-password"
}
```

## Inputs

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `provider_name` | string | — | `aws` or `gcp` |
| `name_prefix` | string | — | Resource name prefix |
| `engine_version` | string | `16` | PostgreSQL major version |
| `instance_class` | string | `placeholder-small` | Provider-specific instance size |
| `high_availability` | bool | `false` | Multi-AZ / regional HA |
| `private_network_id` | string | — | VPC from network module |
| `password_secret_name` | string | — | Secret Manager reference |

## Outputs

| Name | Description |
|------|-------------|
| `host_placeholder` | Non-routable DB host placeholder |
| `port` | PostgreSQL port (5432) |
| `connection_secret_ref` | Secret reference for DB_URL |
| `migration_command` | Command to run migrations |
