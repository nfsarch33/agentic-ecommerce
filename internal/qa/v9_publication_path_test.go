package qa

import (
	"os"
	"strings"
	"testing"
)

// TestV5026BackendPublicationPathGates verifies all evidence required before
// the v9.0.0 semver tag can be cut. The tag gate requires:
//   - release-final evidence doc updated past the v5025 pre-release freeze
//   - release checklist referencing the correct primary-testing pool commands
//   - all backend release gate Makefile targets present
//   - ADR-036 documents the primary-testing contract
//   - VERSION file is exactly 9.0.0
func TestV5026BackendPublicationPathGates(t *testing.T) {
	t.Parallel()

	// VERSION must be exactly 9.0.0 -- no rc suffix, no pre suffix
	raw, err := os.ReadFile("../../VERSION")
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	version := strings.TrimSpace(string(raw))
	if version != "9.0.0" {
		t.Fatalf("VERSION must be exactly 9.0.0 for tag cut, got %q", version)
	}
}

// TestV5026ReleaseFinalEvidenceReflectsV5025Merge verifies the release-final
// evidence doc has been updated to reference the merged v5025 HEAD SHA.
// v5025 merged at ef15859bf46d73029fac404b83b05c7c0cf7b9de (PR #194).
func TestV5026ReleaseFinalEvidenceReflectsV5025Merge(t *testing.T) {
	t.Parallel()

	assertFileContainsAll(t, "../../docs/operations/v9-release-final.md",
		"ef15859bf46d73029fac404b83b05c7c0cf7b9de",
		"v5025 pre-release QA merged",
		"primary-testing",
	)
}

// TestV5026ReleaseChecklistHasAllGateTargets verifies that every backend
// release gate command from the publication runbook is documented in the
// release checklist. These are the blocking gates before tag cut.
func TestV5026ReleaseChecklistHasAllGateTargets(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../docs/release-checklist.md")
	if err != nil {
		t.Fatalf("read release-checklist.md: %v", err)
	}
	checklist := string(raw)

	required := []string{
		"race",
		"coverage-check",
		"govulncheck-scan",
		"contract-test",
		"sentrux",
		"primary-testing",
		"backend-integration",
		"full-stack-e2e",
		"cleanup-testing",
		"node-a-travel",
		"host-a-travel",
	}
	for _, term := range required {
		if !strings.Contains(checklist, term) {
			t.Errorf("release-checklist.md missing required gate term %q", term)
		}
	}
}

// TestV5026MakefileHasStabilityTargets ensures the backend Makefile exposes
// all stability-critical targets added across the v9 sprint programme.
func TestV5026MakefileHasStabilityTargets(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(raw)

	targets := []string{
		"test:",
		"coverage-check:",
		"govulncheck-scan:",
		"contract-test:",
		"sentrux-gate:",
		"shell-leak:",
		"build:",
	}
	for _, target := range targets {
		if !strings.Contains(makefile, target) {
			t.Errorf("Makefile missing stability target %q", target)
		}
	}
}

// TestV5026ADR036DocumentsPrimaryTestingContract verifies that ADR-036
// records the primary-testing-only contract for v9.0.0.
func TestV5026ADR036DocumentsPrimaryTestingContract(t *testing.T) {
	t.Parallel()

	assertFileContainsAll(t, "../../docs/adr/adr-036-v9-release-decisions.md",
		"primary-testing",
		"semver",
		"host-a",
		"node-a",
	)
}

// TestV5026PublicationPathRunbookExists verifies the v5026 publication path
// runbook exists with the correct gate commands.
func TestV5026PublicationPathRunbookExists(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../docs/operations/v9-publication-path.md")
	if err != nil {
		t.Fatalf("v9-publication-path.md not found -- create it as part of v5026: %v", err)
	}
	doc := string(raw)

	required := []string{
		"v9.0.0",
		"primary-testing",
		"backend-integration",
		"full-stack-e2e",
		"ef15859",
		"semver tag",
	}
	for _, term := range required {
		if !strings.Contains(doc, term) {
			t.Errorf("v9-publication-path.md missing %q", term)
		}
	}
}
