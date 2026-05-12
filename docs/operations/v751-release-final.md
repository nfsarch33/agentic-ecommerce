# v7.5.1 Release Final Evidence

**Date**: 2026-05-12  
**Release**: `agentic-ecommerce` v7.5.1  
**Base**: v6.6.0 release tag  
**Head**: backend `main` after PR #153 (`dfa5ac1`)

## Scope

v7.5.1 publishes the backend work that shipped after v6.6.0 across v7 Pair 1
through Pair 6 QA:

- Quality foundation: reduced production cyclomatic hotspots and pinned the
  structural guard.
- Coverage harness: hardened Temporal activity tracing panic/fallback coverage.
- Observability spine: added dashboard-ready metric inventory, EvoMap fields,
  and Agentrace replay contracts.
- Resource-aware orchestration: routed scheduler dispatch through bounded
  worker pools and added close/drain semantics.
- Cloud deployability: aligned Compose, Helm, AWS ECS, and GCP Cloud Run
  workload contracts with credential-free Terraform plan gates.
- Adapter hardening: routed carrier calls through shared retry/timeout hooks
  and documented mock/live sandbox boundaries.

## Release Gates

Required gates for the release branch:

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

GitHub Actions CD now treats Terraform cloud credentials as optional for pull
request validation: when `EC_PROJECT_ID`, `EC_REGION`, or
`TERRAFORM_STATE_BUCKET` are absent, the `Terraform plan` job runs the
credential-free `make tf-plan-contract` path instead of failing on empty cloud
provider variables. Real cloud plans still run when all required inputs exist.

## Carry-Forwards

- Live payment, carrier, and social sandbox execution remains blocked until
  external merchant credentials exist.
- Live OmniParser/uiauto execution remains remote-resource gated; do not run
  VLM workloads locally on the MacBook.
- Remaining v7 sprint work resumes at Pair 7 Marketplace and sync.
