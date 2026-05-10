//go:build v4140_uiauto_compare

package compare_scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/uiauto/compare"
)

type stubExecutor struct {
	passRate   float64
	durationMs int64
}

func (s *stubExecutor) Execute(_ context.Context, sc compare.TestScenario) (compare.ToolResult, error) {
	checks := make([]compare.AssertionCheck, len(sc.Assertions))
	passCount := int(float64(len(sc.Assertions)) * s.passRate)
	for i := range checks {
		checks[i] = compare.AssertionCheck{
			Assertion: sc.Assertions[i],
			Passed:    i < passCount,
		}
		if !checks[i].Passed {
			checks[i].Error = "mock: assertion failed"
		}
	}
	return compare.ToolResult{
		DurationMs:       s.durationMs,
		AssertionResults: checks,
	}, nil
}

func stubLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestAllScenarios_Definitions(t *testing.T) {
	t.Parallel()
	scenarios := AllScenarios("http://localhost:3000")
	if len(scenarios) != 5 {
		t.Fatalf("expected 5 scenarios, got %d", len(scenarios))
	}
	names := map[string]bool{}
	for _, sc := range scenarios {
		if sc.Name == "" {
			t.Error("scenario has empty name")
		}
		if sc.URL == "" {
			t.Error("scenario has empty URL")
		}
		if len(sc.Assertions) == 0 {
			t.Errorf("scenario %q has no assertions", sc.Name)
		}
		if names[sc.Name] {
			t.Errorf("duplicate scenario name %q", sc.Name)
		}
		names[sc.Name] = true
	}
}

func TestAllScenarios_RunWithMocks(t *testing.T) {
	t.Parallel()
	ua := &stubExecutor{passRate: 0.95, durationMs: 1200}
	pw := &stubExecutor{passRate: 1.0, durationMs: 800}
	runner := compare.NewComparisonRunner(ua, pw, stubLogger())
	scenarios := AllScenarios("http://localhost:3000")
	results, err := runner.RunAll(context.Background(), scenarios)
	if err != nil {
		t.Fatalf("RunAll error: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
	for _, r := range results {
		if r.UIAutoResult.Tool != "uiauto" {
			t.Errorf("uiauto tool label got %q", r.UIAutoResult.Tool)
		}
		if r.PlaywrightResult.Tool != "playwright" {
			t.Errorf("playwright tool label got %q", r.PlaywrightResult.Tool)
		}
	}
}

func TestAllScenarios_PersistResults(t *testing.T) {
	t.Parallel()
	ua := &stubExecutor{passRate: 1.0, durationMs: 1000}
	pw := &stubExecutor{passRate: 1.0, durationMs: 600}
	runner := compare.NewComparisonRunner(ua, pw, stubLogger())
	scenarios := AllScenarios("http://localhost:3000")
	results, err := runner.RunAll(context.Background(), scenarios)
	if err != nil {
		t.Fatalf("RunAll error: %v", err)
	}
	dir := t.TempDir()
	ts := time.Now().UTC().Format("20060102T150405Z")
	outPath := filepath.Join(dir, fmt.Sprintf("v4140_%s.json", ts))
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var roundTrip []compare.ComparisonResult
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(roundTrip) != 5 {
		t.Errorf("round-trip len got %d want 5", len(roundTrip))
	}
}

func TestAllScenarios_AggregateMetrics(t *testing.T) {
	t.Parallel()
	ua := &stubExecutor{passRate: 1.0, durationMs: 1000}
	pw := &stubExecutor{passRate: 1.0, durationMs: 600}
	runner := compare.NewComparisonRunner(ua, pw, stubLogger())
	scenarios := AllScenarios("http://localhost:3000")
	results, err := runner.RunAll(context.Background(), scenarios)
	if err != nil {
		t.Fatalf("RunAll error: %v", err)
	}
	m := compare.Aggregate(results)
	if m.AgreementRate != 1.0 {
		t.Errorf("agreement rate got %f want 1.0", m.AgreementRate)
	}
	if m.TotalScenarios != 5 {
		t.Errorf("total scenarios got %d want 5", m.TotalScenarios)
	}
	if m.UIAutoSpeedMs != 1000 {
		t.Errorf("uiauto speed got %f want 1000", m.UIAutoSpeedMs)
	}
	if m.PlaywrightSpeedMs != 600 {
		t.Errorf("playwright speed got %f want 600", m.PlaywrightSpeedMs)
	}
}
