package compare

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type mockExecutor struct {
	result ToolResult
	err    error
}

func (m *mockExecutor) Execute(_ context.Context, _ TestScenario) (ToolResult, error) {
	return m.result, m.err
}

func testLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func baseScenario() TestScenario {
	return TestScenario{
		Name: "product-listing",
		URL:  "http://localhost:3000/products",
		Actions: []Action{
			{Type: ActionNavigate, URL: "http://localhost:3000/products"},
		},
		Assertions: []Assertion{
			{Type: ActionAssertText, Selector: "h1", Expected: "Products"},
			{Type: ActionAssertElement, Selector: ".product-card"},
		},
	}
}

func TestComparisonRunner_BothPass(t *testing.T) {
	t.Parallel()
	ua := &mockExecutor{result: ToolResult{
		DurationMs: 500,
		AssertionResults: []AssertionCheck{
			{Passed: true}, {Passed: true},
		},
	}}
	pw := &mockExecutor{result: ToolResult{
		DurationMs: 300,
		AssertionResults: []AssertionCheck{
			{Passed: true}, {Passed: true},
		},
	}}
	runner := NewComparisonRunner(ua, pw, testLogger())
	res, err := runner.Run(context.Background(), baseScenario())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UIAutoResult.PassRate() != 1.0 {
		t.Errorf("uiauto pass rate got %f want 1.0", res.UIAutoResult.PassRate())
	}
	if res.PlaywrightResult.PassRate() != 1.0 {
		t.Errorf("playwright pass rate got %f want 1.0", res.PlaywrightResult.PassRate())
	}
	if res.TimeDelta != 200 {
		t.Errorf("time delta got %d want 200", res.TimeDelta)
	}
	if res.AccuracyDelta != 0 {
		t.Errorf("accuracy delta got %f want 0", res.AccuracyDelta)
	}
}

func TestComparisonRunner_UIAutoFaster(t *testing.T) {
	t.Parallel()
	ua := &mockExecutor{result: ToolResult{
		DurationMs: 200,
		AssertionResults: []AssertionCheck{
			{Passed: true}, {Passed: true},
		},
	}}
	pw := &mockExecutor{result: ToolResult{
		DurationMs: 800,
		AssertionResults: []AssertionCheck{
			{Passed: true}, {Passed: true},
		},
	}}
	runner := NewComparisonRunner(ua, pw, testLogger())
	res, err := runner.Run(context.Background(), baseScenario())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TimeDelta != -600 {
		t.Errorf("time delta got %d want -600 (uiauto faster)", res.TimeDelta)
	}
}

func TestComparisonRunner_PlaywrightFaster(t *testing.T) {
	t.Parallel()
	ua := &mockExecutor{result: ToolResult{
		DurationMs: 1200,
		AssertionResults: []AssertionCheck{
			{Passed: true}, {Passed: true},
		},
	}}
	pw := &mockExecutor{result: ToolResult{
		DurationMs: 400,
		AssertionResults: []AssertionCheck{
			{Passed: true}, {Passed: true},
		},
	}}
	runner := NewComparisonRunner(ua, pw, testLogger())
	res, err := runner.Run(context.Background(), baseScenario())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TimeDelta != 800 {
		t.Errorf("time delta got %d want 800 (playwright faster)", res.TimeDelta)
	}
}

func TestComparisonRunner_OneFailsOtherPasses(t *testing.T) {
	t.Parallel()
	ua := &mockExecutor{result: ToolResult{
		DurationMs: 500,
		AssertionResults: []AssertionCheck{
			{Passed: true}, {Passed: false, Error: "selector not found"},
		},
	}}
	pw := &mockExecutor{result: ToolResult{
		DurationMs: 300,
		AssertionResults: []AssertionCheck{
			{Passed: true}, {Passed: true},
		},
	}}
	runner := NewComparisonRunner(ua, pw, testLogger())
	res, err := runner.Run(context.Background(), baseScenario())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UIAutoResult.PassRate() != 0.5 {
		t.Errorf("uiauto pass rate got %f want 0.5", res.UIAutoResult.PassRate())
	}
	if res.PlaywrightResult.PassRate() != 1.0 {
		t.Errorf("playwright pass rate got %f want 1.0", res.PlaywrightResult.PassRate())
	}
	if res.AccuracyDelta != -0.5 {
		t.Errorf("accuracy delta got %f want -0.5", res.AccuracyDelta)
	}
}

func TestComparisonRunner_BothFail(t *testing.T) {
	t.Parallel()
	ua := &mockExecutor{
		result: ToolResult{DurationMs: 100},
		err:    errors.New("bridge unavailable"),
	}
	pw := &mockExecutor{
		result: ToolResult{DurationMs: 100},
		err:    errors.New("chromium crashed"),
	}
	runner := NewComparisonRunner(ua, pw, testLogger())
	res, err := runner.Run(context.Background(), baseScenario())
	if err != nil {
		t.Fatalf("Run should not error on tool failures: %v", err)
	}
	if res.UIAutoResult.Error != "bridge unavailable" {
		t.Errorf("uiauto error got %q", res.UIAutoResult.Error)
	}
	if res.PlaywrightResult.Error != "chromium crashed" {
		t.Errorf("playwright error got %q", res.PlaywrightResult.Error)
	}
}

func TestComparisonRunner_RunAll(t *testing.T) {
	t.Parallel()
	ua := &mockExecutor{result: ToolResult{
		DurationMs:       300,
		AssertionResults: []AssertionCheck{{Passed: true}},
	}}
	pw := &mockExecutor{result: ToolResult{
		DurationMs:       200,
		AssertionResults: []AssertionCheck{{Passed: true}},
	}}
	runner := NewComparisonRunner(ua, pw, testLogger())
	scenarios := []TestScenario{baseScenario(), baseScenario()}
	scenarios[1].Name = "onboarding"
	results, err := runner.RunAll(context.Background(), scenarios)
	if err != nil {
		t.Fatalf("RunAll error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results len got %d want 2", len(results))
	}
}

func TestToolResult_PassRate_Empty(t *testing.T) {
	t.Parallel()
	tr := ToolResult{}
	if tr.PassRate() != 0 {
		t.Errorf("empty pass rate got %f want 0", tr.PassRate())
	}
}
