package eventbus

import (
	"errors"
	"testing"
	"time"
)

func TestSourcingProposalPayload_ValidateOk(t *testing.T) {
	t.Parallel()

	p := validSourcingPayload()
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestSourcingProposalPayload_ValidateRejectsMissingFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mut  func(p *SourcingProposalPayload)
	}{
		{name: "version zero", mut: func(p *SourcingProposalPayload) { p.Version = 0 }},
		{name: "tenant missing", mut: func(p *SourcingProposalPayload) { p.TenantID = "" }},
		{name: "keyword missing", mut: func(p *SourcingProposalPayload) { p.Keyword = "" }},
		{name: "no products", mut: func(p *SourcingProposalPayload) { p.SelectedProducts = nil }},
		{name: "product missing id", mut: func(p *SourcingProposalPayload) { p.SelectedProducts[0].ExternalID = "" }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validSourcingPayload()
			tc.mut(&p)
			err := p.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrSourcingPayloadInvalid) {
				t.Fatalf("error not wrapping ErrSourcingPayloadInvalid: %v", err)
			}
		})
	}
}

func TestNewSourcingProposalEvent_DefaultsAndValidation(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		evt, err := NewSourcingProposalEvent("", time.Time{}, validSourcingPayload())
		if err != nil {
			t.Fatalf("NewSourcingProposalEvent: %v", err)
		}
		if evt.Source != "agent.sourcing.china" {
			t.Fatalf("Source = %q, want default", evt.Source)
		}
		if evt.Timestamp.IsZero() {
			t.Fatal("Timestamp not set")
		}
		if evt.Type != ProductSourcingProposed {
			t.Fatalf("Type = %q", evt.Type)
		}
		products, ok := evt.Payload["selected_products"].([]any)
		if !ok || len(products) != 1 {
			t.Fatalf("selected_products payload = %T (%v)", evt.Payload["selected_products"], evt.Payload["selected_products"])
		}
	})

	t.Run("rejects invalid payload", func(t *testing.T) {
		t.Parallel()
		bad := validSourcingPayload()
		bad.TenantID = ""
		_, err := NewSourcingProposalEvent("test", time.Time{}, bad)
		if !errors.Is(err, ErrSourcingPayloadInvalid) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("preserves explicit source and time", func(t *testing.T) {
		t.Parallel()
		when := time.Date(2026, 5, 9, 21, 0, 0, 0, time.UTC)
		evt, err := NewSourcingProposalEvent("custom-source", when, validSourcingPayload())
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if evt.Source != "custom-source" {
			t.Fatalf("source = %q", evt.Source)
		}
		if !evt.Timestamp.Equal(when) {
			t.Fatalf("timestamp = %v", evt.Timestamp)
		}
	})
}

func validSourcingPayload() SourcingProposalPayload {
	return SourcingProposalPayload{
		Version:  SourcingProposalPayloadVersion,
		TenantID: "cylrl",
		Keyword:  "wireless earbuds",
		Source:   "1688",
		SelectedProducts: []SourcingProposalProduct{{
			ExternalID:    "earbud-A",
			Source:        "1688",
			Title:         "Wireless Earbuds Model A",
			Category:      "electronics",
			PriceCNYCents: 1500,
			MOQ:           20,
			LeadTimeDays:  10,
			SupplierID:    "sup-A",
			SupplierScore: 0.85,
			URL:           "https://example.com/earbuds-a",
		}},
		RejectedReasons: map[string]int{"vape": 1},
		RejectedCount:   1,
		SupplierScore:   0.85,
		MarginScore:     0.65,
		TrendScore:      0.42,
		CompositeScore:  0.7,
	}
}
