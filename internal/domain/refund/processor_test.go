package refund_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/domain/refund"
)

func testOrder(amount int) refund.Order {
	return refund.Order{ID: "o1", PaidAmount: amount, PaidAt: time.Now()}
}

func TestFullRefund(t *testing.T) {
	t.Parallel()
	proc := refund.NewRefundProcessor(refund.NewDefaultPolicy(30 * 24 * time.Hour))
	r, err := proc.RequestRefund("o1", testOrder(5000), []refund.RefundItem{{SKU: "SKU-1", Amount: 5000}}, "not needed")
	if err != nil {
		t.Fatalf("RequestRefund: %v", err)
	}
	if r.Total != 5000 {
		t.Fatalf("expected full refund 5000, got %d", r.Total)
	}
	if r.State != refund.StateRequested {
		t.Fatalf("expected requested state, got %s", r.State)
	}
}

func TestPartialRefund(t *testing.T) {
	t.Parallel()
	proc := refund.NewRefundProcessor(refund.NewDefaultPolicy(30 * 24 * time.Hour))
	r, err := proc.RequestRefund("o1", testOrder(5000), []refund.RefundItem{{SKU: "SKU-1", Amount: 2000}}, "damaged item")
	if err != nil {
		t.Fatalf("RequestRefund: %v", err)
	}
	if r.Total != 2000 {
		t.Fatalf("expected partial refund 2000, got %d", r.Total)
	}
}

func TestIneligible_PastWindow(t *testing.T) {
	t.Parallel()
	proc := refund.NewRefundProcessor(refund.NewDefaultPolicy(24 * time.Hour))
	order := refund.Order{ID: "o1", PaidAmount: 5000, PaidAt: time.Now().Add(-48 * time.Hour)}
	_, err := proc.RequestRefund("o1", order, []refund.RefundItem{{SKU: "SKU-1", Amount: 5000}}, "too late")
	if !errors.Is(err, refund.ErrIneligible) {
		t.Fatalf("expected ErrIneligible, got %v", err)
	}
}

func TestOverRefundPrevention(t *testing.T) {
	t.Parallel()
	proc := refund.NewRefundProcessor(refund.NewDefaultPolicy(30 * 24 * time.Hour))
	_, err := proc.RequestRefund("o1", testOrder(1000), []refund.RefundItem{{SKU: "SKU-1", Amount: 2000}}, "over-refund")
	if !errors.Is(err, refund.ErrExceedsOriginal) {
		t.Fatalf("expected ErrExceedsOriginal, got %v", err)
	}
}

func TestStateTransitions(t *testing.T) {
	t.Parallel()
	proc := refund.NewRefundProcessor(refund.NewDefaultPolicy(30 * 24 * time.Hour))
	r, _ := proc.RequestRefund("o1", testOrder(3000), []refund.RefundItem{{SKU: "SKU-1", Amount: 3000}}, "reason")

	r, err := proc.Approve(r.ID)
	if err != nil || r.State != refund.StateApproved {
		t.Fatalf("Approve: %v / %s", err, r.State)
	}
	r, err = proc.Process(r.ID)
	if err != nil || r.State != refund.StateProcessed {
		t.Fatalf("Process: %v / %s", err, r.State)
	}
	r, err = proc.Complete(r.ID)
	if err != nil || r.State != refund.StateCompleted {
		t.Fatalf("Complete: %v / %s", err, r.State)
	}
}

func TestDenyRefund(t *testing.T) {
	t.Parallel()
	proc := refund.NewRefundProcessor(refund.NewDefaultPolicy(30 * 24 * time.Hour))
	r, _ := proc.RequestRefund("o1", testOrder(3000), []refund.RefundItem{{SKU: "SKU-1", Amount: 3000}}, "reason")
	r, err := proc.Deny(r.ID, "policy violation")
	if err != nil || r.State != refund.StateDenied {
		t.Fatalf("Deny: %v / %s", err, r.State)
	}
}

func TestStoreCreditOption(t *testing.T) {
	t.Parallel()
	proc := refund.NewRefundProcessor(refund.NewDefaultPolicy(30 * 24 * time.Hour))
	r, err := proc.RequestRefundWithCredit("o1", testOrder(3000), []refund.RefundItem{{SKU: "SKU-1", Amount: 3000}}, "prefer credit")
	if err != nil {
		t.Fatalf("RequestRefundWithCredit: %v", err)
	}
	if !r.StoreCredit {
		t.Fatal("expected store credit flag")
	}
}
