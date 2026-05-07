package qa

import (
	"os"
	"strings"
	"testing"
)

func TestV180BackendQAPerformanceArtifactsExist(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"../../cmd/mc-api/comprehensive_contract_test.go",
		"../../cmd/mc-api/testdata/contracts/healthz.golden.json",
		"../../tests/load/k6/backend-comprehensive.js",
		"../../scripts/db_performance_audit.sql",
		"../../docs/backend-performance-audit.md",
		"../../docs/backend-qa-performance.md",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected v1.8.0 QA artifact %s: %v", path, err)
		}
	}
}

func TestV180MakefileTargetsExist(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(raw)
	for _, target := range []string{
		"contract-test:",
		"load-test:",
		"db-perf-audit:",
		"docker-image-size:",
		"security-refresh:",
		"qa-v180:",
	} {
		if !strings.Contains(makefile, target) {
			t.Fatalf("Makefile missing target %s", target)
		}
	}
}
