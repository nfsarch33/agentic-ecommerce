# v9.0.0 Release Checklist

Use this checklist before tagging `agentic-ecommerce` v9.0.0. The semver tag
remains uncut until the full mirrored self-hosted regression is green on
`primary-testing and secondary-testing`.

## Version and Docs

- `VERSION` contains `9.0.0`.
- `api/openapi.yaml` has `info.version: 9.0.0`.
- `CHANGELOG.md` includes the v9.0.0 platform-baseline entry summarising the backend release path, mirrored self-hosted regression, and current secondary-pool blocker state.
- `README.md` links quickstart, architecture, API docs, Temporal, n8n, media storage, cloud deployment, and security boundaries.
- `README.md` also records that `v9.0.0` is still RC-only until both pools pass the mirrored release gate.
- `docs/api-reference.md`, `docs/temporal-workflow-specs.md`, and `docs/webhook-contracts.md` reflect the v9.0.0 API, workflow, automation surfaces, and mirrored self-hosted release contract.
- `docs/adr/adr-036-v9-release-decisions.md` is accepted and linked from `docs/adr/README.md`.
- `docs/operations/v9-release-final.md` records final release evidence, current pool status, skipped gates, and carry-forwards.

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

## Self-Hosted Stack Gates

```bash
docker compose --env-file .env.compose -f docker-compose.yml config --quiet
make compose-config
make compose-config-prod
make compose-workers-config
```

Expected result: the self-hosted Docker and worker configuration stays
deterministic, credential-free, and reproducible on both local and remote test
hosts. Cloud deployment assets remain maintained as reference-only material and
do not block the `v9.0.0` tag.

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

## Mirrored Pool Gates

```bash
runx ssh exec --target node-a-travel --cmd ssh-canary-wsl
runx ssh exec --target host-a-travel --cmd ssh-canary-win
runx ssh exec --target node-b-travel --cmd ssh-canary-wsl
runx ssh exec --target node-b --cmd ssh-canary-wsl
runx ssh exec --target host-b --cmd ssh-canary-win
runx ssh exec --target host-b-travel --cmd ssh-canary-win
runx test-lane run --lane backend-integration --pool primary-testing
runx test-lane run --lane backend-integration --pool secondary-testing
runx test-lane run --lane full-stack-e2e --pool primary-testing
runx test-lane run --lane full-stack-e2e --pool secondary-testing
runx test-lane run --lane cleanup-testing --pool primary-testing
runx test-lane run --lane cleanup-testing --pool secondary-testing
```

Expected result: host canaries are green on both pools, backend integration and
full-stack E2E are reproducible on both pools, cleanup evidence is durable on
both pools, and the frontend checklist carries the mirrored
`frontend-playwright-stable` plus `frontend-uiauto-compare` proof required for
the stack tag.

## Security and Public Boundary Gates

```bash
runx shell-leak-scan --repo ecommerce
sentrux gate .
```

Review docs-inclusive output before merge. Public docs must not contain live credentials, private fleet hostnames, internal IPs, personal filesystem paths, account IDs, project IDs, `.tfvars`, browser profiles, or direct MiniMax app-service calls.

## Release Notes

The GitHub release notes should include:

- Backend and frontend commit SHAs used for promotion.
- Primary and secondary pool canary plus lane results.
- Docker image tags and whether they are SHA-based or release tags.
- OpenAPI contract path: `api/openapi.yaml`.
- Support-tool provenance for the controller and both remote pools.
- Any skipped gates with operator-approved rationale.
