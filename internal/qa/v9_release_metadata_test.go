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
	assertFileContains(t, "../../docs/adr/README.md", "ADR-036 | v9.0.0 Release Decisions")
	assertFileContains(t, "../../docs/adr/adr-036-v9-release-decisions.md", "# ADR-036: v9.0.0 Release Decisions")
	assertFileContainsAll(t, "../../docs/release-checklist.md",
		"# v9.0.0 Release Checklist",
		"docs/operations/v9-release-final.md",
		"primary-testing",
		"only blocking release lane",
	)
	assertFileNotContainsAll(t, "../../docs/release-checklist.md",
		"EC_STAGING_BASE_URL",
		"primary-testing and secondary-testing",
		"full mirrored self-hosted regression",
	)
	assertFileContainsAll(t, "../../docs/operations/v9-release-final.md",
		"# EC v9.0.0 Release Final Evidence",
		"primary-testing",
		"only release blocker",
	)
	assertFileNotContainsAll(t, "../../docs/operations/v9-release-final.md",
		"GKE/GCP as the authoritative staging environment",
		"primary-testing and secondary-testing",
		"UIAuto evidence is mirrored",
	)
}

func TestV900ReleaseMetadataUsesSemverOnlyGateLanguage(t *testing.T) {
	t.Parallel()

	releaseFacingFiles := []string{
		"../../README.md",
		"../../docs/release-checklist.md",
		"../../docs/operations/v9-release-final.md",
		"../../docs/adr/adr-036-v9-release-decisions.md",
	}

	for _, path := range releaseFacingFiles {
		assertFileNotContainsAll(t, path,
			"RC-only",
			"Release candidate note",
			"semver tags still stop at `v8.0.0`",
			"Active v8.x CI",
			"v9.0.0-rc",
		)
	}
}

func TestV900ReleaseMetadataPinsExactPromotionInputs(t *testing.T) {
	t.Parallel()

	assertFileContainsAll(t, "../../docs/operations/v9-release-final.md",
		"**Head**: 17eab44dfdfc807e34fbf4ee05b6bd9f2322e41c",
		"Frontend SHA: `7682367be3b5af728649a9730a625aa54c46d87b`",
		"OpenAPI contract path: `api/openapi.yaml`",
		"Current blockers:",
	)
	assertFileNotContains(t, "../../docs/operations/v9-release-final.md", "pending final v9 release merge commit")
}

func TestV900VersionPolicySurfacesAlignWithV9Freeze(t *testing.T) {
	t.Parallel()

	assertFileNotContainsAll(t, "../../internal/api/version.go",
		"v2.9.0",
		"stable through host v3.x",
	)
	assertFileNotContainsAll(t, "../../api/openapi-v2-preview.yaml",
		"version: 2.9.0-preview",
		"stable through host v3.x",
	)
	assertFileNotContainsAll(t, "../../docs/api-versioning.md",
		"stable as of v2.9.0",
		"host **v3.x**",
	)
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

func assertFileContainsAll(t *testing.T, path string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		assertFileContains(t, path, want)
	}
}

func assertFileNotContainsAll(t *testing.T, path string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		assertFileNotContains(t, path, want)
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
