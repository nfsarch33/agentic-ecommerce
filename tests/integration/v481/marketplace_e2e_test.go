//go:build v481_smoke

package v481

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
)

// Scenario 1: Vendor A creates products → Vendor B cannot see them
func TestMarketplace_VendorIsolation(t *testing.T) {
	t.Parallel()
	store := marketplace.NewInMemoryVendorStore()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	vA, _ := marketplace.NewVendor("vA", "t1", "Vendor A", "a@x.com", 1000, now)
	vB, _ := marketplace.NewVendor("vB", "t1", "Vendor B", "b@x.com", 1500, now)
	_ = store.Create(context.Background(), vA)
	_ = store.Create(context.Background(), vB)

	gotA, err := store.Get(context.Background(), "t1", "vA")
	if err != nil {
		t.Fatalf("Get vendor A: %v", err)
	}
	if gotA.Name != "Vendor A" {
		t.Fatalf("vendor A name = %s, want Vendor A", gotA.Name)
	}

	_, err = store.Get(context.Background(), "t2", "vA")
	if err == nil {
		t.Fatal("vendor A should not be accessible from tenant t2")
	}

	t1List, _ := store.List(context.Background(), "t1")
	if len(t1List) != 2 {
		t.Fatalf("t1 vendors = %d, want 2", len(t1List))
	}

	t2List, _ := store.List(context.Background(), "t2")
	if len(t2List) != 0 {
		t.Fatalf("t2 vendors = %d, want 0 (isolation)", len(t2List))
	}
}

// Scenario 2: Commission calculated correctly per vendor
func TestMarketplace_CommissionPerVendor(t *testing.T) {
	t.Parallel()
	vendorStore := marketplace.NewInMemoryVendorStore()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	v1, _ := marketplace.NewVendor("v1", "t1", "Vendor 10%", "v1@x.com", 1000, now)
	v2, _ := marketplace.NewVendor("v2", "t1", "Vendor 20%", "v2@x.com", 2000, now)
	_ = vendorStore.Create(context.Background(), v1)
	_ = vendorStore.Create(context.Background(), v2)

	payoutStore := &inMemPayoutStore{}
	engine := marketplace.NewCommissionEngine(marketplace.CommissionEngineConfig{
		VendorStore: vendorStore,
		PayoutStore: payoutStore,
		Now:         func() time.Time { return now },
	})

	r1, _ := engine.Calculate(context.Background(), "t1", "v1", 100000)
	if r1.CommissionCents != 10000 {
		t.Fatalf("v1 commission = %d, want 10000", r1.CommissionCents)
	}

	r2, _ := engine.Calculate(context.Background(), "t1", "v2", 100000)
	if r2.CommissionCents != 20000 {
		t.Fatalf("v2 commission = %d, want 20000", r2.CommissionCents)
	}
}

// Scenario 3: Payout period aggregation correct
func TestMarketplace_PayoutAggregation(t *testing.T) {
	t.Parallel()
	vendorStore := marketplace.NewInMemoryVendorStore()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	v, _ := marketplace.NewVendor("v1", "t1", "Vendor A", "a@x.com", 1000, now)
	_ = vendorStore.Create(context.Background(), v)

	payoutStore := &inMemPayoutStore{}
	seq := 0
	engine := marketplace.NewCommissionEngine(marketplace.CommissionEngineConfig{
		VendorStore: vendorStore,
		PayoutStore: payoutStore,
		IDFunc:      func() string { seq++; return "po-test-" + string(rune('0'+seq)) },
		Now:         func() time.Time { return now },
	})

	from := now.AddDate(0, -1, 0)
	to := now

	_, _ = engine.RecordPayout(context.Background(), "t1", "v1", 10000, from, to)
	_, _ = engine.RecordPayout(context.Background(), "t1", "v1", 15000, from, to)
	_, _ = engine.RecordPayout(context.Background(), "t1", "v1", 5000, from, to)

	report, err := engine.Report(context.Background(), "t1", "v1", from, to)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if report.TotalPayout != 30000 {
		t.Fatalf("total payout = %d, want 30000", report.TotalPayout)
	}
}

type inMemPayoutStore struct {
	payouts []marketplace.VendorPayout
}

func (s *inMemPayoutStore) SavePayout(_ context.Context, p marketplace.VendorPayout) error {
	s.payouts = append(s.payouts, p)
	return nil
}

func (s *inMemPayoutStore) ListPayouts(_ context.Context, tenantID, vendorID string, from, to time.Time) ([]marketplace.VendorPayout, error) {
	var result []marketplace.VendorPayout
	for _, p := range s.payouts {
		if p.TenantID == tenantID && p.VendorID == vendorID &&
			!p.PeriodEnd.Before(from) && !p.PeriodStart.After(to) {
			result = append(result, p)
		}
	}
	return result, nil
}
