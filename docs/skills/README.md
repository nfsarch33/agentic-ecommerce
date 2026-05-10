# EC-Stack Skill Catalog

This catalog lists the agent skills relevant to working on the `agentic-ecommerce` repository. Skills are installed at `~/.cursor/skills/` -- this document serves as a routing guide for agents operating on this codebase.

## Quick Reference

| Task | Skill to Use |
|------|-------------|
| New Go package or service | `go-clean-architecture` |
| Performance-sensitive code | `go-performance-optimization` |
| Auth, crypto, webhooks | `go-security-review` |
| Temporal workflow or activity | `temporal-developer` |
| Task queue design, SDK patterns | `temporal-orchestration` |
| Dockerfile or docker-compose | `docker-ops` |
| Prometheus, Grafana, alerting | `monitoring-observability` |
| React/Next.js frontend | `react-best-practices` |
| Sprint planning, issues | `project-management` |
| E2E browser tests | `zbt-e2e-testing` |

## Detailed Skill Descriptions

### go-clean-architecture

**When to use:** Creating any new Go package in `internal/`, adding a new port/adapter, restructuring domain logic.

**EC examples from v4.x:**
- `internal/marketplace/` -- vendor port + commission adapter (v4.8.0)
- `internal/residency/` -- data residency policy engine (v4.9.0)
- `internal/worktree/` -- coordination lock domain (v4.15.0)

**Key patterns enforced:**
- Domain in `internal/<name>/` with port interfaces
- Adapters in `internal/adapter/<name>/`
- No framework leakage into domain

### go-performance-optimization

**When to use:** Writing hot-path code (event bus handlers, Temporal activities with high throughput, API middleware).

**EC examples from v4.x:**
- `internal/eventbus/` -- zero-alloc event dispatch (v4.10.0)
- `internal/workerpool/adaptive.go` -- RSS-aware worker scaling (v4.12.0)
- `internal/memwatch/` -- backpressure with minimal GC impact (v4.12.0)

**Key patterns enforced:**
- Allocation profiling before committing hot paths
- sync.Pool for frequently allocated objects
- Context propagation without unnecessary copies

### go-security-review

**When to use:** Any code handling authentication, cryptographic operations, webhook signature verification, or sensitive data.

**EC examples from v4.x:**
- `cmd/mc-api/security.go` -- CORS, CSP, rate limiting (v4.4.0)
- `internal/adapter/carrier/key_rotation.go` -- carrier API key rotation (v4.6.0)
- `cmd/mc-api/stripe_webhook_handlers.go` -- Stripe signature verification (v4.2.0)

**Key patterns enforced:**
- constant-time comparison for secrets
- No secrets in logs or error messages
- mTLS for internal service communication

### temporal-developer

**When to use:** Writing or modifying any Temporal workflow, activity, or worker configuration.

**EC examples from v4.x:**
- `internal/workflow/vendor_onboarding.go` -- saga-pattern onboarding (v4.8.0)
- `cmd/temporal-worker/` -- worker registration (all versions)
- Payment saga orchestration (v4.2.0)

**Key patterns enforced:**
- Deterministic workflow code (no I/O, no random, no time.Now)
- Activity retry policies explicit in registration
- Heartbeat for long-running activities

### temporal-orchestration

**When to use:** Designing task queues, worker topology, or extending the Temporal SDK with interceptors.

**EC examples from v4.x:**
- `internal/observability/temporal/interceptor.go` -- OTel interceptor (v4.5.0)
- Multi-queue design for content vs payment workflows

**Key patterns enforced:**
- One task queue per domain boundary
- SDK interceptors for cross-cutting concerns
- Continue-as-new for long-running workflows

### docker-ops

**When to use:** Modifying Dockerfiles, docker-compose.yml, or container health checks.

**EC examples from v4.x:**
- Multi-stage Dockerfile with distroless runtime (v4.4.0)
- `docker-compose.yml` with health probes for all services
- KEDA-compatible container scaling (v4.4.0)

**Key patterns enforced:**
- Multi-stage builds (build + runtime)
- Non-root runtime user
- Health check endpoints exposed

### monitoring-observability

**When to use:** Adding Prometheus metrics, Grafana dashboards, alerting rules, or structured logging.

**EC examples from v4.x:**
- `internal/observability/otel/provider.go` -- OpenTelemetry setup (v4.5.0)
- `internal/observability/tenant_metrics.go` -- per-tenant KPIs (v4.7.0)
- `monitoring/grafana/` -- dashboard JSON files

**Key patterns enforced:**
- RED method (Rate, Errors, Duration) for all services
- Tenant-scoped metric labels
- SLO-based alerting (not threshold-based)

### react-best-practices

**When to use:** Writing or reviewing any React/Next.js component in the frontend.

**EC examples from v4.x:**
- Dashboard frontend components (v4.3.0)
- Admin mobile API integration (v4.8.0)
- Next.js 16 upgrade patterns (v4.5.0)

**Key patterns enforced:**
- Server components by default, client only when needed
- Avoid waterfall data fetching
- Memoize expensive computations

### project-management

**When to use:** Planning sprints, creating GitHub issues, managing the backlog.

**Key patterns enforced:**
- Epic → Feature → Story → Task hierarchy
- Conventional commit messages
- SESSION.md checkpointing between context windows

### zbt-e2e-testing

**When to use:** Writing end-to-end browser tests for the platform UI.

**EC examples from v4.x:**
- Tenant onboarding E2E flow
- Marketplace submission E2E (v4.8.0)

**Key patterns enforced:**
- Page object pattern
- Deterministic test data (seeded)
- CI-compatible headless execution

## Validation

Run the skill quality gate to validate any skill file:

```bash
ec-cli skill quality-check ~/.cursor/skills/go-clean-architecture/SKILL.md
```

Generate a Codex-compatible variant:

```bash
ec-cli skill codex-gen ~/.cursor/skills/go-clean-architecture/SKILL.md ./codex-skills/
```
