# ADR-024: v1.0.0 Release Decisions and v1.1.0 Preview

Date: 2026-05-07
Status: Accepted
Owner: nfsarch33

## Context

The Agentic Ecommerce stack has completed v0.1.0 through v0.9.0 QA. The backend now owns the Go Mission Control API, worker entry points, OpenAPI contract, Docker Compose stack, cloud dry-run scaffolding, observability, and security baseline. The frontend owns the public Next.js storefront, admin surfaces, BFF route handlers, and typed backend adapters.

v1.0.0 needs a release posture that is safe for public repositories, cloud-agnostic, and reviewable across the backend and frontend repos without adding live infrastructure or secrets.

## Decision Drivers

- Must keep Docker Compose as the local and release-candidate wiring source of truth.
- Must keep AWS ECS and GCP Cloud Run as dry-run deployment paths until cloud ownership, state, IAM, TLS, DNS, and secret-manager boundaries are approved.
- Must keep `api/openapi.yaml` as the backend API contract consumed by `agentic-ecommerce-web`.
- Must keep AI calls behind approved bridge boundaries; app services must not call MiniMax directly.
- Should make release evidence easy to verify from docs, commits, and PRs.
- Should avoid global knowledge-base churn unless a decision affects the wider agent fleet.

## Considered Options

### Option 1: Tag v1.0.0 from runtime branches only

- Pros: Minimal documentation effort.
- Cons: Leaves release gates, deployment boundaries, and v1.1.0 direction implicit.

### Option 2: Prepare repo-local release docs before tagging

- Pros: Keeps release criteria, changelog, versioning, and cloud boundaries close to the code being released.
- Cons: Requires duplicated release notes across backend and frontend repos.

### Option 3: Move release decisions to global-kb first

- Pros: Durable fleet-level visibility.
- Cons: Over-scopes a product release decision and risks stale repo-local docs.

## Decision

Use repo-local release documentation for v1.0.0. The backend records the cross-stack release decision in ADR-024 because it owns the API contract, Compose stack, Terraform dry-run scaffolding, and security boundary that the frontend consumes.

The v1.0.0 release may proceed only after both repos have:

- v1.0.0 changelog entries.
- Version metadata aligned to `1.0.0`.
- README quickstart and architecture references.
- Release checklist docs.
- Docs-inclusive shell-leak validation.
- PR review and merge to `main`.

Global-kb updates are deferred unless a later decision changes fleet-wide repo routing, shared memory, MCP policy, or release automation.

## v1.1.0 Roadmap Preview

v1.1.0 should focus on operational hardening rather than new surface area:

- Convert cloud scaffolding from dry-run contracts to one approved live environment with managed state and secret-manager ownership.
- Replace worker placeholders with scheduled production jobs where the backend orchestrator contract is stable.
- Add signed release artifacts and image provenance for backend and frontend containers.
- Expand contract tests around OpenAPI client generation and BFF route compatibility.
- Promote Lighthouse, k6, and Compose smoke evidence into automated CI gates.

## Consequences

### Positive

- Release readiness is auditable from each repo without requiring private notes.
- API, deployment, and security boundaries are explicit before tagging.
- The frontend can cite backend contracts without duplicating backend internals.

### Negative

- Backend and frontend changelogs must stay aligned manually for this release.
- Cloud deployment remains dry-run only until v1.1.0 or a dedicated infrastructure PR approves live resources.

### Mitigations

- PR descriptions must include final backend and frontend SHAs.
- Release checklist docs list the commands and skipped-gate rationale required before tagging.
- Future automation can convert these checklist items into CI or release workflow gates.
