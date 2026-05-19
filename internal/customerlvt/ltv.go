// Package customerlvt provides customer LTV prediction with RFM scoring and cohort analysis.
package customerlvt

import (
	"fmt"
	"time"
)

// Purchase represents a single customer purchase event.
type Purchase struct {
	CustomerID string
	Amount     float64
	OccurredAt time.Time
}

// RFMScore holds Recency/Frequency/Monetary metrics for a customer.
// Recency is the number of days since the customer's last purchase.
type RFMScore struct {
	CustomerID string
	Recency    int
	Frequency  int
	Monetary   float64
}

// CalculateRFM computes the RFM score for a given customer from a slice of purchases.
// Only purchases belonging to customerID are considered. now is the reference time.
func CalculateRFM(customerID string, purchases []Purchase, now time.Time) RFMScore {
	rfm := RFMScore{CustomerID: customerID}

	var lastPurchase time.Time
	hasAny := false

	for _, p := range purchases {
		if p.CustomerID != customerID {
			continue
		}
		rfm.Frequency++
		rfm.Monetary += p.Amount
		if !hasAny || p.OccurredAt.After(lastPurchase) {
			lastPurchase = p.OccurredAt
			hasAny = true
		}
	}

	if hasAny {
		rfm.Recency = int(now.Sub(lastPurchase).Hours() / 24)
	}

	return rfm
}

// LTVPredictor predicts customer lifetime value.
type LTVPredictor struct{}

// PredictLTV estimates LTV using the formula:
//
//	LTV = avgOrderValue * purchaseFreqPerYear * avgLifespanYears
//
// The rfm parameter is accepted for extensibility (e.g., RFM-weighted adjustments)
// but the base formula uses the provided averages.
func (p *LTVPredictor) PredictLTV(rfm RFMScore, avgOrderValue, purchaseFreqPerYear, avgLifespanYears float64) float64 {
	return avgOrderValue * purchaseFreqPerYear * avgLifespanYears
}

// CohortAnalyzer groups purchases into calendar month cohorts.
type CohortAnalyzer struct{}

// cohortKey returns the "YYYY-MM" string for a given time.
func cohortKey(t time.Time) string {
	return fmt.Sprintf("%04d-%02d", t.Year(), int(t.Month()))
}

// CohortRetention groups the provided purchases by their entry cohort (YYYY-MM of first purchase
// per customer) and counts how many customers made a purchase in each subsequent period.
// periods is the number of month-periods to track after the cohort month (period 0 = cohort month).
// Returns map[cohortKey][]int where each slice has (periods+1) entries.
func (a *CohortAnalyzer) CohortRetention(cohort []Purchase, periods int, now time.Time) map[string][]int {
	if len(cohort) == 0 || periods < 0 {
		return map[string][]int{}
	}

	// Find the first purchase date per customer.
	firstPurchase := make(map[string]time.Time)
	for _, p := range cohort {
		if t, ok := firstPurchase[p.CustomerID]; !ok || p.OccurredAt.Before(t) {
			firstPurchase[p.CustomerID] = p.OccurredAt
		}
	}

	// Map customer -> cohort key.
	customerCohort := make(map[string]string)
	for cid, t := range firstPurchase {
		customerCohort[cid] = cohortKey(t)
	}

	// Collect all cohort keys.
	cohortKeys := make(map[string]struct{})
	for _, ck := range customerCohort {
		cohortKeys[ck] = struct{}{}
	}

	// Initialise result.
	result := make(map[string][]int)
	for ck := range cohortKeys {
		result[ck] = make([]int, periods+1)
	}

	// For each purchase, determine which period relative to the customer's cohort month it falls in.
	for _, p := range cohort {
		ck := customerCohort[p.CustomerID]
		cohortYear, cohortMonth := parseCohortKey(ck)
		py, pm := p.OccurredAt.Year(), int(p.OccurredAt.Month())
		period := (py-cohortYear)*12 + (pm - cohortMonth)
		if period >= 0 && period <= periods {
			result[ck][period]++
		}
	}

	return result
}

// parseCohortKey parses a "YYYY-MM" string into year and month ints.
func parseCohortKey(key string) (int, int) {
	var y, m int
	fmt.Sscanf(key, "%d-%d", &y, &m)
	return y, m
}

