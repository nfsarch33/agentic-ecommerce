# Python CCE Sidecar Evaluation

**Version:** v4.9.0 Story 3  
**Date:** 2026-05-10  
**Status:** DECIDED — NOT NEEDED  
**Decision:** Python sidecar is not required for the EC stack.

## Context

Evaluate whether a Python sidecar is needed for the agentic-ecommerce stack to serve data science, ML model training, or reporting use cases.

### Current Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go (all business logic, API, workflows) |
| Frontend | Next.js 16 (React) |
| Workflows | Temporal (Go SDK) |
| LLM offload | omniparser-bridge on WSL fleet |
| Analytics | SQL materialized views (Postgres) |
| ML/AI | LLM calls routed to external APIs via llm-cluster-router |

### Candidates Evaluated

| Use Case | Python Needed? | Alternative |
|----------|---------------|-------------|
| Data science notebooks | No | SQL materialized views + Grafana dashboards cover all reporting needs |
| ML model training | No | All ML is offloaded to WSL GPU fleet via omniparser-bridge; no on-cluster training |
| pandas-based reporting | No | ROI heatmap, GMV rollup, channel content rollup all use Postgres materialized views with EXPLAIN ANALYZE-validated query plans |
| Custom ML inference | No | LLM inference routed to vLLM cluster via llm-cluster-router; no custom model deployment needed |

### Decision Criteria

1. **Is there a use case not served by Go + existing LLM offload?** — No. All business logic runs in Go. ML/AI inference is offloaded to the WSL fleet where Python already runs.

2. **Would a Python sidecar reduce complexity?** — No. Adding a Python sidecar introduces:
   - Additional Docker image to build and maintain
   - Cross-service communication overhead (gRPC or HTTP between Go and Python)
   - Separate dependency management (pip/poetry alongside Go modules)
   - Additional deployment artifact in Helm charts

3. **Are there performance-critical Python libraries with no Go equivalent?** — No. The stack uses:
   - `pgx` for Postgres (no pandas needed)
   - SQL materialized views for analytics (no numpy/scipy needed)
   - External LLM APIs for AI (no transformers/torch needed)

## Rationale

The agentic-ecommerce backend is a Go-first architecture where:

- **Business logic** is handled entirely in Go with well-decomposed packages.
- **ML inference** is offloaded to the WSL fleet's vLLM instances via the omniparser-bridge, which already runs Python.
- **Analytics** use Postgres materialized views that are EXPLAIN ANALYZE-validated and index-only-scan optimized.
- **Reporting** is served by Grafana dashboards connected to Prometheus metrics.

Adding a Python sidecar would increase operational complexity without providing capabilities not already covered. If a Python-specific need arises in the future (e.g., custom model fine-tuning on production data), the recommended approach is:

1. Add a Temporal activity that dispatches to the WSL fleet (already supported)
2. Use the existing omniparser-bridge protocol for structured inference

## Consequences

- No `deploy/sidecar/python-cce/` directory created.
- No additional Docker images to maintain.
- Stack remains Go + Next.js + Temporal + Postgres.
- If Python needs arise, re-evaluate in a future sprint with a concrete use case.
