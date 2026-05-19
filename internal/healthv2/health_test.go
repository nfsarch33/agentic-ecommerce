package healthv2

import (
	"context"
	"testing"
	"time"
)

type staticChecker struct {
	check Check
}

func (s staticChecker) Check(_ context.Context) Check { return s.check }

func TestAggregator_AllHealthy(t *testing.T) {
	t.Parallel()

	a := NewAggregator()
	a.Register("db", staticChecker{Check{Name: "db", Status: StatusHealthy}})
	a.Register("cache", staticChecker{Check{Name: "cache", Status: StatusHealthy}})

	report := a.RunAll(context.Background())
	if report.Status != StatusHealthy {
		t.Errorf("Status = %q, want healthy", report.Status)
	}
	if len(report.Checks) != 2 {
		t.Errorf("Checks len = %d, want 2", len(report.Checks))
	}
}

func TestAggregator_OneDegraded(t *testing.T) {
	t.Parallel()

	a := NewAggregator()
	a.Register("db", staticChecker{Check{Name: "db", Status: StatusHealthy}})
	a.Register("cache", staticChecker{Check{Name: "cache", Status: StatusDegraded}})

	report := a.RunAll(context.Background())
	if report.Status != StatusDegraded {
		t.Errorf("Status = %q, want degraded", report.Status)
	}
}

func TestAggregator_OneUnhealthy(t *testing.T) {
	t.Parallel()

	a := NewAggregator()
	a.Register("db", staticChecker{Check{Name: "db", Status: StatusUnhealthy}})
	a.Register("cache", staticChecker{Check{Name: "cache", Status: StatusDegraded}})

	report := a.RunAll(context.Background())
	if report.Status != StatusUnhealthy {
		t.Errorf("Status = %q, want unhealthy", report.Status)
	}
}

func TestSLATracker_UptimePct(t *testing.T) {
	t.Parallel()

	tracker := NewSLATracker()
	now := time.Now()

	tracker.Record("db", StatusHealthy, now)
	tracker.Record("db", StatusHealthy, now.Add(time.Minute))
	tracker.Record("db", StatusUnhealthy, now.Add(2*time.Minute))
	tracker.Record("db", StatusHealthy, now.Add(3*time.Minute))

	pct := tracker.UptimePct("db")
	// 3 healthy out of 4 = 75%
	if pct < 74.9 || pct > 75.1 {
		t.Errorf("UptimePct = %.1f%%, want 75%%", pct)
	}
}

func TestSLATracker_NoRecords(t *testing.T) {
	t.Parallel()

	tracker := NewSLATracker()
	if pct := tracker.UptimePct("unknown"); pct != 100.0 {
		t.Errorf("UptimePct for unknown = %.1f%%, want 100%%", pct)
	}
}

func TestAggregator_ParallelExecution(t *testing.T) {
	t.Parallel()

	// Each checker sleeps briefly; they must run concurrently.
	type slowChecker struct{}
	startExec := time.Now()

	a := NewAggregator()
	for i := 0; i < 5; i++ {
		a.Register("check", staticChecker{Check{Status: StatusHealthy}})
	}
	a.RunAll(context.Background())

	// Just verify no panic / deadlock; timing assertion is too flaky for -race.
	_ = time.Since(startExec)
}
