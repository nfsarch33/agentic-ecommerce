package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nfsarch33/agentic-ecommerce/pkg/marketplace/sdk"
)

// pluginValidationReport summarises the issues found in a plugin
// directory. JSON-friendly so CI pipelines can parse the output.
type pluginValidationReport struct {
	Path        string         `json:"path"`
	ManifestOK  bool           `json:"manifest_ok"`
	Manifest    sdk.Manifest   `json:"manifest"`
	Issues      []string       `json:"issues"`
	Suggestions []string       `json:"suggestions"`
	Counts      map[string]int `json:"counts"`
}

func runPlugin(ctx context.Context, args []string, deps appDeps) int {
	if len(args) < 1 {
		fmt.Fprintln(deps.stderr, "ec-cli plugin: subcommand required (validate)")
		return 2
	}
	switch args[0] {
	case "validate":
		return runPluginValidate(ctx, args[1:], deps)
	default:
		fmt.Fprintf(deps.stderr, "ec-cli plugin: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runPluginValidate(_ context.Context, args []string, deps appDeps) int {
	fs := flag.NewFlagSet("plugin validate", flag.ContinueOnError)
	fs.SetOutput(deps.stderr)
	path := fs.String("path", "", "plugin directory to validate (required)")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir := trimEmpty(*path)
	if dir == "" {
		fmt.Fprintln(deps.stderr, "ec-cli plugin validate: --path is required")
		return 2
	}
	report, err := validatePluginDir(dir)
	if err != nil {
		fmt.Fprintf(deps.stderr, "ec-cli plugin validate: %v\n", err)
		return 1
	}
	if *jsonOut {
		if err := encodeJSON(deps.stdout, report); err != nil {
			fmt.Fprintf(deps.stderr, "ec-cli plugin validate: encode json: %v\n", err)
			return 1
		}
	} else {
		writePluginReport(deps, report)
	}
	if !report.ManifestOK || len(report.Issues) > 0 {
		return 1
	}
	return 0
}

// validatePluginDir runs the offline validation checks on a plugin
// directory. It is kept as a pure function (filesystem-only, no
// network) so tests inject any directory and assert on the report.
func validatePluginDir(dir string) (pluginValidationReport, error) {
	report := pluginValidationReport{Path: dir, Counts: map[string]int{}}
	info, err := os.Stat(dir)
	if err != nil {
		return report, fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return report, fmt.Errorf("%s is not a directory", dir)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		report.Issues = append(report.Issues, "manifest.json missing or unreadable: "+err.Error())
		report.Suggestions = append(report.Suggestions, "create manifest.json with slug, name, version, vendor")
		return report, nil
	}
	manifest, err := decodeManifest(manifestBytes)
	if err != nil {
		report.Issues = append(report.Issues, "manifest.json is not valid JSON: "+err.Error())
		return report, nil
	}
	report.Manifest = manifest
	if err := manifest.Validate(); err != nil {
		report.Issues = append(report.Issues, "manifest validation failed: "+err.Error())
		return report, nil
	}
	report.ManifestOK = true

	addCounts(&report, dir)
	addModuleSuggestion(&report, dir)
	addTestSuggestion(&report, dir)
	return report, nil
}

func decodeManifest(raw []byte) (sdk.Manifest, error) {
	var m sdk.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return sdk.Manifest{}, err
	}
	return m, nil
}

func addCounts(report *pluginValidationReport, dir string) {
	goFiles := 0
	testFiles := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			testFiles++
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			goFiles++
		}
		return nil
	})
	report.Counts["go_files"] = goFiles
	report.Counts["test_files"] = testFiles
}

func addModuleSuggestion(report *pluginValidationReport, dir string) {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		report.Suggestions = append(report.Suggestions, "no go.mod -- run `go mod init github.com/your-org/<plugin-name>`")
	}
}

func addTestSuggestion(report *pluginValidationReport, dir string) {
	if report.Counts["test_files"] == 0 {
		report.Suggestions = append(report.Suggestions, "no _test.go files -- add a TestPluginSmoke test that calls sdk.NewTestSandbox(t, manifest).SmokeCheck(ctx, plugin)")
	}
}

func writePluginReport(deps appDeps, report pluginValidationReport) {
	fmt.Fprintf(deps.stdout, "ec-cli plugin validate %s\n", report.Path)
	if report.ManifestOK {
		fmt.Fprintf(deps.stdout, "  manifest OK -- %s@%s by %s\n", report.Manifest.Slug, report.Manifest.Version, report.Manifest.Vendor)
	}
	if got := report.Counts["go_files"]; got > 0 {
		fmt.Fprintf(deps.stdout, "  go files       %d\n", got)
	}
	if got := report.Counts["test_files"]; got > 0 {
		fmt.Fprintf(deps.stdout, "  test files     %d\n", got)
	}
	for _, issue := range report.Issues {
		fmt.Fprintf(deps.stdout, "  ISSUE      %s\n", issue)
	}
	for _, sugg := range report.Suggestions {
		fmt.Fprintf(deps.stdout, "  suggestion %s\n", sugg)
	}
}

// errPluginPathInvalid is exposed for tests.
var errPluginPathInvalid = errors.New("plugin path invalid")
