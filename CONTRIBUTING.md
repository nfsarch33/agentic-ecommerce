# Contributing to Agentic Ecommerce (Backend)

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.26.3+ | [go.dev/dl](https://go.dev/dl/) |
| Docker | 24.0+ | [docker.com](https://docs.docker.com/get-docker/) |
| Helm | 3.14+ | `brew install helm` or [helm.sh](https://helm.sh/docs/intro/install/) |
| PostgreSQL | 16+ | Via Docker Compose (included) |
| Redis | 7+ | Via Docker Compose (included) |

## Development Setup

```bash
git clone git@github.com:nfsarch33/agentic-ecommerce.git
cd agentic-ecommerce

# Build all binaries
make build

# Run tests
make test

# Start local stack (Postgres, Redis, Temporal)
docker compose up -d

# Run migrations
./ec-cli migrate up

# Start the API server
./mc-api
```

## Branch Naming

| Prefix | Use |
|--------|-----|
| `feat/` | New features |
| `fix/` | Bug fixes |
| `refactor/` | Code restructuring (no behaviour change) |
| `perf/` | Performance improvements |
| `test/` | Test additions or fixes |
| `docs/` | Documentation only |
| `release/` | Release preparation |
| `qa/` | QA and validation work |

## Commit Convention

```
type(scope): message
```

Examples:
- `feat(payments): add Alipay sandbox adapter`
- `fix(webhook): correct HMAC signature on TikTok events`
- `refactor(httpclient): extract shared base from adapters`
- `perf(db): add covering index for GMV rollup query`
- `test(workflow): add table-driven tests for payment saga`
- `docs(operations): update deployment runbook for 36 migrations`

## Pull Request Process

1. Branch from `main` using the naming convention above
2. Write tests first (TDD) -- RED tests before GREEN implementation
3. Run quality gates before pushing:

```bash
# Full test suite with race detection
go test -race -p 4 ./...

# Sentrux quality gate
sentrux gate .

# Check coverage (target: >=83%)
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1

# Shell leak scan
shell-leak-scan
```

4. Open a PR against `main`
5. CI must pass (lint, test, sentrux, govulncheck)
6. Merge on green CI

## Quality Gates

| Gate | Threshold | Command |
|------|-----------|---------|
| Tests pass | 100% | `go test -race ./...` |
| Coverage | >= 83% | `go tool cover -func=coverage.out` |
| Sentrux quality | > 7000 | `sentrux gate .` |
| Complex functions | <= 5 | `sentrux gate .` (cyclomatic complexity) |
| Vulnerability scan | 0 findings | `govulncheck ./...` |
| Shell leak scan | 0 leaks | `shell-leak-scan` |
| Build | Clean | `go build ./...` |

## Architecture

The codebase follows Clean Architecture with ports and adapters:

```
internal/
├── adapter/       # External service adapters (social, payment, llm, etc.)
├── agent/         # Agent domain logic (pricing, sourcing, enrichment, etc.)
├── api/           # HTTP handlers and routing
├── domain/        # Core domain types
├── eventbus/      # Event bus and payload types
├── metrics/       # Prometheus metric registry
├── resilience/    # Circuit breaker, rate limiter
├── workflow/      # Temporal workflow definitions
└── ...
```

## Code Style

- Functions < 75 LOC, cyclomatic complexity < 10
- Table-driven tests preferred over repeated test functions
- Tenant-aware: all entities carry `tenant_id`
- Structured logging via `slog`
- No infrastructure hostnames in source code (use env vars)
