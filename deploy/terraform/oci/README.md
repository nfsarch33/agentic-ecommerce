# OCI Terraform — EC Stack mem0 Infrastructure

Provisions Oracle Cloud Infrastructure resources for running mem0 + Qdrant as a
secondary/DR deployment target for the EC stack memory layer.

## Prerequisites

- Terraform >= 1.6.0
- An OCI tenancy with API key authentication configured
- An Ubuntu image OCID for your target region

## Authentication

All credentials are passed via variables (never hardcoded). Set them in a
`terraform.tfvars` file (git-ignored) or via environment variables:

```bash
export TF_VAR_tenancy_ocid="$OCI_TENANCY_OCID"
export TF_VAR_user_ocid="$OCI_USER_OCID"
export TF_VAR_fingerprint="$OCI_FINGERPRINT"
export TF_VAR_private_key_path="$OCI_PRIVATE_KEY_PATH"
export TF_VAR_region="ap-sydney-1"
export TF_VAR_mem0_image_ocid="<your-ubuntu-image-ocid>"
export TF_VAR_ssh_public_key="$(cat $SSH_PUBLIC_KEY_PATH)"
```

## Usage

```bash
cd deploy/terraform/oci
terraform init
terraform validate   # structural check — no OCI account needed
terraform plan
terraform apply
```

## Resources Created

| Resource                | Description                              |
|-------------------------|------------------------------------------|
| Compartment             | Isolated compartment for EC resources    |
| VCN + subnets           | Public + private subnets with NAT        |
| Compute instance        | ARM Flex instance for mem0 + Qdrant      |
| Security lists          | SSH + mem0 (8080) + Qdrant (6333-6334)   |
| Internet + NAT gateways | Outbound connectivity for both subnets   |

## Customisation

See `variables.tf` for all configurable parameters. Key defaults:

- **Shape**: `VM.Standard.A1.Flex` (ARM, Always Free eligible)
- **OCPUs**: 2
- **Memory**: 12 GB
- **Region**: `ap-sydney-1`

## Security Notes

- SSH access is restricted to port 22 from all sources; narrow the CIDR in
  production via a custom security list rule.
- mem0 and Qdrant ports are restricted to VCN-internal traffic only.
- No secrets or OCIDs are committed to this repository.
