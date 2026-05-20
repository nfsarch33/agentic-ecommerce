package security

import (
	"errors"
	"strings"
)

var (
	ErrScanPathNotFound = errors.New("scanner: path not found")
)

type Vulnerability struct {
	Package  string
	CVE      string
	Severity string
	Fixed    string
}

type SecretFinding struct {
	Path    string
	Line    int
	Pattern string
	Excerpt string
}

type Finding struct {
	Target   string
	Type     string
	Severity string
	Detail   string
}

type ScanReport struct {
	Total    int
	Critical int
	High     int
	Medium   int
	Low      int
}

// knownVulns is a minimal in-process vuln database for testing.
var knownVulns = []Vulnerability{
	{Package: "log4j", CVE: "CVE-2021-44228", Severity: "critical", Fixed: "2.15.0"},
	{Package: "spring-core", CVE: "CVE-2022-22965", Severity: "critical", Fixed: "5.3.18"},
	{Package: "struts", CVE: "CVE-2017-5638", Severity: "critical", Fixed: "2.3.32"},
}

// secretPatterns detects common secret patterns.
var secretPatterns = []struct {
	name    string
	markers []string
}{
	{"api_key", []string{"AKIA", "api_key=", "apikey=", "API_KEY="}},
	{"aws_secret", []string{"aws_secret", "AWS_SECRET_ACCESS_KEY"}},
	{"password", []string{"password=", "passwd=", "secret="}},
	{"github_token", []string{"ghp_", "github_token", "GITHUB_TOKEN"}},
}

// DependencyAudit checks a list of package names against the known vuln database.
func DependencyAudit(_ interface{}, packages []string) ([]Vulnerability, error) {
	var found []Vulnerability
	for _, pkg := range packages {
		for _, v := range knownVulns {
			if strings.EqualFold(pkg, v.Package) {
				found = append(found, v)
			}
		}
	}
	return found, nil
}

// SecretScan checks lines of text (paths -> content) for leaked secrets.
func SecretScan(_ interface{}, sources map[string]string) ([]SecretFinding, error) {
	var findings []SecretFinding
	for path, content := range sources {
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			for _, pat := range secretPatterns {
				for _, marker := range pat.markers {
					if strings.Contains(line, marker) {
						findings = append(findings, SecretFinding{
							Path:    path,
							Line:    i + 1,
							Pattern: pat.name,
							Excerpt: truncate(line, 80),
						})
					}
				}
			}
		}
	}
	return findings, nil
}

// VulnerabilityCheck scans a target string for common vulnerability patterns.
func VulnerabilityCheck(_ interface{}, target string) ([]Finding, error) {
	if target == "" {
		return nil, ErrScanPathNotFound
	}
	var findings []Finding
	// SQL injection patterns
	sqli := []string{"'--", "' OR '1'='1", "UNION SELECT", "DROP TABLE"}
	for _, pattern := range sqli {
		if strings.Contains(strings.ToUpper(target), strings.ToUpper(pattern)) {
			findings = append(findings, Finding{
				Target:   target,
				Type:     "sqli",
				Severity: "high",
				Detail:   "potential SQL injection: " + pattern,
			})
		}
	}
	// XSS patterns
	xss := []string{"<script>", "javascript:", "onerror="}
	for _, pattern := range xss {
		if strings.Contains(strings.ToLower(target), pattern) {
			findings = append(findings, Finding{
				Target:   target,
				Type:     "xss",
				Severity: "medium",
				Detail:   "potential XSS: " + pattern,
			})
		}
	}
	return findings, nil
}

// Report aggregates findings by severity.
func Report(findings []Finding) ScanReport {
	r := ScanReport{Total: len(findings)}
	for _, f := range findings {
		switch strings.ToLower(f.Severity) {
		case "critical":
			r.Critical++
		case "high":
			r.High++
		case "medium":
			r.Medium++
		case "low":
			r.Low++
		}
	}
	return r
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
