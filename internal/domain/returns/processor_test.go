package returns_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/domain/returns"
)

type mockRefunder struct{ calls []string }

func (m *mockRefunder) InitiateRefund(requestID, _ string) error {
	m.calls = append(m.calls, requestID)
	return nil
}

func makeReturn(id, orderID string) returns.ReturnRequest {
	return returns.ReturnRequest{
		ID:        id,
		OrderID:   orderID,
		Items:     []returns.ReturnItem{{SKU: "SKU-1", Quantity: 1}},
		Reason:    "defective",
		CreatedAt: time.Now(),
	}
}

func TestReturns_CreateReturn(t *testing.T) {
	t.Parallel()
	rp := returns.NewReturnProcessor(nil, 30*24*time.Hour)
	if err := rp.CreateReturn(makeReturn("R1", "O1")); err != nil {
		t.Fatalf("CreateReturn: %v", err)
	}
}

func TestReturns_ApproveTriggersRefund(t *testing.T) {
	t.Parallel()
	m := &mockRefunder{}
	rp := returns.NewReturnProcessor(m, 30*24*time.Hour)
	rp.CreateReturn(makeReturn("R2", "O2"))
	if err := rp.Approve("R2"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if len(m.calls) != 1 || m.calls[0] != "R2" {
		t.Fatalf("expected refund call, got %v", m.calls)
	}
}

func TestReturns_RejectWithReason(t *testing.T) {
	t.Parallel()
	rp := returns.NewReturnProcessor(nil, 30*24*time.Hour)
	rp.CreateReturn(makeReturn("R3", "O3"))
	if err := rp.Reject("R3", "outside policy"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	r, _ := rp.Get("R3")
	if r.RejectedNote != "outside policy" {
		t.Fatalf("expected rejection note, got %q", r.RejectedNote)
	}
}

func TestReturns_ExchangeCreatesRequest(t *testing.T) {
	t.Parallel()
	rp := returns.NewReturnProcessor(nil, 30*24*time.Hour)
	ex := returns.ExchangeRequest{
		ReturnRequest:    makeReturn("R4", "O4"),
		ReplacementItems: []returns.ReturnItem{{SKU: "SKU-2", Quantity: 1}},
	}
	if err := rp.CreateExchange(ex); err != nil {
		t.Fatalf("CreateExchange: %v", err)
	}
}

func TestReturns_DoubleApproveIdempotent(t *testing.T) {
	t.Parallel()
	m := &mockRefunder{}
	rp := returns.NewReturnProcessor(m, 30*24*time.Hour)
	rp.CreateReturn(makeReturn("R5", "O5"))
	rp.Approve("R5")
	if err := rp.Approve("R5"); err != nil {
		t.Fatalf("second Approve: %v", err)
	}
	if len(m.calls) != 1 {
		t.Fatalf("expected 1 refund call (idempotent), got %d", len(m.calls))
	}
}

func TestReturns_InvalidStateTransition(t *testing.T) {
	t.Parallel()
	rp := returns.NewReturnProcessor(nil, 30*24*time.Hour)
	rp.CreateReturn(makeReturn("R6", "O6"))
	rp.Reject("R6", "reason")
	if err := rp.Approve("R6"); err == nil {
		t.Fatal("expected error approving a rejected request")
	}
}

func TestReturns_ExpiredWindowRejection(t *testing.T) {
	t.Parallel()
	rp := returns.NewReturnProcessor(nil, time.Second)
	r := makeReturn("R7", "O7")
	r.CreatedAt = time.Now().Add(-2 * time.Second)
	if err := rp.CreateReturn(r); err == nil {
		t.Fatal("expected error for expired window")
	}
}
