package postgres_test

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const benchLatencyTextfileDirEnv = "EC_V631_BENCH_TEXTFILE_DIR"

type benchLatencyRecorder struct {
	name    string
	samples []time.Duration
}

type benchLatencySummary struct {
	Name  string
	Count int
	P50   time.Duration
	P95   time.Duration
	P99   time.Duration
}

func newBenchLatencyRecorder(name string, capacity int) *benchLatencyRecorder {
	if capacity < 0 {
		capacity = 0
	}
	return &benchLatencyRecorder{
		name:    sanitiseBenchName(name),
		samples: make([]time.Duration, 0, capacity),
	}
}

func (r *benchLatencyRecorder) observe(d time.Duration) {
	if r == nil {
		return
	}
	r.samples = append(r.samples, d)
}

func (r *benchLatencyRecorder) summary() benchLatencySummary {
	if r == nil || len(r.samples) == 0 {
		return benchLatencySummary{Name: "unknown"}
	}
	samples := append([]time.Duration(nil), r.samples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return benchLatencySummary{
		Name:  r.name,
		Count: len(samples),
		P50:   nearestRank(samples, 50),
		P95:   nearestRank(samples, 95),
		P99:   nearestRank(samples, 99),
	}
}

func finishBenchLatency(b *testing.B, r *benchLatencyRecorder) {
	b.Helper()
	summary := r.summary()
	if summary.Count == 0 {
		return
	}
	b.ReportMetric(float64(summary.P50.Nanoseconds()), "p50_ns/op")
	b.ReportMetric(float64(summary.P95.Nanoseconds()), "p95_ns/op")
	b.ReportMetric(float64(summary.P99.Nanoseconds()), "p99_ns/op")
	dir := os.Getenv(benchLatencyTextfileDirEnv)
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		b.Fatalf("mkdir %s: %v", benchLatencyTextfileDirEnv, err)
	}
	var sb strings.Builder
	if err := writeBenchLatencyPrometheus(&sb, summary); err != nil {
		b.Fatalf("render %s: %v", benchLatencyTextfileDirEnv, err)
	}
	path := filepath.Join(dir, summary.Name+".prom")
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		b.Fatalf("write %s: %v", benchLatencyTextfileDirEnv, err)
	}
}

func writeBenchLatencyPrometheus(w io.Writer, s benchLatencySummary) error {
	name := sanitiseBenchName(s.Name)
	for _, row := range []struct {
		quantile string
		value    time.Duration
	}{
		{quantile: "p50", value: s.P50},
		{quantile: "p95", value: s.P95},
		{quantile: "p99", value: s.P99},
	} {
		if _, err := fmt.Fprintf(w, "ec_v631_pg_benchmark_latency_seconds{benchmark=%q,quantile=%q} %.9f\n", name, row.quantile, row.value.Seconds()); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "ec_v631_pg_benchmark_samples_total{benchmark=%q} %d\n", name, s.Count)
	return err
}

func nearestRank(sorted []time.Duration, percentile int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(float64(percentile) / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

func sanitiseBenchName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var sb strings.Builder
	lastUnderscore := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			sb.WriteByte('_')
			lastUnderscore = true
		}
	}
	cleaned := strings.Trim(sb.String(), "_")
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}
