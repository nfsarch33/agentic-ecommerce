package healthv2

import (
	"context"
	"sync"
	"time"
)

// Health status constants.
const (
	StatusHealthy   = "healthy"
	StatusDegraded  = "degraded"
	StatusUnhealthy = "unhealthy"
)

// Check holds the result of a single health check.
type Check struct {
	Name    string
	Status  string
	Latency time.Duration
	Error   string
}

// Checker is the interface implemented by health check sources.
type Checker interface {
	Check(ctx context.Context) Check
}

// HealthReport is the aggregated health output.
type HealthReport struct {
	Status      string
	Checks      []Check
	GeneratedAt time.Time
}

// Aggregator collects and runs health checks.
type Aggregator struct {
	mu       sync.RWMutex
	checkers map[string]Checker
}

// NewAggregator returns an initialised Aggregator.
func NewAggregator() *Aggregator {
	return &Aggregator{checkers: make(map[string]Checker)}
}

// Register adds a named checker.
func (a *Aggregator) Register(name string, c Checker) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checkers[name] = c
}

// RunAll executes all registered checkers concurrently and returns an aggregated report.
func (a *Aggregator) RunAll(ctx context.Context) HealthReport {
	a.mu.RLock()
	names := make([]string, 0, len(a.checkers))
	checkers := make([]Checker, 0, len(a.checkers))
	for n, c := range a.checkers {
		names = append(names, n)
		checkers = append(checkers, c)
	}
	a.mu.RUnlock()

	type result struct {
		idx   int
		check Check
	}

	results := make(chan result, len(names))
	for i, c := range checkers {
		i, c := i, c
		go func() {
			start := time.Now()
			ch := c.Check(ctx)
			ch.Latency = time.Since(start)
			if ch.Name == "" {
				ch.Name = names[i]
			}
			results <- result{idx: i, check: ch}
		}()
	}

	checks := make([]Check, len(names))
	for range names {
		r := <-results
		checks[r.idx] = r.check
	}

	overall := StatusHealthy
	for _, ch := range checks {
		switch ch.Status {
		case StatusUnhealthy:
			overall = StatusUnhealthy
		case StatusDegraded:
			if overall != StatusUnhealthy {
				overall = StatusDegraded
			}
		}
	}

	return HealthReport{
		Status:      overall,
		Checks:      checks,
		GeneratedAt: time.Now(),
	}
}

// SLATracker tracks health check status history for uptime calculation.
type SLATracker struct {
	mu      sync.Mutex
	records map[string][]slaRecord
}

type slaRecord struct {
	status string
	at     time.Time
}

// NewSLATracker returns an initialised SLATracker.
func NewSLATracker() *SLATracker {
	return &SLATracker{records: make(map[string][]slaRecord)}
}

// Record appends a status observation for a check.
func (s *SLATracker) Record(checkName, status string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[checkName] = append(s.records[checkName], slaRecord{status: status, at: at})
}

// UptimePct returns the percentage of healthy observations for the named check.
func (s *SLATracker) UptimePct(checkName string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs := s.records[checkName]
	if len(recs) == 0 {
		return 100.0
	}
	healthy := 0
	for _, r := range recs {
		if r.status == StatusHealthy {
			healthy++
		}
	}
	return float64(healthy) / float64(len(recs)) * 100.0
}
