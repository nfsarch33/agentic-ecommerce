# Deployment Runbook

Step-by-step guide for deploying the Agentic E-Commerce stack to production.

## Prerequisites

Before starting, ensure you have:

- [ ] Cloud account with appropriate IAM permissions (GKE admin, EKS admin, or OCI tenancy admin)
- [ ] Terraform >= 1.6.0 installed
- [ ] Helm >= 3.14 installed
- [ ] kubectl configured for the target cluster
- [ ] Docker >= 24.0 installed with buildx
- [ ] Cloud CLI authenticated (gcloud / aws / oci)
- [ ] Remote state backend configured (GCS bucket / S3 bucket / OCI Object Storage)
- [ ] Docker registry accessible with push permissions
- [ ] DNS zone configured for the target domain
- [ ] Environment variables set per `deploy/scripts/deploy.sh --help`

## Step 1: Terraform Init + Apply (Infrastructure)

Create the base infrastructure: VPC, Kubernetes cluster, managed Postgres, managed Redis.

```bash
# Set required environment variables
export EC_PROJECT_ID="<your-project-id>"
export EC_REGISTRY_URL="<your-registry-url>"
export EC_TERRAFORM_STATE_BUCKET="<your-state-bucket>"

# Option A: Use the unified deploy script
./deploy/scripts/deploy.sh \
  --cloud gke \
  --env prod \
  --region australia-southeast1 \
  --skip-build --skip-helm --skip-verify

# Option B: Manual Terraform
cd deploy/terraform/gke
terraform init \
  -backend-config="bucket=${EC_TERRAFORM_STATE_BUCKET}" \
  -backend-config="prefix=agentic-ecommerce/prod"
terraform plan -var="project_id=${EC_PROJECT_ID}" -out=tfplan
terraform apply tfplan
```

**Expected duration**: 10-15 minutes for GKE Autopilot, 15-20 minutes for EKS.

**Verify**: `terraform output` should show cluster endpoint, DB host, and Redis endpoint.

## Step 2: Configure Kubeconfig

```bash
# GKE
gcloud container clusters get-credentials agentic-ecommerce \
  --region australia-southeast1 \
  --project "${EC_PROJECT_ID}"

# EKS
aws eks update-kubeconfig --name agentic-ecommerce --region ap-southeast-2

# Verify
kubectl cluster-info
kubectl get nodes
```

## Step 3: Database Migration

Run all 35 migrations against the provisioned Postgres instance.

```bash
# Get the database connection string from Terraform output or Secret Manager
export ECOMMERCE_DB_URL="postgres://ecommerce:<password>@<db-host>:5432/ecommerce?sslmode=require"

# Run migrations using ec-cli
./ec-cli migrate up

# Verify migration status
./ec-cli migrate status
```

**Expected output**: `35 migrations applied successfully`

**Rollback**: `./ec-cli migrate down --steps 1` to undo the last migration.

## Step 4: Build and Push Docker Images

```bash
# All 8 binaries
COMMIT_SHA=$(git rev-parse --short HEAD)
BINARIES="mc-api wc-sync content-worker carrier-bridge frontend agent-worker ec-cli temporal-worker"

for binary in $BINARIES; do
  docker build \
    --build-arg "TARGET=$binary" \
    --build-arg "COMMIT=$COMMIT_SHA" \
    -t "${EC_REGISTRY_URL}:${COMMIT_SHA}-${binary}" .
  docker push "${EC_REGISTRY_URL}:${COMMIT_SHA}-${binary}"
done
```

**Multi-platform build** (for ARM + x86 clusters):

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  --build-arg "TARGET=mc-api" \
  --build-arg "COMMIT=$COMMIT_SHA" \
  -t "${EC_REGISTRY_URL}:${COMMIT_SHA}-mc-api" \
  --push .
```

## Step 5: Helm Deploy

```bash
COMMIT_SHA=$(git rev-parse --short HEAD)

helm upgrade --install agentic-ecommerce deploy/helm/agentic-ecommerce/ \
  --namespace agentic-ecommerce \
  --create-namespace \
  --set global.environment=prod \
  --set global.image.registry="${EC_REGISTRY_URL}" \
  --set global.image.tag="${COMMIT_SHA}" \
  --set postgres.host="<db-host-from-terraform>" \
  --set redis.addr="<redis-host-from-terraform>:6379" \
  --wait \
  --timeout 10m
