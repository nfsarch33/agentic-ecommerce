package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Story 2: Skill Quality Gate (5 RED scenarios) ---

func TestSkillQualityCheck_AllPass(t *testing.T) {
	t.Parallel()
	skill := createTestSkillFile(t, `# My Skill

A short description for routing.

## Triggers

Use when: writing tests, reviewing code, debugging issues, running CI

## Instructions

Do the thing.
`)
	deps, stdout, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "skill", "quality-check", skill}, deps)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d; output=%q", exit, stdout.String())
	}
	if !strings.Contains(stdout.String(), "[PASS]") {
		t.Fatalf("expected PASS markers, got %q", stdout.String())
	}
}

func TestSkillQualityCheck_DescriptionMissing(t *testing.T) {
	t.Parallel()
	skill := createTestSkillFile(t, `## Triggers

Use when: writing tests, reviewing code, debugging issues
`)
	deps, stdout, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "skill", "quality-check", skill}, deps)
	if exit != 1 {
		t.Fatalf("expected exit 1 for missing description, got %d", exit)
	}
	if !strings.Contains(stdout.String(), "description") && !strings.Contains(stdout.String(), "FAIL") {
		t.Fatalf("expected description failure, got %q", stdout.String())
	}
}

func TestSkillQualityCheck_TriggersMissing(t *testing.T) {
	t.Parallel()
	skill := createTestSkillFile(t, `# My Skill

A short description for routing.

## Instructions

Do the thing without any trigger section.
`)
	deps, stdout, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "skill", "quality-check", skill}, deps)
	if exit != 1 {
		t.Fatalf("expected exit 1 for missing triggers, got %d", exit)
	}
	if !strings.Contains(stdout.String(), "trigger") {
		t.Fatalf("expected trigger failure, got %q", stdout.String())
	}
}

func TestSkillQualityCheck_FileTooLarge(t *testing.T) {
	t.Parallel()
	content := "# Big Skill\n\nA description.\n\n## Triggers\n\nUse when: a, b, c, d\n\n"
	content += strings.Repeat("x", 60*1024)
	skill := createTestSkillFile(t, content)
	deps, stdout, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "skill", "quality-check", skill}, deps)
	if exit != 1 {
		t.Fatalf("expected exit 1 for oversized file, got %d", exit)
	}
	if !strings.Contains(stdout.String(), "file_size") || !strings.Contains(stdout.String(), "FAIL") {
		t.Fatalf("expected file_size FAIL, got %q", stdout.String())
	}
}

func TestSkillQualityCheck_InfrastructureHostname(t *testing.T) {
	t.Parallel()
	skill := createTestSkillFile(t, `# My Skill

A short description.

## Triggers

Use when: deploying to gpu-host-1, configuring private-lab, setting up windows-host-1

## Instructions

Connect to 192.168.1.100 for the service.
`)
	deps, stdout, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "skill", "quality-check", skill}, deps)
	if exit != 1 {
		t.Fatalf("expected exit 1 for hostname leak, got %d", exit)
	}
	if !strings.Contains(stdout.String(), "hostnames") || !strings.Contains(stdout.String(), "FAIL") {
		t.Fatalf("expected hostnames FAIL, got %q", stdout.String())
	}
}

// --- Story 3: Codex Generator (4 RED scenarios) ---

func TestSkillCodexGen_SuccessfulConversion(t *testing.T) {
	t.Parallel()
	skillDir := filepath.Join(t.TempDir(), "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(`# My Cool Skill

A description of what this does.

## Triggers

Use when: building APIs, writing tests, reviewing PRs

## Instructions

Use the Task tool to delegate work.
Use TodoWrite to track progress.
Check .cursor/rules/plan-sync.mdc for guidance.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	deps, stdout, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "skill", "codex-gen", skillPath, outDir}, deps)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d; output=%q", exit, stdout.String())
	}
	outPath := filepath.Join(outDir, "my-skill.codex.md")
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if !strings.Contains(string(content), "# Codex Agent Skill: My Cool Skill") {
		t.Fatalf("expected Codex header, got %q", string(content)[:100])
	}
}

func TestSkillCodexGen_PreserveTriggers(t *testing.T) {
	t.Parallel()
	skillDir := filepath.Join(t.TempDir(), "trigger-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(`# Trigger Skill

Skill with triggers.

## Triggers

- building APIs
- writing tests
- reviewing PRs
- deploying services

## Instructions

Normal instructions here.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	deps, _, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "skill", "codex-gen", skillPath, outDir}, deps)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	outPath := filepath.Join(outDir, "trigger-skill.codex.md")
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{"building APIs", "writing tests", "reviewing PRs"} {
		if !strings.Contains(string(content), trigger) {
			t.Fatalf("trigger %q not preserved in output", trigger)
		}
	}
}

func TestSkillCodexGen_StripCursorRefs(t *testing.T) {
	t.Parallel()
	skillDir := filepath.Join(t.TempDir(), "cursor-refs")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(`# Cursor Skill

Description here.

## Triggers

Use when: doing things, more things, other things

## Instructions

Use the Task tool to delegate.
Use TodoWrite to track.
Reference .cursor/rules/plan-sync.mdc for policy.
Reference .cursor/rules/memory.mdc for storage.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	deps, _, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "skill", "codex-gen", skillPath, outDir}, deps)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	outPath := filepath.Join(outDir, "cursor-refs.codex.md")
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if strings.Contains(text, "Task tool") {
		t.Fatal("Task tool reference should be stripped")
	}
	if strings.Contains(text, "TodoWrite") {
		t.Fatal("TodoWrite reference should be stripped")
	}
	if strings.Contains(text, ".mdc") {
		t.Fatal(".mdc references should be converted")
	}
	if !strings.Contains(text, "AGENTS.md section: plan-sync") {
		t.Fatal("expected AGENTS.md section reference for plan-sync")
	}
}

func TestSkillCodexGen_HandleMissingSections(t *testing.T) {
	t.Parallel()
	skillDir := filepath.Join(t.TempDir(), "minimal")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(`Just some text without any headings or structure.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	deps, stdout, _ := newTestDeps(nil)
	exit := runApp(context.Background(), []string{"ec-cli", "skill", "codex-gen", skillPath, outDir}, deps)
	if exit != 0 {
		t.Fatalf("expected exit 0 (graceful handling), got %d; output=%q", exit, stdout.String())
	}
	outPath := filepath.Join(outDir, "minimal.codex.md")
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if !strings.Contains(string(content), "Codex Agent Skill") {
		t.Fatal("expected Codex header even for minimal input")
	}
}

// --- Helpers ---

func createTestSkillFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test skill: %v", err)
	}
	return path
}
