package health

import (
	"sync"
	"time"
)

type Status string

const (
	StatusUp       Status = "up"
	StatusDown     Status = "down"
	StatusDegraded Status = "degraded"
)

type LatencyRecord struct {
	samples []time.Duration
	mu      sync.Mutex
}

func (l *LatencyRecord) Record(d time.Duration) {
	l.mu.Lock()
	l.samples = append(l.samples, d)
	l.mu.Unlock()
}

func (l *LatencyRecord) P50() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.samples) == 0 {
		return 0
	}
	return l.samples[len(l.samples)/2]
}

type Dashboard struct {
	mu         sync.RWMutex
	components map[string]Status
	latency    map[string]*LatencyRecord
}

func NewDashboard() *Dashboard {
	return &Dashboard{
		components: make(map[string]Status),
		latency:    make(map[string]*LatencyRecord),
	}
}

func (d *Dashboard) SetComponent(name string, s Status) {
	d.mu.Lock()
	d.components[name] = s
	d.mu.Unlock()
}

func (d *Dashboard) ComponentStatus(name string) Status {
	d.mu.RLock()
	defer d.mu.RUnlock()
	s, ok := d.components[name]
	if !ok {
		return StatusDown
	}
	return s
}

func (d *Dashboard) DependencyCheck() map[string]Status {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make(map[string]Status, len(d.components))
	for k, v := range d.components {
		out[k] = v
	}
	return out
}

func (d *Dashboard) RecordLatency(endpoint string, dur time.Duration) {
	d.mu.Lock()
	if d.latency[endpoint] == nil {
		d.latency[endpoint] = &LatencyRecord{}
	}
	rec := d.latency[endpoint]
	d.mu.Unlock()
	rec.Record(dur)
}

func (d *Dashboard) LatencyMetrics() map[string]time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make(map[string]time.Duration)
	for k, rec := range d.latency {
		out[k] = rec.P50()
	}
	return out
}
