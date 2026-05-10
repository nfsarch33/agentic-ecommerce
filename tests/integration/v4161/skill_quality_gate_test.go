//go:build v4161_smoke

package v4161_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E_QualityGatePassesKnownGoodSkill validates that a well-formed
// SKILL.md passes all quality checks when run through ec-cli.
func TestE2E_QualityGatePassesKnownGoodSkill(t *testing.T) {
	skillPath := writeTestSkill(t, "good-skill", `# Good Skill

Comprehensive description for routing under 200 chars.

## Triggers

Use when: building APIs, writing Go code, reviewing PRs, running tests

## Instructions

Follow the standard patterns.
`)
	output, exitCode := runEcCLI(t, "skill", "quality-check", skillPath)
	if exitCode != 0 {
		t.Fatalf("expected exit 0 for good skill, got %d; output:\n%s", exitCode, output)
	}
	if !strings.Contains(output, "PASS") {
		t.Fatalf("expected PASS in output, got:\n%s", output)
	}
	failCount := strings.Count(output, "[FAIL]")
	if failCount > 0 {
		t.Fatalf("expected zero FAIL checks, got %d; output:\n%s", failCount, output)
	}
}

// TestE2E_QualityGateReportsSpecificFailures validates that a deliberately
// broken SKILL.md produces specific per-check failure messages.
func TestE2E_QualityGateReportsSpecificFailures(t *testing.T) {
	skillPath := writeTestSkill(t, "bad-skill", `## No H1 Heading

This file has no proper title so description extraction fails.
Also no trigger section at all.
Connect to 192.168.1.50 for the server.
`)
	output, exitCode := runEcCLI(t, "skill", "quality-check", skillPath)
	if exitCode != 1 {
		t.Fatalf("expected exit 1 for bad skill, got %d; output:\n%s", exitCode, output)
	}

	expectedFailures := []string{"description", "triggers", "hostnames"}
	for _, expected := range expectedFailures {
		if !strings.Contains(output, expected) {
			t.Errorf("expected %q failure in output, not found; output:\n%s", expected, output)
		}
	}
}

// TestE2E_CodexGenOutputValidatesAgainstQualityGate verifies that running
// codex-gen on a Cursor skill produces output that itself passes the
// quality gate (round-trip validation).
func TestE2E_CodexGenOutputValidatesAgainstQualityGate(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "source-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(`# Source Skill

A well-described source skill for Codex conversion.

## Triggers

Use when: writing Go code, building services, reviewing architecture, deploying apps

## Instructions

Use the Task tool for delegation.
Reference .cursor/rules/plan-sync.mdc for guidance.
Standard implementation patterns apply.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	genOutput, genExit := runEcCLI(t, "skill", "codex-gen", skillPath, outDir)
	if genExit != 0 {
		t.Fatalf("codex-gen failed with exit %d; output:\n%s", genExit, genOutput)
	}

	codexPath := filepath.Join(outDir, "source-skill.codex.md")
	if _, err := os.Stat(codexPath); err != nil {
		t.Fatalf("codex output not found at %s: %v", codexPath, err)
	}

	qcOutput, qcExit := runEcCLI(t, "skill", "quality-check", codexPath)
	if qcExit != 0 {
		t.Fatalf("quality-check on codex output failed with exit %d; output:\n%s", qcExit, qcOutput)
	}
}

// --- Helpers ---

func writeTestSkill(t *testing.T, name, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runEcCLI(t *testing.T, args ...string) (string, int) {
	t.Helper()
	binary := buildEcCLI(t)
	cmd := exec.CommandContext(context.Background(), binary, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("exec error (not exit): %v", err)
		}
	}
	return out.String(), exitCode
}

var cachedBinary string

func buildEcCLI(t *testing.T) string {
	t.Helper()
	if cachedBinary != "" {
		return cachedBinary
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "ec-cli")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/ec-cli")
	cmd.Dir = findRepoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build ec-cli: %v\n%s", err, out)
	}
	cachedBinary = binary
	return binary
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}
