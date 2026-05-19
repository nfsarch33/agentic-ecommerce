package returnlabel

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStubGenerator_GenerateLabel(t *testing.T) {
	t.Parallel()
	g := &StubGenerator{}
	req := ReturnRequest{
		OrderID:  "ord-100",
		Carrier:  CarrierAusPost,
		From:     Address{Name: "Customer", City: "Sydney", Country: "AU"},
		To:       Address{Name: "Warehouse", City: "Melbourne", Country: "AU"},
		Weight:   1.5,
	}

	lbl, err := g.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lbl.TrackingNo == "" {
		t.Error("expected non-empty tracking number")
	}
	if lbl.ID == "" {
		t.Error("expected non-empty label ID")
	}
	if lbl.Carrier != CarrierAusPost {
		t.Errorf("expected carrier %s, got %s", CarrierAusPost, lbl.Carrier)
	}
}

func TestStubGenerator_CorrectCarrier(t *testing.T) {
	t.Parallel()
	g := &StubGenerator{}
	carriers := []string{CarrierAusPost, CarrierFedEx, CarrierUPS, CarrierDHL}

	for _, c := range carriers {
		c := c
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			req := ReturnRequest{OrderID: "ord-200", Carrier: c}
			lbl, err := g.Generate(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lbl.Carrier != c {
				t.Errorf("expected carrier %s, got %s", c, lbl.Carrier)
			}
		})
	}
}

func TestStubGenerator_EmptyOrderID(t *testing.T) {
	t.Parallel()
	g := &StubGenerator{}
	_, err := g.Generate(context.Background(), ReturnRequest{Carrier: CarrierUPS})
	if err == nil {
		t.Error("expected error for empty order ID")
	}
}

func TestQRData_ContainsTracking(t *testing.T) {
	t.Parallel()
	tracking := "ups-ord-999"
	qr := QRData(tracking)

	if !strings.Contains(qr, "tracking=") {
		t.Errorf("QRData should contain 'tracking=', got: %s", qr)
	}
	if !strings.Contains(qr, "ups-ord-999") {
		t.Errorf("QRData should contain the tracking number (possibly encoded), got: %s", qr)
	}
}

func TestLabelStore_SaveGet(t *testing.T) {
	t.Parallel()
	g := &StubGenerator{}
	req := ReturnRequest{OrderID: "ord-300", Carrier: CarrierFedEx}
	lbl, err := g.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	store := &LabelStore{}
	store.Save(lbl)

	got, err := store.Get(lbl.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TrackingNo != lbl.TrackingNo {
		t.Errorf("expected tracking %s, got %s", lbl.TrackingNo, got.TrackingNo)
	}
}

func TestLabelStore_GetNotFound(t *testing.T) {
	t.Parallel()
	store := &LabelStore{}
	_, err := store.Get("nonexistent-id")
	if !errors.Is(err, ErrLabelNotFound) {
		t.Errorf("expected ErrLabelNotFound, got %v", err)
	}
}

func TestLabelStore_ByOrder(t *testing.T) {
	t.Parallel()
	g := &StubGenerator{}
	store := &LabelStore{}

	// Generate two labels for the same order with different carriers.
	for _, carrier := range []string{CarrierUPS, CarrierDHL} {
		req := ReturnRequest{OrderID: "ord-400", Carrier: carrier}
		lbl, err := g.Generate(context.Background(), req)
		if err != nil {
			t.Fatalf("generate error: %v", err)
		}
		store.Save(lbl)
	}

	// Add a label for a different order.
	other := ReturnRequest{OrderID: "ord-999", Carrier: CarrierFedEx}
	otherLbl, _ := g.Generate(context.Background(), other)
	store.Save(otherLbl)

	lbls := store.ByOrder("ord-400")
	if len(lbls) != 2 {
		t.Errorf("expected 2 labels for ord-400, got %d", len(lbls))
	}
}

func TestLabelStore_ByOrderEmpty(t *testing.T) {
	t.Parallel()
	store := &LabelStore{}
	lbls := store.ByOrder("missing-order")
	if len(lbls) != 0 {
		t.Errorf("expected empty slice, got %d", len(lbls))
	}
}

func TestStubGenerator_Deterministic(t *testing.T) {
	t.Parallel()
	g := &StubGenerator{}
	req := ReturnRequest{OrderID: "ord-111", Carrier: CarrierAusPost}

	lbl1, _ := g.Generate(context.Background(), req)
	lbl2, _ := g.Generate(context.Background(), req)

	if lbl1.TrackingNo != lbl2.TrackingNo {
		t.Errorf("expected deterministic tracking: %s != %s", lbl1.TrackingNo, lbl2.TrackingNo)
	}
	if lbl1.ID != lbl2.ID {
		t.Errorf("expected deterministic ID: %s != %s", lbl1.ID, lbl2.ID)
	}
}
