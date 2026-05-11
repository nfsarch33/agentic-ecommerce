// File scope: v6.1.0 coverage backfill -- payloads_pricing.go had
// 17.9% coverage after v6.0.0 because the SupplierCost / PriceChange /
// CompetitorPrice constructors and validation paths were only
// exercised indirectly through workflow tests.
package eventbus

import (
	"errors"
	"testing"
	"time"
)

func TestSupplierCostChangedPayloadValidate(t *testing.T) {
	t.Parallel()
	base := SupplierCostChangedPayload{
		Version:          SupplierCostChangedPayloadVersion,
		TenantID:         "tenant-sc",
		Source:           "1688",
		SupplierSKU:      "SKU-1",
		BaselineCNYCents: 1000,
		ObservedCNYCents: 1100,
		DeltaPct:         0.1,
		Direction:        "up",
		ThresholdPct:     0.05,
		ObservedAt:       time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
	cases := []struct {
		name    string
		mutate  func(*SupplierCostChangedPayload)
		wantErr bool
	}{
		{name: "ok", mutate: func(*SupplierCostChangedPayload) {}},
		{name: "version zero", mutate: func(p *SupplierCostChangedPayload) { p.Version = 0 }, wantErr: true},
		{name: "missing tenant", mutate: func(p *SupplierCostChangedPayload) { p.TenantID = "" }, wantErr: true},
		{name: "missing source", mutate: func(p *SupplierCostChangedPayload) { p.Source = "" }, wantErr: true},
		{name: "missing sku", mutate: func(p *SupplierCostChangedPayload) { p.SupplierSKU = "" }, wantErr: true},
		{name: "negative baseline", mutate: func(p *SupplierCostChangedPayload) { p.BaselineCNYCents = -1 }, wantErr: true},
		{name: "bad direction", mutate: func(p *SupplierCostChangedPayload) { p.Direction = "sideways" }, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := base
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && !errors.Is(err, ErrSupplierCostChangedInvalid) {
				t.Fatalf("err = %v, want ErrSupplierCostChangedInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestNewSupplierCostChangedEventDefaultsAndShape(t *testing.T) {
	t.Parallel()
	p := SupplierCostChangedPayload{
		TenantID:         "tenant-sc",
		Source:           "1688",
		SupplierSKU:      "SKU-1",
		BaselineCNYCents: 1000,
		ObservedCNYCents: 1200,
		DeltaPct:         0.2,
		Direction:        "up",
	}
	ev, err := NewSupplierCostChangedEvent("", time.Time{}, p)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if ev.Source != "monitor.supplier_cost" {
		t.Fatalf("Source = %q, want monitor.supplier_cost", ev.Source)
	}
	if ev.Type != SupplierCostChanged {
		t.Fatalf("Type = %v", ev.Type)
	}
	if ev.Timestamp.IsZero() {
		t.Fatal("Timestamp default not set")
	}
}

func TestPriceChangeApprovalPayloadValidate(t *testing.T) {
	t.Parallel()
	base := PriceChangeApprovalPayload{
		Version:               PriceChangePayloadVersion,
		TenantID:              "tenant-pc",
		ProductID:             "prod-1",
		Channel:               "tiktok",
		OldPriceAUDCents:      1000,
		ProposedPriceAUDCents: 900,
		DeltaPct:              -0.1,
		Reason:                "competitor undercut",
		DecisionSource:        "agent",
		OccurredAt:            time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
	cases := []struct {
		name    string
		mutate  func(*PriceChangeApprovalPayload)
		wantErr bool
	}{
		{name: "ok", mutate: func(*PriceChangeApprovalPayload) {}},
		{name: "version zero", mutate: func(p *PriceChangeApprovalPayload) { p.Version = 0 }, wantErr: true},
		{name: "missing tenant", mutate: func(p *PriceChangeApprovalPayload) { p.TenantID = "" }, wantErr: true},
		{name: "missing product", mutate: func(p *PriceChangeApprovalPayload) { p.ProductID = "" }, wantErr: true},
		{name: "missing channel", mutate: func(p *PriceChangeApprovalPayload) { p.Channel = "" }, wantErr: true},
		{name: "proposed zero", mutate: func(p *PriceChangeApprovalPayload) { p.ProposedPriceAUDCents = 0 }, wantErr: true},
		{name: "old negative", mutate: func(p *PriceChangeApprovalPayload) { p.OldPriceAUDCents = -1 }, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := base
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && !errors.Is(err, ErrPriceChangePayloadInvalid) {
				t.Fatalf("err = %v, want ErrPriceChangePayloadInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestPriceChangeConstructorsDefault(t *testing.T) {
	t.Parallel()
	p := PriceChangeApprovalPayload{
		TenantID:              "tenant-pc",
		ProductID:             "prod-1",
		Channel:               "tiktok",
		ProposedPriceAUDCents: 1000,
	}
	pending, err := NewPriceChangePendingApprovalEvent("", time.Time{}, p)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if pending.Type != PriceChangePendingApproval {
		t.Fatalf("type = %v", pending.Type)
	}
	applied, err := NewPriceChangeAppliedEvent("explicit-src", time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC), p)
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if applied.Source != "explicit-src" {
		t.Fatalf("Source = %q, want explicit-src", applied.Source)
	}
	if applied.Type != PriceChangeApplied {
		t.Fatalf("type = %v", applied.Type)
	}
}

func TestCompetitorPricePayloadValidate(t *testing.T) {
	t.Parallel()
	base := CompetitorPricePayload{
		Version:       CompetitorPricePayloadVersion,
		TenantID:      "tenant-cp",
		SKU:           "SKU-A",
		Channel:       "tiktok",
		CompetitorID:  "comp-1",
		PriceAUDCents: 500,
		ObservedAt:    time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
	cases := []struct {
		name    string
		mutate  func(*CompetitorPricePayload)
		wantErr bool
	}{
		{name: "ok", mutate: func(*CompetitorPricePayload) {}},
		{name: "version zero", mutate: func(p *CompetitorPricePayload) { p.Version = 0 }, wantErr: true},
		{name: "missing tenant", mutate: func(p *CompetitorPricePayload) { p.TenantID = "" }, wantErr: true},
		{name: "missing sku", mutate: func(p *CompetitorPricePayload) { p.SKU = "" }, wantErr: true},
		{name: "missing channel", mutate: func(p *CompetitorPricePayload) { p.Channel = "" }, wantErr: true},
		{name: "missing competitor", mutate: func(p *CompetitorPricePayload) { p.CompetitorID = "" }, wantErr: true},
		{name: "negative price", mutate: func(p *CompetitorPricePayload) { p.PriceAUDCents = -1 }, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := base
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && !errors.Is(err, ErrCompetitorPricePayloadInvalid) {
				t.Fatalf("err = %v, want ErrCompetitorPricePayloadInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestCompetitorPriceConstructorsDefaults(t *testing.T) {
	t.Parallel()
	p := CompetitorPricePayload{
		TenantID:      "tenant-cp",
		SKU:           "SKU-A",
		Channel:       "tiktok",
		CompetitorID:  "comp-1",
		PriceAUDCents: 500,
	}
	obs, err := NewCompetitorPriceObservedEvent("", time.Time{}, p)
	if err != nil {
		t.Fatalf("obs: %v", err)
	}
	if obs.Source != "agent.pricing.competitor_scraper" {
		t.Fatalf("Source = %q, want default", obs.Source)
	}
	if obs.Type != CompetitorPriceObserved {
		t.Fatalf("type = %v", obs.Type)
	}
	cut, err := NewCompetitorUndercutEvent("custom", time.Time{}, p)
	if err != nil {
		t.Fatalf("undercut: %v", err)
	}
	if cut.Source != "custom" {
		t.Fatalf("Source = %q, want custom", cut.Source)
	}
	if cut.Type != CompetitorUndercut {
		t.Fatalf("type = %v", cut.Type)
	}
}
