package security_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/security"
)

func cleanOrder() security.FraudOrder {
	return security.FraudOrder{ID: "O1", Email: "good@example.com", IP: "1.2.3.4", Country: "AU", Amount: 1000}
}

func TestFraud_LowRiskOrderScoresLow(t *testing.T) {
	t.Parallel()
	re := security.NewRuleEngine()
	score := re.RiskScore(cleanOrder(), nil)
	if score > 20 {
		t.Fatalf("expected low risk score, got %d", score)
	}
}

func TestFraud_HighVelocityScoresHigh(t *testing.T) {
	t.Parallel()
	re := security.NewRuleEngine()
	history := []security.FraudOrder{
		{Email: "spammer@x.com"}, {Email: "spammer@x.com"},
		{Email: "spammer@x.com"}, {Email: "spammer@x.com"},
	}
	order := security.FraudOrder{ID: "O2", Email: "spammer@x.com", IP: "5.6.7.8", Country: "AU", Amount: 500}
	score := re.RiskScore(order, history)
	if score < 30 {
		t.Fatalf("expected high velocity score >= 30, got %d", score)
	}
}

func TestFraud_BlocklistedEmailCaught(t *testing.T) {
	t.Parallel()
	re := security.NewRuleEngine()
	re.AddToBlocklist("bad@actor.com")
	order := security.FraudOrder{ID: "O3", Email: "bad@actor.com", IP: "1.2.3.4", Country: "AU", Amount: 500}
	score := re.RiskScore(order, nil)
	if score < 80 {
		t.Fatalf("expected blocklisted score >= 80, got %d", score)
	}
}

func TestFraud_GeoMismatchFlagged(t *testing.T) {
	t.Parallel()
	re := security.NewRuleEngine()
	order := security.FraudOrder{ID: "O4", Email: "user@x.com", IP: "9.9.9.9", Country: "XX", Amount: 500}
	score := re.RiskScore(order, nil)
	if score < 15 {
		t.Fatalf("expected geo mismatch score >= 15, got %d", score)
	}
}

func TestFraud_ManualReviewSetsFlag(t *testing.T) {
	t.Parallel()
	re := security.NewRuleEngine()
	re.ManualReview("ORD-5")
	if !re.IsFlagged("ORD-5") {
		t.Fatal("expected order to be flagged")
	}
}

func TestFraud_MultipleRulesCompound(t *testing.T) {
	t.Parallel()
	re := security.NewRuleEngine()
	re.AddToBlocklist("bad@actor.com")
	history := []security.FraudOrder{
		{Email: "bad@actor.com"}, {Email: "bad@actor.com"},
		{Email: "bad@actor.com"}, {Email: "bad@actor.com"},
	}
	order := security.FraudOrder{ID: "O6", Email: "bad@actor.com", IP: "1.2.3.4", Country: "AU", Amount: 200000}
	score := re.RiskScore(order, history)
	if score < 80 {
		t.Fatalf("expected compound score >= 80, got %d", score)
	}
}

func TestFraud_CleanOrderPassesAllRules(t *testing.T) {
	t.Parallel()
	re := security.NewRuleEngine()
	score := re.RiskScore(cleanOrder(), []security.FraudOrder{{Email: "other@x.com"}})
	if score > 20 {
		t.Fatalf("clean order should score low, got %d", score)
	}
}
