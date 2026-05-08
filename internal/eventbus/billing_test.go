package eventbus

import (
	"errors"
	"testing"
	"time"
)

func TestIsBillingEvent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   EventType
		want bool
	}{
		{"sub created", SubscriptionCreated, true},
		{"sub updated", SubscriptionUpdated, true},
		{"sub canceled", SubscriptionCanceled, true},
		{"invoice paid", InvoicePaid, true},
		{"invoice failed", InvoiceFailed, true},
		{"membership not billing", MembershipCreated, false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsBillingEvent(tc.in); got != tc.want {
				t.Fatalf("IsBillingEvent(%q) = %v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestBillingPayloadValidate(t *testing.T) {
	t.Parallel()
	base := BillingPayload{Version: BillingPayloadVersion, TenantID: "t", SubscriptionID: "sub_1"}
	if err := base.Validate(); err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*BillingPayload)
	}{
		{"missing version", func(p *BillingPayload) { p.Version = 0 }},
		{"missing tenant", func(p *BillingPayload) { p.TenantID = "" }},
		{"missing both ids", func(p *BillingPayload) { p.SubscriptionID = ""; p.InvoiceID = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := base
			tc.mutate(&p)
			if err := p.Validate(); !errors.Is(err, ErrBillingPayloadInvalid) {
				t.Fatalf("expected ErrBillingPayloadInvalid, got %v", err)
			}
		})
	}
}

func TestNewBillingEvent(t *testing.T) {
	t.Parallel()
	occurred := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	payload := BillingPayload{TenantID: "tenant-a", SubscriptionID: "sub_1", PlanID: "free", State: "trialing"}
	cases := []struct {
		name      string
		eventType EventType
		source    string
		expSource string
		wantErr   bool
	}{
		{"created with default source", SubscriptionCreated, "", "mc-api.billing", false},
		{"updated explicit source", SubscriptionUpdated, "stripe", "stripe", false},
		{"canceled", SubscriptionCanceled, "", "mc-api.billing", false},
		{"non-billing rejected", ProductCreated, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			evt, err := NewBillingEvent(tc.eventType, tc.source, occurred, payload)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if evt.Type != tc.eventType {
				t.Fatalf("type = %v", evt.Type)
			}
			if evt.Source != tc.expSource {
				t.Fatalf("source = %s", evt.Source)
			}
			if evt.TenantID != "tenant-a" {
				t.Fatalf("tenant = %s", evt.TenantID)
			}
		})
	}
}

func TestNewBillingEventDefaults(t *testing.T) {
	t.Parallel()
	payload := BillingPayload{TenantID: "tenant-a", InvoiceID: "inv_1"}
	evt, err := NewBillingEvent(InvoicePaid, "", time.Time{}, payload)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if evt.Timestamp.IsZero() {
		t.Fatalf("expected default timestamp")
	}
}
