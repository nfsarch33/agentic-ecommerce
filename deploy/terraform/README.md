# Terraform Deployment Scaffolding

This directory contains v1.0.0 cloud deployment scaffolding for `agentic-ecommerce`.

The modules are intentionally provider-neutral contracts. They do not create live AWS or GCP resources yet, so `terraform validate` can run without cloud credentials, account IDs, private regions, or committed secrets. The provider-specific example roots show how the backend stack maps to AWS ECS Fargate and GCP Cloud Run before real provider resources are added.

## Layout

- `modules/network`: VPC or Serverless VPC Access connector contract.
- `modules/postgres`: RDS PostgreSQL or Cloud SQL PostgreSQL contract and migration output.
- `modules/redis`: ElastiCache Redis or Memorystore Redis contract.
- `modules/service`: ECS Fargate or Cloud Run service contract for `mc-api`, workers, and frontend.
- `aws-ecs`: Credential-free AWS ECS Fargate example root.
- `gcp-cloudrun`: Credential-free GCP Cloud Run example root.

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
