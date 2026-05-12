package evomap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LoadCapsules reads NDJSON capsules from the given path. Each line
// is JSON-decoded into a Capsule. Malformed lines are reported via
// the returned skip slice but do not abort the load.
func LoadCapsules(path string) ([]Capsule, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("evomap: open: %w", err)
	}
	defer f.Close()
	return loadFromReader(f)
}

func loadFromReader(r io.Reader) ([]Capsule, []string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var caps []Capsule
	var skip []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var c Capsule
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			skip = append(skip, line)
			continue
		}
		caps = append(caps, c)
	}
	if err := scanner.Err(); err != nil {
		return nil, skip, err
	}
	return caps, skip, nil
}

// RolloverGlob returns the slice of NDJSON files matching the
// rolled-day pattern (basename + suffix). Used by cmd/evomap-rollup
// to pick up rotated files from previous days.
func RolloverGlob(basePath string) ([]string, error) {
	dir, base := filepath.Split(basePath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	pattern := filepath.Join(dir, stem+"*"+ext)
	return filepath.Glob(pattern)
}

// RenderCapsuleMarkdown formats an aggregate result as a markdown
// document destined for ~/Code/global-kb/global-memories/evoloop-capsules/
// per the v2.10.0 Story 5 contract.
func RenderCapsuleMarkdown(now time.Time, agg AggregateResult) string {
	var sb strings.Builder
	day := now.UTC().Format("2006-01-02")
	fmt.Fprintf(&sb, "---\n")
	fmt.Fprintf(&sb, "kind: evoloop_rollup\n")
	fmt.Fprintf(&sb, "host: ec-stack\n")
	fmt.Fprintf(&sb, "recorded_at: %s\n", now.UTC().Format(time.RFC3339))
	fmt.Fprintf(&sb, "window_start: %s\n", agg.WindowStart.UTC().Format(time.RFC3339))
	fmt.Fprintf(&sb, "window_end: %s\n", agg.WindowEnd.UTC().Format(time.RFC3339))
	fmt.Fprintf(&sb, "samples: %d\n", agg.SampleCount)
	fmt.Fprintf(&sb, "---\n\n")
	fmt.Fprintf(&sb, "# EC Stack Daily Rollup -- %s\n\n", day)
	fmt.Fprintf(&sb, "## KPIs\n\n")
	fmt.Fprintf(&sb, "- mean throughput rps: %.2f\n", agg.MeanThroughputRPS)
	fmt.Fprintf(&sb, "- max p95 ms: %.2f\n", agg.MaxP95Ms)
	fmt.Fprintf(&sb, "- mean error rate: %.4f\n", agg.MeanErrorRate)
	fmt.Fprintf(&sb, "- total OOM alarms: %d\n", agg.TotalOOMAlarms)
	fmt.Fprintf(&sb, "- max goroutine count: %d\n", agg.MaxGoroutineCount)
	fmt.Fprintf(&sb, "- max heap-in-use bytes: %d\n", agg.MaxHeapInUseBytes)
	fmt.Fprintf(&sb, "- mean GC pause p99 us: %.2f\n\n", agg.MeanGCPauseP99Us)
	fmt.Fprintf(&sb, "- total resource guard alerts: %d\n", agg.TotalResourceGuardAlerts)
	fmt.Fprintf(&sb, "- max Sentrux desktop processes: %d\n", agg.MaxSentruxDesktopProcessCount)
	fmt.Fprintf(&sb, "- total workerpool resizes: %d\n\n", agg.TotalWorkerpoolResizes)
	fmt.Fprintf(&sb, "- total self-improvement evidence: %d\n", agg.TotalSelfImprovementEvidence)
	fmt.Fprintf(&sb, "- self-improvement promoted/rejected/rework: %d/%d/%d\n",
		agg.TotalSelfImprovementPromoted,
		agg.TotalSelfImprovementRejected,
		agg.TotalSelfImprovementRework,
	)
	fmt.Fprintf(&sb, "- mean self-improvement reward: %.3f\n", agg.MeanSelfImprovementReward)
	fmt.Fprintf(&sb, "- total Agenttrace evidence inputs: %d\n\n", agg.TotalAgentraceEvidence)
	fmt.Fprintf(&sb, "## Binary Distribution\n\n")
	for binary, count := range agg.BinaryDistribution {
		fmt.Fprintf(&sb, "- %s: %d capsules\n", binary, count)
	}
	return sb.String()
}

// WriteCapsule writes the markdown to the given path, creating the
// parent directory if missing.
func WriteCapsule(_ context.Context, path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("evomap: mkdir: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
