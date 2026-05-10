package eventbus

import (
	"testing"
	"time"
)

func TestReturnsSagaPayload_Validate_Coverage(t *testing.T) {
	t.Parallel()
	valid := ReturnsSagaPayload{
		Version: 1, TenantID: "t1", RMAID: "rma-1",
		OrderID: "o1", Reason: "defective",
		RefundAmountCents: 500, State: "requested",
		OccurredAt: time.Now().UTC(),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	invalid := ReturnsSagaPayload{Version: 1}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected error for incomplete returns saga")
	}
}

func TestReturnsSagaPayload_AsMapAndEvent(t *testing.T) {
	t.Parallel()
	p := ReturnsSagaPayload{
		Version: 1, TenantID: "t1", RMAID: "rma-1",
		OrderID: "o1", Reason: "defective", State: "requested",
		RefundAmountCents: 500,
		OccurredAt:        time.Now().UTC(),
	}
	m := p.asMap()
	if m["tenant_id"] != "t1" {
		t.Fatalf("tenant_id=%v want t1", m["tenant_id"])
	}
	evt, err := NewReturnRequestedEvent("test", time.Now().UTC(), p)
	if err != nil {
		t.Fatalf("NewReturnRequestedEvent: %v", err)
	}
	if evt.Type != ReturnRequested {
		t.Fatalf("type=%v want=%v", evt.Type, ReturnRequested)
	}
	_, err = NewReturnsSagaCompletedEvent("test", time.Now().UTC(), p)
	if err != nil {
		t.Fatalf("NewReturnsSagaCompletedEvent: %v", err)
	}
	_, err = NewReturnsSagaRolledBackEvent("test", time.Now().UTC(), p)
	if err != nil {
		t.Fatalf("NewReturnsSagaRolledBackEvent: %v", err)
	}
}

func TestPaymentSagaPayload_Validate_Coverage(t *testing.T) {
	t.Parallel()
	valid := PaymentSagaPayload{
		Version: 1, TenantID: "t1", OrderID: "o1",
		Provider: "stripe", AmountCents: 500, Currency: "AUD",
		Status: "completed",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	invalid := PaymentSagaPayload{Version: 1}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected error for incomplete payment saga")
	}
}

func TestPaymentSagaPayload_AsMapAndEvent(t *testing.T) {
	t.Parallel()
	p := PaymentSagaPayload{
		Version: 1, TenantID: "t1", OrderID: "o1",
		Provider: "stripe", AmountCents: 500, Currency: "AUD",
		Status: "completed",
	}
	m := p.asMap()
	if m["tenant_id"] != "t1" {
		t.Fatalf("tenant_id=%v want t1", m["tenant_id"])
	}
	evt, err := NewPaymentCompletedEvent("test", time.Now().UTC(), p)
	if err != nil {
		t.Fatalf("NewPaymentCompletedEvent: %v", err)
	}
	if evt.Type != PaymentCompleted {
		t.Fatalf("type=%v want=%v", evt.Type, PaymentCompleted)
	}
	_, err = NewPaymentFailedEvent("test", time.Now().UTC(), p)
	if err != nil {
		t.Fatalf("NewPaymentFailedEvent: %v", err)
	}
	_, err = NewPaymentRefundRequestedEvent("test", time.Now().UTC(), p)
	if err != nil {
		t.Fatalf("NewPaymentRefundRequestedEvent: %v", err)
	}
}

func TestSupplierCostChangedPayload_Validate_EdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload SupplierCostChangedPayload
		wantErr bool
	}{
		{name: "missing_tenant", payload: SupplierCostChangedPayload{Version: 1, Source: "1688", SupplierSKU: "s1"}, wantErr: true},
		{name: "missing_source", payload: SupplierCostChangedPayload{Version: 1, TenantID: "t1", SupplierSKU: "s1"}, wantErr: true},
		{name: "missing_sku", payload: SupplierCostChangedPayload{Version: 1, TenantID: "t1", Source: "1688"}, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.payload.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestOrderReceivedPayload_Validate_EdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload OrderReceivedPayload
		wantErr bool
	}{
		{name: "missing_order_id", payload: OrderReceivedPayload{
			Version: OrderReceivedPayloadVersion, TenantID: "t1",
			Channel: "tiktok", TotalCents: 500, Currency: "AUD",
			Items:          []OrderReceivedLine{{SKU: "s1", Quantity: 1, UnitCents: 500}},
			IdempotencyKey: "idem-1",
		}, wantErr: true},
		{name: "missing_channel", payload: OrderReceivedPayload{
			Version: OrderReceivedPayloadVersion, TenantID: "t1",
			OrderID: "o1", TotalCents: 500, Currency: "AUD",
			Items:          []OrderReceivedLine{{SKU: "s1", Quantity: 1, UnitCents: 500}},
			IdempotencyKey: "idem-1",
		}, wantErr: true},
		{name: "missing_idempotency", payload: OrderReceivedPayload{
			Version: OrderReceivedPayloadVersion, TenantID: "t1",
			OrderID: "o1", Channel: "tiktok", TotalCents: 500, Currency: "AUD",
			Items: []OrderReceivedLine{{SKU: "s1", Quantity: 1, UnitCents: 500}},
		}, wantErr: true},
		{name: "empty_items", payload: OrderReceivedPayload{
			Version: OrderReceivedPayloadVersion, TenantID: "t1",
			OrderID: "o1", Channel: "tiktok", TotalCents: 500, Currency: "AUD",
			Items:          []OrderReceivedLine{},
			IdempotencyKey: "idem-1",
		}, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.payload.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestTikTokListingRollbackPayload_Validate_EdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload TikTokListingRollbackPayload
		wantErr bool
	}{
		{name: "valid", payload: TikTokListingRollbackPayload{
			Version: 1, TenantID: "t1", ProductID: "p1", Reason: "r", Stage: "s",
		}},
		{name: "missing_version", payload: TikTokListingRollbackPayload{
			TenantID: "t1", ProductID: "p1", Reason: "r",
		}, wantErr: true},
		{name: "missing_tenant", payload: TikTokListingRollbackPayload{
			Version: 1, ProductID: "p1", Reason: "r",
		}, wantErr: true},
		{name: "missing_product", payload: TikTokListingRollbackPayload{
			Version: 1, TenantID: "t1", Reason: "r",
		}, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.payload.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
