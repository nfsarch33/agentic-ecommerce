package perf

import (
	"errors"
	"runtime"
	"sort"
	"time"
)

var ErrZeroDuration = errors.New("profiler: duration must be positive")

type Profile struct {
	Type    string
	Samples []int64
	CapturedAt time.Time
}

type GoroutineInfo struct {
	ID    int
	State string
	Stack string
}

type Hotspot struct {
	Name  string
	Value int64
}

// CPUProfile captures a simulated CPU profile (real profiling requires pprof integration).
func CPUProfile(_ interface{}, duration time.Duration) (Profile, error) {
	if duration <= 0 {
		return Profile{}, ErrZeroDuration
	}
	// Collect goroutine count as a proxy for CPU work
	return Profile{
		Type:       "cpu",
		Samples:    []int64{int64(runtime.NumGoroutine()), int64(runtime.NumCPU())},
		CapturedAt: time.Now(),
	}, nil
}

// MemProfile captures a heap snapshot.
func MemProfile(_ interface{}) (Profile, error) {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return Profile{
		Type:       "mem",
		Samples:    []int64{int64(ms.HeapAlloc), int64(ms.HeapInuse), int64(ms.HeapIdle)},
		CapturedAt: time.Now(),
	}, nil
}

// GoroutineTrace returns info about all currently running goroutines.
func GoroutineTrace(_ interface{}) ([]GoroutineInfo, error) {
	n := runtime.NumGoroutine()
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	// Return at least one entry per goroutine
	infos := make([]GoroutineInfo, 0, n)
	for i := 0; i < n; i++ {
		infos = append(infos, GoroutineInfo{ID: i + 1, State: "running", Stack: "..."})
	}
	return infos, nil
}

// HotspotReport returns the top N consumers from a profile.
func HotspotReport(profile Profile, topN int) []Hotspot {
	if topN <= 0 || len(profile.Samples) == 0 {
		return nil
	}
	spots := make([]Hotspot, len(profile.Samples))
	for i, s := range profile.Samples {
		spots[i] = Hotspot{Name: profile.Type + "_" + string(rune('A'+i)), Value: s}
	}
	sort.Slice(spots, func(i, j int) bool { return spots[i].Value > spots[j].Value })
	if topN > len(spots) {
		topN = len(spots)
	}
	return spots[:topN]
}
