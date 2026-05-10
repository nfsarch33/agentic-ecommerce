//go:build v4141_smoke

package v4141

import (
	"context"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/uiauto/compare"
)

type mockToolExecutor struct {
	results    map[string]compare.ToolResult
	defaultRes compare.ToolResult
}

func (m *mockToolExecutor) Execute(_ context.Context, sc compare.TestScenario) (compare.ToolResult, error) {
	if res, ok := m.results[sc.Name]; ok {
		return res, nil
	}
	return m.defaultRes, nil
}

func testLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func allPassChecks(n int) []compare.AssertionCheck {
	checks := make([]compare.AssertionCheck, n)
	for i := range checks {
		checks[i] = compare.AssertionCheck{Passed: true}
	}
	return checks
}

func mixedChecks(pass, fail int) []compare.AssertionCheck {
	checks := make([]compare.AssertionCheck, pass+fail)
	for i := range checks {
		checks[i] = compare.AssertionCheck{Passed: i < pass}
		if !checks[i].Passed {
			checks[i].Error = "mock assertion failed"
		}
	}
	return checks
}

func TestE2E_BothAgree100Percent(t *testing.T) {
	t.Parallel()
	scenarios := makeScenarios(3)
	ua := &mockToolExecutor{defaultRes: compare.ToolResult{
		DurationMs:       800,
		AssertionResults: allPassChecks(3),
	}}
	pw := &mockToolExecutor{defaultRes: compare.ToolResult{
		DurationMs:       500,
		AssertionResults: allPassChecks(3),
	}}
	runner := compare.NewComparisonRunner(ua, pw, testLogger())
	results, err := runner.RunAll(context.Background(), scenarios)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	m := compare.Aggregate(results)
	if m.AgreementRate != 1.0 {
		t.Errorf("agreement rate got %f want 1.0", m.AgreementRate)
	}
	if m.UIAutoAccuracy != 1.0 {
		t.Errorf("uiauto accuracy got %f want 1.0", m.UIAutoAccuracy)
	}
	if m.PlaywrightAccuracy != 1.0 {
		t.Errorf("playwright accuracy got %f want 1.0", m.PlaywrightAccuracy)
	}
}

func TestE2E_ToolsDisagreeOn1of5(t *testing.T) {
	t.Parallel()
	scenarios := makeScenarios(5)
	uaResults := map[string]compare.ToolResult{
		"scenario-3": {DurationMs: 900, AssertionResults: mixedChecks(0, 1)},
	}
	ua := &mockToolExecutor{
		results:    uaResults,
		defaultRes: compare.ToolResult{DurationMs: 800, AssertionResults: allPassChecks(1)},
	}
	pw := &mockToolExecutor{defaultRes: compare.ToolResult{
		DurationMs:       500,
		AssertionResults: allPassChecks(1),
	}}
	runner := compare.NewComparisonRunner(ua, pw, testLogger())
	results, err := runner.RunAll(context.Background(), scenarios)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	m := compare.Aggregate(results)
	wantAgreement := 0.8
	if math.Abs(m.AgreementRate-wantAgreement) > 0.01 {
		t.Errorf("agreement rate got %f want %f", m.AgreementRate, wantAgreement)
	}
}

func TestE2E_TimingCaptured(t *testing.T) {
	t.Parallel()
	scenarios := makeScenarios(2)
	ua := &mockToolExecutor{defaultRes: compare.ToolResult{
		DurationMs:       1200,
		AssertionResults: allPassChecks(1),
	}}
	pw := &mockToolExecutor{defaultRes: compare.ToolResult{
		DurationMs:       600,
		AssertionResults: allPassChecks(1),
	}}
	runner := compare.NewComparisonRunner(ua, pw, testLogger())
	results, err := runner.RunAll(context.Background(), scenarios)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	m := compare.Aggregate(results)
	if m.UIAutoSpeedMs != 1200 {
		t.Errorf("uiauto speed got %f want 1200", m.UIAutoSpeedMs)
	}
	if m.PlaywrightSpeedMs != 600 {
		t.Errorf("playwright speed got %f want 600", m.PlaywrightSpeedMs)
	}
	wantRatio := 2.0
	if math.Abs(m.SpeedRatio-wantRatio) > 0.01 {
		t.Errorf("speed ratio got %f want %f", m.SpeedRatio, wantRatio)
	}
	md := compare.FormatMarkdownReport(m, results, time.Now().UTC())
	if md == "" {
		t.Error("markdown report is empty")
	}
}

func makeScenarios(n int) []compare.TestScenario {
	scenarios := make([]compare.TestScenario, n)
	for i := range scenarios {
		scenarios[i] = compare.TestScenario{
			Name: "scenario-" + string(rune('0'+i)),
			URL:  "http://localhost:3000/test",
			Assertions: []compare.Assertion{
				{Type: compare.ActionAssertText, Selector: "h1", Expected: "Test"},
			},
		}
	}
	return scenarios
}
