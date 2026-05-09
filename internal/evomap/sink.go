// Package evomap implements the v2.10.0 Story 5 NDJSON sink that
// feeds the EvoMap-Evolver / EvoLoop-DRL pipeline. Each EC binary
// writes one Capsule per minute (driven by memwatch.Sampler) into a
// rotating NDJSON file at tests/metrics/evomap.ndjson; the daily
// rollup binary (cmd/evomap-rollup) aggregates them into a markdown
// capsule consumed by the existing fleet evoloop pipeline.
//
// Design notes:
//   - Append-only with one JSON object per line (NDJSON contract).
//   - Daily rotation by ISO date suffix when Rotate=true.
//   - Reopens existing files for append on restart so cmd/* restarts
//     do not lose history.
//   - Schema mirrors the existing fleet evoloop capsule; this keeps
//     selfimprove.RecordCycle a drop-in consumer.
package evomap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Capsule is the single-line NDJSON record emitted to evomap.ndjson.
// Field tags use snake_case to match the existing fleet capsule
// schema in evoloop-capsules/.
type Capsule struct {
	RecordedAt time.Time `json:"recorded_at"`
	EventAt    time.Time `json:"event_at"`
	Binary     string    `json:"binary"`
	TenantID   string    `json:"tenant_id,omitempty"`
	KPIs       KPIs      `json:"kpis"`
}

// KPIs carries the numeric measurements per capsule.
type KPIs struct {
	ThroughputRPS  float64 `json:"throughput_rps"`
	P95Ms          float64 `json:"p95_ms"`
	ErrorRate      float64 `json:"error_rate"`
	OOMAlarms      int     `json:"oom_alarms"`
	GoroutineCount int     `json:"goroutine_count"`
	GCPauseP99Us   float64 `json:"gc_pause_p99_us"`
	HeapInUseBytes uint64  `json:"heap_in_use_bytes"`
}

// Config controls Sink construction.
type Config struct {
	Path   string           // base file path
	Binary string           // default Binary on capsules
	Rotate bool             // enable daily rotation by ISO date suffix
	Now    func() time.Time // injectable clock for tests
}

// Sink writes Capsules to disk one per line.
type Sink struct {
	cfg    Config
	logger *slog.Logger

	mu         sync.Mutex
	file       *os.File
	writer     *bufio.Writer
	currentDay string
	closed     bool
}

// NewSink opens or creates the NDJSON file. Parent directories are
// created if missing.
func NewSink(logger *slog.Logger, cfg Config) (*Sink, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("evomap: sink path required")
	}
	if cfg.Binary == "" {
		cfg.Binary = "unknown"
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Sink{cfg: cfg, logger: logger}
	if err := s.rotateIfNeeded(cfg.Now()); err != nil {
		return nil, err
	}
	return s, nil
}

// Write appends a Capsule. If the binary is unset on the capsule the
// Sink default is applied; same for RecordedAt (now).
func (s *Sink) Write(ctx context.Context, c Capsule) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("evomap: sink closed")
	}
	now := s.cfg.Now()
	if c.Binary == "" {
		c.Binary = s.cfg.Binary
	}
	if c.RecordedAt.IsZero() {
		c.RecordedAt = now
	}
	if c.EventAt.IsZero() {
		c.EventAt = now
	}
	if err := s.rotateIfNeededLocked(now); err != nil {
		return err
	}
	if err := writeJSONLine(s.writer, c); err != nil {
		return err
	}
	return s.writer.Flush()
}

// Close flushes and closes the underlying file. Implements lifecycle.Closer.
func (s *Sink) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.writer != nil {
		_ = s.writer.Flush()
	}
	if s.file != nil {
		err := s.file.Close()
		s.file = nil
		s.writer = nil
		return err
	}
	return nil
}

func (s *Sink) rotateIfNeeded(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rotateIfNeededLocked(now)
}

func (s *Sink) rotateIfNeededLocked(now time.Time) error {
	day := now.UTC().Format("2006-01-02")
	if s.file != nil && (!s.cfg.Rotate || day == s.currentDay) {
		return nil
	}
	if s.file != nil {
		_ = s.writer.Flush()
		_ = s.file.Close()
	}
	path := s.cfg.Path
	if s.cfg.Rotate {
		dir, base := filepath.Split(path)
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		path = filepath.Join(dir, fmt.Sprintf("%s-%s%s", stem, day, ext))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("evomap: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("evomap: open: %w", err)
	}
	s.file = f
	s.writer = bufio.NewWriter(f)
	s.currentDay = day
	return nil
}

func writeJSONLine(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// AggregateResult is the daily aggregation output produced by
// cmd/evomap-rollup. Pure value type so tests are trivial.
type AggregateResult struct {
	SampleCount        int
	MeanThroughputRPS  float64
	MaxP95Ms           float64
	MeanErrorRate      float64
	TotalOOMAlarms     int
	MaxGoroutineCount  int
	MaxHeapInUseBytes  uint64
	MeanGCPauseP99Us   float64
	WindowStart        time.Time
	WindowEnd          time.Time
	BinaryDistribution map[string]int
}

// Aggregate computes summary KPIs across a slice of capsules.
func Aggregate(caps []Capsule) AggregateResult {
	res := AggregateResult{BinaryDistribution: map[string]int{}}
	if len(caps) == 0 {
		return res
	}
	res.SampleCount = len(caps)
	res.WindowStart = caps[0].EventAt
	res.WindowEnd = caps[0].EventAt
	var sumRPS, sumErr, sumGC float64
	for _, c := range caps {
		sumRPS += c.KPIs.ThroughputRPS
		sumErr += c.KPIs.ErrorRate
		sumGC += c.KPIs.GCPauseP99Us
		if c.KPIs.P95Ms > res.MaxP95Ms {
			res.MaxP95Ms = c.KPIs.P95Ms
		}
		if c.KPIs.GoroutineCount > res.MaxGoroutineCount {
			res.MaxGoroutineCount = c.KPIs.GoroutineCount
		}
		if c.KPIs.HeapInUseBytes > res.MaxHeapInUseBytes {
			res.MaxHeapInUseBytes = c.KPIs.HeapInUseBytes
		}
		res.TotalOOMAlarms += c.KPIs.OOMAlarms
		res.BinaryDistribution[c.Binary]++
		if c.EventAt.Before(res.WindowStart) {
			res.WindowStart = c.EventAt
		}
		if c.EventAt.After(res.WindowEnd) {
			res.WindowEnd = c.EventAt
		}
	}
	n := float64(len(caps))
	res.MeanThroughputRPS = sumRPS / n
	res.MeanErrorRate = sumErr / n
	res.MeanGCPauseP99Us = sumGC / n
	return res
}
