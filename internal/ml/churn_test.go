package ml_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/ml"
)

var testNow = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestChurn_EngagementScoreActiveUser(t *testing.T) {
	t.Parallel()
	u := ml.UserActivity{
		UserID:        "U1",
		LastActive:    testNow.Add(-2 * 24 * time.Hour), // 2 days ago
		SessionCount:  20,
		PurchaseCount: 5,
	}
	score := ml.EngagementScore(u, testNow)
	if score < 0.9 {
		t.Fatalf("expected high engagement score, got %f", score)
	}
}

func TestChurn_EngagementScoreInactiveUser(t *testing.T) {
	t.Parallel()
	u := ml.UserActivity{
		UserID:        "U2",
		LastActive:    testNow.Add(-100 * 24 * time.Hour), // 100 days ago
		SessionCount:  0,
		PurchaseCount: 0,
	}
	score := ml.EngagementScore(u, testNow)
	if score > 0.2 {
		t.Fatalf("expected low engagement score, got %f", score)
	}
}

func TestChurn_RiskIndicatorsIdentifiesIssues(t *testing.T) {
	t.Parallel()
	u := ml.UserActivity{
		UserID:        "U3",
		LastActive:    testNow.Add(-60 * 24 * time.Hour),
		SessionCount:  1,
		PurchaseCount: 0,
		EmailOpens:    0,
	}
	indicators := ml.RiskIndicators(u, testNow)
	if len(indicators) == 0 {
		t.Fatal("expected risk indicators for churned user")
	}
	found := false
	for _, ind := range indicators {
		if ind == "inactive_30d" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected inactive_30d indicator")
	}
}

func TestChurn_RetentionActionHighScore(t *testing.T) {
	t.Parallel()
	action := ml.RetentionAction(0.9)
	if action != "no_action" {
		t.Fatalf("expected no_action for high score, got %s", action)
	}
}

func TestChurn_RetentionActionMidScore(t *testing.T) {
	t.Parallel()
	action := ml.RetentionAction(0.5)
	if action != "send_discount" {
		t.Fatalf("expected send_discount for mid score, got %s", action)
	}
}

func TestChurn_RetentionActionLowScore(t *testing.T) {
	t.Parallel()
	action := ml.RetentionAction(0.1)
	if action != "personal_outreach" {
		t.Fatalf("expected personal_outreach for low score, got %s", action)
	}
}

func TestChurn_AnalyseChurnBatch(t *testing.T) {
	t.Parallel()
	users := []ml.UserActivity{
		{UserID: "U1", LastActive: testNow.Add(-2 * 24 * time.Hour), SessionCount: 20, PurchaseCount: 5},
		{UserID: "U2", LastActive: testNow.Add(-100 * 24 * time.Hour), SessionCount: 0, PurchaseCount: 0},
	}
	results := ml.AnalyseChurn(users, testNow)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// U1 should have lower churn risk than U2
	if results[0].Score >= results[1].Score {
		t.Fatalf("expected U1 lower churn risk than U2, got %f vs %f", results[0].Score, results[1].Score)
	}
}

func TestChurn_CohortRetention(t *testing.T) {
	t.Parallel()
	users := []ml.UserActivity{
		{UserID: "U1", LastActive: testNow.Add(-5 * 24 * time.Hour)},
		{UserID: "U2", LastActive: testNow.Add(-5 * 24 * time.Hour)},
		{UserID: "U3", LastActive: testNow.Add(-60 * 24 * time.Hour)},
	}
	cohortFn := func(u ml.UserActivity) string { return "jan" }
	retention := ml.CohortRetention(users, cohortFn, testNow)
	// 2 of 3 active within 30d => 0.666
	if retention["jan"] < 0.6 || retention["jan"] > 0.8 {
		t.Fatalf("expected ~0.666 retention, got %f", retention["jan"])
	}
}
