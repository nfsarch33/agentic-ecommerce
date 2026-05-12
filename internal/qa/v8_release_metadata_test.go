package qa

import (
	"os"
	"strings"
	"testing"
)

func TestV800ReleaseMetadataAligned(t *testing.T) {
	t.Parallel()

	assertFileContains(t, "../../VERSION", "8.0.0")
	assertFileContains(t, "../../api/openapi.yaml", "version: 8.0.0")
	assertFileContains(t, "../../api/openapi.yaml", "stable through host v8.x")
	assertFileContains(t, "../../README.md", "Current release: **v8.0.0**")
	assertFileContains(t, "../../CHANGELOG.md", "## [8.0.0] - 2026-05-13 -- v8 TDD implementation release")
	assertFileContains(t, "../../docs/release-checklist.md", "# v8.0.0 Release Checklist")
	assertFileContains(t, "../../docs/release-checklist.md", "docs/operations/v8-release-final.md")
	assertFileContains(t, "../../docs/adr/README.md", "ADR-035 | v8.0.0 Release Decisions")
	assertFileContains(t, "../../docs/adr/adr-035-v8-release-decisions.md", "# ADR-035: v8.0.0 Release Decisions")
	assertFileContains(t, "../../docs/operations/v8-release-final.md", "# EC v8.0.0 Release Final Evidence")
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
