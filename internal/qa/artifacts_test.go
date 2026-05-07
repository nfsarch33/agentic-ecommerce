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

func TestV200ReleasePerformanceArtifactsCoverFullSurface(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../tests/load/k6/backend-comprehensive.js")
	if err != nil {
		t.Fatalf("read k6 script: %v", err)
	}
	script := string(raw)
	for _, required := range []string{
		"product_catalog",
		"order_creation",
		"ai_generation_mocked",
		"temporal_workflow_start",
		"media_validation",
		"webhook_delivery",
		"/api/v1/media/source",
		"/api/v1/media/process",
		"/api/v1/media/",
		"/api/v1/webhooks",
		"/test",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("k6 release regression script missing %q", required)
		}
	}
}

func TestV200ReleaseDemoRunbookExists(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../docs/v2.0.0-release-demo.md")
	if err != nil {
		t.Fatalf("expected v2.0.0 release demo runbook: %v", err)
	}
	runbook := string(raw)
	for _, required := range []string{
		"storefront browse",
		"cart",
		"checkout",
		"admin login",
		"Temporal",
		"MIS",
		"n8n",
		"mock",
		"expected checkpoints",
	} {
		if !strings.Contains(runbook, required) {
			t.Fatalf("release demo runbook missing %q", required)
		}
	}
}
