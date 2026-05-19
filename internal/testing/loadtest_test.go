package testing_test

import (
	"context"
	"testing"
	"time"

	lt "github.com/nfsarch33/helixon-ec/internal/testing"
)

var testScenario = lt.Scenario{Name: "checkout", Endpoint: "/api/checkout", Method: "POST"}

func TestLoadTest_RampIncreasesLoad(t *testing.T) {
	t.Parallel()
	result := lt.Ramp(context.Background(), testScenario, 10, 100, time.Second)
	if result.EndRPS != 100 {
		t.Fatalf("expected end RPS 100, got %d", result.EndRPS)
	}
	if result.StartRPS != 10 {
		t.Fatalf("expected start RPS 10, got %d", result.StartRPS)
	}
}

func TestLoadTest_SustainHoldsSteady(t *testing.T) {
	t.Parallel()
	result, err := lt.Sustain(context.Background(), testScenario, 50, 2*time.Second)
	if err != nil {
		t.Fatalf("sustain failed: %v", err)
	}
	if result.RPS != 50 {
		t.Fatalf("expected 50 RPS, got %d", result.RPS)
	}
}

func TestLoadTest_ZeroRPSError(t *testing.T) {
	t.Parallel()
	_, err := lt.Sustain(context.Background(), testScenario, 0, time.Second)
	if err != lt.ErrZeroRPS {
		t.Fatalf("expected ErrZeroRPS, got %v", err)
	}
}

func TestLoadTest_ReportCalculatesPercentiles(t *testing.T) {
	t.Parallel()
	phases := []lt.PhaseResult{
		{
			Latencies: []time.Duration{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
			Total:     10,
		},
	}
	loadReport := lt.LoadReport(phases)
	if loadReport.P50 == 0 {
		t.Fatal("expected non-zero P50")
	}
	if loadReport.P99 < loadReport.P50 {
		t.Fatal("expected P99 >= P50")
	}
}

func TestLoadTest_ScenarioValidation(t *testing.T) {
	t.Parallel()
	s := lt.Scenario{Name: "test", Endpoint: "/health", Method: "GET"}
	if s.Name == "" || s.Endpoint == "" {
		t.Fatal("expected valid scenario")
	}
}

func TestLoadTest_CooldownWaitsForCompletion(t *testing.T) {
	t.Parallel()
	start := time.Now()
	err := lt.Cooldown(context.Background(), 10*time.Millisecond)
	if err != nil {
		t.Fatalf("cooldown failed: %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Fatal("expected cooldown to wait")
	}
}

func TestLoadTest_ReportEmptyPhases(t *testing.T) {
	t.Parallel()
	report := lt.LoadReport(nil)
	if report.TotalReqs != 0 {
		t.Fatalf("expected 0 total reqs for empty phases, got %d", report.TotalReqs)
	}
}
