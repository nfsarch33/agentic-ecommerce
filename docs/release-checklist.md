# v9.0.0 Release Checklist

Use this checklist before tagging `agentic-ecommerce` v9.0.0.

## Version and Docs

- `VERSION` contains `9.0.0`.
- `api/openapi.yaml` has `info.version: 9.0.0`.
- `CHANGELOG.md` includes the v9.0.0 platform-baseline entry summarising the backend release path, testing-pool contract, and staging baseline.
- `README.md` links quickstart, architecture, API docs, Temporal, n8n, media storage, cloud deployment, and security boundaries.
- `docs/api-reference.md`, `docs/temporal-workflow-specs.md`, and `docs/webhook-contracts.md` reflect the v9.0.0 API, workflow, and automation surfaces.
- `docs/adr/adr-036-v9-release-decisions.md` is accepted and linked from `docs/adr/README.md`.
- `docs/operations/v9-release-final.md` records final release evidence, skipped gates, and carry-forwards.

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

Target release threshold: no race-test regression, no `go vet` findings, no monitoring config regressions, and backend coverage at or above the sustained 85% gate.

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
helm lint deploy/helm/agentic-ecommerce
make compose-workers-config
make tf-fmt-check
make tf-validate
make tf-plan-contract
```

GKE/GCP is the authoritative staging target for v9.0.0, but live provider
roots remain operator-gated until credentials, state backend ownership, IAM,
TLS, DNS, secret-manager mappings, Temporal persistence topology, and rollback
docs are approved. `make tf-plan-contract` remains the credential-free parity
proof for dry-run roots while Helm lint keeps the checked-in chart aligned.

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

Remote staging validation also requires:

```bash
runx test-lane run --lane staging-smoke --pool primary-testing
```

Expected result: once `EC_STAGING_BASE_URL` is provisioned, the primary-testing
lane reaches the staging ingress instead of failing on path drift or timeout
drift.

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
