# v7.4.0 Cloud Deployability MVP

**Recorded**: 2026-05-12T13:07:00+10:00  
**Pair**: v7 Pair 5 MVP  
**Branch**: `feat/v740-cloud-deployability`

## Scope

This MVP tightens the AWS/GCP deployment contract without provisioning live
resources. The goal is to keep cloud deployability testable on a developer
machine and to prevent silent drift between production Compose, Helm values, and
Terraform service contracts.

## TDD Evidence

`TestV740CoreWorkloadsHaveComposeHelmAndCloudContracts` was added before the
Terraform change. The RED state found that `content-worker` was present in both
`docker-compose.yml` and `deploy/helm/agentic-ecommerce/values.yaml`, but was
missing from both AWS and GCP Terraform roots:

```text
deploy/terraform/aws-ecs/main.tf missing "module \"content_worker_service\""
```

The GREEN state adds `content_worker_service` to both `aws-ecs` and
`gcp-cloudrun`, includes it in deployment summaries, and exposes provider-neutral
min/max instance variables.

`TestV740TerraformProviderLockPolicyIsDocumented` was added before the docs
change. The RED state found that `deploy/terraform/README.md` had no provider
lock policy. The GREEN state documents the split between credential-free
contract roots (`aws-ecs`, `gcp-cloudrun`) and live-provider roots (`gke`,
`eks`, `oci`, `dr`).

## Cloud Contract Changes

- `content-worker` now has AWS ECS Fargate and GCP Cloud Run contract modules.
- The worker is private, uses the `worker` protocol contract, and does not
  expose public ingress.
- The worker receives only the non-secret embedding/RAG runtime env it needs and
  secret references for AI bridge, embedding bridge, and Redis endpoint.
- `deployment_summary.services` now includes `content_worker` for both clouds.
- `deploy/terraform/README.md` records when provider lock files are required.

## Validation

Completed:

```text
go test ./deploy/terraform -run 'TestV740CoreWorkloadsHaveComposeHelmAndCloudContracts|TestV740TerraformProviderLockPolicyIsDocumented' -count=1
go test ./deploy/terraform -count=1
terraform fmt -recursive deploy/terraform
make tf-fmt-check
make tf-validate
make compose-config-prod
helm lint deploy/helm/agentic-ecommerce
go test -race -p 1 -count=1 ./...
go test -covermode=atomic -coverprofile=coverage.out -p 1 -count=1 ./...
go tool cover -func=coverage.out
govulncheck ./...
cursor-tools docsync check .
runx shell-leak-scan --root . --include-docs
sentrux gate .
git diff --check
```

Gate snapshot:

```text
coverage: 85.1%
govulncheck: no vulnerabilities found
docsync: PASS
shell-leak-scan: 147 files scanned, no findings
sentrux: Quality 6041 -> 6037, Coupling 0.04 -> 0.04, Cycles 1 -> 1, God files 0 -> 0
memory_pressure: 46% free before final gate batch
```
