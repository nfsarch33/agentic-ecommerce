// Command uiauto-compare produces a side-by-side comparison of Playwright
// and uiauto-framework results for the v2.1.0 research-mode integration.
//
// The binary is deliberately thin: flag parsing, mode dispatch, and exit
// code shaping. All semantic work lives in
// github.com/nfsarch33/helixon-ec/internal/uiauto/compare which
// has table-driven tests covering parse, diff, and report rendering.
//
// Usage:
//
//	uiauto-compare \
//	  --mode=fixtures \
//	  --fixtures-dir=test/uiauto/fixtures \
//	  --output-dir=reports/uiauto-comparison/$(date -u +%F)
//
//	uiauto-compare \
//	  --mode=runtime \
//	  --playwright-results-dir=$dir/playwright \
//	  --uiauto-results-dir=$dir/uiauto \
//	  --output-dir=reports/uiauto-comparison/$(date -u +%F)
//
// Exit codes:
//
//	0  -- report generated successfully
//	2  -- input/IO error
//	3  -- runtime mode invoked with missing --playwright-results-dir or
//	      --uiauto-results-dir
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/uiauto/compare"
)

const (
	modeFixtures = "fixtures"
	modeRuntime  = "runtime"
)

type options struct {
	mode          string
	scenariosDir  string
	fixturesDir   string
	playwrightDir string
	uiautoDir     string
	mappingPath   string
	outputDir     string
	stdoutSummary bool
}

func main() {
	os.Exit(mainImpl(os.Args[1:], os.Stdout, os.Stderr))
}

// mainImpl is the testable entry point. It returns the process exit
// code so main() reduces to os.Exit(mainImpl(...)). Following the
// go-clean-architecture pattern: cmd/* main reduces to dependency
// build + delegate, with errors translated to exit codes by a single
// pure function.
func mainImpl(args []string, stdout, stderr io.Writer) int {
	if err := run(args, stdout, stderr); err != nil {
		exitCode := 2
		var ce *cmdError
		if errAs(err, &ce) {
			exitCode = ce.code
		}
		fmt.Fprintf(stderr, "uiauto-compare: %s\n", err)
		return exitCode
	}
	return 0
}

func run(args []string, stdout, stderr io.Writer) error {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}
	src, err := loadSources(opts)
	if err != nil {
		return &cmdError{code: 2, err: err}
	}
	report := compare.Report{
		GeneratedAt: time.Now().UTC(),
		Mode:        opts.mode,
		Items:       compare.Compose(src),
	}
	report.Summary = compare.Summarize(report.Items)
	jsonPath, mdPath, err := compare.WriteReport(report, opts.outputDir)
	if err != nil {
		return &cmdError{code: 2, err: err}
	}
	fmt.Fprintf(stdout, "uiauto-compare: wrote %s\n", jsonPath)
	fmt.Fprintf(stdout, "uiauto-compare: wrote %s\n", mdPath)
	if opts.stdoutSummary {
		fmt.Fprintf(stdout, "uiauto-compare: total=%d agreed=%d disagreed=%d both_pass=%d both_fail=%d pw_only_pass=%d ui_only_pass=%d selfheal=%d\n",
			report.Summary.Total,
			report.Summary.Agreed,
			report.Summary.Disagreed,
			report.Summary.BothPass,
			report.Summary.BothFail,
			report.Summary.PlaywrightOnlyPass,
			report.Summary.UIAutoOnlyPass,
			report.Summary.SelfHealEvents,
		)
	}
	return nil
}

func parseFlags(args []string, stderr io.Writer) (options, error) {
	fs := flag.NewFlagSet("uiauto-compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts options
	fs.StringVar(&opts.mode, "mode", modeFixtures, "comparison mode: fixtures | runtime")
	fs.StringVar(&opts.scenariosDir, "scenarios-dir", "", "path to canonical scenarios directory (informational; unused in v2.1.0)")
	fs.StringVar(&opts.fixturesDir, "fixtures-dir", "test/uiauto/fixtures", "fixtures root for mode=fixtures")
	fs.StringVar(&opts.playwrightDir, "playwright-results-dir", "", "directory of per-spec playwright JSON reports for mode=runtime")
	fs.StringVar(&opts.uiautoDir, "uiauto-results-dir", "", "directory of per-scenario demo-metrics.json for mode=runtime")
	fs.StringVar(&opts.mappingPath, "mapping", "", "optional explicit mapping JSON path; defaults to <fixtures-dir>/mapping.json or inferred")
	fs.StringVar(&opts.outputDir, "output-dir", "", "directory to write diff.json + summary.md (required)")
	fs.BoolVar(&opts.stdoutSummary, "stdout-summary", true, "print a one-line summary on stdout after writing the report")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.outputDir == "" {
		return options{}, &cmdError{code: 3, err: fmt.Errorf("--output-dir is required")}
	}
	switch opts.mode {
	case modeFixtures, modeRuntime:
	default:
		return options{}, &cmdError{code: 3, err: fmt.Errorf("--mode must be one of: %s, %s", modeFixtures, modeRuntime)}
	}
	if opts.mode == modeRuntime {
		if opts.playwrightDir == "" || opts.uiautoDir == "" {
			return options{}, &cmdError{code: 3, err: fmt.Errorf("mode=runtime requires --playwright-results-dir and --uiauto-results-dir")}
		}
	}
	return opts, nil
}

func loadSources(opts options) (compare.Sources, error) {
	switch opts.mode {
	case modeFixtures:
		return compare.LoadFixtures(opts.fixturesDir)
	case modeRuntime:
		return compare.LoadFromDirs(opts.playwrightDir, opts.uiautoDir, opts.mappingPath)
	default:
		return compare.Sources{}, fmt.Errorf("unsupported mode %q", opts.mode)
	}
}

// cmdError carries an exit code so main can shape os.Exit without losing
// the underlying %w chain. Avoids the Unix signal sentinel pattern.
type cmdError struct {
	code int
	err  error
}

func (c *cmdError) Error() string { return c.err.Error() }
func (c *cmdError) Unwrap() error { return c.err }

// errAs is a tiny stand-in for errors.As to keep the import surface
// minimal in this command. Walks the Unwrap chain.
func errAs(err error, target **cmdError) bool {
	for err != nil {
		if ce, ok := err.(*cmdError); ok {
			*target = ce
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
