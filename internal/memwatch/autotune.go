package memwatch

import (
	"log/slog"
	"runtime"
	"sync/atomic"
)

const (
	MinHeapCeiling      = 256 << 20 // 256 MiB absolute floor
	MinGoroutineCeiling = 1_000
	heapPerGoroutine    = 100 << 10 // 100 KiB per goroutine budget
	targetRatio         = 0.7       // use 70% of available memory
)

// AutoTuneConfig controls ceiling auto-detection.
type AutoTuneConfig struct {
	DetectMemFunc         func() uint64 // returns available system memory; injectable for testing
	HeapCeilingOverride   uint64        // if non-zero, takes precedence over detection
	GoroutineCeilOverride int           // if non-zero, takes precedence
}

// TuneResult holds the computed ceilings.
type TuneResult struct {
	HeapCeiling      uint64
	GoroutineCeiling int
}

// AutoTuner detects system memory and computes ceilings.
type AutoTuner struct {
	cfg    AutoTuneConfig
	logger *slog.Logger

	adjustments atomic.Int64
}

// NewAutoTuner creates a tuner with resolved defaults.
func NewAutoTuner(logger *slog.Logger, cfg AutoTuneConfig) *AutoTuner {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.DetectMemFunc == nil {
		cfg.DetectMemFunc = detectSystemMemory
	}
	return &AutoTuner{cfg: cfg, logger: logger}
}

// Tune computes or re-evaluates ceilings. Safe to call periodically.
func (at *AutoTuner) Tune() TuneResult {
	at.adjustments.Add(1)
	result := at.compute()
	at.logger.Info("memwatch.autotune",
		"heap_ceiling_bytes", result.HeapCeiling,
		"goroutine_ceiling", result.GoroutineCeiling,
		"adjustments_total", at.adjustments.Load(),
	)
	return result
}

// Adjustments returns how many times Tune has been called.
func (at *AutoTuner) Adjustments() int64 { return at.adjustments.Load() }

func (at *AutoTuner) compute() TuneResult {
	var result TuneResult

	if at.cfg.HeapCeilingOverride > 0 {
		result.HeapCeiling = at.cfg.HeapCeilingOverride
	} else {
		available := at.cfg.DetectMemFunc()
		result.HeapCeiling = uint64(float64(available) * targetRatio)
		if result.HeapCeiling < MinHeapCeiling {
			result.HeapCeiling = MinHeapCeiling
		}
	}

	if at.cfg.GoroutineCeilOverride > 0 {
		result.GoroutineCeiling = at.cfg.GoroutineCeilOverride
	} else {
		result.GoroutineCeiling = int(result.HeapCeiling / heapPerGoroutine)
		if result.GoroutineCeiling < MinGoroutineCeiling {
			result.GoroutineCeiling = MinGoroutineCeiling
		}
	}

	return result
}

func detectSystemMemory() uint64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.Sys
}
