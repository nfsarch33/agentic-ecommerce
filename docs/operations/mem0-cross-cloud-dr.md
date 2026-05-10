# mem0 Cross-Cloud Disaster Recovery Plan

## Overview

This document describes the disaster recovery (DR) strategy for the mem0 memory
layer used by the EC stack. The strategy spans three deployment targets across
self-hosted and cloud infrastructure.

## Deployment Tiers

| Priority  | Target              | Terraform Module                | Notes                                    |
|-----------|---------------------|---------------------------------|------------------------------------------|
| Primary   | WSL Fleet (self-hosted) | N/A (Docker Compose)        | Lowest latency; always-on                |
| Secondary | Oracle Cloud (OCI)  | `deploy/terraform/oci/`         | ARM Flex; cost-effective standby          |
| Tertiary  | GKE Autopilot       | `deploy/terraform/gke/`         | K8s-native; from Pair 4 infra            |

All targets run identical mem0 + Qdrant stacks via Docker Compose or Helm.

## Data Backup Strategy

### Qdrant Vector DB

- **Mechanism**: Qdrant snapshot API (`POST /collections/{name}/snapshots`)
- **Storage targets**:
  - OCI Object Storage bucket (secondary)
  - GCS bucket (tertiary)
- **Frequency**:
  - Non-critical environments: daily at 02:00 UTC
  - Production: hourly
- **Retention**: 7 daily + 24 hourly snapshots (rolling window)
- **Automation**: Cron job on primary host; snapshot uploaded via `rclone` to
  object storage

### mem0 Metadata (Postgres-backed)

- **Mechanism**: Existing EC Postgres backup pipeline (`pg_dump` + WAL archiving)
- **Frequency**: Aligned with EC Postgres schedule (hourly WAL, daily full)
- **Retention**: 30 days of point-in-time recovery
- **Restore**: Standard `pg_restore` into secondary/tertiary Postgres instance

### Backup Verification

Monthly restore drills on the secondary target:

1. Restore latest Qdrant snapshot to OCI instance
2. Restore latest Postgres backup
3. Run mem0 health check + sample Search query
4. Log drill result in ops runbook

## Failover Procedure

### Detection

The EC backend health check loop monitors `GET <EC_MEM0_ENDPOINT>/health`:

- **Interval**: 30 seconds
- **Failure criteria**: 3 consecutive failures within 5 minutes
- **Alert channel**: Prometheus `ec_mem0_requests_total{status="error"}` fires
  alert rule → PagerDuty / Slack notification

### Failover Steps

```
1. DETECT   │ Health check fails 3× in 5 min
            ▼
2. PROMOTE  │ Update EC_MEM0_ENDPOINT to secondary target
            │ Option A: DNS-based (Cloudflare DNS record update)
            │ Option B: Env var swap + rolling restart
            ▼
3. VERIFY   │ Confirm secondary health: GET <new-endpoint>/health → 200
            │ Run sample Store + Search to validate data access
            ▼
4. NOTIFY   │ Post incident to #ec-ops Slack channel
            │ Create incident ticket
            ▼
5. RESTORE  │ When primary recovers:
            │ a. Sync Qdrant snapshots from secondary → primary
            │ b. Verify primary health
            │ c. Switch EC_MEM0_ENDPOINT back to primary
            │ d. Monitor for 30 min before closing incident
```

### Failover Decision Matrix

| Scenario                    | Action                          | RTO Target | RPO Target |
|-----------------------------|---------------------------------|------------|------------|
| Primary host down           | Promote secondary (OCI)         | < 5 min    | < 1 hour   |
| Primary + secondary down    | Promote tertiary (GKE)          | < 15 min   | < 1 hour   |
| Data corruption (primary)   | Restore from latest snapshot    | < 30 min   | < 1 hour   |
| Network partition            | EC degrades gracefully (circuit breaker) | 0 min | N/A |

## Graceful Degradation

If all mem0 targets are unreachable, the EC stack continues operating:

- Circuit breaker opens after 5 consecutive failures (30s cooldown)
- `Search()` returns empty results; `Store()` is a no-op
- EvoMap capsules fall back to file-based NDJSON at `data/evomap/`
- No customer-facing functionality is impacted (mem0 is an enhancement layer)

## Configuration

Each deployment target uses the same env vars:

```bash
EC_MEM0_ENDPOINT=http://<target-host>:8080
EC_MEM0_TIMEOUT_SECONDS=5
EC_MEM0_ENABLED=true
```

The failover procedure only changes `EC_MEM0_ENDPOINT`. All other configuration
remains constant.

## Related Documents

- `docs/operations/mem0-fleet-hardening.md` — Primary deployment details
- `docs/operations/disaster-recovery.md` — EC stack-wide DR plan
- `deploy/terraform/oci/` — Secondary target IaC
- `deploy/terraform/gke/` — Tertiary target IaC
