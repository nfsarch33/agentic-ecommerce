package compare

import (
	"fmt"
	"strings"
	"time"
)

// AggregateMetrics holds the computed metrics across a batch of
// ComparisonResult values. Corresponds to the v4.14.0 story 2
// metric definitions.
type AggregateMetrics struct {
	AgreementRate      float64 `json:"accuracy_agreement_rate"`
	UIAutoAccuracy     float64 `json:"uiauto_accuracy"`
	PlaywrightAccuracy float64 `json:"playwright_accuracy"`
	UIAutoSpeedMs      float64 `json:"uiauto_speed_ms"`
	PlaywrightSpeedMs  float64 `json:"playwright_speed_ms"`
	SpeedRatio         float64 `json:"speed_ratio"`
	TotalScenarios     int     `json:"total_scenarios"`
	TotalAssertions    int     `json:"total_assertions"`
}

// Aggregate computes comparison metrics from a set of results.
// Decomposed into computeAccuracy + computeSpeed per the plan.
func Aggregate(results []ComparisonResult) AggregateMetrics {
	m := AggregateMetrics{TotalScenarios: len(results)}
	if len(results) == 0 {
		return m
	}
	m.AgreementRate, m.UIAutoAccuracy, m.PlaywrightAccuracy, m.TotalAssertions = computeAccuracy(results)
	m.UIAutoSpeedMs, m.PlaywrightSpeedMs, m.SpeedRatio = computeSpeed(results)
	return m
}

func computeAccuracy(results []ComparisonResult) (agreement, uaAcc, pwAcc float64, total int) {
	var agreed, uaPassed, pwPassed int
	for _, r := range results {
		for i := range r.UIAutoResult.AssertionResults {
			total++
			uaOK := r.UIAutoResult.AssertionResults[i].Passed
			pwOK := i < len(r.PlaywrightResult.AssertionResults) &&
				r.PlaywrightResult.AssertionResults[i].Passed
			if uaOK == pwOK {
				agreed++
			}
			if uaOK {
				uaPassed++
			}
			if pwOK {
				pwPassed++
			}
		}
	}
	if total == 0 {
		return 0, 0, 0, 0
	}
	return float64(agreed) / float64(total),
		float64(uaPassed) / float64(total),
		float64(pwPassed) / float64(total),
		total
}

func computeSpeed(results []ComparisonResult) (uaAvg, pwAvg, ratio float64) {
	if len(results) == 0 {
		return 0, 0, 0
	}
	var uaSum, pwSum int64
	for _, r := range results {
		uaSum += r.UIAutoResult.DurationMs
		pwSum += r.PlaywrightResult.DurationMs
	}
	n := float64(len(results))
	uaAvg = float64(uaSum) / n
	pwAvg = float64(pwSum) / n
	if pwAvg > 0 {
		ratio = uaAvg / pwAvg
	}
	return uaAvg, pwAvg, ratio
}

// FormatMarkdownReport produces the markdown summary report.
func FormatMarkdownReport(
	m AggregateMetrics,
	results []ComparisonResult,
	genTime time.Time,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# uiauto vs Playwright Comparison Report\n\n")
	fmt.Fprintf(&b, "Generated: %s\n\n", genTime.UTC().Format(time.RFC3339))
	fmt.Fprintln(&b, "## Aggregate Metrics")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Metric | Value |")
	fmt.Fprintln(&b, "|---|---:|")
	fmt.Fprintf(&b, "| Total scenarios | %d |\n", m.TotalScenarios)
	fmt.Fprintf(&b, "| Total assertions | %d |\n", m.TotalAssertions)
	fmt.Fprintf(&b, "| Agreement rate | %.1f%% |\n", m.AgreementRate*100)
	fmt.Fprintf(&b, "| uiauto accuracy | %.1f%% |\n", m.UIAutoAccuracy*100)
	fmt.Fprintf(&b, "| Playwright accuracy | %.1f%% |\n", m.PlaywrightAccuracy*100)
	fmt.Fprintf(&b, "| uiauto avg speed | %.0fms |\n", m.UIAutoSpeedMs)
	fmt.Fprintf(&b, "| Playwright avg speed | %.0fms |\n", m.PlaywrightSpeedMs)
	fmt.Fprintf(&b, "| Speed ratio (ua/pw) | %.2f |\n\n", m.SpeedRatio)
	formatPerScenarioTable(&b, results)
	return b.String()
}

func formatPerScenarioTable(b *strings.Builder, results []ComparisonResult) {
	fmt.Fprintln(b, "## Per-Scenario Detail")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "| Scenario | uiauto (ms) | Playwright (ms) | uiauto pass% | PW pass% | Delta (ms) |")
	fmt.Fprintln(b, "|---|---:|---:|---:|---:|---:|")
	for _, r := range results {
		fmt.Fprintf(b, "| %s | %d | %d | %.0f%% | %.0f%% | %d |\n",
			r.Scenario.Name,
			r.UIAutoResult.DurationMs,
			r.PlaywrightResult.DurationMs,
			r.UIAutoResult.PassRate()*100,
			r.PlaywrightResult.PassRate()*100,
			r.TimeDelta,
		)
	}
}
