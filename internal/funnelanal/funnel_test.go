package funnelanal_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/funnelanal"
)

func TestEventStore_RecordAndRetrieve(t *testing.T) {
	t.Parallel()

	store := funnelanal.NewEventStore()
	now := time.Now()

	e1 := funnelanal.FunnelEvent{SessionID: "s1", UserID: "u1", Step: "view", OccurredAt: now}
	e2 := funnelanal.FunnelEvent{SessionID: "s1", UserID: "u1", Step: "add_cart", OccurredAt: now.Add(time.Minute)}
	e3 := funnelanal.FunnelEvent{SessionID: "s2", UserID: "u2", Step: "view", OccurredAt: now}

	store.Record(e1)
	store.Record(e2)
	store.Record(e3)

	evts := store.SessionEvents("s1")
	if len(evts) != 2 {
		t.Fatalf("expected 2 events for s1, got %d", len(evts))
	}
	if evts[0].Step != "view" || evts[1].Step != "add_cart" {
		t.Errorf("unexpected events: %v", evts)
	}

	evts2 := store.SessionEvents("s2")
	if len(evts2) != 1 {
		t.Fatalf("expected 1 event for s2, got %d", len(evts2))
	}

	// Non-existent session returns nil.
	if store.SessionEvents("unknown") != nil {
		t.Error("expected nil for unknown session")
	}
}

func TestFunnelAnalyzer_SimpleAnalysis(t *testing.T) {
	t.Parallel()

	funnel := funnelanal.Funnel{
		ID:   "checkout",
		Name: "Checkout Funnel",
		Steps: []funnelanal.Step{
			{Name: "view", Order: 1},
			{Name: "add_cart", Order: 2},
			{Name: "purchase", Order: 3},
		},
	}

	now := time.Now()
	events := []funnelanal.FunnelEvent{
		{SessionID: "s1", Step: "view", OccurredAt: now},
		{SessionID: "s2", Step: "view", OccurredAt: now},
		{SessionID: "s3", Step: "view", OccurredAt: now},
		{SessionID: "s4", Step: "view", OccurredAt: now},
		{SessionID: "s1", Step: "add_cart", OccurredAt: now.Add(time.Minute)},
		{SessionID: "s2", Step: "add_cart", OccurredAt: now.Add(time.Minute)},
		{SessionID: "s1", Step: "purchase", OccurredAt: now.Add(2 * time.Minute)},
	}

	var a funnelanal.FunnelAnalyzer
	report := a.Analyze(funnel, events)

	if report.StepCounts["view"] != 4 {
		t.Errorf("expected 4 view events, got %d", report.StepCounts["view"])
	}
	if report.StepCounts["add_cart"] != 2 {
		t.Errorf("expected 2 add_cart events, got %d", report.StepCounts["add_cart"])
	}
	if report.StepCounts["purchase"] != 1 {
		t.Errorf("expected 1 purchase event, got %d", report.StepCounts["purchase"])
	}
}

func TestFunnelAnalyzer_DropOffDetection(t *testing.T) {
	t.Parallel()

	funnel := funnelanal.Funnel{
		ID:   "onboard",
		Name: "Onboarding",
		Steps: []funnelanal.Step{
			{Name: "signup", Order: 1},
			{Name: "verify", Order: 2},
			{Name: "complete", Order: 3},
		},
	}

	now := time.Now()
	events := []funnelanal.FunnelEvent{
		{SessionID: "s1", Step: "signup", OccurredAt: now},
		{SessionID: "s2", Step: "signup", OccurredAt: now},
		{SessionID: "s3", Step: "signup", OccurredAt: now},
		{SessionID: "s4", Step: "signup", OccurredAt: now},
		{SessionID: "s1", Step: "verify", OccurredAt: now.Add(time.Minute)},
		{SessionID: "s2", Step: "verify", OccurredAt: now.Add(time.Minute)},
	}

	var a funnelanal.FunnelAnalyzer
	report := a.Analyze(funnel, events)

	// Drop-off from signup(4) to verify(2) = (4-2)/4 = 0.5
	dropVerify := report.DropOffRates["verify"]
	if dropVerify < 0.499 || dropVerify > 0.501 {
		t.Errorf("expected verify drop-off ~0.5, got %f", dropVerify)
	}

	// Drop-off from verify(2) to complete(0) = (2-0)/2 = 1.0
	dropComplete := report.DropOffRates["complete"]
	if dropComplete < 0.999 || dropComplete > 1.001 {
		t.Errorf("expected complete drop-off ~1.0, got %f", dropComplete)
	}
}

func TestFunnelAnalyzer_ConversionRate(t *testing.T) {
	t.Parallel()

	funnel := funnelanal.Funnel{
		ID:   "sale",
		Name: "Sale",
		Steps: []funnelanal.Step{
			{Name: "landing", Order: 1},
			{Name: "checkout", Order: 2},
		},
	}

	now := time.Now()
	events := []funnelanal.FunnelEvent{
		{SessionID: "s1", Step: "landing", OccurredAt: now},
		{SessionID: "s2", Step: "landing", OccurredAt: now},
		{SessionID: "s3", Step: "landing", OccurredAt: now},
		{SessionID: "s4", Step: "landing", OccurredAt: now},
		{SessionID: "s1", Step: "checkout", OccurredAt: now.Add(time.Minute)},
	}

	var a funnelanal.FunnelAnalyzer
	report := a.Analyze(funnel, events)

	// 1 checkout / 4 landing = 0.25
	if report.ConversionRate < 0.249 || report.ConversionRate > 0.251 {
		t.Errorf("expected conversion rate ~0.25, got %f", report.ConversionRate)
	}
}

func TestFunnelAnalyzer_EmptyFunnel(t *testing.T) {
	t.Parallel()

	funnel := funnelanal.Funnel{ID: "empty", Name: "Empty"}
	var a funnelanal.FunnelAnalyzer
	report := a.Analyze(funnel, nil)

	if report.ConversionRate != 0 {
		t.Errorf("expected 0 conversion rate for empty funnel, got %f", report.ConversionRate)
	}
	if len(report.StepCounts) != 0 {
		t.Error("expected empty StepCounts for empty funnel")
	}
}
