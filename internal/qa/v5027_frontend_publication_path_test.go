package qa

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func webRepoPath(t *testing.T, rel string) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	return filepath.Join(home, "Code", "personal", "agentic-ecommerce-web", rel)
}

// TestV5027FrontendVersionIs9 verifies the canonical frontend package.json
// version is exactly 9.0.0 before the v9.0.0 semver tag is cut.
func TestV5027FrontendVersionIs9(t *testing.T) {
	t.Parallel()

	path := webRepoPath(t, "package.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("frontend repo not available at expected path: %v", err)
	}
	if !strings.Contains(string(raw), `"version": "9.0.0"`) {
		t.Errorf("frontend package.json version is not 9.0.0:\n%s", raw[:min(200, len(raw))])
	}
}

// TestV5027FrontendChangelogDocumentsV9 verifies the frontend CHANGELOG has
// the v9.0.0 release entry.
func TestV5027FrontendChangelogDocumentsV9(t *testing.T) {
	t.Parallel()

	assertFileContainsAll(t, webRepoPath(t, "CHANGELOG.md"),
		"[9.0.0]",
		"v9.0.0",
	)
}

// TestV5027FrontendReleaseChecklistExists verifies the v9 frontend release
// checklist artefact is present and gates the semver tag.
func TestV5027FrontendReleaseChecklistExists(t *testing.T) {
	t.Parallel()

	assertFileContainsAll(t,
		webRepoPath(t, "docs/v9-frontend-release-checklist.md"),
		"9.0.0",
		"semver",
		"bun run test",
		"bun run build",
	)
}

// TestV5027FrontendReleaseFinalDocumentsCrossStackEvidence verifies the v9
// frontend release final evidence doc records cross-stack gate outcomes.
func TestV5027FrontendReleaseFinalDocumentsCrossStackEvidence(t *testing.T) {
	t.Parallel()

	assertFileContainsAll(t,
		webRepoPath(t, "docs/v9-frontend-release-final.md"),
		"9.0.0",
		"backend",
		"primary-testing",
	)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
