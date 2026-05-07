# v1.8.0 Backend QA + Performance

This document ties the v1.8.0 backend QA artifacts to repeatable commands.

## Contract Tests

`cmd/mc-api/comprehensive_contract_test.go` verifies that every OpenAPI operation has an explicit contract coverage entry. Representative success responses are normalized and compared against JSON golden files in `cmd/mc-api/testdata/contracts/`.

Run:

```bash
make contract-test
```

## Load Tests

`tests/load/k6/backend-comprehensive.js` defines tagged k6 scenarios and thresholds:

- Product catalog: `p95 < 100ms`.
- Order creation: `p95 < 200ms`.
- AI generation: `p95 < 2s`, intended for a mocked AI bridge or the in-process Go smoke gate.
- Temporal workflow start: `p95 < 500ms`, intended for a local Temporal dev server or the in-process Go smoke gate.
- Media validation: `p95 < 500ms`, intended for the local MIS fixture path.
- Webhook delivery: `p95 < 500ms`, intended for a loopback/mock n8n receiver.

Run k6 against a configured local API:

```bash
BASE_URL=http://127.0.0.1:8080 \
BEARER_TOKEN=<operator-or-admin-token> \
PRODUCT_ID=<seed-product-uuid> \
MEDIA_SOURCE_URL=http://127.0.0.1:18081/fixtures/resistance-band.png \
WEBHOOK_URL=http://127.0.0.1:18081/n8n/order-placed \
make load-test
```

For deterministic CI/local evidence without external services, run:

```bash
make release-perf-smoke
```

The Go smoke test uses an in-process HTTP server, mocked AI generation, a fake Temporal client, local MIS image sourcing, and a loopback webhook receiver while enforcing the same endpoint budgets.

## Database Audit

Run:

```bash
make db-perf-audit
```

See `docs/backend-performance-audit.md` for query coverage and expected `EXPLAIN ANALYZE` interpretation.

## Docker Image Size

Run:

```bash
make docker-image-size
```

The target builds the `mc-api` image and reports `docker image ls` plus `docker history` so image size regressions are visible in PR evidence.

## Security Refresh

Run:

```bash
make security-refresh
```

The target runs available local scanners (`govulncheck`, `gitleaks`, `trivy`) and prints install guidance for missing tools instead of hiding skipped checks.
