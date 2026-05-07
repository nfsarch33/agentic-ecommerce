# Cloud Deployment Guide

v1.0.0 keeps a safe cloud deployment path for the Agentic Ecommerce stack. Docker Compose remains the local release-candidate contract, followed by Terraform dry-runs for AWS ECS Fargate and GCP Cloud Run. The Terraform examples intentionally use placeholders and secret references only; do not commit account IDs, live project IDs, private regions, real endpoints, or secret values.

## Deployment Shape

The production-like Compose stack remains the source of truth for service wiring:

- `mc-api` exposes `/healthz`, `/readyz`, and `/metrics`.
- `frontend` calls `mc-api` through `MC_API_BASE_URL`.
- `wc-sync`, `content-worker`, and `agent-worker` run as worker services or jobs.
- PostgreSQL with pgvector backs catalog, order, and media data.
- Redis backs cart/session storage and the sync event bus.
- Prometheus and Grafana provide local observability validation before cloud rollout.

The Terraform contracts under `deploy/terraform/` mirror that shape without provisioning live resources yet. They validate the interfaces for network, PostgreSQL, Redis, and services so provider resources can be added later in small, reviewed steps.

## Image Tags

Cloud deployments should use immutable SHA tags, not `latest`:

```bash
export IMAGE_TAG="$(git rev-parse --short=12 HEAD)"
make docker-build TAG="$IMAGE_TAG"
```

CI should push:

- `ghcr.io/nfsarch33/agentic-ecommerce:$IMAGE_TAG` for `mc-api`.
- `ghcr.io/nfsarch33/agentic-ecommerce:$IMAGE_TAG-wc-sync` for `wc-sync`.
- `ghcr.io/nfsarch33/agentic-ecommerce:$IMAGE_TAG-content-worker` for `content-worker`.
- `ghcr.io/nfsarch33/agentic-ecommerce:$IMAGE_TAG-agent-worker` for `agent-worker`.
- `ghcr.io/nfsarch33/agentic-ecommerce-web:$IMAGE_TAG` for the frontend once the frontend repo builds the matching SHA.

Use the backend SHA and frontend SHA from the v1.0.0 release note or PR description when promoting a stack. Do not promote a mutable `main` or `latest` tag.

## AWS ECS Fargate Path

Start with `deploy/terraform/aws-ecs`. The example maps the stack to these AWS services:

- Networking: VPC with public subnets for an ALB and private subnets for ECS tasks, RDS, and ElastiCache.
- Compute: ECS Fargate services for `mc-api`, `frontend`, and `agent-worker`; `wc-sync` can be a scheduled or one-off Fargate task.
- Database: RDS PostgreSQL 16 or Aurora PostgreSQL with pgvector support where available.
- Cache: ElastiCache Redis with TLS and auth token support.
- Media storage: S3 bucket placeholder from the shared `objectstore` module; add IAM, encryption, lifecycle, and CloudFront in the cloud-hardening slice.
- Logs and metrics: CloudWatch Logs for container logs, plus Prometheus/Grafana or managed Prometheus later.
- Secrets: AWS Secrets Manager injected into ECS task definitions by ARN or name.

Dry-run locally:

```bash
make tf-fmt-check
make tf-validate
terraform -chdir=deploy/terraform/aws-ecs plan -var "image_tag=$IMAGE_TAG"
```

Before converting placeholders into live resources, decide the Terraform state backend, AWS account, IAM roles, deployment region, TLS certificate ownership, DNS zone, and least-privilege policies. Keep databases and caches private; expose only ALB HTTPS ingress.

## GCP Cloud Run Path

Start with `deploy/terraform/gcp-cloudrun`. The example maps the stack to these GCP services:

- Networking: Serverless VPC Access connector for Cloud Run egress to private services.
- Compute: Cloud Run services for `mc-api`, `frontend`, and `agent-worker`; `wc-sync` can be a Cloud Run job.
- Database: Cloud SQL PostgreSQL 16 with private IP and pgvector support where available.
- Cache: Memorystore for Redis on the same private network.
- Media storage: GCS bucket placeholder from the shared `objectstore` module; add IAM, encryption, lifecycle, and Cloud CDN in the cloud-hardening slice.
- Logs and metrics: Cloud Logging and Cloud Monitoring, with Prometheus-compatible export added later if needed.
- Secrets: Secret Manager references mounted or injected into Cloud Run revisions.

Dry-run locally:

```bash
make tf-fmt-check
make tf-validate
terraform -chdir=deploy/terraform/gcp-cloudrun plan -var "image_tag=$IMAGE_TAG"
```

Before converting placeholders into live resources, decide the Terraform state bucket, GCP project, region, service accounts, IAM bindings, TLS and domain mapping, and ingress policy. Keep Cloud SQL and Memorystore private; expose only HTTPS Cloud Run services that are intended to be public.

## Secret Manager Mapping

Store complete connection strings or credentials in the cloud secret manager. Terraform may reference secret names, but secret values must be created out-of-band by an operator or CI job with approved access.

Backend secrets:

