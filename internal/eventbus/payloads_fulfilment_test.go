// File scope: v6.1.0 coverage backfill -- payloads_fulfilment.go was
// at 23.5% after v6.0.0 because order normalisation / dropship /
// shipment / returns payloads were exercised indirectly through saga
// tests that did not cover the validate gates or default branches.
package eventbus

import (
	"errors"
	"testing"
	"time"
)

func validOrderNormalised() OrderNormalisedPayload {
	return OrderNormalisedPayload{
		Version:         OrderNormalisedPayloadVersion,
		TenantID:        "tenant-on",
		OrderID:         "ord-1",
		ExternalOrderID: "ext-1",
		Channel:         "tiktok",
		Currency:        "AUD",
		TotalAUDCents:   2000,
		Items:           []OrderNormalisedLine{{SKU: "SKU-1", Quantity: 1, UnitCents: 2000}},
		OccurredAt:      time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
}

func TestOrderNormalisedPayloadValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*OrderNormalisedPayload)
		wantErr bool
	}{
		{name: "ok", mutate: func(*OrderNormalisedPayload) {}},
		{name: "version zero", mutate: func(p *OrderNormalisedPayload) { p.Version = 0 }, wantErr: true},
		{name: "missing tenant", mutate: func(p *OrderNormalisedPayload) { p.TenantID = "" }, wantErr: true},
		{name: "missing order", mutate: func(p *OrderNormalisedPayload) { p.OrderID = "" }, wantErr: true},
		{name: "missing external", mutate: func(p *OrderNormalisedPayload) { p.ExternalOrderID = "" }, wantErr: true},
		{name: "missing channel", mutate: func(p *OrderNormalisedPayload) { p.Channel = "" }, wantErr: true},
		{name: "no items", mutate: func(p *OrderNormalisedPayload) { p.Items = nil }, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validOrderNormalised()
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && !errors.Is(err, ErrOrderNormalisedInvalid) {
				t.Fatalf("err = %v, want ErrOrderNormalisedInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestNewOrderNormalisedEventDefaults(t *testing.T) {
	t.Parallel()
	p := validOrderNormalised()
	p.Version = 0
	ev, err := NewOrderNormalisedEvent("", time.Time{}, p)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if ev.Source != "workflow.order_aggregator" {
		t.Fatalf("Source = %q", ev.Source)
	}
	if ev.Type != OrderNormalised {
		t.Fatalf("Type = %v", ev.Type)
	}
}

func validDropship() DropshipOrderPayload {
	return DropshipOrderPayload{
		Version:       DropshipOrderPayloadVersion,
		TenantID:      "tenant-d",
		OrderID:       "ord-1",
		Supplier:      "1688",
		TotalAUDCents: 5000,
		OccurredAt:    time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
}

func TestDropshipOrderPayloadValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*DropshipOrderPayload)
		wantErr bool
	}{
		{name: "ok", mutate: func(*DropshipOrderPayload) {}},
		{name: "version zero", mutate: func(p *DropshipOrderPayload) { p.Version = 0 }, wantErr: true},
		{name: "missing tenant", mutate: func(p *DropshipOrderPayload) { p.TenantID = "" }, wantErr: true},
		{name: "missing order", mutate: func(p *DropshipOrderPayload) { p.OrderID = "" }, wantErr: true},
		{name: "missing supplier", mutate: func(p *DropshipOrderPayload) { p.Supplier = "" }, wantErr: true},
		{name: "negative total", mutate: func(p *DropshipOrderPayload) { p.TotalAUDCents = -1 }, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validDropship()
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && !errors.Is(err, ErrDropshipOrderPayloadInvalid) {
				t.Fatalf("err = %v, want ErrDropshipOrderPayloadInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestDropshipConstructorsAllKinds(t *testing.T) {
	t.Parallel()
	p := validDropship()
	for _, tc := range []struct {
		name string
		ctor func(string, time.Time, DropshipOrderPayload) (Event, error)
		kind EventType
	}{
		{"pending", NewLargeDropshipOrderPendingApprovalEvent, LargeDropshipOrderPendingApproval},
		{"placed", NewDropshipOrderPlacedEvent, DropshipOrderPlaced},
		{"rolled-back", NewDropshipOrderRolledBackEvent, DropshipOrderRolledBack},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev, err := tc.ctor("", time.Time{}, p)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if ev.Type != tc.kind {
				t.Fatalf("%s: Type = %v, want %v", tc.name, ev.Type, tc.kind)
			}
		})
	}
}

func validShipmentLabel() ShipmentLabelGeneratedPayload {
	return ShipmentLabelGeneratedPayload{
		Version:        ShipmentLabelGeneratedPayloadVersion,
		TenantID:       "tenant-sl",
		OrderID:        "ord-1",
		Carrier:        "auspost",
		TrackingNumber: "AP-12345",
		LabelPDFURL:    "https://example.com/label.pdf",
		CostAUDCents:   2000,
		ETADays:        4,
		SLADays:        5,
		OccurredAt:     time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
}

func TestShipmentLabelPayloadValidateAndDefaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*ShipmentLabelGeneratedPayload)
		wantErr bool
	}{
		{name: "ok", mutate: func(*ShipmentLabelGeneratedPayload) {}},
		{name: "version zero", mutate: func(p *ShipmentLabelGeneratedPayload) { p.Version = 0 }, wantErr: true},
		{name: "missing tenant", mutate: func(p *ShipmentLabelGeneratedPayload) { p.TenantID = "" }, wantErr: true},
		{name: "missing order", mutate: func(p *ShipmentLabelGeneratedPayload) { p.OrderID = "" }, wantErr: true},
		{name: "missing carrier", mutate: func(p *ShipmentLabelGeneratedPayload) { p.Carrier = "" }, wantErr: true},
		{name: "missing tracking", mutate: func(p *ShipmentLabelGeneratedPayload) { p.TrackingNumber = "" }, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validShipmentLabel()
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && !errors.Is(err, ErrShipmentLabelGeneratedInvalid) {
				t.Fatalf("err = %v, want ErrShipmentLabelGeneratedInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
	p := validShipmentLabel()
	p.Version = 0
	ev, err := NewShipmentLabelGeneratedEvent("", time.Time{}, p)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if ev.Source != "agent.fulfilment.shipping_label" {
		t.Fatalf("Source = %q", ev.Source)
	}
	if ev.Type != ShipmentLabelGenerated {
		t.Fatalf("Type = %v", ev.Type)
	}
}

func validShipmentStatus() ShipmentStatusUpdatedPayload {
	return ShipmentStatusUpdatedPayload{
		Version:        ShipmentStatusUpdatedPayloadVersion,
		TenantID:       "tenant-ss",
		OrderID:        "ord-1",
		Carrier:        "auspost",
		TrackingNumber: "AP-12345",
		Status:         "in_transit",
		EventID:        "evt-1",
		OccurredAt:     time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
}

func TestShipmentStatusValidateAndConstructorMapsDeliveredToOrderDelivered(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*ShipmentStatusUpdatedPayload)
		wantErr bool
	}{
		{name: "ok", mutate: func(*ShipmentStatusUpdatedPayload) {}},
		{name: "version zero", mutate: func(p *ShipmentStatusUpdatedPayload) { p.Version = 0 }, wantErr: true},
		{name: "missing tenant", mutate: func(p *ShipmentStatusUpdatedPayload) { p.TenantID = "" }, wantErr: true},
		{name: "missing order", mutate: func(p *ShipmentStatusUpdatedPayload) { p.OrderID = "" }, wantErr: true},
		{name: "missing tracking", mutate: func(p *ShipmentStatusUpdatedPayload) { p.TrackingNumber = "" }, wantErr: true},
		{name: "missing status", mutate: func(p *ShipmentStatusUpdatedPayload) { p.Status = "" }, wantErr: true},
		{name: "missing event_id", mutate: func(p *ShipmentStatusUpdatedPayload) { p.EventID = "" }, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validShipmentStatus()
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && !errors.Is(err, ErrShipmentStatusUpdatedInvalid) {
				t.Fatalf("err = %v, want ErrShipmentStatusUpdatedInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
	// in-transit -> ShipmentStatusUpdated
	transit, err := NewShipmentStatusUpdatedEvent("", time.Time{}, validShipmentStatus())
	if err != nil {
		t.Fatalf("ctor transit: %v", err)
	}
	if transit.Type != ShipmentStatusUpdated {
		t.Fatalf("transit type = %v", transit.Type)
	}
	// delivered -> OrderDelivered
	delivered := validShipmentStatus()
	delivered.Status = "delivered"
	deliveredEv, err := NewShipmentStatusUpdatedEvent("", time.Time{}, delivered)
	if err != nil {
		t.Fatalf("ctor delivered: %v", err)
	}
	if deliveredEv.Type != OrderDelivered {
		t.Fatalf("delivered type = %v, want OrderDelivered", deliveredEv.Type)
	}
}

func validReturnsSaga() ReturnsSagaPayload {
	return ReturnsSagaPayload{
		Version:           ReturnsSagaPayloadVersion,
		TenantID:          "tenant-r",
		RMAID:             "rma-1",
		OrderID:           "ord-1",
		Reason:            "wrong item",
		RefundAmountCents: 2000,
		AutoApproved:      false,
		State:             "requested",
		OccurredAt:        time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
}

func TestReturnsSagaValidateAndConstructors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*ReturnsSagaPayload)
		wantErr bool
	}{
		{name: "ok", mutate: func(*ReturnsSagaPayload) {}},
		{name: "version zero", mutate: func(p *ReturnsSagaPayload) { p.Version = 0 }, wantErr: true},
		{name: "missing tenant", mutate: func(p *ReturnsSagaPayload) { p.TenantID = "" }, wantErr: true},
		{name: "missing rma", mutate: func(p *ReturnsSagaPayload) { p.RMAID = "" }, wantErr: true},
		{name: "missing order", mutate: func(p *ReturnsSagaPayload) { p.OrderID = "" }, wantErr: true},
		{name: "negative refund", mutate: func(p *ReturnsSagaPayload) { p.RefundAmountCents = -1 }, wantErr: true},
		{name: "missing state", mutate: func(p *ReturnsSagaPayload) { p.State = "" }, wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validReturnsSaga()
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr && !errors.Is(err, ErrReturnsSagaPayloadInvalid) {
				t.Fatalf("err = %v, want ErrReturnsSagaPayloadInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		ctor func(string, time.Time, ReturnsSagaPayload) (Event, error)
		kind EventType
	}{
		{"requested", NewReturnRequestedEvent, ReturnRequested},
		{"pending", NewLargeRefundPendingApprovalEvent, LargeRefundPendingApproval},
		{"completed", NewReturnsSagaCompletedEvent, ReturnsSagaCompleted},
		{"rolled-back", NewReturnsSagaRolledBackEvent, ReturnsSagaRolledBack},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev, err := tc.ctor("", time.Time{}, validReturnsSaga())
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if ev.Type != tc.kind {
				t.Fatalf("%s type = %v, want %v", tc.name, ev.Type, tc.kind)
			}
		})
	}
}
