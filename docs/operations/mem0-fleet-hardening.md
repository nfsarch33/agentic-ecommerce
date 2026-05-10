# mem0 Operational Hardening for EC Stack

## Overview

The EC stack uses [mem0](https://github.com/mem0ai/mem0) as its memory layer for
agent context persistence and EvoMap capsule storage. mem0 runs on the WSL fleet
(not on the MacBook) to prevent OOM conditions observed during overnight agent runs.

The EC backend connects to mem0 via the `EC_MEM0_ENDPOINT` environment variable.
No infrastructure addresses are hardcoded.

## Architecture

```
┌─────────────────────┐        ┌──────────────────────────────┐
│   EC Backend (Go)   │──HTTP──│  mem0 (WSL Fleet)            │
│                     │        │  ├─ mem0 API (:8080)         │
│  adapter/mem0/      │        │  └─ Qdrant vector DB (:6333) │
│  └─ client.go       │        └──────────────────────────────┘
│     (circuit-broken)│
└─────────────────────┘
```

## Connection Configuration

| Env Var                      | Required | Default | Description                           |
|------------------------------|----------|---------|---------------------------------------|
| `EC_MEM0_ENDPOINT`           | Yes      | —       | Full URL to mem0 API (e.g. `http://....:8080`) |
| `EC_MEM0_TIMEOUT_SECONDS`    | No       | `5`     | Per-request timeout                   |
| `EC_MEM0_ENABLED`            | No       | `true`  | Kill-switch; `false` disables all mem0 calls |

## Connection Resilience

The mem0 client wraps all HTTP calls with the shared circuit breaker from
`internal/resilience/circuit_breaker.go` (introduced in Pair 12).

- **Failure threshold**: 5 consecutive failures → circuit opens
- **Cooldown**: 30 seconds before half-open probe
- **Success threshold**: 2 consecutive successes in half-open → circuit closes

## Graceful Degradation

When mem0 is unavailable (circuit open, disabled, or endpoint unset):

| Operation          | Degraded Behaviour                                     |
|--------------------|--------------------------------------------------------|
| `Store()`          | No-op; returns `nil` (fire-and-forget semantics)       |
| `Search()`         | Returns empty `[]MemoryResult` (callers handle empty)  |
| `Delete()`         | No-op; returns `nil`                                   |
| EvoMap capsules    | Fall back to file-based NDJSON at `data/evomap/`       |

## Health Check

```
GET <EC_MEM0_ENDPOINT>/health → 200 OK
```

The EC stack's readiness probe includes mem0 health as a soft dependency:
mem0 health failure degrades the probe response but does not fail it.

## Observability

Prometheus metrics (prefix `ec_mem0_`):

| Metric                                | Type      | Labels       | Description                   |
|---------------------------------------|-----------|--------------|-------------------------------|
| `ec_mem0_requests_total`              | Counter   | `op, status` | Total requests by operation   |
| `ec_mem0_request_duration_seconds`    | Histogram | `op`         | Latency per operation         |

Estimated cardinality: ~20 series (3 ops × 3 statuses + histogram buckets).

## Deployment

mem0 deployment is managed separately on the WSL fleet. See:

- `deploy/mem0/docker-compose.mem0.yml` — Docker Compose for mem0 + Qdrant
- `deploy/mem0/config.example.yaml` — mem0 configuration template
- `deploy/mem0/qdrant-config.yaml` — Qdrant vector DB configuration
- `docs/operations/mem0-cross-cloud-dr.md` — Disaster recovery plan
