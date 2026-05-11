# v6.6.0 Release Checklist

Use this checklist before tagging `agentic-ecommerce` v6.6.0.

## Version and Docs

- `VERSION` contains `6.6.0`.
- `api/openapi.yaml` has `info.version: 6.6.0`.
- `CHANGELOG.md` includes the v6.6.0 release entry summarising v6.1.x-v6.5.x cleanup work.
- `README.md` links quickstart, architecture, API docs, Temporal, n8n, media storage, cloud deployment, and security boundaries.
- `docs/api-reference.md`, `docs/temporal-workflow-specs.md`, and `docs/webhook-contracts.md` reflect the v6.6.0 API, workflow, and automation surfaces.
- `docs/adr/adr-033-v660-release-decisions.md` is accepted and linked from `docs/adr/README.md`.

## Backend Quality Gates

```bash
go test -race ./...
go vet ./...
make build
make coverage-check
make contract-test
make release-perf-smoke
make monitoring-validate
```

Target release threshold: no race-test regression, no `go vet` findings, and no monitoring config regressions. The 85% coverage target remains an explicit v7 carry-forward if the durable release measurement stays at 84.8%.

## Workflow and Automation Gates

```bash
make compose-temporal-config
go test ./internal/workflow/...
make compose-agent-schedules-config
make n8n-config
make n8n-workflows-validate
```

Expected result: Temporal workflow specs remain deterministic, the `ec-workflows` task queue is documented, n8n templates stay inactive and credential-free, and outbound webhook contracts match `api/openapi.yaml`.

## Compose and Deployment Gates

```bash
docker compose --env-file .env.compose -f docker-compose.yml config --quiet
make compose-config
make compose-config-prod
make compose-workers-config
make tf-fmt-check
make tf-validate
terraform -chdir=deploy/terraform/aws-ecs plan -var "image_tag=$IMAGE_TAG"
terraform -chdir=deploy/terraform/gcp-cloudrun plan -var "image_tag=$IMAGE_TAG"
```

The Terraform commands are dry-run only for v6.6.0. Do not apply cloud resources until account, project, state backend, IAM, TLS, DNS, secret-manager ownership, Temporal persistence topology, and n8n exposure boundaries are approved.

## Runtime Smoke Gates

```bash
cp .env.compose.example .env.compose
docker compose --env-file .env.compose -f docker-compose.yml up -d --build
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
curl http://127.0.0.1:8080/metrics
docker compose --env-file .env.compose -f docker-compose.yml down
```

Expected result: the stack boots in under 90 seconds on a healthy local Docker runtime, `mc-api` is live, configured dependencies are ready, and metrics expose build information plus HTTP RED counters.

## Security and Public Boundary Gates

```bash
runx shell-leak-scan --repo ecommerce
sentrux gate .
```

Review docs-inclusive output before merge. Public docs must not contain live credentials, private fleet hostnames, internal IPs, personal filesystem paths, account IDs, project IDs, `.tfvars`, browser profiles, or direct MiniMax app-service calls.

## Release Notes

The GitHub release notes should include:

- Backend and frontend commit SHAs used for promotion.
- Docker image tags and whether they are SHA-based or release tags.
- OpenAPI contract path: `api/openapi.yaml`.
- Required environment variables and secret-manager ownership.
- Any skipped gates with operator-approved rationale.
