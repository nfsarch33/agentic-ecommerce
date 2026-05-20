package giftcard_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/giftcard"
)

func TestGiftCard_CreateGeneratesUniqueCode(t *testing.T) {
	t.Parallel()
	m := giftcard.NewManager()
	gc1, _ := m.Create(5000, "AUD")
	gc2, _ := m.Create(5000, "AUD")
	if gc1.Code == gc2.Code {
		t.Fatal("expected unique codes")
	}
}

func TestGiftCard_ActivateInactiveCard(t *testing.T) {
	t.Parallel()
	m := giftcard.NewManager()
	gc, _ := m.Create(5000, "AUD")
	if err := m.Activate(gc.Code); err != nil {
		t.Fatalf("Activate: %v", err)
	}
}

func TestGiftCard_RedeemPartial(t *testing.T) {
	t.Parallel()
	m := giftcard.NewManager()
	gc, _ := m.Create(5000, "AUD")
	m.Activate(gc.Code)
	remaining, err := m.Redeem(gc.Code, 1000)
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if remaining != 4000 {
		t.Fatalf("expected 4000 remaining, got %d", remaining)
	}
}

func TestGiftCard_RedeemFull(t *testing.T) {
	t.Parallel()
	m := giftcard.NewManager()
	gc, _ := m.Create(1000, "AUD")
	m.Activate(gc.Code)
	remaining, err := m.Redeem(gc.Code, 1000)
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected 0 remaining, got %d", remaining)
	}
}

func TestGiftCard_RedeemOverBalance(t *testing.T) {
	t.Parallel()
	m := giftcard.NewManager()
	gc, _ := m.Create(500, "AUD")
	m.Activate(gc.Code)
	if _, err := m.Redeem(gc.Code, 1000); err == nil {
		t.Fatal("expected insufficient funds error")
	}
}

func TestGiftCard_CheckBalance(t *testing.T) {
	t.Parallel()
	m := giftcard.NewManager()
	gc, _ := m.Create(3000, "AUD")
	bal, err := m.CheckBalance(gc.Code)
	if err != nil {
		t.Fatalf("CheckBalance: %v", err)
	}
	if bal != 3000 {
		t.Fatalf("expected 3000, got %d", bal)
	}
}

func TestGiftCard_Transfer(t *testing.T) {
	t.Parallel()
	m := giftcard.NewManager()
	gc, _ := m.Create(2000, "AUD")
	if err := m.Transfer(gc.Code, "USER-99"); err != nil {
		t.Fatalf("Transfer: %v", err)
	}
}
