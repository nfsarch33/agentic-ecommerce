package postgres_test

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestBenchLatencyRecorderReportsNearestRankPercentiles(t *testing.T) {
	rec := newBenchLatencyRecorder("products_list", 10)
	for i := 1; i <= 10; i++ {
		rec.observe(time.Duration(i) * time.Millisecond)
	}

	got := rec.summary()
	if got.Count != 10 {
		t.Fatalf("count = %d, want 10", got.Count)
	}
	if got.P50 != 5*time.Millisecond {
		t.Fatalf("p50 = %s, want 5ms", got.P50)
	}
	if got.P95 != 10*time.Millisecond {
		t.Fatalf("p95 = %s, want 10ms", got.P95)
	}
	if got.P99 != 10*time.Millisecond {
		t.Fatalf("p99 = %s, want 10ms", got.P99)
	}
}

func TestWriteBenchLatencyPrometheusUsesBoundedLabels(t *testing.T) {
	summary := benchLatencySummary{
		Name:  "orders_create",
		Count: 42,
		P50:   1500 * time.Microsecond,
		P95:   2 * time.Millisecond,
		P99:   2500 * time.Microsecond,
	}
	var buf bytes.Buffer

	if err := writeBenchLatencyPrometheus(&buf, summary); err != nil {
		t.Fatalf("write prometheus: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		`ec_v631_pg_benchmark_latency_seconds{benchmark="orders_create",quantile="p50"} 0.001500000`,
		`ec_v631_pg_benchmark_latency_seconds{benchmark="orders_create",quantile="p95"} 0.002000000`,
		`ec_v631_pg_benchmark_latency_seconds{benchmark="orders_create",quantile="p99"} 0.002500000`,
		`ec_v631_pg_benchmark_samples_total{benchmark="orders_create"} 42`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
