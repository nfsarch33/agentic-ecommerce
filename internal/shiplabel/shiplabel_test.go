package shiplabel

import (
	"context"
	"errors"
	"testing"
	"time"
)

var sampleRates = []Rate{
	{Carrier: "AusPost", Service: "Standard", PriceCents: 999, EstimatedDays: 5},
	{Carrier: "FedEx", Service: "Express", PriceCents: 2500, EstimatedDays: 2},
	{Carrier: "DHL", Service: "Economy", PriceCents: 750, EstimatedDays: 7},
	{Carrier: "UPS", Service: "Priority", PriceCents: 3200, EstimatedDays: 1},
}

func TestStubRateProvider_GetRates(t *testing.T) {
	t.Parallel()
	p := &StubRateProvider{Rates: sampleRates}
	rates, err := p.GetRates(context.Background(), RateRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rates) != len(sampleRates) {
		t.Errorf("expected %d rates, got %d", len(sampleRates), len(rates))
	}
}

func TestStubRateProvider_Error(t *testing.T) {
	t.Parallel()
	want := errors.New("provider unavailable")
	p := &StubRateProvider{Err: want}
	_, err := p.GetRates(context.Background(), RateRequest{})
	if !errors.Is(err, want) {
		t.Errorf("expected configured error, got %v", err)
	}
}

func TestRateComparator_CheapestRate(t *testing.T) {
	t.Parallel()
	rc := RateComparator{}
	best, err := rc.CheapestRate(sampleRates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.Carrier != "DHL" || best.PriceCents != 750 {
		t.Errorf("expected DHL/750, got %s/%d", best.Carrier, best.PriceCents)
	}
}

func TestRateComparator_CheapestRate_Empty(t *testing.T) {
	t.Parallel()
	rc := RateComparator{}
	_, err := rc.CheapestRate(nil)
	if !errors.Is(err, ErrNoRates) {
		t.Errorf("expected ErrNoRates, got %v", err)
	}
}

func TestRateComparator_FastestRate(t *testing.T) {
	t.Parallel()
	rc := RateComparator{}
	best, err := rc.FastestRate(sampleRates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.Carrier != "UPS" || best.EstimatedDays != 1 {
		t.Errorf("expected UPS/1day, got %s/%d", best.Carrier, best.EstimatedDays)
	}
}

func TestRateComparator_FastestRate_Empty(t *testing.T) {
	t.Parallel()
	rc := RateComparator{}
	_, err := rc.FastestRate(nil)
	if !errors.Is(err, ErrNoRates) {
		t.Errorf("expected ErrNoRates, got %v", err)
	}
}

func TestRateComparator_BestValue_WithinLimit(t *testing.T) {
	t.Parallel()
	rc := RateComparator{}
	// Within 5 days: AusPost(999,5d), FedEx(2500,2d), UPS(3200,1d)
	// Cheapest within 5 days is AusPost at 999.
	best, err := rc.BestValue(sampleRates, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.Carrier != "AusPost" {
		t.Errorf("expected AusPost as best value within 5d, got %s", best.Carrier)
	}
}

func TestRateComparator_BestValue_TightLimit(t *testing.T) {
	t.Parallel()
	rc := RateComparator{}
	// Within 2 days: FedEx(2500,2d), UPS(3200,1d) -> FedEx is cheaper
	best, err := rc.BestValue(sampleRates, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.Carrier != "FedEx" {
		t.Errorf("expected FedEx as best value within 2d, got %s", best.Carrier)
	}
}

func TestRateComparator_BestValue_ExceedsLimit(t *testing.T) {
	t.Parallel()
	rc := RateComparator{}
	// maxDays=0 excludes everything.
	_, err := rc.BestValue(sampleRates, 0)
	if !errors.Is(err, ErrNoRatesWithinDays) {
		t.Errorf("expected ErrNoRatesWithinDays, got %v", err)
	}
}

func TestRateComparator_BestValue_Empty(t *testing.T) {
	t.Parallel()
	rc := RateComparator{}
	_, err := rc.BestValue(nil, 5)
	if !errors.Is(err, ErrNoRates) {
		t.Errorf("expected ErrNoRates, got %v", err)
	}
}

func TestPrintBatch_CapturesAllLabels(t *testing.T) {
	t.Parallel()
	labels := [][]byte{
		[]byte("label-1"),
		[]byte("label-2"),
		[]byte("label-3"),
	}
	before := time.Now()
	bp := PrintBatch(labels)
	after := time.Now()

	if len(bp.Labels) != 3 {
		t.Errorf("expected 3 labels, got %d", len(bp.Labels))
	}
	for i, l := range bp.Labels {
		if string(l) != string(labels[i]) {
			t.Errorf("label[%d] mismatch: want %q, got %q", i, labels[i], l)
		}
	}
	if bp.PrintedAt.Before(before) || bp.PrintedAt.After(after) {
		t.Errorf("PrintedAt %v not in expected range [%v, %v]", bp.PrintedAt, before, after)
	}
}

func TestPrintBatch_Empty(t *testing.T) {
	t.Parallel()
	bp := PrintBatch(nil)
	if len(bp.Labels) != 0 {
		t.Errorf("expected empty labels, got %d", len(bp.Labels))
	}
}

func TestPrintBatch_MutationIsolation(t *testing.T) {
	t.Parallel()
	original := [][]byte{[]byte("original")}
	bp := PrintBatch(original)

	// Mutate the original; the batch should be unaffected.
	original[0][0] = 'X'
	if bp.Labels[0][0] == 'X' {
		t.Error("PrintBatch should copy labels, not share underlying slices")
	}
}
