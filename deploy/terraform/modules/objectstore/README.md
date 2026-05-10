# Object Store Module

Cloud-agnostic object storage abstraction (GCS / S3 / OCI Object Storage).

## Usage

```hcl
module "objectstore" {
  source = "../modules/objectstore"

  provider_name = "gcp"
  name_prefix   = "ec"
  environment   = "prod"
}
```

See `variables.tf` for the full input specification and `outputs.tf` for available outputs.
