package ml_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/ml"
)

func TestRecommend_CollaborativeFilter(t *testing.T) {
	t.Parallel()
	interactions := []ml.Interaction{
		{UserID: "U1", ProductID: "P1", Count: 2},
		{UserID: "U2", ProductID: "P1", Count: 1},
		{UserID: "U2", ProductID: "P2", Count: 3},
	}
	scores := ml.CollaborativeFilter("U1", interactions)
	// P2 should appear because U2 bought both P1 (which U1 bought) and P2
	found := false
	for _, s := range scores {
		if s.ProductID == "P2" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected P2 in CF results")
	}
}

func TestRecommend_ContentBased(t *testing.T) {
	t.Parallel()
	catalog := []ml.Product{
		{ID: "P1", Category: "electronics"},
		{ID: "P2", Category: "electronics"},
		{ID: "P3", Category: "clothing"},
	}
	scores := ml.ContentBased("P1", catalog)
	if len(scores) == 0 {
		t.Fatal("expected at least one content-based recommendation")
	}
	if scores[0].ProductID != "P2" {
		t.Fatalf("expected P2 (same category), got %s", scores[0].ProductID)
	}
}

func TestRecommend_HybridMergesScores(t *testing.T) {
	t.Parallel()
	cf := []ml.ProductScore{{ProductID: "P1", Score: 2.0}}
	cb := []ml.ProductScore{{ProductID: "P2", Score: 1.0}}
	hybrid := ml.HybridScore(cf, cb, [2]float64{0.6, 0.4})
	if len(hybrid) != 2 {
		t.Fatalf("expected 2 products in hybrid, got %d", len(hybrid))
	}
}

func TestRecommend_TopNTruncates(t *testing.T) {
	t.Parallel()
	scores := []ml.ProductScore{{ProductID: "P1", Score: 3}, {ProductID: "P2", Score: 2}, {ProductID: "P3", Score: 1}}
	top := ml.TopN(scores, 2)
	if len(top) != 2 {
		t.Fatalf("expected 2, got %d", len(top))
	}
}

func TestRecommend_EmptyInteractionsReturnsEmpty(t *testing.T) {
	t.Parallel()
	scores := ml.CollaborativeFilter("U1", nil)
	if len(scores) != 0 {
		t.Fatalf("expected empty, got %d", len(scores))
	}
}

func TestRecommend_WeightNormalization(t *testing.T) {
	t.Parallel()
	cf := []ml.ProductScore{{ProductID: "P1", Score: 1.0}}
	cb := []ml.ProductScore{{ProductID: "P1", Score: 1.0}}
	hybrid := ml.HybridScore(cf, cb, [2]float64{0.5, 0.5})
	if hybrid[0].Score != 1.0 {
		t.Fatalf("expected 1.0 (0.5+0.5), got %.2f", hybrid[0].Score)
	}
}

func TestRecommend_DuplicateProductDedup(t *testing.T) {
	t.Parallel()
	cf := []ml.ProductScore{{ProductID: "P1", Score: 2.0}, {ProductID: "P1", Score: 1.0}}
	merged := ml.HybridScore(cf, nil, [2]float64{1.0, 0.0})
	count := 0
	for _, s := range merged {
		if s.ProductID == "P1" {
			count++
		}
	}
	if count > 1 {
		t.Fatalf("expected dedup, got %d entries for P1", count)
	}
}