```

**Verify pods**: `kubectl get pods -n agentic-ecommerce`

All pods should reach `Running` status within 5 minutes.

## Step 6: Health Verification

```bash
# Get the external endpoint
MC_API_URL=$(kubectl get svc mc-api -n agentic-ecommerce \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Health check
curl -sf "http://${MC_API_URL}:8080/healthz"
# Expected: {"status":"ok","version":"<commit-sha>"}

# Readiness check
curl -sf "http://${MC_API_URL}:8080/readyz"
# Expected: {"status":"ready","checks":{"postgres":"ok","redis":"ok","temporal":"ok"}}
```

## Step 7: Smoke Test

```bash
# Create a test tenant
curl -X POST "http://${MC_API_URL}:8080/api/v1/tenants" \
  -H "Content-Type: application/json" \
  -d '{"name":"smoke-test","plan":"free"}'

# List products (should return empty array for new tenant)
curl "http://${MC_API_URL}:8080/api/v1/products" \
  -H "X-Tenant-ID: smoke-test"

# Verify Temporal workflows are registered
kubectl exec -n agentic-ecommerce deploy/temporal-worker -- \
  temporal workflow list --namespace agentic-ecommerce
```

## Step 8: Monitoring Setup

### Import Grafana Dashboards

```bash
# If using the bundled Grafana deployment
kubectl port-forward svc/grafana -n monitoring 3000:3000 &

# Import dashboards from the OTel configuration
curl -X POST http://localhost:3000/api/dashboards/import \
  -H "Content-Type: application/json" \
  -d @deploy/otel/dashboards/agentic-ecommerce-overview.json
```

### Configure Alert Rules

Key alerts to enable:

| Alert | Condition | Severity |
|-------|-----------|----------|
| API Error Rate | > 1% 5xx over 5min | Critical |
| Pod Restart | > 3 restarts in 15min | Warning |
| DB Connection Pool | > 80% utilization | Warning |
| Redis Memory | > 80% used | Warning |
| Temporal Workflow Failure | > 5% failure rate | Critical |

## Rollback Procedure

### Quick Rollback (Helm)

```bash
# List release history
helm history agentic-ecommerce -n agentic-ecommerce

# Rollback to previous revision
helm rollback agentic-ecommerce <revision> -n agentic-ecommerce --wait

# Verify rollback
kubectl get pods -n agentic-ecommerce
curl -sf "http://${MC_API_URL}:8080/healthz"
```

### Database Rollback Considerations

- Helm rollback does NOT revert database migrations
- If the new version added migrations, check whether rollback is safe:
  - **Additive migrations** (new tables/columns): safe to leave in place
  - **Destructive migrations** (dropped columns): must run `ec-cli migrate down` first
- Always test migration rollback in staging before production

### Full Infrastructure Rollback

```bash
# Revert to previous Terraform state
cd deploy/terraform/gke
terraform state pull > backup.tfstate
terraform apply -target=<resource> -var="..." # Selective revert
```

## Troubleshooting

### Pods stuck in CrashLoopBackOff

```bash
kubectl logs -n agentic-ecommerce <pod-name> --previous
kubectl describe pod -n agentic-ecommerce <pod-name>
```

Common causes:
- Missing environment variable → check Helm values
- Database unreachable → verify Cloud SQL/RDS security group
- Redis connection refused → verify Memorystore/ElastiCache network access

### Terraform state lock

```bash
# If another apply is holding the lock
terraform force-unlock <lock-id>
```

### Helm release stuck in pending-upgrade

```bash
# Check for stuck jobs
kubectl get jobs -n agentic-ecommerce

# Force cleanup
helm rollback agentic-ecommerce 0 -n agentic-ecommerce
```

### High latency after deploy

1. Check pod resource requests vs actual usage: `kubectl top pods -n agentic-ecommerce`
2. Check DB connection pool: query `pg_stat_activity`
3. Check Redis memory: `redis-cli info memory`
4. Review OTel traces for slow spans

## Scaling Guide

### Horizontal Scaling

| Service | Scale Method | Recommended Max |
|---------|-------------|-----------------|
| mc-api | HPA on CPU (70%) | 10 replicas |
| frontend | HPA on CPU (60%) | 8 replicas |
| content-worker | KEDA on queue depth | 20 replicas |
| agent-worker | KEDA on queue depth | 15 replicas |
| wc-sync | HPA on CPU | 5 replicas |
| temporal-worker | HPA on CPU | 10 replicas |

### Vertical Scaling

| Service | Min Resources | Prod Recommended |
|---------|--------------|------------------|
| mc-api | 256m / 512Mi | 500m / 1Gi |
| Postgres | db-custom-1-3840 | db-custom-4-15360 |
| Redis | 1GB | 5GB (Standard HA) |
| Temporal | 512m / 1Gi | 1000m / 2Gi |

### Database Scaling

- **Read replicas**: add Cloud SQL read replicas or RDS read replicas for read-heavy tenants
- **Connection pooling**: PgBouncer sidecar (already included in Helm chart)
- **Partition strategy**: per-tenant schema isolation handles up to ~1000 tenants per instance
