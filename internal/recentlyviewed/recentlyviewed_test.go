package recentlyviewed_test

import (
	"math"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/recentlyviewed"
)

var now = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

func ev(productID string, hoursAgo float64) recentlyviewed.ViewEvent {
	return recentlyviewed.ViewEvent{
		ProductID: productID,
		ViewedAt:  now.Add(-time.Duration(hoursAgo * float64(time.Hour))),
		SessionID: "sess-1",
	}
}

// Store tests

func TestStore_RecordAndRecent(t *testing.T) {
	t.Parallel()
	s := recentlyviewed.NewStore()
	s.Record("user-1", ev("prod-a", 2))
	s.Record("user-1", ev("prod-b", 1))
	s.Record("user-1", ev("prod-c", 0))

	got := s.Recent("user-1", 3)
	if len(got) != 3 {
		t.Fatalf("want 3 events, got %d", len(got))
	}
	// Most recent first
	if got[0].ProductID != "prod-c" {
		t.Errorf("want prod-c first, got %s", got[0].ProductID)
	}
	if got[2].ProductID != "prod-a" {
		t.Errorf("want prod-a last, got %s", got[2].ProductID)
	}
}

func TestStore_Recent_Limit(t *testing.T) {
	t.Parallel()
	s := recentlyviewed.NewStore()
	for i := 0; i < 5; i++ {
		s.Record("user-lim", ev("prod", float64(5-i)))
	}
	got := s.Recent("user-lim", 2)
	if len(got) != 2 {
		t.Errorf("want 2, got %d", len(got))
	}
}

func TestStore_Recent_Unknown(t *testing.T) {
	t.Parallel()
	s := recentlyviewed.NewStore()
	got := s.Recent("ghost", 10)
	if got != nil {
		t.Errorf("want nil for unknown user, got %v", got)
	}
}

func TestStore_MaxFIFOEviction(t *testing.T) {
	t.Parallel()
	s := recentlyviewed.NewStore()
	// Fill to capacity + 1
	for i := 0; i < 51; i++ {
		s.Record("user-fifo", recentlyviewed.ViewEvent{
			ProductID: "prod-" + itoa(i),
			ViewedAt:  now.Add(time.Duration(i) * time.Minute),
			SessionID: "s",
		})
	}
	got := s.Recent("user-fifo", 100)
	if len(got) != 50 {
		t.Errorf("want max 50, got %d", len(got))
	}
	// prod-0 (oldest) should have been evicted; prod-1 should be the oldest remaining
	oldest := got[len(got)-1]
	if oldest.ProductID == "prod-0" {
		t.Errorf("prod-0 should have been evicted, but still present")
	}
}

func TestStore_Clear(t *testing.T) {
	t.Parallel()
	s := recentlyviewed.NewStore()
	s.Record("user-clr", ev("p1", 1))
	s.Record("user-clr", ev("p2", 2))
	s.Clear("user-clr")
	got := s.Recent("user-clr", 10)
	if len(got) != 0 {
		t.Errorf("want empty after clear, got %d", len(got))
	}
}

// DecayScorer tests

func TestDecayScorer_Recent(t *testing.T) {
	t.Parallel()
	scorer := recentlyviewed.DecayScorer{}
	event := ev("p", 0)
	score := scorer.Score(event, now, 24.0)
	if math.Abs(score-1.0) > 1e-9 {
		t.Errorf("want ~1.0 for just-viewed, got %f", score)
	}
}

func TestDecayScorer_HalfLife(t *testing.T) {
	t.Parallel()
	scorer := recentlyviewed.DecayScorer{}
	event := ev("p", 24) // exactly 24 hours ago
	score := scorer.Score(event, now, 24.0)
	if math.Abs(score-0.5) > 1e-9 {
		t.Errorf("want 0.5 at one half-life, got %f", score)
	}
}

func TestDecayScorer_OlderIsLower(t *testing.T) {
	t.Parallel()
	scorer := recentlyviewed.DecayScorer{}
	recent := scorer.Score(ev("p", 1), now, 24.0)
	older := scorer.Score(ev("p", 48), now, 24.0)
	if recent <= older {
		t.Errorf("want recent (%f) > older (%f)", recent, older)
	}
}

func TestDecayScorer_ZeroHalfLife(t *testing.T) {
	t.Parallel()
	scorer := recentlyviewed.DecayScorer{}
	score := scorer.Score(ev("p", 100), now, 0)
	if score != 1.0 {
		t.Errorf("want 1.0 for zero halfLife, got %f", score)
	}
}

// PersonalizationSignal tests

func TestPersonalizationSignal_TopProducts(t *testing.T) {
	t.Parallel()
	s := recentlyviewed.NewStore()
	sig := recentlyviewed.PersonalizationSignal{}

	// popular viewed recently and often
	s.Record("user-ps", ev("popular", 1))
	s.Record("user-ps", ev("popular", 2))
	s.Record("user-ps", ev("popular", 3))
	// rare viewed once a long time ago
	s.Record("user-ps", ev("rare", 200))

	top := sig.TopProducts("user-ps", s, now, 24.0, 5)
	if len(top) == 0 {
		t.Fatal("want at least 1 result")
	}
	if top[0].ProductID != "popular" {
		t.Errorf("want popular at top, got %s", top[0].ProductID)
	}
	if top[0].Score <= top[len(top)-1].Score {
		t.Error("want descending order")
	}
}

func TestPersonalizationSignal_Limit(t *testing.T) {
	t.Parallel()
	s := recentlyviewed.NewStore()
	sig := recentlyviewed.PersonalizationSignal{}
	s.Record("u", ev("a", 1))
	s.Record("u", ev("b", 2))
	s.Record("u", ev("c", 3))

	top := sig.TopProducts("u", s, now, 24.0, 2)
	if len(top) != 2 {
		t.Errorf("want limit 2, got %d", len(top))
	}
}

func TestPersonalizationSignal_Empty(t *testing.T) {
	t.Parallel()
	s := recentlyviewed.NewStore()
	sig := recentlyviewed.PersonalizationSignal{}
	top := sig.TopProducts("nobody", s, now, 24.0, 5)
	if len(top) != 0 {
		t.Errorf("want 0 for unknown user, got %d", len(top))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
