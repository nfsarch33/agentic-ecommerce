# Disaster Recovery: GKE Primary + EKS DR

## Architecture Overview

```
┌─────────────────────────────────┐     ┌─────────────────────────────────┐
│  GCP (australia-southeast1)     │     │  AWS (ap-southeast-2)           │
│  ┌────────────────────────────┐ │     │  ┌────────────────────────────┐ │
│  │  GKE Autopilot (PRIMARY)  │ │     │  │  EKS Managed (DR)         │ │
│  │  ├── api pods             │ │     │  │  ├── api pods (standby)   │ │
│  │  ├── worker pods          │ │     │  │  ├── worker pods (standby)│ │
│  │  └── temporal workers     │ │     │  │  └── temporal workers     │ │
│  └────────────────────────────┘ │     │  └────────────────────────────┘ │
│                                 │     │                                 │
│  ┌────────────────────────────┐ │     │  ┌────────────────────────────┐ │
│  │  CloudSQL PostgreSQL       │─┼─ ─ ─┼─▶│  RDS PostgreSQL (replica) │ │
│  │  (HA, regional)            │ │     │  │  (multi-AZ)               │ │
│  └────────────────────────────┘ │     │  └────────────────────────────┘ │
│                                 │     │                                 │
│  ┌────────────────────────────┐ │     │  ┌────────────────────────────┐ │
│  │  Memorystore Redis         │ │     │  │  ElastiCache Redis (warm) │ │
│  │  (STANDARD_HA)             │ │     │  │  (auto-failover)          │ │
│  └────────────────────────────┘ │     │  └────────────────────────────┘ │
└─────────────────────────────────┘     └─────────────────────────────────┘
                    │                                     │
                    └──────── Route53 Failover ───────────┘
                           (health check: /health/ready)
```

## Failover Topology

| Component     | Primary (GKE)               | DR (EKS)                    |
|---------------|-----------------------------|-----------------------------|
| Compute       | GKE Autopilot, AU-SE1       | EKS Managed, AP-SE-2        |
| Database      | CloudSQL PostgreSQL (HA)    | RDS PostgreSQL (multi-AZ)   |
| Cache         | Memorystore Redis (HA)      | ElastiCache Redis (auto-FO) |
| DNS           | Route53 PRIMARY failover    | Route53 SECONDARY failover  |
| Health Check  | HTTPS /health/ready (30s)   | HTTPS /health/ready (30s)   |

## Failover Trigger

Route53 health checks monitor the `/health/ready` endpoint on both
clusters every 30 seconds with a failure threshold of 3. When the
primary fails 3 consecutive checks, Route53 automatically routes
traffic to the DR endpoint.

## Recovery Time Objective (RTO)

- **DNS failover**: ~60-90 seconds (3 failed checks × 30s interval)
- **Application warm-up**: ~30 seconds (pod readiness probes)
- **Total RTO target**: <3 minutes

## Recovery Point Objective (RPO)

- **Database replication lag**: depends on replication method
  - pglogical: near-real-time (<1s typical)
  - pg_dump periodic: configurable (5-15 min)
- **Redis**: warm standby (no real-time replication; cache rebuilds
  from DB on failover)
- **Target RPO**: <5 minutes for database, cache is best-effort

## Terraform Modules

| Module                          | Purpose                          |
|---------------------------------|----------------------------------|
| `deploy/terraform/eks/`         | EKS cluster + VPC + node groups  |
| `deploy/terraform/dr/failover.tf` | Route53 health checks + failover |
| `deploy/terraform/dr/postgres_replica.tf` | RDS target for cross-cloud replication |
| `deploy/terraform/dr/redis_failover.tf` | ElastiCache warm standby |

## Manual Failover Procedure

1. Verify DR cluster health: `kubectl --context eks-dr get pods -n agentic-ecommerce`
2. Force Route53 failover: disable primary health check in AWS Console
3. Verify DNS resolution: `dig api.agentic-ecommerce.example.com`
4. Monitor DR application logs for errors
5. Verify database connectivity from DR pods

## Failback Procedure

1. Restore primary cluster to healthy state
2. Re-enable primary Route53 health check
3. Verify primary passes 3 consecutive health checks
4. Route53 automatically routes traffic back to primary
5. Drain any in-flight requests on DR cluster

## Limitations (v4.7.0)

- Cross-cloud Postgres replication is a concept -- actual replication
  tool selection (pglogical vs Bucardo vs managed service) deferred
  to production readiness review
- Redis failover is warm standby (cache miss on failover, rebuilds
  from DB); real-time replication deferred
- Temporal workflow state is NOT replicated; in-flight workflows may
  need manual restart after failover
- Live failover testing requires both clusters provisioned; v4.7.0
  validates structural correctness only (terraform validate)
