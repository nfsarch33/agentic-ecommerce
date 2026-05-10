# Cloud Cost Optimization Guide

> Last verified: 2026-05-11

Cost analysis and optimization strategies for running the Agentic E-Commerce stack
across GKE Autopilot, EKS, and OCI.

## Per-Cloud Cost Analysis

### Cost Comparison Table (USD/month estimates)

| Component | GKE Autopilot | EKS | OCI (Always Free) |
|-----------|--------------|-----|-------------------|
| **Cluster management** | $74.40 (Autopilot fee) | $73 (EKS control plane) | $0 (OKE free tier) |
| **Compute (dev)** | ~$50-80 (Autopilot pods) | ~$60-90 (2× t3.medium) | $0 (ARM A1, 4 OCPU free) |
| **Compute (staging)** | ~$150-250 | ~$180-300 (3× t3.large) | ~$30-50 (ARM A1 flex) |
| **Compute (prod)** | ~$400-700 | ~$500-800 (4× m6i.xlarge) | ~$150-300 |
| **Postgres** | ~$50-80 (Cloud SQL db-custom-1) | ~$30-60 (RDS db.t4g.micro) | $0 (Always Free ATP) |
| **Postgres (prod)** | ~$200-350 (regional HA) | ~$250-400 (Multi-AZ RDS) | ~$100-200 (paid ATP) |
| **Redis** | ~$35-55 (1GB Memorystore) | ~$15-25 (cache.t4g.micro) | N/A (self-hosted on A1) |
| **Redis (prod)** | ~$110-180 (Standard HA) | ~$80-140 (r6g.large) | ~$0 (self-hosted A1) |
| **Object storage** | ~$2-5 (GCS Standard) | ~$2-5 (S3 Standard) | $0 (10GB free tier) |
| **Data transfer** | ~$10-30 | ~$20-50 | $0 (10TB/mo free) |
| **Monitoring** | ~$0-10 (Cloud Ops free tier) | ~$10-30 (CloudWatch) | $0 (OCI Monitoring free) |
| **Total (dev)** | **~$220-310** | **~$210-330** | **~$0-30** |
| **Total (staging)** | **~$420-640** | **~$530-820** | **~$180-350** |
| **Total (prod)** | **~$800-1,350** | **~$960-1,520** | **~$250-500** |

> **Key finding**: OCI Always Free tier provides the most cost-effective dev/staging
> environment, especially for mem0 + Qdrant workloads on ARM A1 instances.
> GKE Autopilot offers the simplest operations model for production.
> EKS provides the most mature ecosystem but highest baseline cost.

### GKE Autopilot Breakdown

| Tier | Autopilot Fee | Pod Compute | Cloud SQL | Memorystore | Total |
|------|--------------|-------------|-----------|-------------|-------|
| Dev | $74.40 | ~$60 | ~$50 | ~$35 | ~$220 |
| Staging | $74.40 | ~$200 | ~$120 | ~$75 | ~$470 |
| Prod | $74.40 | ~$500 | ~$280 | ~$150 | ~$1,005 |

Autopilot pricing is per-pod (vCPU: $0.0445/hr, memory: $0.0049/GB/hr).
Idle pods are charged at request amounts; burst is charged at limit amounts.

### EKS Breakdown

| Tier | Control Plane | EC2 Nodes | RDS | ElastiCache | Data Transfer | Total |
|------|--------------|-----------|-----|-------------|---------------|-------|
| Dev | $73 | ~$70 | ~$35 | ~$18 | ~$15 | ~$210 |
| Staging | $73 | ~$220 | ~$100 | ~$55 | ~$25 | ~$475 |
| Prod | $73 | ~$600 | ~$320 | ~$120 | ~$45 | ~$1,160 |

EKS managed node groups with Amazon Linux 2023. Consider Graviton (ARM)
instances for 20-30% cost reduction.

### OCI Always Free Utilization

The OCI Always Free tier provides significant resources at zero cost:

| Resource | Free Tier Allowance | EC Stack Usage |
|----------|-------------------|----------------|
| ARM A1 Compute | 4 OCPU, 24GB RAM | mem0 server + Qdrant |
| AMD Micro | 2 instances, 1GB each | Monitoring/bastion |
| Block Storage | 200GB total | OS + data volumes |
| Object Storage | 10GB Standard | Backups and archives |
| Outbound Transfer | 10TB/month | More than sufficient |
| ATP Database | 2 instances, 20GB each | Dev/staging Postgres |
| Load Balancer | 1 flexible LB | Ingress routing |

## Cost Reduction Strategies

### 1. Spot/Preemptible Instances for Non-Critical Workers

```hcl
# GKE: use Spot pods for content-worker and agent-worker
nodeSelector:
  cloud.google.com/gke-spot: "true"
tolerations:
  - key: cloud.google.com/gke-spot
    operator: Equal
    value: "true"
    effect: NoSchedule

# EKS: use Spot instances for worker node groups
resource "aws_eks_node_group" "spot_workers" {
  capacity_type  = "SPOT"
  instance_types = ["t3.medium", "t3.large", "m5.large"]
  # 60-90% savings over on-demand
}
```

