# EC v8.0.0 Release Final Evidence

**Date**: 2026-05-13  
**Release**: `agentic-ecommerce` v8.0.0  
**Base**: v7.5.1 release tag  
**Head**: backend `main` after PR #168 (`6a31e5b`)

## Scope

v8.0.0 publishes the backend portions of the v8 TDD implementation run:

- Pair 1 marketplace sync core: idempotent sync, ledger, DLQ, replay, and
  reconciliation evidence.
- Pair 2 Shopify adapter: connector behind shared marketplace ports with
  contract cassettes and sandbox-boundary docs.
- Pair 3 Shopee adapter: official-doc capture, signing/auth tests, and
  guarded no-live-call replay proof.
- Pair 4 image editing: provider-neutral request contracts, approval states,
  remote large-asset routing, and media KPI output.
- Pair 6 Temporal orchestration: deterministic sync and image approval
  workflows with replay, signals, queries, cancellation, retry, and shutdown
  evidence.
- Pair 8 OOM observability: resource guard, workerpool/memwatch metrics,
  Sentrux cleanup checks, Agenttrace, and EvoMap wiring.
- Pair 9 self-improvement: autoresearch producer-reviewer evidence,
  Agenttrace replay, and EvoLoop/DRL reward artifacts.
- Pair 10 release hardening: metadata guard tests, ADR-035, changelog, release
  checklist, artifacts, and handoff hooks.

Pair 5 frontend media UX and Pair 7 docsync automation are released from their
owning repositories and are referenced from the global-kb v8 release handoff.

## Release Gates

Required backend release gates for this branch:

- `runx git diff --repo ecommerce -- --check`
- `runx docs check --repo ecommerce`
- `runx shell-leak-scan --repo ecommerce`
- `runx go test --repo ecommerce -- -race -p 1 -count=1 ./...`
- `runx make --repo ecommerce -- coverage-check`
- `runx make --repo ecommerce -- govulncheck-scan`
- `runx make --repo ecommerce -- build`
- `runx make --repo ecommerce -- compose-config-prod`
- `runx make --repo ecommerce -- tf-fmt-check`
- `runx make --repo ecommerce -- tf-validate`
- `runx make --repo ecommerce -- tf-plan-contract`
- `runx sentrux gate --repo ecommerce`

Focused release metadata guard:

- `runx worktree run --repo ecommerce --branch release/v8-final-hardening -- /opt/homebrew/bin/rtk go test ./internal/qa -run TestV800ReleaseMetadataAligned -count=1`

## Carry-Forwards

- Frontend `v8.0.0` metadata and QA release must land before the stack-level
  global-kb release handoff is final.
- Live Shopify/Shopee/payment/carrier/social sandbox execution remains blocked
  until external operator credentials and sandbox approvals exist.
- Live OmniParser/uiauto/VLM execution remains remote-resource gated; do not
  run those workloads locally on the MacBook.
- Mem0 hot recall remains optional while endpoint reliability is degraded; Git
  KB remains durable truth.
