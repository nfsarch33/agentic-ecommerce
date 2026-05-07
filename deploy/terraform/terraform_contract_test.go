package terraform_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCloudTerraformExamplesUsePublicSafePlaceholders(t *testing.T) {
	t.Parallel()

	for _, rel := range []string{
		"aws-ecs/terraform.tfvars.example",
		"gcp-cloudrun/terraform.tfvars.example",
	} {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			body := readTerraformFile(t, rel)

			for _, pattern := range []string{
				`\b\d{12}\b`,
				`AKIA[0-9A-Z]{16}`,
				`AIza[0-9A-Za-z_-]{35}`,
				`-----BEGIN [A-Z ]+PRIVATE KEY-----`,
				`(?i)(password|secret|token)\s*=\s*"[^"]*(real|prod|live|actual)[^"]*"`,
				`\b10\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`,
				`\b172\.(1[6-9]|2\d|3[0-1])\.\d{1,3}\.\d{1,3}\b`,
				`\b192\.168\.\d{1,3}\.\d{1,3}\b`,
			} {
				if regexp.MustCompile(pattern).MatchString(body) {
					t.Fatalf("%s contains non-placeholder-looking value matching %q", rel, pattern)
				}
			}

			for _, want := range []string{
				`allowed_origin`,
				`media_public_base_url`,
				`temporal_task_queue`,
				`sync_dry_run`,
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing expected placeholder %q", rel, want)
				}
			}
		})
	}
}

func TestCloudTerraformContractsCoverV17HardeningScope(t *testing.T) {
	t.Parallel()

	required := map[string][]string{
		"aws-ecs/main.tf": {
			`module "temporal_server_service"`,
			`module "temporal_worker_service"`,
			`module "media_store"`,
			`ECOMMERCE_MEDIA_PUBLIC_BASE_URL`,
			`aws-secretsmanager:`,
			`health_check_path    = "/readyz"`,
			`local.mc_api_autoscaling_policy`,
			`local.temporal_worker_autoscaling_policy`,
			`allow_public_ingress = false`,
		},
		"gcp-cloudrun/main.tf": {
			`module "temporal_server_service"`,
			`module "temporal_worker_service"`,
			`module "media_store"`,
			`ECOMMERCE_MEDIA_PUBLIC_BASE_URL`,
			`gcp-secret-manager:`,
			`health_check_path       = "/readyz"`,
			`local.mc_api_autoscaling_policy`,
			`local.temporal_worker_autoscaling_policy`,
			`allow_public_ingress    = false`,
		},
		"modules/objectstore/main.tf": {
			`provider_label`,
			`CloudFront`,
			`Cloud CDN`,
			`origin_access_required = true`,
			`viewer_protocol_policy`,
			`public access blocked`,
		},
		"modules/objectstore/variables.tf": {
			`cdn_allowed_methods`,
			`["GET", "HEAD", "OPTIONS"]`,
			`cdn_viewer_protocol_policy`,
			`redirect-to-https`,
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
					t.Fatalf("%s missing v1.7.0 cloud contract marker %q", rel, needle)
				}
			}
		})
	}
}

func readTerraformFile(t *testing.T, rel string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(".", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(body)
}
