package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	maxDescriptionLen = 200
	minTriggerPhrases = 3
	maxFileSizeBytes  = 50 * 1024 // 50KB
)

// infrastructureHostnames is the set of patterns that should never
// appear in a public skill file per the public-repo-gate policy.
var infrastructureHostnames = []string{
	"gpu-host-1", "windows-host-1", "operator-laptop", "private-lab",
	"192.168.", "10.0.", "172.16.",
	"localhost:8", "localhost:9",
}

// hardcodedPathPrefixes detects leaked local paths per no-shell-leak.
var hardcodedPathPrefixes = []string{
	"/Users/jason", "/home/jason",
	"/Users/user", "/home/user",
	"C:\\Users\\",
}

// skillQualityReport holds the outcome of a skill quality check.
type skillQualityReport struct {
	Path   string             `json:"path"`
	Passed bool               `json:"passed"`
	Checks []skillCheckResult `json:"checks"`
}

type skillCheckResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

func runSkill(ctx context.Context, args []string, deps appDeps) int {
	if len(args) < 1 {
		fmt.Fprintln(deps.stderr, "ec-cli skill: subcommand required (quality-check, codex-gen)")
		return 2
	}
	switch args[0] {
	case "quality-check":
		return runSkillQualityCheck(ctx, args[1:], deps)
	case "codex-gen":
		return runSkillCodexGen(ctx, args[1:], deps)
	default:
		fmt.Fprintf(deps.stderr, "ec-cli skill: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runSkillQualityCheck(_ context.Context, args []string, deps appDeps) int {
	if len(args) < 1 {
		fmt.Fprintln(deps.stderr, "ec-cli skill quality-check: path argument required")
		return 2
	}
	path := args[0]
	report, err := checkSkillQuality(path)
	if err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli skill quality-check: %v\n", err)
		return 1
	}
	writeSkillQualityReport(deps, report)
	if !report.Passed {
		return 1
	}
	return 0
}

func checkSkillQuality(path string) (skillQualityReport, error) {
	report := skillQualityReport{Path: path}

	content, err := os.ReadFile(path)
	if err != nil {
		return report, fmt.Errorf("read %s: %w", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return report, fmt.Errorf("stat %s: %w", path, err)
	}

	text := string(content)
	report.Checks = append(report.Checks, checkDescription(text))
	report.Checks = append(report.Checks, checkTriggers(text))
	report.Checks = append(report.Checks, checkFileSize(info.Size()))
	report.Checks = append(report.Checks, checkHostnames(text))
	report.Checks = append(report.Checks, checkPaths(text))

	report.Passed = allChecksPassed(report.Checks)
	return report, nil
}

func checkDescription(content string) skillCheckResult {
	result := skillCheckResult{Name: "description"}
	desc := extractDescription(content)
	if desc == "" {
		result.Detail = "no description found (expected text after first # heading)"
		return result
	}
	if len(desc) > maxDescriptionLen {
		result.Detail = fmt.Sprintf("description too long (%d chars, max %d)", len(desc), maxDescriptionLen)
		return result
	}
	result.Passed = true
	result.Detail = fmt.Sprintf("ok (%d chars)", len(desc))
	return result
}

func checkTriggers(content string) skillCheckResult {
	result := skillCheckResult{Name: "triggers"}
	triggers := extractTriggerPhrases(content)
	if len(triggers) == 0 {
		result.Detail = "no trigger phrases section found"
		return result
	}
	if len(triggers) < minTriggerPhrases {
		result.Detail = fmt.Sprintf("only %d trigger phrases (min %d)", len(triggers), minTriggerPhrases)
		return result
	}
	result.Passed = true
	result.Detail = fmt.Sprintf("ok (%d phrases)", len(triggers))
	return result
}

func checkFileSize(size int64) skillCheckResult {
	result := skillCheckResult{Name: "file_size"}
	if size > int64(maxFileSizeBytes) {
		result.Detail = fmt.Sprintf("file too large (%d bytes, max %d)", size, maxFileSizeBytes)
		return result
	}
	result.Passed = true
	result.Detail = fmt.Sprintf("ok (%d bytes)", size)
	return result
}

func checkHostnames(content string) skillCheckResult {
	result := skillCheckResult{Name: "hostnames"}
	lower := strings.ToLower(content)
	for _, h := range infrastructureHostnames {
		if strings.Contains(lower, strings.ToLower(h)) {
			result.Detail = fmt.Sprintf("infrastructure hostname detected: %q", h)
			return result
		}
	}
	result.Passed = true
	result.Detail = "ok (no infrastructure hostnames)"
	return result
}

func checkPaths(content string) skillCheckResult {
	result := skillCheckResult{Name: "paths"}
	for _, prefix := range hardcodedPathPrefixes {
		if strings.Contains(content, prefix) {
			result.Detail = fmt.Sprintf("hardcoded path detected: %q", prefix)
			return result
		}
	}
	result.Passed = true
	result.Detail = "ok (no hardcoded paths)"
	return result
}

func allChecksPassed(checks []skillCheckResult) bool {
	for _, c := range checks {
		if !c.Passed {
			return false
		}
	}
	return true
}

// extractDescription pulls the first non-heading, non-empty paragraph
// after the first h1 (`# Title`) line. This matches the SKILL.md convention.
// If no h1 exists, returns empty (skill has no proper title/description).
func extractDescription(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	foundH1 := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !foundH1 {
			if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
				foundH1 = true
			}
			continue
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			break
		}
		return line
	}
	return ""
}

var triggerLineRe = regexp.MustCompile(`^[-*]\s+(.+)`)

// extractTriggerPhrases looks for a section named "Trigger" (case-insensitive)
// and collects bullet items beneath it. Also matches "Use when:" patterns
// which are the convention in Cursor skills.
func extractTriggerPhrases(content string) []string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inSection := false
	var phrases []string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		if strings.Contains(lower, "trigger") && strings.HasPrefix(trimmed, "#") {
			inSection = true
			continue
		}
		if strings.Contains(lower, "use when") || strings.Contains(lower, "use this") {
			inSection = true
			useWhenPhrases := parseUseWhenLine(trimmed)
			phrases = append(phrases, useWhenPhrases...)
			continue
		}
		if inSection {
			if strings.HasPrefix(trimmed, "#") {
				break
			}
			if m := triggerLineRe.FindStringSubmatch(trimmed); len(m) > 1 {
				phrases = append(phrases, m[1])
			}
		}
	}
	return phrases
}

func parseUseWhenLine(line string) []string {
	idx := strings.Index(strings.ToLower(line), "use when")
	if idx < 0 {
		idx = strings.Index(strings.ToLower(line), "use this")
	}
	if idx < 0 {
		return nil
	}
	rest := line[idx:]
	colonIdx := strings.Index(rest, ":")
	if colonIdx < 0 {
		return []string{rest}
	}
	afterColon := rest[colonIdx+1:]
	parts := strings.Split(afterColon, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		trimmed = strings.TrimSuffix(trimmed, ".")
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return []string{rest}
	}
	return result
}

func writeSkillQualityReport(deps appDeps, report skillQualityReport) {
	verdict := "PASS"
	if !report.Passed {
		verdict = "FAIL"
	}
	fmt.Fprintf(deps.stdout, "ec-cli skill quality-check %s [%s]\n", report.Path, verdict)
	for _, c := range report.Checks {
		marker := "PASS"
		if !c.Passed {
			marker = "FAIL"
		}
		fmt.Fprintf(deps.stdout, "  [%s] %-12s %s\n", marker, c.Name, c.Detail)
	}
}
