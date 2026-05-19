package security_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/security"
)

func TestScanner_DependencyAuditFindsKnownVuln(t *testing.T) {
	t.Parallel()
	vulns, err := security.DependencyAudit(nil, []string{"log4j", "spring-core", "safe-lib"})
	if err != nil {
		t.Fatalf("dependency audit failed: %v", err)
	}
	if len(vulns) < 2 {
		t.Fatalf("expected 2+ vulnerabilities, got %d", len(vulns))
	}
}

func TestScanner_SecretScanDetectsAPIKey(t *testing.T) {
	t.Parallel()
	sources := map[string]string{
		"config.yaml": "api_key=AKIAIOSFODNN7EXAMPLE\nother=value",
	}
	findings, err := security.SecretScan(nil, sources)
	if err != nil {
		t.Fatalf("secret scan failed: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected API key secret finding")
	}
	if findings[0].Path != "config.yaml" {
		t.Fatalf("expected path config.yaml, got %s", findings[0].Path)
	}
}

func TestScanner_VulnerabilityCheckIdentifiesSQLi(t *testing.T) {
	t.Parallel()
	findings, err := security.VulnerabilityCheck(nil, "SELECT * FROM users WHERE id='' OR '1'='1'")
	if err != nil {
		t.Fatalf("vuln check failed: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected SQLi finding")
	}
	if findings[0].Type != "sqli" {
		t.Fatalf("expected sqli finding type, got %s", findings[0].Type)
	}
}

func TestScanner_ReportGroupsBySeverity(t *testing.T) {
	t.Parallel()
	findings := []security.Finding{
		{Severity: "high"},
		{Severity: "medium"},
		{Severity: "high"},
		{Severity: "critical"},
	}
	report := security.Report(findings)
	if report.High != 2 {
		t.Fatalf("expected 2 high findings, got %d", report.High)
	}
	if report.Critical != 1 {
		t.Fatalf("expected 1 critical, got %d", report.Critical)
	}
	if report.Total != 4 {
		t.Fatalf("expected total 4, got %d", report.Total)
	}
}

func TestScanner_CleanScanReturnsEmpty(t *testing.T) {
	t.Parallel()
	vulns, _ := security.DependencyAudit(nil, []string{"safe-lib", "another-safe"})
	if len(vulns) != 0 {
		t.Fatalf("expected 0 vulnerabilities for clean packages, got %d", len(vulns))
	}
}

func TestScanner_ScanNonExistentPathError(t *testing.T) {
	t.Parallel()
	_, err := security.VulnerabilityCheck(nil, "")
	if err != security.ErrScanPathNotFound {
		t.Fatalf("expected ErrScanPathNotFound, got %v", err)
	}
}
