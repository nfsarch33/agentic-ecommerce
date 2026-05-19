package anomalydetect

import (
	"math"
	"sync"
	"time"
)

// Metric represents a single measurement for an entity.
type Metric struct {
	Name       string
	EntityID   string
	Value      float64
	OccurredAt time.Time
}

// Baseline holds the statistical summary of a metric series.
type Baseline struct {
	Mean       float64
	StdDev     float64
	WindowSize int
}

// BaselineCalculator computes baselines from metric slices.
type BaselineCalculator struct{}

// Calculate returns the mean and population standard deviation of the given metrics.
func (BaselineCalculator) Calculate(metrics []Metric) Baseline {
	n := len(metrics)
	if n == 0 {
		return Baseline{}
	}

	var sum float64
	for _, m := range metrics {
		sum += m.Value
	}
	mean := sum / float64(n)

	var variance float64
	for _, m := range metrics {
		diff := m.Value - mean
		variance += diff * diff
	}
	variance /= float64(n)

	return Baseline{
		Mean:       mean,
		StdDev:     math.Sqrt(variance),
		WindowSize: n,
	}
}

// ZScoreDetector detects anomalies using the z-score method.
type ZScoreDetector struct {
	// Threshold is the z-score above which a metric is considered anomalous.
	// Defaults to 3.0 when zero.
	Threshold float64
}

// Detect returns whether the metric is anomalous relative to the baseline.
func (z ZScoreDetector) Detect(metric Metric, baseline Baseline) (isAnomaly bool, zScore float64, threshold float64) {
	threshold = z.Threshold
	if threshold == 0 {
		threshold = 3.0
	}

	if baseline.StdDev == 0 {
		return false, 0, threshold
	}

	zScore = math.Abs(metric.Value-baseline.Mean) / baseline.StdDev
	isAnomaly = zScore > threshold
	return isAnomaly, zScore, threshold
}

// AlertRule defines a threshold-based alert for a metric.
type AlertRule struct {
	MetricName string
	EntityID   string
	Threshold  float64
}

// AlertManager manages alert rules and fires alerts when thresholds are breached.
type AlertManager struct {
	mu    sync.RWMutex
	rules []AlertRule
}

// AddRule registers an alert rule.
func (a *AlertManager) AddRule(rule AlertRule) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rules = append(a.rules, rule)
}

// Check returns all rules that fire for the given metric.
func (a *AlertManager) Check(metric Metric) []AlertRule {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var fired []AlertRule
	for _, rule := range a.rules {
		if rule.MetricName != metric.Name {
			continue
		}
		if rule.EntityID != "" && rule.EntityID != metric.EntityID {
			continue
		}
		if metric.Value >= rule.Threshold {
			fired = append(fired, rule)
		}
	}
	return fired
}
