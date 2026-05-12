package terraform_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV740CoreWorkloadsHaveComposeHelmAndCloudContracts(t *testing.T) {
	t.Parallel()

	compose := readRepoFile(t, "docker-compose.yml")
	helmValues := readRepoFile(t, "deploy", "helm", "agentic-ecommerce", "values.yaml")
	awsMain := readTerraformFile(t, "aws-ecs/main.tf")
	gcpMain := readTerraformFile(t, "gcp-cloudrun/main.tf")

	for _, workload := range []struct {
		name          string
		composeName   string
		helmName      string
		terraformName string
	}{
		{name: "mc-api", composeName: "mc-api", helmName: "mc-api", terraformName: "mc_api_service"},
		{name: "wc-sync", composeName: "wc-sync", helmName: "wc-sync", terraformName: "wc_sync_service"},
		{name: "content-worker", composeName: "content-worker", helmName: "content-worker", terraformName: "content_worker_service"},
		{name: "agent-worker", composeName: "agent-worker", helmName: "agent-worker", terraformName: "agent_worker_service"},
		{name: "temporal-worker", composeName: "temporal-worker", helmName: "temporal-worker", terraformName: "temporal_worker_service"},
	} {
		workload := workload
		t.Run(workload.name, func(t *testing.T) {
			t.Parallel()
			assertContains(t, "docker-compose.yml", compose, "\n  "+workload.composeName+":")
			assertContains(t, "deploy/helm/agentic-ecommerce/values.yaml", helmValues, "\n  "+workload.helmName+":")
			assertContains(t, "deploy/terraform/aws-ecs/main.tf", awsMain, `module "`+workload.terraformName+`"`)
			assertContains(t, "deploy/terraform/gcp-cloudrun/main.tf", gcpMain, `module "`+workload.terraformName+`"`)
		})
	}
}

func TestV740TerraformProviderLockPolicyIsDocumented(t *testing.T) {
	t.Parallel()

	readme := readTerraformFile(t, "README.md")
	assertContains(t, "deploy/terraform/README.md", readme, "## Provider Lock Policy")
	for _, root := range []string{"aws-ecs", "gcp-cloudrun", "gke", "eks", "oci", "dr"} {
		assertContains(t, "deploy/terraform/README.md", readme, "`"+root+"`")
	}

	if _, err := os.Stat(filepath.Join("gke", ".terraform.lock.hcl")); err != nil {
		t.Fatalf("gke provider-backed root must keep a provider lock file: %v", err)
	}
	for _, rel := range []string{"aws-ecs/main.tf", "gcp-cloudrun/main.tf"} {
		body := readTerraformFile(t, rel)
		assertContains(t, rel, body, `required_version = ">= 1.6.0"`)
		if strings.Contains(body, `provider "`) {
			t.Fatalf("%s should remain credential-free and providerless until live resources are introduced", rel)
		}
	}
}

func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", ".."}, parts...)...)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}
	return string(body)
}

func assertContains(t *testing.T, name, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("%s missing %q", name, want)
	}
}
