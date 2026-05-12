package terraform_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV741MakefileDefinesCredentialFreePlanMatrix(t *testing.T) {
	t.Parallel()

	body := readRepoFileV741(t, "Makefile")
	for _, want := range []string{
		"TF_PLAN_DIRS :=",
		"$(TF_DIR)/aws-ecs",
		"$(TF_DIR)/gcp-cloudrun",
		"tf-plan-contract:",
		"terraform -chdir=$$dir init -backend=false -input=false",
		"terraform -chdir=$$dir plan -refresh=false -lock=false -input=false -no-color",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Makefile missing credential-free plan matrix marker %q", want)
		}
	}
}

func TestV741MakefileValidateDirsCoverCredentialFreeModules(t *testing.T) {
	t.Parallel()

	body := readRepoFileV741(t, "Makefile")
	for _, want := range []string{
		"$(TF_DIR)/modules/network",
		"$(TF_DIR)/modules/objectstore",
		"$(TF_DIR)/modules/postgres",
		"$(TF_DIR)/modules/redis",
		"$(TF_DIR)/modules/service",
		"$(TF_DIR)/modules/container_cluster",
		"$(TF_DIR)/modules/tenant_provisioning",
		"$(TF_DIR)/aws-ecs",
		"$(TF_DIR)/gcp-cloudrun",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Makefile TF_VALIDATE_DIRS missing credential-free root %q", want)
		}
	}
	for _, liveRoot := range []string{"$(TF_DIR)/gke", "$(TF_DIR)/eks", "$(TF_DIR)/oci", "$(TF_DIR)/dr"} {
		if strings.Contains(body, liveRoot) {
			t.Fatalf("Makefile TF_VALIDATE_DIRS should not include live-provider root %q", liveRoot)
		}
	}
}

func TestV741CloudDeployabilityQADocumentsMatrixAndRollback(t *testing.T) {
	t.Parallel()

	body := readRepoFileV741(t, "docs", "operations", "v741-cloud-deployability-qa.md")
	for _, want := range []string{
		"# v7.4.1 Cloud Deployability QA",
		"## Credential-Free Validation Matrix",
		"`make tf-validate`",
		"`make tf-plan-contract`",
		"`deploy/terraform/aws-ecs`",
		"`deploy/terraform/gcp-cloudrun`",
		"## Rollback Boundary",
		"Helm rollback does not revert Terraform state",
		"live provider roots stay operator-gated",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("v741 QA doc missing %q", want)
		}
	}
}

func TestV741TerraformReadmeDocumentsPlanValidateMatrix(t *testing.T) {
	t.Parallel()

	body := readTerraformFile(t, "README.md")
	for _, want := range []string{
		"## Provider Lock Policy",
		"### Credential-Free Validation Matrix",
		"`modules/container_cluster`",
		"`modules/tenant_provisioning`",
		"`make tf-validate`",
		"`make tf-plan-contract`",
		"Operator-gated only",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("deploy/terraform/README.md missing %q", want)
		}
	}
}

func TestV741DeploymentRunbookTightensInfrastructureRollback(t *testing.T) {
	t.Parallel()

	body := readRepoFileV741(t, "docs", "operations", "deployment-runbook.md")
	for _, want := range []string{
		"targeted apply is an exception path",
		"terraform plan must be reviewed before any infrastructure rollback",
		"Do not use `terraform apply -target` as the default rollback path",
		"re-run `make tf-plan-contract`",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("deployment runbook rollback section missing %q", want)
		}
	}
}

func readRepoFileV741(t *testing.T, parts ...string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}
	return string(body)
}
