package memwatch

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func bpLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestBackpressureLevel_NonePassesThrough(t *testing.T) {
	t.Parallel()
	bp := NewBackpressure(bpLogger(), BackpressureConfig{
		HeapCeiling: 1 << 30,
		SampleFunc:  func() uint64 { return 400 << 20 }, // 40% => None
	})
	if bp.Level() != BPNone {
		t.Fatalf("level=%d, want BPNone(0)", bp.Level())
	}

	handler := bp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Backpressure") != "" {
		t.Fatal("unexpected X-Backpressure header at None level")
	}
}

func TestBackpressureLevel_WarningAddsHeader(t *testing.T) {
	t.Parallel()
	bp := NewBackpressure(bpLogger(), BackpressureConfig{
		HeapCeiling: 1 << 30,
		SampleFunc:  func() uint64 { return 650 << 20 }, // 63.5% => Warning
	})
	if bp.Level() != BPWarning {
		t.Fatalf("level=%d, want BPWarning(1)", bp.Level())
	}

	handler := bp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Backpressure") != "warning" {
		t.Fatalf("X-Backpressure=%q, want 'warning'", rec.Header().Get("X-Backpressure"))
	}
}

func TestBackpressureLevel_CriticalReturns503(t *testing.T) {
	t.Parallel()
	bp := NewBackpressure(bpLogger(), BackpressureConfig{
		HeapCeiling: 1 << 30,
		SampleFunc:  func() uint64 { return 850 << 20 }, // 83% => Critical
	})
	if bp.Level() != BPCritical {
		t.Fatalf("level=%d, want BPCritical(2)", bp.Level())
	}

	handler := bp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", rec.Code)
	}
	ra := rec.Header().Get("Retry-After")
	if ra != "5" {
		t.Fatalf("Retry-After=%q, want '5'", ra)
	}
}

func TestBackpressureLevel_EmergencyReturns503LongRetry(t *testing.T) {
	t.Parallel()
	bp := NewBackpressure(bpLogger(), BackpressureConfig{
		HeapCeiling: 1 << 30,
		SampleFunc:  func() uint64 { return 950 << 20 }, // 92.8% => Emergency
	})
	if bp.Level() != BPEmergency {
		t.Fatalf("level=%d, want BPEmergency(3)", bp.Level())
	}

	handler := bp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", rec.Code)
	}
	ra, _ := strconv.Atoi(rec.Header().Get("Retry-After"))
	if ra != 30 {
		t.Fatalf("Retry-After=%d, want 30", ra)
	}
}

func TestBackpressure_TemporalActivityBlocked(t *testing.T) {
	t.Parallel()
	bp := NewBackpressure(bpLogger(), BackpressureConfig{
		HeapCeiling: 1 << 30,
		SampleFunc:  func() uint64 { return 850 << 20 }, // Critical
	})
	if bp.AllowNewActivity() {
		t.Fatal("should block new activities at Critical level")
	}
}

func TestBackpressure_RecoveryAfterPressureDrops(t *testing.T) {
	t.Parallel()
	var heapVal uint64 = 850 << 20 // start Critical
	bp := NewBackpressure(bpLogger(), BackpressureConfig{
		HeapCeiling: 1 << 30,
		SampleFunc:  func() uint64 { return heapVal },
	})
	if bp.Level() != BPCritical {
		t.Fatalf("level=%d, want BPCritical(2)", bp.Level())
	}
	if bp.AllowNewActivity() {
		t.Fatal("should block at Critical")
	}

	heapVal = 300 << 20 // drop to None
	if bp.Level() != BPNone {
		t.Fatalf("level=%d after recovery, want BPNone(0)", bp.Level())
	}
	if !bp.AllowNewActivity() {
		t.Fatal("should allow activities after recovery")
	}

	handler := bp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d after recovery, want 200", rec.Code)
	}
}
