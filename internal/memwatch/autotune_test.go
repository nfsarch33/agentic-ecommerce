package memwatch

import (
	"io"
	"log/slog"
	"testing"
)

func atLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestAutoTune_DetectsSystemMemory(t *testing.T) {
	t.Parallel()
	at := NewAutoTuner(atLogger(), AutoTuneConfig{
		DetectMemFunc: func() uint64 { return 8 << 30 }, // 8 GiB
	})
	result := at.Tune()
	eightGiB := uint64(8) << 30
	want := uint64(float64(eightGiB) * 0.7)
	tolerance := uint64(100 << 20) // 100 MiB tolerance
	if result.HeapCeiling < want-tolerance || result.HeapCeiling > want+tolerance {
		t.Fatalf("HeapCeiling=%d, want ~%d (70%% of 8GiB)", result.HeapCeiling, want)
	}
	if result.GoroutineCeiling <= 0 {
		t.Fatalf("GoroutineCeiling=%d, want >0", result.GoroutineCeiling)
	}
}

func TestAutoTune_OverrideTakesPrecedence(t *testing.T) {
	t.Parallel()
	at := NewAutoTuner(atLogger(), AutoTuneConfig{
		DetectMemFunc:         func() uint64 { return 8 << 30 },
		HeapCeilingOverride:   2 << 30, // 2 GiB
		GoroutineCeilOverride: 10_000,
	})
	result := at.Tune()
	if result.HeapCeiling != 2<<30 {
		t.Fatalf("HeapCeiling=%d, want 2GiB override", result.HeapCeiling)
	}
	if result.GoroutineCeiling != 10_000 {
		t.Fatalf("GoroutineCeiling=%d, want 10000 override", result.GoroutineCeiling)
	}
}

func TestAutoTune_ReEvaluationAdjusts(t *testing.T) {
	t.Parallel()
	callCount := 0
	at := NewAutoTuner(atLogger(), AutoTuneConfig{
		DetectMemFunc: func() uint64 {
			callCount++
			if callCount <= 1 {
				return 8 << 30 // 8 GiB first call
			}
			return 16 << 30 // 16 GiB second call (memory freed by others)
		},
	})

	first := at.Tune()
	second := at.Tune()
	if second.HeapCeiling <= first.HeapCeiling {
		t.Fatalf("re-evaluation didn't adjust: first=%d, second=%d", first.HeapCeiling, second.HeapCeiling)
	}
}

func TestAutoTune_MinimumFloorEnforced(t *testing.T) {
	t.Parallel()
	at := NewAutoTuner(atLogger(), AutoTuneConfig{
		DetectMemFunc: func() uint64 { return 128 << 20 }, // 128 MiB (tiny)
	})
	result := at.Tune()
	if result.HeapCeiling < MinHeapCeiling {
		t.Fatalf("HeapCeiling=%d below min floor %d", result.HeapCeiling, MinHeapCeiling)
	}
	if result.GoroutineCeiling < MinGoroutineCeiling {
		t.Fatalf("GoroutineCeiling=%d below min floor %d", result.GoroutineCeiling, MinGoroutineCeiling)
	}
}
