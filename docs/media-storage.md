# Media Storage Architecture

v1.4.0 introduces the storage infrastructure contract for Media Intelligence
without implementing the core sourcing, processing, or QA pipeline. The backend
already owns the media validation package and a `port.MediaStore` abstraction;
this slice wires local and cloud deployment surfaces around that boundary.

## Runtime Contract

Backend services that may source, validate, or process media receive the same
non-secret storage configuration:

```bash
ECOMMERCE_MEDIA_STORAGE_DRIVER=filesystem
ECOMMERCE_MEDIA_STORE=filesystem
ECOMMERCE_MEDIA_BASE_PATH=/var/lib/agentic-ecommerce/media
ECOMMERCE_MEDIA_ROOT=/var/lib/agentic-ecommerce/media
ECOMMERCE_MEDIA_PUBLIC_BASE_URL=/media
ECOMMERCE_MEDIA_MAX_SIZE_BYTES=5242880
ECOMMERCE_MEDIA_ALLOWED_MIME_TYPES=image/jpeg,image/png,image/webp
```

`ECOMMERCE_MEDIA_STORE` and `ECOMMERCE_MEDIA_ROOT` remain as compatibility
aliases for the existing filesystem adapter. New v1.4.0 integrations should
prefer `ECOMMERCE_MEDIA_STORAGE_DRIVER` and `ECOMMERCE_MEDIA_BASE_PATH`.

Cloud or MinIO-style object stores add these placeholders:

```bash
ECOMMERCE_MEDIA_BUCKET=
ECOMMERCE_MEDIA_REGION=
ECOMMERCE_MEDIA_ENDPOINT=
ECOMMERCE_MEDIA_FORCE_PATH_STYLE=false
ECOMMERCE_MEDIA_ACCESS_KEY_ID=
ECOMMERCE_MEDIA_SECRET_ACCESS_KEY=
```

Do not commit real access keys. AWS ECS should prefer task-role access to S3,
and GCP Cloud Run should prefer service-account IAM to GCS.

## Local Filesystem Fallback

The default compose path stores media on a local filesystem volume:

- `docker-compose.dev.yml` bind-mounts `${ECOMMERCE_MEDIA_HOST_DIR:-./.local/media-uploads}`.
- `docker-compose.yml` mounts the named `media-assets` volume into backend services.
- `.local/` is ignored by git, so supplier, customer, and generated media stay out of version control.

Seed or clear local media state with:

```bash
make media-store-seed
make media-store-clean
```

The old `make media-seed` and `make media-clean` targets remain as aliases.

## Optional MinIO Profile

MinIO is opt-in for local adapter work:

```bash
MINIO_ROOT_USER=local-only-user \
MINIO_ROOT_PASSWORD=local-only-password \
ECOMMERCE_MEDIA_STORAGE_DRIVER=s3 \
ECOMMERCE_MEDIA_STORE=s3 \
ECOMMERCE_MEDIA_BUCKET=agentic-ecommerce-local-media \
ECOMMERCE_MEDIA_ENDPOINT=http://minio:9000 \
ECOMMERCE_MEDIA_FORCE_PATH_STYLE=true \
make compose-media-config
```

Start the profile only when testing an S3-compatible adapter:

```bash
docker compose -f docker-compose.dev.yml --profile media-objectstore up -d minio
```

Create buckets manually with a local tool or one-off script after MinIO starts;
compose intentionally does not include committed root credentials or bucket
bootstrap secrets.

## Cloud Mapping

Terraform keeps object storage as provider-neutral placeholders until the cloud
hardening slice adds real resources:

- AWS: `deploy/terraform/aws-ecs` maps media to the `objectstore` module with
  `storage_driver=s3`, S3 bucket naming, and a public URL placeholder that can
  later become CloudFront.
- GCP: `deploy/terraform/gcp-cloudrun` maps media to the same module with
  `storage_driver=gcs`, GCS bucket naming, and a public URL placeholder that can
  later become Cloud CDN.

Before turning placeholders into live resources, add least-privilege IAM for
the runtime service account, encryption, versioning, lifecycle rules, CDN/cache
policy, bucket CORS, and private/public access decisions in a reviewed PR.

## Observability

Prometheus scrapes `mc-api` and `agent-worker` with `media_pipeline=v1.4.0`
labels. Existing media metrics are reserved for the Media Intelligence pipeline:

```promql
agentic_ecommerce_media_validation_failures_total
agentic_ecommerce_agent_worker_media_validation_failures_total
```

`monitoring/alerts.yml` includes a warning alert when backend and worker media
validation failures spike above the v1.4.0 threshold.