- `ECOMMERCE_DB_URL`: full PostgreSQL DSN for `mc-api`, `wc-sync`, and `agent-worker`.
- `ECOMMERCE_REDIS_ADDR`: Redis host:port, or a runtime-composed value if the provider resource exposes it directly.
- `ECOMMERCE_API_TOKEN`: optional legacy backend bearer token for migration windows only.
- `ECOMMERCE_WC_CONSUMER_KEY`: WooCommerce REST consumer key.
- `ECOMMERCE_WC_CONSUMER_SECRET`: WooCommerce REST consumer secret.
- `ECOMMERCE_WC_WEBHOOK_SECRET`: WooCommerce webhook HMAC secret when webhook ingestion is enabled.
- `ECOMMERCE_AI_BRIDGE_URL`: fleet-hosted MiniMax bridge URL; do not point app services directly at MiniMax.
- `ECOMMERCE_EMBEDDING_BRIDGE_URL`: fleet-hosted embedding bridge URL for RAG; this may be the same approved bridge as chat completions when it exposes embeddings.

Frontend secrets:

- `FLEET_AI_BRIDGE_URL`: only if the frontend BFF route needs to call the approved fleet bridge.

Non-secret environment variables can stay in Terraform state, including `ECOMMERCE_ALLOWED_ORIGIN`, `ECOMMERCE_JWT_ISSUER`, `ECOMMERCE_JWT_AUDIENCE`, `ECOMMERCE_JWT_ACCESS_TTL`, `ECOMMERCE_REFRESH_TTL`, `ECOMMERCE_RATE_LIMIT_CAPACITY`, `ECOMMERCE_RATE_LIMIT_REFILL`, `ECOMMERCE_CSP_CONNECT_SRC`, `ECOMMERCE_CSP_REPORT_URI`, `ECOMMERCE_EVENTBUS_DRIVER`, `ECOMMERCE_EVENTBUS_CHANNEL_SYNC`, `ECOMMERCE_EVENTBUS_CHANNEL_DLQ`, `ECOMMERCE_EMBEDDING_MODEL`, `ECOMMERCE_EMBEDDING_DIMENSIONS`, `ECOMMERCE_RAG_CHUNK_SIZE`, `ECOMMERCE_MEDIA_STORAGE_DRIVER`, `ECOMMERCE_MEDIA_BUCKET`, `ECOMMERCE_MEDIA_BASE_PATH`, `ECOMMERCE_MEDIA_PUBLIC_BASE_URL`, `ECOMMERCE_MEDIA_REGION`, `ECOMMERCE_MEDIA_MAX_SIZE_BYTES`, `ECOMMERCE_MEDIA_ALLOWED_MIME_TYPES`, and worker scheduling flags.

For cloud media storage, prefer IAM over static credentials:

- AWS ECS: set `ECOMMERCE_MEDIA_STORAGE_DRIVER=s3` and grant the task role least-privilege S3 access to the media bucket.
- GCP Cloud Run: set `ECOMMERCE_MEDIA_STORAGE_DRIVER=gcs` and grant the runtime service account least-privilege GCS access to the media bucket.
- CDN hosts should become `ECOMMERCE_MEDIA_PUBLIC_BASE_URL` only after cache, CORS, and access-control decisions are reviewed.

## Browser Boundary Headers

Set `ECOMMERCE_ALLOWED_ORIGIN` to the exact HTTPS storefront origin before enabling authenticated browser traffic. Do not use wildcard CORS with JWT, refresh-token, or API-key authenticated routes.

Set Content Security Policy headers at the CDN, load balancer, reverse proxy, or frontend platform. The baseline should be deny-by-default and allow only the deployed storefront, backend API, approved image/media hosts, and fleet bridge endpoints required by BFF routes. Keep report-only mode separate from enforcement by sending CSP reports to `ECOMMERCE_CSP_REPORT_URI` first, then promote the policy after violations are reviewed.

## Database Migrations

The repo currently uses ordered SQL files under `migrations/` and the `make migrate-up` target. For cloud deployment:

1. Provision PostgreSQL privately and enable required extensions such as pgvector. The local compose contract uses `pgvector/pgvector:pg16`; cloud PostgreSQL must provide the same `vector` extension before `migrations/0005_enable_pgvector_rag.up.sql` runs.
2. Resolve `ECOMMERCE_DB_URL` from the cloud secret manager in a trusted runner.
3. Run migrations from a one-off ECS task, Cloud Run job, or CI release job with network access to the database.
4. Run migrations before shifting traffic to the new service revision.
5. Keep rollback explicit by pairing the deployed image SHA with the migration files used for that release.

Example runner command:

```bash
make migrate-up DB_URL="$ECOMMERCE_DB_URL"
```

Do not run migrations from developer laptops against production databases.

## Observability Readiness

Before cloud rollout, validate the local stack:

```bash
docker compose --env-file .env.compose -f docker-compose.yml config
docker compose --env-file .env.compose -f docker-compose.yml up -d --build
curl http://127.0.0.1:8080/readyz
curl http://127.0.0.1:8080/metrics
```

Carry these alert thresholds into cloud monitoring:

- API p95 latency above 500 ms for 5 minutes.
- API 5xx error rate above 1% for 5 minutes.
- WooCommerce sync lag above 5 minutes.
- `mc-api` scrape or health failure for 1 minute.
- Media validation failure spikes above 5 failures in 15 minutes.

CloudWatch or Cloud Logging should receive structured JSON logs. Use request IDs and service labels so API, worker, sync, and frontend events can be correlated.

## Safety Rules

- Never commit `.tfvars` files with real account IDs, project IDs, VPC IDs, database URLs, or secret values.
- Run `terraform plan` before every apply and keep plans tied to immutable image SHAs.
- Keep `wc-sync` in dry-run mode until WooCommerce ownership, webhook secrets, and rollback steps are reviewed.
- Keep data stores private and expose only managed HTTPS ingress.
- Treat the current Terraform as deploy-contract scaffolding until provider resources and state backends are approved.
