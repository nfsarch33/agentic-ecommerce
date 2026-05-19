// cmd/evomap-rollup is the v2.10.0 Story 5 daily rollup binary.
//
// It reads the rotated NDJSON capsules under tests/metrics/, aggregates
// their KPIs into an evoloop markdown capsule, and writes the result
// to ~/Code/global-kb/global-memories/evoloop-capsules/ so the
// existing fleet evomap-evolver pipeline picks it up unchanged.
//
// Lifecycle: short-lived; uses lifecycle.Manager to ensure any open
// files / sinks are closed before returning. Designed to run as a
// daily cron via runx.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/evomap"
	"github.com/nfsarch33/helixon-ec/internal/lifecycle"
)

var (
	version = "dev"
	commit  = "unknown"
)

// rollupConfig is the testable input bundle.
type rollupConfig struct {
	BasePath string
	OutPath  string
	WhenISO  string
}

// mainImpl is the testable entry point. Returns OS exit code.
func mainImpl(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer, getenv func(string) string) int {
	cfg, err := parseArgs(args, getenv, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "evomap-rollup: invalid args: %v\n", err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(stdout, nil))

	mgr := lifecycle.New(logger, 10*time.Second)
	exitCode := 0
	err = mgr.Run(ctx, func(_ context.Context) error {
		caps, err := loadAllCapsules(cfg.BasePath)
		if err != nil {
			return fmt.Errorf("load: %w", err)
		}
		if len(caps) == 0 {
			fmt.Fprintln(stderr, "evomap-rollup: no capsules found")
			exitCode = 0
			return nil
		}
		now := time.Now().UTC()
		if cfg.WhenISO != "" {
			parsed, err := time.Parse(time.RFC3339, cfg.WhenISO)
			if err != nil {
				return fmt.Errorf("parse --when: %w", err)
			}
			now = parsed
		}
		agg := evomap.Aggregate(caps)
		md := evomap.RenderCapsuleMarkdown(now, agg)
		outPath := cfg.OutPath
		if outPath == "" {
			outPath = filepath.Join(getenvOrDefault(getenv, "ECOMMERCE_EVOMAP_CAPSULE_DIR", "tests/metrics/capsules"),
				fmt.Sprintf("ec-stack-%s.md", now.UTC().Format("2006-01-02")))
		}
		if err := evomap.WriteCapsule(ctx, outPath, md); err != nil {
			return fmt.Errorf("write capsule: %w", err)
		}
		fmt.Fprintf(stdout, "evomap-rollup: wrote %s (%d samples)\n", outPath, agg.SampleCount)
		return nil
	})
	if err != nil {
		fmt.Fprintf(stderr, "evomap-rollup: failed: %v\n", err)
		return 1
	}
	return exitCode
}

func parseArgs(args []string, getenv func(string) string, stderr io.Writer) (rollupConfig, error) {
	fs := flag.NewFlagSet("evomap-rollup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := rollupConfig{}
	fs.StringVar(&cfg.BasePath, "in", getenvOrDefault(getenv, "ECOMMERCE_EVOMAP_NDJSON", "tests/metrics/evomap.ndjson"), "Base NDJSON path (rotated days are auto-discovered)")
	fs.StringVar(&cfg.OutPath, "out", getenvOrDefault(getenv, "ECOMMERCE_EVOMAP_OUT", ""), "Output capsule markdown path")
	fs.StringVar(&cfg.WhenISO, "when", "", "Override 'now' (RFC 3339) for deterministic capsule paths")
	if err := fs.Parse(args[1:]); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func loadAllCapsules(basePath string) ([]evomap.Capsule, error) {
	matches, err := evomap.RolloverGlob(basePath)
	if err != nil {
		return nil, fmt.Errorf("glob: %w", err)
	}
	if len(matches) == 0 {
		// fall back to the un-rotated base file
		if _, err := os.Stat(basePath); err == nil {
			matches = []string{basePath}
		}
	}
	sort.Strings(matches)
	var out []evomap.Capsule
	for _, m := range matches {
		caps, _, err := evomap.LoadCapsules(m)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", m, err)
		}
		out = append(out, caps...)
	}
	return out, nil
}

func getenvOrDefault(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}