**Applicable services**: content-worker, agent-worker, temporal-worker
**NOT recommended for**: mc-api, frontend, carrier-bridge (availability-sensitive)
**Estimated savings**: 60-70% on worker compute (~$150-250/mo in prod)

### 2. Reserved Instances / Committed Use Discounts

| Service | Commitment | GKE CUD | AWS RI |
|---------|-----------|---------|--------|
| Postgres (prod) | 1-year | 25% off | 30% off |
| Postgres (prod) | 3-year | 52% off | 55% off |
| Redis (prod) | 1-year | 25% off | 30% off |
| Compute (prod) | 1-year | 28% off | 36% off |

**Estimated savings (1yr commit)**: ~$200-350/mo in prod

### 3. Object Storage for Archival

Move historical data to cold/archive storage tiers:

- **Product image archives**: GCS Nearline / S3 Infrequent Access (~$0.01/GB/mo)
- **Audit logs > 90 days**: GCS Coldline / S3 Glacier (~$0.004/GB/mo)
- **Database backups > 30 days**: Archive tier (~$0.0012/GB/mo)

### 4. CDN for Frontend Static Assets

| Provider | Service | Free Tier | Beyond Free |
|----------|---------|-----------|-------------|
| GCP | Cloud CDN | N/A | ~$0.08/GB |
| AWS | CloudFront | 1TB/mo free | ~$0.085/GB |
| Cloudflare | CDN | Unlimited (free plan) | $0 |

**Recommendation**: Use Cloudflare free tier for static frontend assets.
Reduces origin egress costs and improves global latency.

### 5. KEDA Scale-to-Zero for Dev Environments

```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: mc-api-dev
spec:
  minReplicaCount: 0    # Scale to zero when idle
  maxReplicaCount: 3
  cooldownPeriod: 300    # 5 min idle before scale-down
  triggers:
    - type: prometheus
      metadata:
        query: sum(rate(http_requests_total{service="mc-api"}[5m]))
        threshold: "1"
```

**Estimated savings**: ~$100-150/mo per dev environment during off-hours

### 6. Right-Sizing Recommendations

Run `kubectl top pods` in each environment to identify over-provisioned services:

| Service | Default Request | Typical Usage | Recommended |
|---------|----------------|---------------|-------------|
| mc-api | 512m CPU / 1Gi | 100m / 256Mi | 256m / 512Mi |
| frontend | 512m / 1Gi | 50m / 128Mi | 128m / 256Mi |
| agent-worker | 1000m / 2Gi | 400m / 1Gi | 512m / 1Gi |
| temporal-worker | 512m / 1Gi | 200m / 512Mi | 256m / 512Mi |

## Budget Alerts

### GCP Billing Budget (Terraform)

```hcl
resource "google_billing_budget" "ec_monthly" {
  billing_account = var.billing_account_id
  display_name    = "agentic-ecommerce-${var.environment}"

  budget_filter {
    projects = ["projects/${var.project_number}"]
    labels = {
      app = ["agentic-ecommerce"]
    }
  }

  amount {
    specified_amount {
      currency_code = "USD"
      units         = var.environment == "prod" ? "1500" : "500"
    }
  }

  threshold_rules {
    threshold_percent = 0.5
  }
  threshold_rules {
    threshold_percent = 0.8
  }
  threshold_rules {
    threshold_percent = 1.0
  }

  all_updates_rule {
    monitoring_notification_channels = var.notification_channels
  }
}
```

### AWS Budget Alarm (Terraform)

```hcl
resource "aws_budgets_budget" "ec_monthly" {
  name         = "agentic-ecommerce-${var.environment}"
  budget_type  = "COST"
  limit_amount = var.environment == "prod" ? "1500" : "500"
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  cost_filter {
    name   = "TagKeyValue"
    values = ["user:app$agentic-ecommerce"]
  }

  notification {
    comparison_operator       = "GREATER_THAN"
    threshold                 = 80
    threshold_type            = "PERCENTAGE"
    notification_type         = "FORECASTED"
    subscriber_email_addresses = var.alert_emails
  }

  notification {
    comparison_operator       = "GREATER_THAN"
    threshold                 = 100
    threshold_type            = "PERCENTAGE"
    notification_type         = "ACTUAL"
    subscriber_email_addresses = var.alert_emails
  }
}
```

## Summary: Recommended Cost Profile

| Strategy | Annual Savings (Prod) | Effort |
|----------|----------------------|--------|
| Spot workers | ~$2,000-3,000 | Low |
| 1-year CUDs/RIs | ~$2,500-4,200 | Low |
| KEDA scale-to-zero (dev) | ~$1,200-1,800 | Medium |
| Right-sizing | ~$1,000-2,000 | Medium |
| CDN (Cloudflare) | ~$200-500 | Low |
| OCI for dev/mem0 | ~$2,600-3,700 | Medium |
| **Total potential** | **~$9,500-15,200** | — |
