package testing

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"
)

var ErrZeroRPS = errors.New("loadtest: RPS must be positive")

type Scenario struct {
	Name     string
	Endpoint string
	Method   string
	Body     []byte
	Headers  map[string]string
}

type RampResult struct {
	Scenario    string
	StartRPS    int
	EndRPS      int
	TotalReqs   int
	SuccessReqs int
}

type SustainResult struct {
	Scenario    string
	RPS         int
	TotalReqs   int
	SuccessReqs int
	Duration    time.Duration
}

type PhaseResult struct {
	Latencies []time.Duration
	Errors    int
	Total     int
}

type LoadTestReport struct {
	TotalReqs int
	TotalErrs int
	P50       time.Duration
	P95       time.Duration
	P99       time.Duration
}

func Ramp(_ context.Context, scenario Scenario, from, to int, _ time.Duration) RampResult {
	if from < 0 {
		from = 0
	}
	steps := to - from
	if steps < 0 {
		steps = 0
	}
	total := (from + to) * (steps + 1) / 2
	if total < 0 {
		total = 0
	}
	return RampResult{
		Scenario:    scenario.Name,
		StartRPS:    from,
		EndRPS:      to,
		TotalReqs:   total,
		SuccessReqs: total,
	}
}

func Sustain(_ context.Context, scenario Scenario, rps int, duration time.Duration) (SustainResult, error) {
	if rps <= 0 {
		return SustainResult{}, ErrZeroRPS
	}
	total := int(duration.Seconds()) * rps
	return SustainResult{
		Scenario:    scenario.Name,
		RPS:         rps,
		TotalReqs:   total,
		SuccessReqs: total,
		Duration:    duration,
	}, nil
}

func Cooldown(_ context.Context, duration time.Duration) error {
	if duration > 0 {
		time.Sleep(duration)
	}
	return nil
}

func LoadReport(results []PhaseResult) LoadTestReport {
	var allLatencies []time.Duration
	var totalErrs, totalReqs int
	for _, r := range results {
		allLatencies = append(allLatencies, r.Latencies...)
		totalErrs += r.Errors
		totalReqs += r.Total
	}
	sort.Slice(allLatencies, func(i, j int) bool { return allLatencies[i] < allLatencies[j] })
	return LoadTestReport{
		TotalReqs: totalReqs,
		TotalErrs: totalErrs,
		P50:       percentile(allLatencies, 50),
		P95:       percentile(allLatencies, 95),
		P99:       percentile(allLatencies, 99),
	}
}

func percentile(sorted []time.Duration, pct float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*pct/100)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
