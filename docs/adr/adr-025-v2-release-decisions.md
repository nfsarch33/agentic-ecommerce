# ADR-025: v2.0.0 Release Decisions and v3.0.0 Preview

Date: 2026-05-08
Status: Accepted
Owner: nfsarch33

## Context

The Agentic Ecommerce stack has completed the v1.1.0 through v1.9.0 roadmap after the v1.0.0 public release baseline. The backend now owns Redis Streams events, tenant-aware persistence and reporting, Temporal workflow APIs and worker wiring, pgvector RAG and fact-checking, Media Intelligence contracts, outbound webhooks for n8n automation, recurring agent schedule controls, Compose profiles, cloud dry-run hardening, and release QA gates.

v2.0.0 needs to consolidate those capabilities into a release posture that is easy to review from the backend and frontend repos, safe for public collaboration, and explicit about what remains dry-run or operator-approved before live production use.

## Decision Drivers

- Must keep `api/openapi.yaml` as the backend API source of truth for the Next.js frontend.
- Must keep AI calls behind approved bridge boundaries; app services must not call MiniMax directly.
- Must keep Docker Compose as the local release-candidate wiring source of truth.
- Must keep Temporal, n8n, object storage, CDN, and cloud infrastructure credential-free by default.
- Must document tenant-aware behavior without claiming full tenant provisioning or billing support.
- Should make workflow, webhook, and release validation evidence auditable from repo-local docs and PRs.
- Should avoid global-kb churn unless the release changes shared agent-fleet policy.

## Considered Options

### Option 1: Tag v2.0.0 from the runtime branches only

- Pros: Minimal documentation effort.
- Cons: Leaves the v1.1-v2.0 capability set, workflow contracts, n8n boundary, and v3 direction implicit.

### Option 2: Prepare repo-local release docs and backend ADR before tagging

- Pros: Keeps release criteria, changelog, API docs, workflow specs, webhook contracts, and frontend admin docs close to the code that implements them.
- Cons: Requires coordinated backend/frontend PRs and manual release-note alignment.

### Option 3: Move the v2.0.0 decision into global-kb first

- Pros: Durable fleet-level visibility.
- Cons: Over-scopes a product release decision and risks stale repo-local docs unless the repo contracts are updated first.

## Decision

Use repo-local release documentation for v2.0.0, with the backend owning this cross-stack ADR because it owns the API contract, Temporal workflow layer, n8n webhook boundary, Compose stack, Terraform dry-run scaffolding, and security posture that the frontend consumes.

The v2.0.0 release may proceed only after both repos have:

- v2.0.0 changelog entries.
- Version metadata aligned to `2.0.0`.
- README updates for Temporal, n8n, Media Intelligence, tenant-aware admin, and RAG.
- Backend API, Temporal workflow, and webhook contract docs.
- Frontend admin operations docs for workflows, media, tenant settings, and webhooks/n8n.
- Docs-inclusive shell-leak validation.
- PR review and merge to `main`.

Global-kb updates are deferred unless a later release changes fleet-wide repo routing, shared memory, MCP policy, or reusable release automation.

## v3.0.0 Roadmap Preview

v3.0.0 should focus on turning v2.0.0's operator-approved contracts into managed product surfaces:

- Full multi-tenant provisioning with account lifecycle, tenant selector persistence, tenant-scoped uniqueness, billing hooks, and self-service onboarding.
- Optional Python CCE sidecar for ML-heavy evaluation if Go RAG and deterministic fact-checking are insufficient.
- Marketplace hub with plugin packaging, tenant-approved integrations, and stricter sandbox/runtime permissions.
- Flutter admin app for mobile operator workflows backed by the same OpenAPI and workflow contracts.
- MADRL-style multi-agent coordination with shared reward signals for sourcing, pricing, compliance, and content quality.
- Self-testing Temporal loops that create disposable test tenants, run scripted storefront/admin journeys, compare metrics, and file release-blocking incidents automatically.

## Consequences

### Positive

- Release readiness is auditable from public repo docs without private notes.
- Workflow and webhook contracts are explicit before tagging.
- Frontend admin docs can cite backend contracts instead of duplicating backend internals.
- v3.0.0 scope is clear without inflating the v2.0.0 release.

### Negative

- Backend and frontend changelogs must stay aligned manually for this release.
- Temporal, n8n, object storage, CDN, and cloud deploy paths remain operator-approved contracts, not fully managed production services.
- Tenant provisioning, billing, and mobile admin are explicitly deferred.

### Mitigations

- PR descriptions must include final backend and frontend SHAs.
- Release checklist docs list commands and skipped-gate rationale required before tagging.
- Future automation can convert the checklist items into CI or release workflow gates.
