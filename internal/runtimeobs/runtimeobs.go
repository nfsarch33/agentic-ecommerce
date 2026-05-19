// Package runtimeobs fans runtime sampler observations into Prometheus
// gauges and the EvoMap NDJSON sink used by the self-improvement loop.
package runtimeobs

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/evomap"
	"github.com/nfsarch33/helixon-ec/internal/memwatch"
	"github.com/nfsarch33/helixon-ec/internal/metrics"
)

const defaultEvomapPath = "tests/metrics/evomap.ndjson"

// Config controls optional EvoMap persistence.
type Config struct {
	EvomapPath         string
	Rotate             bool
	AgentraceJSONLPath string
}

// DefaultEvomapPath resolves the shared runtime NDJSON path.
func DefaultEvomapPath(getenv func(string) string) string {
	if getenv != nil {
		if path := getenv("ECOMMERCE_EVOMAP_NDJSON"); path != "" {
			return path
		}
	}
	if runningUnderGoTest() {
		return ""
	}
	return defaultEvomapPath
}

// DefaultAgentraceJSONLPath resolves the default Agentrace events.jsonl path.
func DefaultAgentraceJSONLPath(getenv func(string) string) string {
	if getenv != nil {
		if path := getenv("ECOMMERCE_AGENTRACE_JSONL"); path != "" {
			return path
		}
	}
	if runningUnderGoTest() {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agentrace", "events.jsonl")
}

func runningUnderGoTest() bool {
	return len(os.Args) > 0 && strings.HasSuffix(os.Args[0], ".test")
}

// RuntimeObservability owns the per-binary metrics registry and optional
// EvoMap sink.
type RuntimeObservability struct {
	logger    *slog.Logger
	reg       *metrics.Registry
	sink      *evomap.Sink
	agentrace *evomap.AgentraceAdapter
}

// New creates a runtime observability fanout. EvoMap sink failures degrade
// to Prometheus-only metrics so application startup is not blocked by a
// writable-path issue.
func New(logger *slog.Logger, binary string, cfg Config) *RuntimeObservability {
	if logger == nil {
		logger = slog.Default()
	}
	if binary == "" {
		binary = "unknown"
	}
	rt := &RuntimeObservability{
		logger: logger,
		reg:    metrics.NewRegistry(binary),
	}
	if cfg.AgentraceJSONLPath != "" {
		rt.agentrace = evomap.NewAgentraceAdapter(evomap.AgentraceAdapterConfig{
			JSONLPath:   cfg.AgentraceJSONLPath,
			PreferJSONL: true,
			Logger:      logger,
		})
	}
	path := cfg.EvomapPath
	if path == "" {
		return rt
	}
	sink, err := evomap.NewSink(logger, evomap.Config{
		Path:   path,
		Binary: binary,
		Rotate: cfg.Rotate,
	})
	if err != nil {
		logger.Warn("runtimeobs.evomap_sink_unavailable", "binary", binary, "path", path, "error", err)
		return rt
	}
	rt.sink = sink
	return rt
}

// Registry returns the Prometheus registry owned by this runtime fanout.
func (rt *RuntimeObservability) Registry() *metrics.Registry {
	if rt == nil {
		return nil
	}
	return rt.reg
}

// Emit satisfies memwatch.Sink and records the sample in all configured
// observability backends.
func (rt *RuntimeObservability) Emit(ctx context.Context, s memwatch.Sample) {
	if rt == nil {
		return
	}
	rt.reg.GoroutineCount.Set(float64(s.GoroutineCount), metrics.Labels{})
	rt.reg.HeapBytes.Set(float64(s.HeapInUseBytes), metrics.Labels{})
	var aKPIs evomap.AgentraceKPIs
	if rt.agentrace != nil {
		aKPIs = rt.agentrace.Read(ctx)
		metrics.ApplyAgentraceKPIs(rt.reg, aKPIs)
	}
	if rt.sink == nil {
		return
	}
	cap := evomap.Capsule{
		RecordedAt: s.RecordedAt,
		EventAt:    s.RecordedAt,
		Binary:     s.Binary,
		KPIs: evomap.KPIs{
			GoroutineCount:              s.GoroutineCount,
			GCPauseP99Us:                float64(s.GCPauseLastNs) / float64(time.Microsecond),
			HeapInUseBytes:              s.HeapInUseBytes,
			AgentraceAvailable:          aKPIs.Available,
			AgentraceSessionDurationSec: aKPIs.SessionDurationSec,
			AgentraceToolCallCount:      aKPIs.ToolCallCount,
			AgentraceCostUSD:            aKPIs.CostUSD,
			AgentraceBottleneckCount:    aKPIs.BottleneckCount,
			AgentraceParallelismRatio:   aKPIs.ParallelismRatio,
		},
	}
	if err := rt.sink.Write(ctx, cap); err != nil {
		rt.logger.Warn("runtimeobs.evomap_write_failed", "binary", s.Binary, "error", err)
	}
}

// Close flushes the optional EvoMap sink.
func (rt *RuntimeObservability) Close(ctx context.Context) error {
	if rt == nil || rt.sink == nil {
		return nil
	}
	return rt.sink.Close(ctx)
}
