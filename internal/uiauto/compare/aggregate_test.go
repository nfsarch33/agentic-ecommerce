package compare

import (
	"math"
	"strings"
	"testing"
	"time"
)

func makeResults(ua, pw [][]bool, uaMs, pwMs []int64) []ComparisonResult {
	results := make([]ComparisonResult, len(ua))
	for i := range ua {
		uaChecks := make([]AssertionCheck, len(ua[i]))
		for j, p := range ua[i] {
			uaChecks[j] = AssertionCheck{Passed: p}
		}
		pwChecks := make([]AssertionCheck, len(pw[i]))
		for j, p := range pw[i] {
			pwChecks[j] = AssertionCheck{Passed: p}
		}
		results[i] = ComparisonResult{
			Scenario:         TestScenario{Name: "scenario"},
			UIAutoResult:     ToolResult{DurationMs: uaMs[i], AssertionResults: uaChecks},
			PlaywrightResult: ToolResult{DurationMs: pwMs[i], AssertionResults: pwChecks},
			TimeDelta:        uaMs[i] - pwMs[i],
			AccuracyDelta:    0,
		}
	}
	return results
}

func TestAggregate_AllAgree(t *testing.T) {
	t.Parallel()
	results := makeResults(
		[][]bool{{true, true}, {true, true}},
		[][]bool{{true, true}, {true, true}},
		[]int64{400, 600},
		[]int64{300, 500},
	)
	m := Aggregate(results)
	if m.AgreementRate != 1.0 {
		t.Errorf("agreement rate got %f want 1.0", m.AgreementRate)
	}
	if m.UIAutoAccuracy != 1.0 {
		t.Errorf("uiauto accuracy got %f want 1.0", m.UIAutoAccuracy)
	}
	if m.TotalAssertions != 4 {
		t.Errorf("total assertions got %d want 4", m.TotalAssertions)
	}
}

func TestAggregate_PartialDisagree(t *testing.T) {
	t.Parallel()
	results := makeResults(
		[][]bool{{true, true, true, false, true}},
		[][]bool{{true, true, true, true, true}},
		[]int64{500},
		[]int64{400},
	)
	m := Aggregate(results)
	if m.AgreementRate != 0.8 {
		t.Errorf("agreement rate got %f want 0.8", m.AgreementRate)
	}
	if m.UIAutoAccuracy != 0.8 {
		t.Errorf("uiauto accuracy got %f want 0.8", m.UIAutoAccuracy)
	}
	if m.PlaywrightAccuracy != 1.0 {
		t.Errorf("playwright accuracy got %f want 1.0", m.PlaywrightAccuracy)
	}
}

func TestAggregate_SpeedCalculation(t *testing.T) {
	t.Parallel()
	results := makeResults(
		[][]bool{{true}, {true}},
		[][]bool{{true}, {true}},
		[]int64{400, 600},
		[]int64{200, 400},
	)
	m := Aggregate(results)
	if m.UIAutoSpeedMs != 500 {
		t.Errorf("uiauto speed got %f want 500", m.UIAutoSpeedMs)
	}
	if m.PlaywrightSpeedMs != 300 {
		t.Errorf("playwright speed got %f want 300", m.PlaywrightSpeedMs)
	}
	wantRatio := 500.0 / 300.0
	if math.Abs(m.SpeedRatio-wantRatio) > 0.01 {
		t.Errorf("speed ratio got %f want %f", m.SpeedRatio, wantRatio)
	}
}

func TestAggregate_Empty(t *testing.T) {
	t.Parallel()
	m := Aggregate(nil)
	if m.TotalScenarios != 0 || m.AgreementRate != 0 {
		t.Errorf("empty aggregate should be zero: %+v", m)
	}
}

func TestFormatMarkdownReport(t *testing.T) {
	t.Parallel()
	results := makeResults(
		[][]bool{{true, true}},
		[][]bool{{true, true}},
		[]int64{500},
		[]int64{300},
	)
	m := Aggregate(results)
	genTime := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	md := FormatMarkdownReport(m, results, genTime)
	wantSubstrings := []string{
		"# uiauto vs Playwright Comparison Report",
		"2026-05-11T00:00:00Z",
		"| Agreement rate | 100.0% |",
		"| Speed ratio (ua/pw) |",
		"## Per-Scenario Detail",
		"| scenario |",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(md, s) {
			t.Errorf("markdown missing %q", s)
		}
	}
}
