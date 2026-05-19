package anomalydetect

import (
	"math"
	"testing"
	"time"
)

func TestBaselineCalculator_Calculate(t *testing.T) {
	t.Parallel()

	calc := BaselineCalculator{}
	now := time.Now()

	metrics := []Metric{
		{Name: "orders", EntityID: "store1", Value: 10, OccurredAt: now},
		{Name: "orders", EntityID: "store1", Value: 20, OccurredAt: now},
		{Name: "orders", EntityID: "store1", Value: 30, OccurredAt: now},
	}

	b := calc.Calculate(metrics)

	wantMean := 20.0
	if math.Abs(b.Mean-wantMean) > 1e-9 {
		t.Errorf("Mean = %f, want %f", b.Mean, wantMean)
	}

	// Population stddev of {10,20,30}: variance = ((10-20)^2+(20-20)^2+(30-20)^2)/3 = 200/3
	wantStdDev := math.Sqrt(200.0 / 3.0)
	if math.Abs(b.StdDev-wantStdDev) > 1e-9 {
		t.Errorf("StdDev = %f, want %f", b.StdDev, wantStdDev)
	}

	if b.WindowSize != 3 {
		t.Errorf("WindowSize = %d, want 3", b.WindowSize)
	}
}

func TestBaselineCalculator_Empty(t *testing.T) {
	t.Parallel()

	calc := BaselineCalculator{}
	b := calc.Calculate(nil)
	if b.Mean != 0 || b.StdDev != 0 || b.WindowSize != 0 {
		t.Errorf("empty baseline should be zero, got %+v", b)
	}
}

func TestZScoreDetector_Anomaly(t *testing.T) {
	t.Parallel()

	// Baseline with mean=10, stddev=2; value=20 is 5 stddevs away.
	baseline := Baseline{Mean: 10, StdDev: 2, WindowSize: 10}
	detector := ZScoreDetector{}

	m := Metric{Name: "orders", Value: 20}
	isAnomaly, zScore, threshold := detector.Detect(m, baseline)

	if !isAnomaly {
		t.Error("expected anomaly")
	}
	if math.Abs(zScore-5.0) > 1e-9 {
		t.Errorf("zScore = %f, want 5.0", zScore)
	}
	if threshold != 3.0 {
		t.Errorf("default threshold = %f, want 3.0", threshold)
	}
}

func TestZScoreDetector_NotAnomaly(t *testing.T) {
	t.Parallel()

	baseline := Baseline{Mean: 10, StdDev: 2, WindowSize: 10}
	detector := ZScoreDetector{}

	m := Metric{Name: "orders", Value: 11}
	isAnomaly, zScore, _ := detector.Detect(m, baseline)

	if isAnomaly {
		t.Errorf("value 11 should not be anomaly (zScore=%.2f)", zScore)
	}
}

func TestZScoreDetector_ZeroStdDev(t *testing.T) {
	t.Parallel()

	baseline := Baseline{Mean: 10, StdDev: 0, WindowSize: 5}
	detector := ZScoreDetector{}

	m := Metric{Name: "orders", Value: 100}
	isAnomaly, _, _ := detector.Detect(m, baseline)
	if isAnomaly {
		t.Error("zero stddev should not detect anomaly")
	}
}

func TestAlertManager_Fires(t *testing.T) {
	t.Parallel()

	am := &AlertManager{}
	am.AddRule(AlertRule{MetricName: "velocity", EntityID: "user1", Threshold: 100})
	am.AddRule(AlertRule{MetricName: "velocity", EntityID: "user2", Threshold: 200})

	m := Metric{Name: "velocity", EntityID: "user1", Value: 150}
	fired := am.Check(m)

	if len(fired) != 1 {
		t.Fatalf("expected 1 fired rule, got %d", len(fired))
	}
	if fired[0].EntityID != "user1" {
		t.Errorf("fired rule entity = %q, want user1", fired[0].EntityID)
	}
}

func TestAlertManager_BelowThreshold(t *testing.T) {
	t.Parallel()

	am := &AlertManager{}
	am.AddRule(AlertRule{MetricName: "fraud", Threshold: 0.9})

	m := Metric{Name: "fraud", Value: 0.5}
	fired := am.Check(m)
	if len(fired) != 0 {
		t.Errorf("expected no alerts, got %d", len(fired))
	}
}

func TestAlertManager_WrongMetricName(t *testing.T) {
	t.Parallel()

	am := &AlertManager{}
	am.AddRule(AlertRule{MetricName: "orders", Threshold: 10})

	m := Metric{Name: "fraud", Value: 100}
	if len(am.Check(m)) != 0 {
		t.Error("should not fire for wrong metric name")
	}
}
