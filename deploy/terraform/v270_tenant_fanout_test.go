package terraform_test

import (
	"strings"
	"testing"
)

// TestV270TenantFanoutContractIsWired verifies that the v2.7.0
// tenant_provisioning module is reachable from both the AWS ECS and
// GCP Cloud Run roots. The contract test reads the terraform sources
// without invoking `terraform init` so the assertion stays hermetic
// and fast in CI, mirroring the v1.7.0 pattern that already lives in
// terraform_contract_test.go.
func TestV270TenantFanoutContractIsWired(t *testing.T) {
	t.Parallel()

	required := map[string][]string{
		"aws-ecs/main.tf": {
			`module "tenant_fanout"`,
			`source = "../modules/tenant_provisioning"`,
			`tenants            = var.tenants`,
			`tenant_secret_path_prefix`,
			`local.agent_worker_autoscaling_policy`,
			`AgentRunsPending`,
			`TemporalQueueDepth`,
			`target_value               = 70`,
		},
		"gcp-cloudrun/main.tf": {
			`module "tenant_fanout"`,
			`source = "../modules/tenant_provisioning"`,
			`tenants            = var.tenants`,
			`tenant_secret_path_prefix`,
			`local.agent_worker_autoscaling_policy`,
			`agent_runs_pending`,
			`temporal_queue_depth`,
			`target_value               = 70`,
		},
		"aws-ecs/outputs.tf": {
			`tenant_contracts`,
			`module.tenant_fanout.deployment_contract`,
			`module.tenant_fanout.tenant_contracts`,
		},
		"gcp-cloudrun/outputs.tf": {
			`tenant_contracts`,
			`module.tenant_fanout.deployment_contract`,
			`module.tenant_fanout.tenant_contracts`,
		},
		"aws-ecs/variables.tf": {
			`variable "tenants"`,
			`variable "tenant_secret_path_prefix"`,
			`variable "agent_worker_min_instances"`,
			`variable "agent_worker_max_instances"`,
			`default     = 20`,
			`default     = 15`,
			`default     = 10`,
		},
		"gcp-cloudrun/variables.tf": {
			`variable "tenants"`,
			`variable "tenant_secret_path_prefix"`,
			`variable "agent_worker_min_instances"`,
			`variable "agent_worker_max_instances"`,
		},
		"modules/tenant_provisioning/main.tf": {
			`tenant_contracts`,
			`billing_webhook`,
			`cdn`,
			`secrets`,
			`auto_scaling`,
			`tenant_count`,
			`provider_label`,
			`CloudFront`,
			`Cloud CDN`,
		},
		"modules/tenant_provisioning/variables.tf": {
			`variable "tenants"`,
			`variable "auto_scaling_targets"`,
			`target_cpu_percent`,
			`queue_metric`,
			`max_replicas       = 20`,
			`max_replicas = 10`,
			`max_replicas = 15`,
		},
		"modules/tenant_provisioning/outputs.tf": {
			`output "tenant_contracts"`,
			`output "tenant_ids"`,
			`output "deployment_contract"`,
		},
	}

	for rel, needles := range required {
		rel := rel
		needles := needles
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			body := readTerraformFile(t, rel)
			for _, needle := range needles {
				if !strings.Contains(body, needle) {
					t.Fatalf("%s missing v2.7.0 marker %q", rel, needle)
				}
			}
		})
	}
}

// TestV270TenantFanoutBlocksLeakedIPs ensures the new module does
// not embed any private LAN or Tailscale IP literals when authors
// add example tenants in tfvars.
func TestV270TenantFanoutBlocksLeakedIPs(t *testing.T) {
	t.Parallel()
	body := readTerraformFile(t, "modules/tenant_provisioning/main.tf")
	for _, frag := range []string{"100.", "10.0.5.", "192.168.", "172.16.", "ocid1."} {
		if strings.Contains(body, frag) {
			t.Fatalf("tenant_provisioning module embeds prohibited literal %q", frag)
		}
	}
}
