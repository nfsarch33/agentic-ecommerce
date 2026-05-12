# v7.4.1 Cloud Deployability QA

**Recorded**: 2026-05-12T13:35:00+10:00  
**Pair**: v7 Pair 5 QA/Retro  
**Branch**: `qa/v741-cloud-deployability-qa`

## Scope

This QA slice validates the v7.4.0 cloud deployability MVP without touching
live AWS, GCP, OCI, or Kubernetes resources. The intent is to keep providerless
contract roots cheap to verify on a developer machine while keeping live
provider roots explicitly operator-gated.

## Credential-Free Validation Matrix

| Surface | Command | Credential boundary | Expected result |
| --- | --- | --- | --- |
| Terraform contracts | `go test ./deploy/terraform -count=1` | Reads local files only. | Contract tests pass. |
| Terraform formatting | `make tf-fmt-check` | Reads local Terraform files only. | No formatting drift. |
| Terraform validate | `make tf-validate` | Uses local modules and providerless roots. | `modules/network`, `modules/objectstore`, `modules/postgres`, `modules/redis`, `modules/service`, `modules/container_cluster`, `modules/tenant_provisioning`, `deploy/terraform/aws-ecs`, and `deploy/terraform/gcp-cloudrun` validate. |
| Terraform plan | `make tf-plan-contract` | Runs only credential-free roots with `-backend=false`, `-refresh=false`, and `-lock=false`. | `deploy/terraform/aws-ecs` and `deploy/terraform/gcp-cloudrun` produce plans without cloud credentials. |
| Production Compose | `make compose-config-prod` | Local Docker Compose config render only. | Production service graph renders. |
| Helm chart | `helm lint deploy/helm/agentic-ecommerce` | Static chart lint only. | Chart lints. |

live provider roots stay operator-gated: `deploy/terraform/gke`,
`deploy/terraform/eks`, `deploy/terraform/oci`, and `deploy/terraform/dr`
require explicit account, backend, IAM, and state-owner decisions before
`terraform plan` or `terraform apply`.

## Rollback Boundary

Helm rollback does not revert Terraform state. Rollbacks must keep these
boundaries separate:

- Application rollback: use Helm release history and `helm rollback` per
  `docs/operations/deployment-runbook.md`.
- Database rollback: review migration reversibility before any downgrade.
- Infrastructure rollback: use remote-state backup, reviewed Terraform plan,
  and targeted apply only under operator approval.
- Credential-free contract roots: rerun `make tf-plan-contract` after rollback
  docs or service-contract changes to prove the providerless AWS/GCP examples
  still plan without live cloud access.

Do not run live-provider plan/apply work on the MacBook without explicit cloud
credentials, budget, state backend, and operator approval.

## Validation

Completed:

```text
go test ./deploy/terraform -run 'TestV741MakefileDefinesCredentialFreePlanMatrix|TestV741CloudDeployabilityQADocumentsMatrixAndRollback' -count=1
go test ./deploy/terraform -count=1
make tf-fmt-check
make tf-validate
make tf-plan-contract
make compose-config-prod
helm lint deploy/helm/agentic-ecommerce
go test -race -p 1 -count=1 ./...
govulncheck ./...
cursor-tools docs-check --repo .
runx shell-leak-scan --root . --include-docs
sentrux gate .
git diff --check
```

Gate snapshot so far:

```text
tf-validate: all seven credential-free modules plus aws-ecs and gcp-cloudrun passed
tf-plan-contract: aws-ecs and gcp-cloudrun planned with backend=false, refresh=false, lock=false
compose-config-prod: PASS
helm lint: PASS
full race: PASS under -p 1
coverage: 85.1%
govulncheck: no vulnerabilities found
docs-check: PASS
shell-leak-scan: 148 files scanned, no findings
sentrux: Quality 6041 -> 6037, Coupling 0.04, Cycles 1, God files 0
```
