package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_Fixtures_HappyPath(t *testing.T) {
	t.Parallel()
	fixtures := writeFixtureTree(t)
	out := filepath.Join(t.TempDir(), "report")
	var stdout, stderr bytes.Buffer
	args := []string{
		"--mode=fixtures",
		"--fixtures-dir=" + fixtures,
		"--output-dir=" + out,
	}
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}
	diffPath := filepath.Join(out, "diff.json")
	mdPath := filepath.Join(out, "summary.md")
	mustExist(t, diffPath)
	mustExist(t, mdPath)
	raw, _ := os.ReadFile(diffPath)
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("decode diff.json: %v", err)
	}
	if probe["mode"] != "fixtures" {
		t.Errorf("mode lost: %v", probe["mode"])
	}
	out0 := stdout.String()
	if !strings.Contains(out0, "diff.json") || !strings.Contains(out0, "summary.md") {
		t.Errorf("stdout missing artifact paths: %q", out0)
	}
}

func TestRun_Runtime_RequiresBothDirs(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "report")
	var stdout, stderr bytes.Buffer
	args := []string{
		"--mode=runtime",
		"--playwright-results-dir=/does/not/matter",
		"--output-dir=" + out,
	}
	err := run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing uiauto-results-dir")
	}
	if !strings.Contains(err.Error(), "uiauto-results-dir") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRun_Fixtures_BadMode(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "report")
	var stdout, stderr bytes.Buffer
	args := []string{
		"--mode=bogus",
		"--output-dir=" + out,
	}
	err := run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected mode validation error")
	}
}

func TestRun_RequiresOutputDir(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--mode=fixtures"}, &stdout, &stderr); err == nil {
		t.Fatal("expected --output-dir error")
	}
}

func TestRun_Runtime_NoMappingInfersFromDirs(t *testing.T) {
	t.Parallel()
	fixtures := writeFixtureTree(t)
	out := filepath.Join(t.TempDir(), "rt")
	var stdout, stderr bytes.Buffer
	args := []string{
		"--mode=runtime",
		"--playwright-results-dir=" + filepath.Join(fixtures, "playwright"),
		"--uiauto-results-dir=" + filepath.Join(fixtures, "uiauto"),
		"--output-dir=" + out,
	}
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}
	mustExist(t, filepath.Join(out, "diff.json"))
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func writeFixtureTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pwDir := filepath.Join(dir, "playwright")
	uiDir := filepath.Join(dir, "uiauto")
	for _, d := range []string{pwDir, uiDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(pwDir, "home.json"), `{"suites":[{"file":"e2e/home.spec.ts","specs":[{"title":"home","file":"e2e/home.spec.ts","tests":[{"results":[{"status":"passed","duration":120}]}]}]}]}`)
	mustWrite(t, filepath.Join(uiDir, "home.json"), `{"scenario_id":"home","total_steps":1,"passed_steps":1,"total_latency_ms":350,"tier_breakdown":{"light":1},"steps":[{"step_index":0,"status":"passed","selector":"h1","tier":"light"}]}`)
	mustWrite(t, filepath.Join(dir, "mapping.json"), `[{"spec":"home","scenario":"home"}]`)
	return dir
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
