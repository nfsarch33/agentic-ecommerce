# Container Cluster Module

Cloud-agnostic Kubernetes cluster abstraction for the Agentic E-Commerce stack.

## Supported Providers

| Provider | Service | Notes |
|----------|---------|-------|
| `gcp` | GKE Autopilot | Autopilot by default; node pool settings ignored |
| `aws` | EKS | Managed node group with autoscaling |

## Usage

```hcl
module "cluster" {
  source = "../modules/container_cluster"

  provider_name    = "gcp"
  name_prefix      = "ec"
  environment      = "prod"
  cluster_version  = "1.30"
  enable_autopilot = true
  network_id       = module.network.network_id
}
```

## Inputs

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `provider_name` | string | — | `aws` or `gcp` |
| `name_prefix` | string | — | Resource name prefix |
| `environment` | string | `dev` | Environment label |
| `cluster_version` | string | `1.30` | Kubernetes version |
| `node_count` | number | `3` | Desired nodes (ignored for Autopilot) |
| `machine_type` | string | `placeholder-standard` | Instance type |
| `enable_autopilot` | bool | `true` | GKE Autopilot mode |
| `network_id` | string | — | VPC/network from network module |

## Outputs

| Name | Description |
|------|-------------|
| `cluster_name` | Derived cluster name |
| `cluster_endpoint` | Placeholder API endpoint |
| `kubeconfig_command` | CLI command to get kubeconfig |
| `cluster_contract` | Full contract object |
