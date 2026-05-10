package memwatch

import (
	"log/slog"
	"net/http"
	"runtime"
	"sync/atomic"
)

// BackpressureLevel represents the severity of memory pressure.
type BackpressureLevel int

const (
	BPNone      BackpressureLevel = 0
	BPWarning   BackpressureLevel = 1
	BPCritical  BackpressureLevel = 2
	BPEmergency BackpressureLevel = 3
)

// BackpressureConfig controls backpressure thresholds.
type BackpressureConfig struct {
	HeapCeiling        uint64 // bytes; 0 defaults to 4 GiB
	WarningThreshold   float64
	CriticalThreshold  float64
	EmergencyThreshold float64
	SampleFunc         func() uint64 // injectable for testing
}

// Backpressure provides an RSS-based signal that HTTP handlers and
// Temporal activity wrappers can query before accepting new work.
type Backpressure struct {
	cfg    BackpressureConfig
	logger *slog.Logger

	rejections atomic.Int64
}

// NewBackpressure creates a Backpressure instance with resolved defaults.
func NewBackpressure(logger *slog.Logger, cfg BackpressureConfig) *Backpressure {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.HeapCeiling == 0 {
		cfg.HeapCeiling = 4 << 30
	}
	if cfg.WarningThreshold <= 0 {
		cfg.WarningThreshold = 0.6
	}
	if cfg.CriticalThreshold <= 0 {
		cfg.CriticalThreshold = 0.8
	}
	if cfg.EmergencyThreshold <= 0 {
		cfg.EmergencyThreshold = 0.9
	}
	return &Backpressure{cfg: cfg, logger: logger}
}

// Level returns the current backpressure level by sampling heap usage.
func (bp *Backpressure) Level() BackpressureLevel {
	heapInUse := bp.sampleHeap()
	return bp.checkLevel(heapInUse)
}

// AllowNewActivity returns false when pressure is Critical or higher,
// signalling that Temporal activity wrappers should not start new work.
func (bp *Backpressure) AllowNewActivity() bool {
	return bp.Level() < BPCritical
}

// Rejections returns the total number of rejected requests.
func (bp *Backpressure) Rejections() int64 { return bp.rejections.Load() }

// Middleware wraps an HTTP handler with backpressure enforcement.
func (bp *Backpressure) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		level := bp.Level()
		switch {
		case level >= BPEmergency:
			bp.writeRejectResponse(w, 30, "emergency")
		case level >= BPCritical:
			bp.writeRejectResponse(w, 5, "critical")
		case level >= BPWarning:
			w.Header().Set("X-Backpressure", "warning")
			next.ServeHTTP(w, r)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func (bp *Backpressure) sampleHeap() uint64 {
	if bp.cfg.SampleFunc != nil {
		return bp.cfg.SampleFunc()
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapInuse
}

func (bp *Backpressure) checkLevel(heapInUse uint64) BackpressureLevel {
	ratio := float64(heapInUse) / float64(bp.cfg.HeapCeiling)
	switch {
	case ratio >= bp.cfg.EmergencyThreshold:
		return BPEmergency
	case ratio >= bp.cfg.CriticalThreshold:
		return BPCritical
	case ratio >= bp.cfg.WarningThreshold:
		return BPWarning
	default:
		return BPNone
	}
}

func (bp *Backpressure) writeRejectResponse(w http.ResponseWriter, retryAfter int, level string) {
	bp.rejections.Add(1)
	bp.logger.Warn("memwatch.backpressure_reject",
		"level", level,
		"retry_after", retryAfter,
	)
	w.Header().Set("Retry-After", itoa(retryAfter))
	w.Header().Set("X-Backpressure", level)
	w.WriteHeader(http.StatusServiceUnavailable)
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
