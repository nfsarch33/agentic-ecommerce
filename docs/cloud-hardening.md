# v1.7.0 Cloud Hardening

This slice extends the credential-free Terraform deployment contracts for AWS ECS
Fargate and GCP Cloud Run. The examples still do not create live cloud resources:
they emit reviewed deployment contracts for Temporal, media storage, secret
references, CDN placeholders, and autoscaling policy intent.

## Terraform Scope

- `deploy/terraform/aws-ecs` models the stack on ECS Fargate, RDS/ElastiCache
  placeholders, S3 media storage, CloudFront media CDN stubs, and ECS service
  autoscaling policy stubs.
- `deploy/terraform/gcp-cloudrun` models the stack on Cloud Run, Cloud SQL and
  Memorystore placeholders, GCS media storage, Cloud CDN media stubs, and Cloud
  Run autoscaling policy stubs.
- `deploy/terraform/modules/service` now records protocol, command, endpoint,
  and autoscaling contracts for HTTP, gRPC, and worker containers.
- `deploy/terraform/modules/objectstore` now records provider-specific CDN
  contracts with HTTPS-only edge behavior, read-only methods, origin access
  required, and public bucket access blocked by policy.

## Temporal Contract

Both cloud examples include:

- `temporal-server`: private gRPC service placeholder on port `7233`, pinned by
  `temporal_image` and `temporal_image_tag`.
- `temporal-worker`: private worker service using the backend
  `${image_tag}-temporal-worker` image.
- `ECOMMERCE_TEMPORAL_ADDR`, `ECOMMERCE_TEMPORAL_NAMESPACE`, and
  `ECOMMERCE_TEMPORAL_TASK_QUEUE` as non-secret runtime config.
- `TEMPORAL_DB_URL` as a secret-manager placeholder for the Temporal server
  persistence connection.

Do not expose Temporal gRPC or UI directly to the public internet. Real
deployments should choose a production Temporal topology, persistence schema,
TLS/mTLS boundary, and operator access path before replacing these placeholders.

## Secret Mapping

Terraform may hold secret names, not secret values. The cloud examples map these
runtime variables to AWS Secrets Manager or GCP Secret Manager references:

- `ECOMMERCE_DB_URL`
- `ECOMMERCE_REDIS_ADDR`
- `ECOMMERCE_JWT_SECRET`
- `ECOMMERCE_ADMIN_USERNAME`
- `ECOMMERCE_ADMIN_PASSWORD`
- `ECOMMERCE_API_TOKEN`
- `ECOMMERCE_AI_BRIDGE_URL`
- `ECOMMERCE_EMBEDDING_BRIDGE_URL`
- `ECOMMERCE_WC_STORE_URL`
- `ECOMMERCE_WC_CONSUMER_KEY`
- `ECOMMERCE_WC_CONSUMER_SECRET`
- `ECOMMERCE_WC_WEBHOOK_SECRET`
- `TEMPORAL_DB_URL`

Media storage should use task-role or service-account IAM rather than static
object-store credentials.

## CDN Defaults

The media CDN contract is a stub only. Safe defaults are:

- bucket public access remains blocked;
- CDN origin access is required;
- only `GET`, `HEAD`, and `OPTIONS` are allowed;
- viewers are redirected to HTTPS;
- default cache TTL is one hour, maximum TTL is one day.

Promote the CDN hostname into `ECOMMERCE_MEDIA_PUBLIC_BASE_URL` only after
CORS, cache invalidation, signed URL requirements, image privacy rules, and DNS
ownership are reviewed.

## Autoscaling

The examples record autoscaling intent without creating provider policies:

- `mc-api`: scale from the configured min/max count on request concurrency or
  average CPU.
- `temporal-worker`: scale from the configured min/max count on CPU until queue
  backlog metrics are available in cloud monitoring.
- `temporal-server`: fixed at one instance in the placeholder contract.

Before applying real scaling policies, add queue-depth or workflow-latency
signals for Temporal workers and pair scale-in cooldowns with graceful shutdown
timeouts.

## Validation

Run dry-run validation only:

```bash
make tf-fmt-check
make tf-validate
terraform -chdir=deploy/terraform/aws-ecs plan -var-file=terraform.tfvars.example
terraform -chdir=deploy/terraform/gcp-cloudrun plan -var-file=terraform.tfvars.example
runx shell-leak-scan --repo ecommerce
sentrux scan .
```

Do not run `terraform apply` from this repo until state backends, cloud accounts,
IAM owners, TLS/DNS, secret ownership, and production Temporal topology are
approved.
