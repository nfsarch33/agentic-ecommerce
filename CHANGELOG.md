# Changelog

All notable changes to the Agentic Ecommerce backend are documented here.

## v1.0.0 - 2026-05-07

### Release Summary

v1.0.0 graduates the Go backend from scaffold to release-ready Mission Control spine for the Agentic Ecommerce stack. The release consolidates the v0.1.0-v0.9.0 work into a public, documented backend with product and order APIs, WooCommerce sync contracts, AI-assisted content generation, compliance and SEO checks, worker scaffolding, Docker Compose operations, cloud deployment dry-runs, observability, JWT/RBAC security, and release gates.

### Capabilities Included

- Product catalog API with PostgreSQL-backed repository contracts, CRUD endpoints, media placeholders, OpenAPI coverage, and generated frontend client compatibility.
- Order, cart, checkout, and order status contracts for the storefront purchase flow.
- WooCommerce sync worker path with dry-run defaults, webhook contract coverage, Redis event-bus channel names, and conflict-resolution API surface.
- AI content agent endpoints routed through the approved fleet bridge boundary, with content suggestions, generated descriptions, and quality scoring hooks.
- Agent orchestration contracts for sourcing, content, pricing, and compliance agents, including scheduler and worker runtime placeholders.
- Content compliance, SEO, and media-processing contracts for publish-readiness checks.
- Production-like Docker Compose stack with `mc-api`, frontend image wiring, PostgreSQL + pgvector, Redis, Prometheus, Grafana, and opt-in worker profiles.
- Credential-free AWS ECS and GCP Cloud Run Terraform dry-run scaffolding under `deploy/terraform/`.
- Observability baseline with `/healthz`, `/readyz`, `/metrics`, structured request logs, request IDs, alert rules, and Grafana dashboard provisioning.
- Security baseline with JWT login, RBAC roles, rate limiting, audit logging for mutations, public safety guidance, and explicit secret boundaries.

### Release Gates

- Backend quality gates: `go test -race ./...`, `go vet ./...`, `make build`, `make monitoring-validate`, and coverage review against the v1.0.0 threshold.
- Deployment gates: Docker Compose config validation, full-stack smoke before promotion, and Terraform `fmt`/`validate` plus plan-only cloud dry-runs.
- Security gates: docs-inclusive shell-leak scan, no committed live credentials, no private endpoints in docs, and no direct MiniMax calls from app services.
- Contract gates: `api/openapi.yaml` remains the source of truth for backend APIs consumed by the frontend.

### Notes

- This release prepares documentation and versioning for v1.0.0. It does not tag or publish a GitHub release by itself.
- ADR-024 records the release decisions and v1.1.0 roadmap preview.
