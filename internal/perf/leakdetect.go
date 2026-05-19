package perf

import (
	"errors"
	"sync"
	"time"
)

var ErrLabelNotFound = errors.New("leakdetect: label not found")

type allocRecord struct {
	label     string
	size      int
	recordedAt time.Time
}

type GrowthReport struct {
	Label   string
	Initial int
	Current int
	Growth  int
	IsLeak  bool
}

type LeakReport struct {
	Goroutine GoroutineLeak_
	Allocs    []GrowthReport
}

type GoroutineLeak_ struct {
	Baseline int
	Current  int
	IsLeak   bool
}

type LeakDetector struct {
	mu      sync.Mutex
	records map[string][]allocRecord
}

func NewLeakDetector() *LeakDetector {
	return &LeakDetector{records: make(map[string][]allocRecord)}
}

func (d *LeakDetector) TrackAlloc(label string, size int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.records[label] = append(d.records[label], allocRecord{
		label:      label,
		size:       size,
		recordedAt: time.Now(),
	})
}

func (d *LeakDetector) DetectGrowth(label string, window time.Duration) (GrowthReport, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	recs, ok := d.records[label]
	if !ok || len(recs) == 0 {
		return GrowthReport{}, ErrLabelNotFound
	}
	cutoff := time.Now().Add(-window)
	var inWindow []allocRecord
	for _, r := range recs {
		if r.recordedAt.After(cutoff) {
			inWindow = append(inWindow, r)
		}
	}
	if len(inWindow) < 2 {
		return GrowthReport{Label: label, IsLeak: false}, nil
	}
	initial := inWindow[0].size
	current := inWindow[len(inWindow)-1].size
	growth := current - initial
	return GrowthReport{
		Label:   label,
		Initial: initial,
		Current: current,
		Growth:  growth,
		IsLeak:  growth > 0,
	}, nil
}

func GoroutineLeak(baseline, current int) bool {
	return current > baseline*2
}

func (d *LeakDetector) Report(_ interface{}, baseline, current int) LeakReport {
	d.mu.Lock()
	defer d.mu.Unlock()
	var allocs []GrowthReport
	for label, recs := range d.records {
		if len(recs) >= 2 {
			first := recs[0].size
			last := recs[len(recs)-1].size
			allocs = append(allocs, GrowthReport{
				Label:   label,
				Initial: first,
				Current: last,
				Growth:  last - first,
				IsLeak:  last > first,
			})
		}
	}
	return LeakReport{
		Goroutine: GoroutineLeak_{
			Baseline: baseline,
			Current:  current,
			IsLeak:   GoroutineLeak(baseline, current),
		},
		Allocs: allocs,
	}
}
