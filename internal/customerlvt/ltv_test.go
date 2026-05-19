package customerlvt_test

import (
	"math"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/customerlvt"
)

func TestCalculateRFM_KnownDates(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	lastPurchase := time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC) // 5 days ago

	purchases := []customerlvt.Purchase{
		{CustomerID: "u1", Amount: 50.0, OccurredAt: time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)},
		{CustomerID: "u1", Amount: 75.0, OccurredAt: lastPurchase},
		{CustomerID: "u2", Amount: 100.0, OccurredAt: now}, // different customer
	}

	rfm := customerlvt.CalculateRFM("u1", purchases, now)

	if rfm.CustomerID != "u1" {
		t.Errorf("expected CustomerID u1, got %s", rfm.CustomerID)
	}
	if rfm.Recency != 5 {
		t.Errorf("expected Recency 5, got %d", rfm.Recency)
	}
	if rfm.Frequency != 2 {
		t.Errorf("expected Frequency 2, got %d", rfm.Frequency)
	}
	if math.Abs(rfm.Monetary-125.0) > 0.001 {
		t.Errorf("expected Monetary 125.0, got %f", rfm.Monetary)
	}
}

func TestCalculateRFM_NoPurchases(t *testing.T) {
	t.Parallel()

	now := time.Now()
	rfm := customerlvt.CalculateRFM("u99", nil, now)
	if rfm.Frequency != 0 {
		t.Errorf("expected 0 frequency, got %d", rfm.Frequency)
	}
	if rfm.Monetary != 0 {
		t.Errorf("expected 0 monetary, got %f", rfm.Monetary)
	}
	if rfm.Recency != 0 {
		t.Errorf("expected 0 recency for no purchases, got %d", rfm.Recency)
	}
}

func TestLTVPredictor_Formula(t *testing.T) {
	t.Parallel()

	pred := &customerlvt.LTVPredictor{}
	rfm := customerlvt.RFMScore{CustomerID: "u1", Recency: 10, Frequency: 5, Monetary: 500}

	// LTV = avgOrder * freq * lifespan = 50 * 12 * 3 = 1800
	ltv := pred.PredictLTV(rfm, 50.0, 12.0, 3.0)
	if math.Abs(ltv-1800.0) > 0.001 {
		t.Errorf("expected LTV 1800.0, got %f", ltv)
	}
}

func TestCohortRetention_Grouping(t *testing.T) {
	t.Parallel()

	// Two customers; u1 started in 2024-01, u2 started in 2024-02.
	purchases := []customerlvt.Purchase{
		{CustomerID: "u1", Amount: 10, OccurredAt: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)},
		{CustomerID: "u1", Amount: 10, OccurredAt: time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC)},
		{CustomerID: "u2", Amount: 20, OccurredAt: time.Date(2024, 2, 10, 0, 0, 0, 0, time.UTC)},
	}

	var a customerlvt.CohortAnalyzer
	now := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	retention := a.CohortRetention(purchases, 2, now)

	// Cohort 2024-01: period 0 = Jan (1 purchase), period 1 = Feb (1 purchase).
	cohort01, ok := retention["2024-01"]
	if !ok {
		t.Fatal("expected cohort 2024-01 to exist")
	}
	if cohort01[0] != 1 {
		t.Errorf("2024-01 period 0: expected 1, got %d", cohort01[0])
	}
	if cohort01[1] != 1 {
		t.Errorf("2024-01 period 1: expected 1, got %d", cohort01[1])
	}

	// Cohort 2024-02: period 0 = Feb (1 purchase from u2).
	cohort02, ok := retention["2024-02"]
	if !ok {
		t.Fatal("expected cohort 2024-02 to exist")
	}
	if cohort02[0] != 1 {
		t.Errorf("2024-02 period 0: expected 1, got %d", cohort02[0])
	}
}

func TestCohortRetention_Empty(t *testing.T) {
	t.Parallel()

	var a customerlvt.CohortAnalyzer
	result := a.CohortRetention(nil, 3, time.Now())
	if len(result) != 0 {
		t.Errorf("expected empty result for nil cohort, got %v", result)
	}
}
