package eventbus

import (
	"testing"
	"time"
)

func TestProductEnrichedPayload_Validate_Coverage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload ProductEnrichedPayload
		wantErr bool
	}{
		{
			name: "valid",
			payload: ProductEnrichedPayload{
				Version: ProductEnrichedPayloadVersion, TenantID: "t1",
				ProductID: "p1", EnglishTitle: "Title", PriceCents: 100, Currency: "AUD",
			},
		},
		{name: "missing_tenant", payload: ProductEnrichedPayload{
			Version: ProductEnrichedPayloadVersion, ProductID: "p1",
			EnglishTitle: "Title", PriceCents: 100, Currency: "AUD",
		}, wantErr: true},
		{name: "missing_product", payload: ProductEnrichedPayload{
			Version: ProductEnrichedPayloadVersion, TenantID: "t1",
			EnglishTitle: "Title", PriceCents: 100, Currency: "AUD",
		}, wantErr: true},
		{name: "missing_title", payload: ProductEnrichedPayload{
			Version: ProductEnrichedPayloadVersion, TenantID: "t1",
			ProductID: "p1", PriceCents: 100, Currency: "AUD",
		}, wantErr: true},
		{name: "zero_price", payload: ProductEnrichedPayload{
			Version: ProductEnrichedPayloadVersion, TenantID: "t1",
			ProductID: "p1", EnglishTitle: "Title", PriceCents: 0, Currency: "AUD",
		}, wantErr: true},
		{name: "negative_price", payload: ProductEnrichedPayload{
			Version: ProductEnrichedPayloadVersion, TenantID: "t1",
			ProductID: "p1", EnglishTitle: "Title", PriceCents: -1, Currency: "AUD",
		}, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.payload.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestProductEnrichedPayload_AsMap_Coverage(t *testing.T) {
	t.Parallel()
	p := ProductEnrichedPayload{
		Version: ProductEnrichedPayloadVersion, TenantID: "t1",
		ProductID: "p1", EnglishTitle: "Test", PriceCents: 500, Currency: "AUD",
	}
	m := p.asMap()
	if m["tenant_id"] != "t1" {
		t.Fatalf("tenant_id=%v want t1", m["tenant_id"])
	}
	if m["product_id"] != "p1" {
		t.Fatalf("product_id=%v want p1", m["product_id"])
	}
}

func TestNewProductEnrichedEvent_Coverage(t *testing.T) {
	t.Parallel()
	evt, err := NewProductEnrichedEvent("test", time.Now().UTC(), ProductEnrichedPayload{
		Version: ProductEnrichedPayloadVersion, TenantID: "t1",
		ProductID: "p1", EnglishTitle: "Title", PriceCents: 100, Currency: "AUD",
	})
	if err != nil {
		t.Fatalf("NewProductEnrichedEvent: %v", err)
	}
	if evt.Type != ProductEnriched {
		t.Fatalf("type=%v want=%v", evt.Type, ProductEnriched)
	}
}

func TestNewProductEnrichedEvent_Invalid_Coverage(t *testing.T) {
	t.Parallel()
	_, err := NewProductEnrichedEvent("test", time.Now().UTC(), ProductEnrichedPayload{})
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestOrderReceivedPayload_Validate_Coverage(t *testing.T) {
	t.Parallel()
	valid := OrderReceivedPayload{
		Version: OrderReceivedPayloadVersion, TenantID: "t1",
		OrderID: "o1", Channel: "tiktok", TotalCents: 500, Currency: "AUD",
		Items:          []OrderReceivedLine{{SKU: "sku-1", Quantity: 1, UnitCents: 500}},
		IdempotencyKey: "idem-1",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Validate: %v", err)
	}

	invalid := OrderReceivedPayload{Version: OrderReceivedPayloadVersion}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected error for incomplete payload")
	}
}

func TestOrderReceivedPayload_AsMap_Coverage(t *testing.T) {
	t.Parallel()
	p := OrderReceivedPayload{
		Version: OrderReceivedPayloadVersion, TenantID: "t1",
		OrderID: "o1", Channel: "tiktok", TotalCents: 500, Currency: "AUD",
		Items:          []OrderReceivedLine{{SKU: "sku-1", Quantity: 1, UnitCents: 500}},
		IdempotencyKey: "idem-1",
	}
	m := p.asMap()
	if m["tenant_id"] != "t1" {
		t.Fatalf("tenant_id=%v want t1", m["tenant_id"])
	}
}

func TestNewOrderReceivedEvent_Coverage(t *testing.T) {
	t.Parallel()
	evt, err := NewOrderReceivedEvent("test", time.Now().UTC(), OrderReceivedPayload{
		Version: OrderReceivedPayloadVersion, TenantID: "t1",
		OrderID: "o1", Channel: "tiktok", TotalCents: 500, Currency: "AUD",
		Items:          []OrderReceivedLine{{SKU: "sku-1", Quantity: 1, UnitCents: 500}},
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("NewOrderReceivedEvent: %v", err)
	}
	if evt.Type != OrderReceived {
		t.Fatalf("type=%v want=%v", evt.Type, OrderReceived)
	}
}

func TestTikTokListingRollbackPayload_Coverage(t *testing.T) {
	t.Parallel()
	p := TikTokListingRollbackPayload{
		Version: 1, TenantID: "t1", ProductID: "p1",
		Reason: "test", Stage: "publish",
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	m := p.asMap()
	if m["tenant_id"] != "t1" {
		t.Fatalf("tenant_id=%v want t1", m["tenant_id"])
	}
	evt, err := NewTikTokListingRollbackEvent("test", time.Now().UTC(), p)
	if err != nil {
		t.Fatalf("NewTikTokListingRollbackEvent: %v", err)
	}
	if evt.Type != TikTokListingRolledBack {
		t.Fatalf("type=%v want=%v", evt.Type, TikTokListingRolledBack)
	}
}

func TestSupplierCostChangedPayload_Coverage(t *testing.T) {
	t.Parallel()
	p := SupplierCostChangedPayload{
		Version: 1, TenantID: "t1", Source: "1688",
		SupplierSKU: "sku-1", BaselineCNYCents: 100,
		ObservedCNYCents: 200, DeltaPct: 100.0,
		Direction: "up", ThresholdPct: 10.0,
		ObservedAt: time.Now().UTC(),
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	m := p.asMap()
	if m["tenant_id"] != "t1" {
		t.Fatalf("tenant_id=%v want t1", m["tenant_id"])
	}
	evt, err := NewSupplierCostChangedEvent("test", time.Now().UTC(), p)
	if err != nil {
		t.Fatalf("NewSupplierCostChangedEvent: %v", err)
	}
	if evt.Type != SupplierCostChanged {
		t.Fatalf("type=%v want=%v", evt.Type, SupplierCostChanged)
	}
}
