package marketplace

import (
	"context"
	"testing"
	"time"
)

type inMemoryPayoutStore struct {
	payouts []VendorPayout
}

func (s *inMemoryPayoutStore) SavePayout(_ context.Context, p VendorPayout) error {
	s.payouts = append(s.payouts, p)
	return nil
}

func (s *inMemoryPayoutStore) ListPayouts(_ context.Context, tenantID, vendorID string, from, to time.Time) ([]VendorPayout, error) {
	var result []VendorPayout
	for _, p := range s.payouts {
		if p.TenantID == tenantID && p.VendorID == vendorID &&
			!p.PeriodEnd.Before(from) && !p.PeriodStart.After(to) {
			result = append(result, p)
		}
	}
	return result, nil
}

type recordingCommissionMetrics struct {
	records []commissionMetricRecord
}

type commissionMetricRecord struct {
	TenantID    string
	VendorID    string
	AmountCents int64
}

func (m *recordingCommissionMetrics) RecordCommission(tenantID, vendorID string, amountCents int64) {
	m.records = append(m.records, commissionMetricRecord{
		TenantID:    tenantID,
		VendorID:    vendorID,
		AmountCents: amountCents,
	})
}

func newCommissionHarness(t *testing.T) (*CommissionEngine, *InMemoryVendorStore, *inMemoryPayoutStore, *recordingCommissionMetrics) {
	t.Helper()
	vendorStore := NewInMemoryVendorStore()
	payoutStore := &inMemoryPayoutStore{}
	metrics := &recordingCommissionMetrics{}

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	v, _ := NewVendor("v1", "t1", "Vendor Alpha", "alpha@x.com", 1500, now)
	_ = vendorStore.Create(context.Background(), v)

	v2, _ := NewVendor("v2", "t1", "Vendor Zero", "zero@x.com", 0, now)
	_ = vendorStore.Create(context.Background(), v2)

	seq := 0
	engine := NewCommissionEngine(CommissionEngineConfig{
		VendorStore: vendorStore,
		PayoutStore: payoutStore,
		Metrics:     metrics,
		IDFunc:      func() string { seq++; return "po-" + string(rune('0'+seq)) },
		Now:         func() time.Time { return now },
	})
	return engine, vendorStore, payoutStore, metrics
}

func TestCommission_Calculation(t *testing.T) {
	t.Parallel()
	engine, _, _, metrics := newCommissionHarness(t)

	result, err := engine.Calculate(context.Background(), "t1", "v1", 100000)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	// 100000 * 1500 / 10000 = 15000
	if result.CommissionCents != 15000 {
		t.Fatalf("commission = %d, want 15000", result.CommissionCents)
	}
	if result.VendorPayoutCents != 85000 {
		t.Fatalf("vendor_payout = %d, want 85000", result.VendorPayoutCents)
	}
	if result.RateBPS != 1500 {
		t.Fatalf("rate = %d, want 1500", result.RateBPS)
	}
	if len(metrics.records) != 1 {
		t.Fatalf("metrics = %d, want 1", len(metrics.records))
	}
}

func TestCommission_Report(t *testing.T) {
	t.Parallel()
	engine, _, _, _ := newCommissionHarness(t)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	from := now.AddDate(0, -1, 0)
	to := now

	_, _ = engine.RecordPayout(context.Background(), "t1", "v1", 15000, from, to)
	_, _ = engine.RecordPayout(context.Background(), "t1", "v1", 8000, from, to)

	report, err := engine.Report(context.Background(), "t1", "v1", from, to)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if report.TotalPayout != 23000 {
		t.Fatalf("total_payout = %d, want 23000", report.TotalPayout)
	}
}

func TestCommission_PayoutTracking(t *testing.T) {
	t.Parallel()
	engine, _, payoutStore, _ := newCommissionHarness(t)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	payout, err := engine.RecordPayout(context.Background(), "t1", "v1", 15000, now.AddDate(0, -1, 0), now)
	if err != nil {
		t.Fatalf("RecordPayout: %v", err)
	}
	if payout.Status != PayoutStatusPending {
		t.Fatalf("status = %s, want pending", payout.Status)
	}
	if payout.AmountCents != 15000 {
		t.Fatalf("amount = %d, want 15000", payout.AmountCents)
	}
	if len(payoutStore.payouts) != 1 {
		t.Fatalf("payouts = %d, want 1", len(payoutStore.payouts))
	}
}

func TestCommission_ZeroCommissionVendor(t *testing.T) {
	t.Parallel()
	engine, _, _, _ := newCommissionHarness(t)

	result, err := engine.Calculate(context.Background(), "t1", "v2", 50000)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if result.CommissionCents != 0 {
		t.Fatalf("commission = %d, want 0 for zero-rate vendor", result.CommissionCents)
	}
	if result.VendorPayoutCents != 50000 {
		t.Fatalf("vendor_payout = %d, want 50000", result.VendorPayoutCents)
	}
}
