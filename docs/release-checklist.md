# v1.0.0 Release Checklist

Use this checklist before tagging `agentic-ecommerce` v1.0.0.

## Version and Docs

- `VERSION` contains `1.0.0`.
- `api/openapi.yaml` has `info.version: 1.0.0`.
- `CHANGELOG.md` includes the v1.0.0 release entry.
- `README.md` links quickstart, architecture, API docs, Docker Compose, cloud deployment, and security boundaries.
- `docs/adr/adr-024-v1-release-decisions.md` is accepted and linked from `docs/adr/README.md`.

## Backend Quality Gates

```bash
go test -race ./...
go vet ./...
make build
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
make monitoring-validate
```

Target release threshold: at least 80% backend coverage, no `go vet` findings, and no monitoring config regressions.

## Compose and Deployment Gates

```bash
docker compose --env-file .env.compose -f docker-compose.yml config --quiet
make compose-workers-config
make tf-fmt-check
make tf-validate
terraform -chdir=deploy/terraform/aws-ecs plan -var "image_tag=$IMAGE_TAG"
terraform -chdir=deploy/terraform/gcp-cloudrun plan -var "image_tag=$IMAGE_TAG"
```

The Terraform commands are dry-run only for v1.0.0. Do not apply cloud resources until account, project, state backend, IAM, TLS, DNS, and secret-manager ownership are approved.

## Runtime Smoke Gates

```bash
cp .env.compose.example .env.compose
docker compose --env-file .env.compose -f docker-compose.yml up -d --build
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
curl http://127.0.0.1:8080/metrics
docker compose --env-file .env.compose -f docker-compose.yml down
```

Expected result: the stack boots in under 60 seconds on a healthy local Docker runtime, `mc-api` is live, configured dependencies are ready, and metrics expose build information plus HTTP RED counters.

## Security and Public Boundary Gates

```bash
runx shell-leak-scan --repo ecommerce
sentrux scan .
```

Review docs-inclusive output before merge. Public docs must not contain live credentials, private fleet hostnames, internal IPs, personal filesystem paths, account IDs, project IDs, `.tfvars`, browser profiles, or direct MiniMax app-service calls.

## Release Notes

The GitHub release notes should include:

- Backend and frontend commit SHAs used for promotion.
- Docker image tags and whether they are SHA-based or release tags.
- OpenAPI contract path: `api/openapi.yaml`.
- Required environment variables and secret-manager ownership.
- Any skipped gates with operator-approved rationale.
