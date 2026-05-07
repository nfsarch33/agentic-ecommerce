# ADR v302-001: Polyrepo Split for Agentic Ecommerce

Date: 2026-05-05
Status: Draft for v302 day-1 review
Owner: nfsarch33

## Context

v301 was changed to cleanup-first after the post-v300 audit found incorrect retro evidence paths, shell-leak carry-forward, and targeted business test failures. The ecommerce product work now starts in v302, with WooCommerce cashflow first and a polyrepo strategy selected by the operator.

The current `agentic-ecommerce` repo remains the bootstrap/prewire surface. v302 decides whether to keep it as a meta/control repo or migrate its initial code into the new repo set once the first repo is created and gated.

## Decision

Use a polyrepo layout for the brand product:

| Repo | Visibility | Responsibility |
| --- | --- | --- |
| `agentic-ec-mission-control` | private | Mission Control API, tenant/config service, operator dashboard API, health and audit endpoints. |
| `agentic-ec-cce` | private | Content Compliance Engine: quality scoring, SEO/readability/factual checks, publish approval contracts. |
| `agentic-ec-mis` | private | Media Intelligence Service: supplier media ingestion, image normalization, metadata, and asset provenance. |
| `agentic-ec-orchestration` | private | Temporal/n8n workflow glue, publish pipelines, webhook replay, and cross-service job orchestration. |
| `agentic-ec-shared` | private first, possible public later | Shared Go contracts: domain models, DTOs, event schemas, test fixtures, and generated clients. |

Each repo is Go-first, config-file driven, and must use Clean Architecture boundaries. No repo may depend on a sibling through private filesystem paths; use Go modules, versioned contracts, or generated clients.

## `runx` Alias Plan

Add aliases only after each repo exists locally and has a remote:

| Alias | Repo |
| --- | --- |
| `ec-mc` | `agentic-ec-mission-control` |
| `ec-cce` | `agentic-ec-cce` |
| `ec-mis` | `agentic-ec-mis` |
| `ec-orch` | `agentic-ec-orchestration` |
| `ec-shared` | `agentic-ec-shared` |

The current `ecommerce` alias stays pointed at `agentic-ecommerce` until v302 migration decides otherwise. The v302 drift gate must extend v301 rather than replacing it silently.

## Cashflow Continuity

WooCommerce remains the first revenue path. The split must not block the existing cashflow stack:

- Keep `wc-sync` and product CRUD as the first vertical.
- Keep publish operations dry-run or operator-approved until DreamHost/WooCommerce live credentials are verified.
- Preserve jobhunt/cashflow tooling in `ai-agent-business-stack` until ecommerce revenue reporting is proven in the new stack.
- Treat Shopify, coaching, Flutter admin, and MADRL as later roadmap work, not v302 blockers.

## Consequences

Positive:

- Clear ownership and release cadence per service.
- Smaller blast radius for secrets and external API dependencies.
- Easier to make `agentic-ec-shared` public later if contracts become reusable.
- Each service can carry focused Sentrux and coverage gates.

Costs:

- More repo bootstrapping and `runx` alias maintenance.
- Cross-repo contract drift risk.
- More CI setup before feature velocity improves.

Mitigations:

- Start with one repo at a time: shared contracts, then Mission Control, then CCE.
- Require `runx shell-leak-scan --repo <alias>` and Sentrux check before first PR in each repo.
- Add contract tests in both provider and consumer repos for every shared schema.
- Keep `agentic-ecommerce` as the migration ledger until all splits are proven.

## Validation Plan

1. Create the first repo and alias through `runx` only.
2. Add a minimal Go module, README, `.cursor/rules/no-shell-leak.mdc`, and CI.
3. Run `runx go test`, `runx sentrux check`, and `runx shell-leak-scan`.
4. Add the repo to `global-memories/repo-index.md` and `component-index.md`.
5. Update the v302 sprint retro with exact PR and command evidence.

## Open Questions

- Should `agentic-ecommerce` become a meta repo, or should it be archived after migration?
- Should `agentic-ec-shared` be public once contracts are stable and sanitized?
- Which repo owns WooCommerce credential rotation: Mission Control or Orchestration?
