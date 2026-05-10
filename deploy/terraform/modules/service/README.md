# Service Module

Cloud-agnostic container service abstraction (Cloud Run / ECS Fargate).

## Supported Runtimes

| Runtime | Provider | Notes |
|---------|----------|-------|
| `cloud-run` | GCP | Serverless with VPC connector |
| `ecs-fargate` | AWS | Fargate tasks with ALB |

## Usage

```hcl
module "mc_api" {
  source = "../modules/service"

  provider_name  = "gcp"
  runtime_target = "cloud-run"
  name_prefix    = "ec"
  environment    = "prod"
  service_name   = "mc-api"
  image          = "REGISTRY_URL/agentic-ecommerce"
  image_tag      = "abc1234"
  container_port = 8080
  protocol       = "http"
}
```

## Inputs

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `provider_name` | string | — | `aws` or `gcp` |
| `runtime_target` | string | — | `ecs-fargate` or `cloud-run` |
| `service_name` | string | — | Logical service name |
| `image` | string | — | Container image without tag |
| `image_tag` | string | — | Immutable image tag |
| `cpu` | number | `512` | CPU units/vCPU |
| `memory_mb` | number | `1024` | Memory limit in MiB |
| `min_instances` | number | `1` | Min replicas |
| `max_instances` | number | `3` | Max replicas |

## Outputs

| Name | Description |
|------|-------------|
| `service_name` | Derived service name |
| `image_ref` | Full image:tag reference |
| `deployment_contract` | Normalized contract object |
