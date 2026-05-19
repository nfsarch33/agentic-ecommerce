package revenueattr_test

import (
	"math"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/revenueattr"
)

func TestAttributeRevenue_FirstTouch(t *testing.T) {
	t.Parallel()

	now := time.Now()
	conv := revenueattr.Conversion{
		ID:      "c1",
		Revenue: 100.0,
		TouchPoints: []revenueattr.TouchPoint{
			{Channel: "email", OccurredAt: now.Add(-2 * time.Hour)},
			{Channel: "social", OccurredAt: now.Add(-1 * time.Hour)},
			{Channel: "search", OccurredAt: now},
		},
		OccurredAt: now,
	}

	attr := revenueattr.AttributeRevenue(conv, revenueattr.ModelFirstTouch)
	if attr["email"] != 100.0 {
		t.Errorf("expected email to get all revenue (100), got %f", attr["email"])
	}
	if v, ok := attr["social"]; ok && v != 0 {
		t.Errorf("social should not get revenue in first-touch, got %f", v)
	}
}

func TestAttributeRevenue_LastTouch(t *testing.T) {
	t.Parallel()

	now := time.Now()
	conv := revenueattr.Conversion{
		ID:      "c2",
		Revenue: 200.0,
		TouchPoints: []revenueattr.TouchPoint{
			{Channel: "email", OccurredAt: now.Add(-2 * time.Hour)},
			{Channel: "social", OccurredAt: now.Add(-1 * time.Hour)},
			{Channel: "search", OccurredAt: now},
		},
		OccurredAt: now.Add(time.Minute),
	}

	attr := revenueattr.AttributeRevenue(conv, revenueattr.ModelLastTouch)
	if attr["search"] != 200.0 {
		t.Errorf("expected search to get all revenue (200), got %f", attr["search"])
	}
}

func TestAttributeRevenue_Linear(t *testing.T) {
	t.Parallel()

	now := time.Now()
	conv := revenueattr.Conversion{
		ID:      "c3",
		Revenue: 90.0,
		TouchPoints: []revenueattr.TouchPoint{
			{Channel: "email", OccurredAt: now.Add(-3 * time.Hour)},
			{Channel: "social", OccurredAt: now.Add(-2 * time.Hour)},
			{Channel: "search", OccurredAt: now.Add(-1 * time.Hour)},
		},
		OccurredAt: now,
	}

	attr := revenueattr.AttributeRevenue(conv, revenueattr.ModelLinear)
	for _, ch := range []string{"email", "social", "search"} {
		if math.Abs(attr[ch]-30.0) > 0.001 {
			t.Errorf("expected %s to get 30.0 (linear split), got %f", ch, attr[ch])
		}
	}
}

func TestAttributeRevenue_TimeDecay(t *testing.T) {
	t.Parallel()

	now := time.Now()
	conv := revenueattr.Conversion{
		ID:      "c4",
		Revenue: 100.0,
		TouchPoints: []revenueattr.TouchPoint{
			{Channel: "old", OccurredAt: now.Add(-24 * time.Hour)},  // far away
			{Channel: "recent", OccurredAt: now.Add(-1 * time.Hour)}, // close
		},
		OccurredAt: now,
	}

	attr := revenueattr.AttributeRevenue(conv, revenueattr.ModelTimeDecay)
	// Recent channel should get more credit than old channel.
	if attr["recent"] <= attr["old"] {
		t.Errorf("time-decay: recent channel (%f) should outweigh old channel (%f)", attr["recent"], attr["old"])
	}
	// Total should sum to revenue.
	total := attr["old"] + attr["recent"]
	if math.Abs(total-100.0) > 0.001 {
		t.Errorf("total attributed revenue should be 100, got %f", total)
	}
}

func TestROAS_Calculation(t *testing.T) {
	t.Parallel()

	if r := revenueattr.ROAS(500, 100); math.Abs(r-5.0) > 0.001 {
		t.Errorf("expected ROAS 5.0, got %f", r)
	}
	if r := revenueattr.ROAS(100, 0); r != 0 {
		t.Errorf("expected ROAS 0 when spend=0, got %f", r)
	}
}

func TestAttributeRevenue_EmptyTouchPoints(t *testing.T) {
	t.Parallel()

	conv := revenueattr.Conversion{
		ID:      "c5",
		Revenue: 100.0,
		OccurredAt: time.Now(),
	}

	for _, model := range []revenueattr.Model{
		revenueattr.ModelFirstTouch,
		revenueattr.ModelLastTouch,
		revenueattr.ModelLinear,
		revenueattr.ModelTimeDecay,
	} {
		attr := revenueattr.AttributeRevenue(conv, model)
		if len(attr) != 0 {
			t.Errorf("model %s: expected empty map for no touch points, got %v", model, attr)
		}
	}
}

func TestSummaryReport(t *testing.T) {
	t.Parallel()

	now := time.Now()
	conversions := []revenueattr.Conversion{
		{
			ID:      "c1",
			Revenue: 100.0,
			TouchPoints: []revenueattr.TouchPoint{
				{Channel: "email", OccurredAt: now.Add(-time.Hour)},
			},
			OccurredAt: now,
		},
		{
			ID:      "c2",
			Revenue: 200.0,
			TouchPoints: []revenueattr.TouchPoint{
				{Channel: "social", OccurredAt: now.Add(-time.Hour)},
			},
			OccurredAt: now,
		},
	}

	spends := map[string]float64{
		"email":  50.0,
		"social": 100.0,
	}

	summaries := revenueattr.SummaryReport(conversions, revenueattr.ModelLastTouch, spends)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 channel summaries, got %d", len(summaries))
	}

	// Summaries are sorted by channel name: email < social.
	if summaries[0].Channel != "email" {
		t.Errorf("expected first channel to be email, got %s", summaries[0].Channel)
	}
	if math.Abs(summaries[0].ROAS-2.0) > 0.001 {
		t.Errorf("expected email ROAS 2.0, got %f", summaries[0].ROAS)
	}
	if math.Abs(summaries[1].ROAS-2.0) > 0.001 {
		t.Errorf("expected social ROAS 2.0, got %f", summaries[1].ROAS)
	}
}
