package qa

import (
	"os"
	"strings"
	"testing"
)

func TestV900ReleaseMetadataAligned(t *testing.T) {
	t.Parallel()

	assertFileContains(t, "../../VERSION", "9.0.0")
	assertFileContains(t, "../../api/openapi.yaml", "version: 9.0.0")
	assertFileContains(t, "../../api/openapi.yaml", "stable through host v9.x")
	assertFileContains(t, "../../README.md", "Current release: **v9.0.0**")
	assertFileContains(t, "../../CHANGELOG.md", "## [9.0.0] - 2026-05-14 -- v9 platform baseline release")
	assertFileContains(t, "../../docs/release-checklist.md", "# v9.0.0 Release Checklist")
	assertFileContains(t, "../../docs/release-checklist.md", "docs/operations/v9-release-final.md")
	assertFileContains(t, "../../docs/release-checklist.md", "primary-testing and secondary-testing")
	assertFileContains(t, "../../docs/release-checklist.md", "full mirrored self-hosted regression")
	assertFileContains(t, "../../docs/adr/README.md", "ADR-036 | v9.0.0 Release Decisions")
	assertFileContains(t, "../../docs/adr/adr-036-v9-release-decisions.md", "# ADR-036: v9.0.0 Release Decisions")
	assertFileContains(t, "../../docs/operations/v9-release-final.md", "# EC v9.0.0 Release Final Evidence")
	assertFileContains(t, "../../docs/operations/v9-release-final.md", "primary-testing and secondary-testing")
	assertFileContains(t, "../../docs/operations/v9-release-final.md", "UIAuto evidence is mirrored")
	assertFileNotContains(t, "../../docs/release-checklist.md", "EC_STAGING_BASE_URL")
	assertFileNotContains(t, "../../docs/operations/v9-release-final.md", "GKE/GCP as the authoritative staging environment")
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(raw), want) {
		t.Fatalf("%s missing %q", path, want)
	}
}

func assertFileNotContains(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(raw), want) {
		t.Fatalf("%s unexpectedly contains %q", path, want)
	}
}

func TestV800TerraformTargetsSkipWhenTerraformMissing(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(raw)

	for _, target := range []string{"tf-validate", "tf-plan-contract"} {
		recipe := makeTargetRecipe(t, makefile, target)
		if !strings.Contains(recipe, "fi; \\\n\tset -e;") {
			t.Fatalf("%s should keep the terraform-missing guard and terraform loop in one shell", target)
		}
		if strings.Contains(recipe, "\n\t@set -e;") {
			t.Fatalf("%s starts the terraform loop in a second shell after the skip guard", target)
		}
	}
}

func makeTargetRecipe(t *testing.T, makefile, target string) string {
	t.Helper()
	startMarker := target + ":\n"
	start := strings.Index(makefile, startMarker)
	if start == -1 {
		t.Fatalf("Makefile missing target %s", target)
	}
	rest := makefile[start+len(startMarker):]
	end := len(rest)
	for _, marker := range []string{"\n\n", "\n.PHONY"} {
		if idx := strings.Index(rest, marker); idx >= 0 && idx < end {
			end = idx
		}
	}
	return rest[:end]
}
