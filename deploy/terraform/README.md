# Terraform Deployment Scaffolding

This directory contains v1.0.0 cloud deployment scaffolding for `agentic-ecommerce`.

The modules are intentionally provider-neutral contracts. They do not create live AWS or GCP resources yet, so `terraform validate` can run without cloud credentials, account IDs, private regions, or committed secrets. The provider-specific example roots show how the backend stack maps to AWS ECS Fargate and GCP Cloud Run before real provider resources are added.

## Layout

- `modules/network`: VPC or Serverless VPC Access connector contract.
- `modules/objectstore`: S3 or GCS media bucket contract with CDN, public URL, and lifecycle placeholders.
- `modules/postgres`: RDS PostgreSQL or Cloud SQL PostgreSQL contract and migration output.
- `modules/redis`: ElastiCache Redis or Memorystore Redis contract.
- `modules/service`: ECS Fargate or Cloud Run service contract for `mc-api`, Temporal, workers, and frontend.
- `aws-ecs`: Credential-free AWS ECS Fargate example root.
- `gcp-cloudrun`: Credential-free GCP Cloud Run example root.

The v1.7.0 cloud-hardening pass adds Temporal server/worker placeholders,
S3/GCS media bucket contracts, CloudFront/Cloud CDN stubs, expanded
Secrets Manager mappings, and autoscaling policy intent while preserving the
no-credentials/no-apply boundary. See `../../docs/cloud-hardening.md`.

## Provider Lock Policy

Terraform roots are split into credential-free contract roots and live-provider
roots:

- `aws-ecs` and `gcp-cloudrun` are credential-free contract roots. They use
  local modules only today, intentionally have no `provider` blocks, and are
  covered by `make tf-validate` without cloud credentials.
- `gke`, `eks`, `oci`, and `dr` are live-provider roots. Keep provider lock
  files with those roots before operator-driven plan/apply work. `gke` already
  carries `.terraform.lock.hcl`; the remaining live-provider roots should add
  lock files in the same PR that refreshes their provider initialization.

Do not introduce a live provider block into a credential-free root without also
adding the matching `.terraform.lock.hcl`, state backend decision, IAM owner,
and rollback note.

## Validation

```bash
make tf-fmt
make tf-validate
```

For direct Terraform use:

```bash
terraform fmt -recursive deploy/terraform
terraform -chdir=deploy/terraform/aws-ecs init -backend=false -input=false
terraform -chdir=deploy/terraform/aws-ecs validate
terraform -chdir=deploy/terraform/gcp-cloudrun init -backend=false -input=false
terraform -chdir=deploy/terraform/gcp-cloudrun validate
```

Real deployment work should replace the placeholder contracts with provider resources in small PRs after state backend, IAM, secret ownership, and target accounts are approved.
