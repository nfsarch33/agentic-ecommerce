package ml_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/ml"
)

func recentOrder(userID string, revenue int, daysAgo int) ml.Order {
	return ml.Order{UserID: userID, Revenue: revenue, CreatedAt: time.Now().Add(-time.Duration(daysAgo) * 24 * time.Hour)}
}

func TestSegment_RFMScoring(t *testing.T) {
	t.Parallel()
	orders := []ml.Order{
		recentOrder("U1", 10000, 5),
		recentOrder("U1", 20000, 10),
	}
	results := ml.RFMScore(orders)
	u1 := results["U1"]
	if u1.Frequency != 2 {
		t.Fatalf("expected frequency 2, got %d", u1.Frequency)
	}
	if u1.Monetary != 30000 {
		t.Fatalf("expected monetary 30000, got %d", u1.Monetary)
	}
}

func TestSegment_VIPAssignmentHighValue(t *testing.T) {
	t.Parallel()
	rfm := ml.RFMResult{UserID: "VIP-USER", Recency: 5, Frequency: 10, Monetary: 100000}
	seg := ml.ClusterAssign(rfm, ml.SegmentRules)
	if seg != "VIP" {
		t.Fatalf("expected VIP, got %s", seg)
	}
}

func TestSegment_AtRiskAssignmentLapsed(t *testing.T) {
	t.Parallel()
	rfm := ml.RFMResult{UserID: "LAPSED", Recency: 120, Frequency: 1, Monetary: 5000}
	seg := ml.ClusterAssign(rfm, ml.SegmentRules)
	if seg != "AtRisk" && seg != "Lost" {
		t.Fatalf("expected AtRisk or Lost, got %s", seg)
	}
}

func TestSegment_MigrateRecordsTransition(t *testing.T) {
	t.Parallel()
	ss := ml.NewSegmentStore()
	if err := ss.Migrate("U1", "Regular", "VIP"); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

func TestSegment_EmptyOrdersHandled(t *testing.T) {
	t.Parallel()
	results := ml.RFMScore(nil)
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d", len(results))
	}
}

func TestSegment_CohortGroupsByPeriod(t *testing.T) {
	t.Parallel()
	users := []ml.RFMResult{
		{Recency: 5, Frequency: 10, Monetary: 100000},
		{Recency: 60, Frequency: 2, Monetary: 8000},
	}
	groups := ml.CohortAnalysis(users, "2026-Q2")
	if len(groups) == 0 {
		t.Fatal("expected cohort groups")
	}
}

func TestSegment_BoundaryValues(t *testing.T) {
	t.Parallel()
	rfm := ml.RFMResult{UserID: "BOUNDARY", Recency: 30, Frequency: 5, Monetary: 50000}
	seg := ml.ClusterAssign(rfm, ml.SegmentRules)
	if seg == "Unknown" {
		t.Fatalf("expected a segment, got Unknown")
	}
}
