# Tenant Provisioning Module

Terraform module for automated tenant resource provisioning in the Agentic E-Commerce multi-tenant architecture.

## Usage

```hcl
module "tenant" {
  source = "../modules/tenant_provisioning"

  name_prefix = "ec"
  environment = "prod"
  tenant_id   = "tenant-abc123"
}
```

See `variables.tf` for the full input specification and `outputs.tf` for available outputs.
