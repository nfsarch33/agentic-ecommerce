package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// cursorToolRefs are patterns referencing Cursor-specific tools that
// should be stripped from a Codex-compatible skill variant.
var cursorToolRefs = []string{
	"Task tool", "TodoWrite", "SwitchMode",
	"EditNotebook", "SetActiveBranch",
	"GenerateImage", "CallMcpTool",
	"FetchMcpResource",
}

// cursorRuleRefRe matches references to .cursor/rules/*.mdc files.
var cursorRuleRefRe = regexp.MustCompile(
	`\.cursor/rules/[^\s)]+\.mdc`,
)

func runSkillCodexGen(_ context.Context, args []string, deps appDeps) int {
	if len(args) < 2 {
		fmt.Fprintln(deps.stderr, "ec-cli skill codex-gen: requires <cursor-skill-path> <output-dir>")
		return 2
	}
	srcPath := args[0]
	outDir := args[1]

	output, err := convertToCodex(srcPath, outDir)
	if err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli skill codex-gen: %v\n", err)
		return 1
	}
	fmt.Fprintf(deps.stdout, "ec-cli skill codex-gen: wrote %s\n", output)
	return 0
}

func convertToCodex(srcPath, outDir string) (string, error) {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return "", fmt.Errorf("read source: %w", err)
	}

	parsed := parseSkillContent(string(content))
	converted := stripCursorRefs(parsed)
	converted = convertRuleRefs(converted)
	withHeader := addCodexHeader(converted, parsed.title)

	outName := skillOutputName(srcPath)
	outPath := filepath.Join(outDir, outName)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", outDir, err)
	}
	if err := os.WriteFile(outPath, []byte(withHeader), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", outPath, err)
	}
	return outPath, nil
}

type parsedSkill struct {
	title    string
	body     string
	triggers string
}

func parseSkillContent(content string) parsedSkill {
	var ps parsedSkill
	scanner := bufio.NewScanner(strings.NewReader(content))
	var bodyLines []string
	var triggerLines []string
	inTrigger := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if ps.title == "" && strings.HasPrefix(trimmed, "# ") {
			ps.title = strings.TrimPrefix(trimmed, "# ")
			continue
		}

		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "trigger") && strings.HasPrefix(trimmed, "#") {
			inTrigger = true
			bodyLines = append(bodyLines, line)
			continue
		}
		if inTrigger && strings.HasPrefix(trimmed, "#") {
			inTrigger = false
		}
		if inTrigger {
			triggerLines = append(triggerLines, line)
		}
		bodyLines = append(bodyLines, line)
	}

	ps.body = strings.Join(bodyLines, "\n")
	ps.triggers = strings.Join(triggerLines, "\n")
	return ps
}

func stripCursorRefs(ps parsedSkill) string {
	result := ps.body
	for _, ref := range cursorToolRefs {
		result = strings.ReplaceAll(result, ref, "")
		result = strings.ReplaceAll(result, "`"+ref+"`", "")
	}
	lines := strings.Split(result, "\n")
	var filtered []string
	for _, line := range lines {
		skip := false
		for _, ref := range cursorToolRefs {
			if strings.Contains(line, ref) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}

func convertRuleRefs(content string) string {
	return cursorRuleRefRe.ReplaceAllStringFunc(content, func(match string) string {
		base := filepath.Base(match)
		name := strings.TrimSuffix(base, ".mdc")
		return "AGENTS.md section: " + name
	})
}

func addCodexHeader(body, title string) string {
	if title == "" {
		title = "Unnamed Skill"
	}
	header := fmt.Sprintf("# Codex Agent Skill: %s\n\n", title)
	header += "> Auto-generated from Cursor SKILL.md. Do not edit manually.\n\n"
	return header + body
}

func skillOutputName(srcPath string) string {
	dir := filepath.Dir(srcPath)
	name := filepath.Base(dir)
	if name == "." || name == "/" {
		name = strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	}
	return name + ".codex.md"
}
