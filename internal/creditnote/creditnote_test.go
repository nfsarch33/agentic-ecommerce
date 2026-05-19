package creditnote_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/creditnote"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newNote(id, customerID, noteType string, amount float64, expiresIn time.Duration) creditnote.CreditNote {
	now := time.Now()
	return creditnote.CreditNote{
		ID:         id,
		OrderID:    "ORD-001",
		CustomerID: customerID,
		Amount:     amount,
		Type:       noteType,
		Status:     creditnote.StatusActive,
		IssuedAt:   now,
		ExpiresAt:  now.Add(expiresIn),
	}
}

// ---------------------------------------------------------------------------
// Store tests
// ---------------------------------------------------------------------------

func TestStoreIssueAndGet(t *testing.T) {
	t.Parallel()
	store := creditnote.NewStore()

	note := newNote("CN-1", "CUST-A", creditnote.TypePartialRefund, 50.00, 24*time.Hour)
	if err := store.Issue(note); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := store.Get("CN-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Amount != 50.00 {
		t.Fatalf("want amount 50.00, got %.2f", got.Amount)
	}
}

func TestStoreIssueNegativeAmountFails(t *testing.T) {
	t.Parallel()
	store := creditnote.NewStore()
	note := newNote("CN-NEG", "CUST-B", creditnote.TypeStoreCredit, -10.00, time.Hour)
	if err := store.Issue(note); err == nil {
		t.Fatal("expected error for negative amount, got nil")
	}
}

func TestStoreGetNotFound(t *testing.T) {
	t.Parallel()
	store := creditnote.NewStore()
	_, err := store.Get("GHOST")
	if err == nil {
		t.Fatal("expected error for missing note, got nil")
	}
}

func TestStoreByCustomer(t *testing.T) {
	t.Parallel()
	store := creditnote.NewStore()

	for i, id := range []string{"N1", "N2", "N3"} {
		_ = i
		n := newNote(id, "CUST-C", creditnote.TypeStoreCredit, float64(i+1)*10, time.Hour)
		if err := store.Issue(n); err != nil {
			t.Fatalf("unexpected issue error: %v", err)
		}
	}
	// Different customer
	other := newNote("OTHER", "CUST-D", creditnote.TypeStoreCredit, 5.00, time.Hour)
	_ = store.Issue(other)

	notes := store.ByCustomer("CUST-C")
	if len(notes) != 3 {
		t.Fatalf("want 3 notes for CUST-C, got %d", len(notes))
	}
}

// ---------------------------------------------------------------------------
// Redeemer tests
// ---------------------------------------------------------------------------

func TestRedeemerSuccess(t *testing.T) {
	t.Parallel()
	store := creditnote.NewStore()
	redeemer := &creditnote.Redeemer{}

	note := newNote("CN-R1", "CUST-E", creditnote.TypePartialRefund, 75.00, time.Hour)
	_ = store.Issue(note)

	now := time.Now()
	amount, err := redeemer.Redeem(store, "CN-R1", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if amount != 75.00 {
		t.Fatalf("want amount 75.00, got %.2f", amount)
	}

	// Verify status updated.
	got, _ := store.Get("CN-R1")
	if got.Status != creditnote.StatusUsed {
		t.Fatalf("want status USED, got %q", got.Status)
	}
	if got.UsedAt == nil {
		t.Fatal("want UsedAt set, got nil")
	}
}

func TestRedeemerExpiredFails(t *testing.T) {
	t.Parallel()
	store := creditnote.NewStore()
	redeemer := &creditnote.Redeemer{}

	// Issue a note that expired an hour ago.
	n := newNote("CN-EXP", "CUST-F", creditnote.TypeStoreCredit, 20.00, -time.Hour)
	_ = store.Issue(n)

	_, err := redeemer.Redeem(store, "CN-EXP", time.Now())
	if err == nil {
		t.Fatal("expected error for expired note, got nil")
	}
}

func TestRedeemerAlreadyUsedFails(t *testing.T) {
	t.Parallel()
	store := creditnote.NewStore()
	redeemer := &creditnote.Redeemer{}

	note := newNote("CN-USED", "CUST-G", creditnote.TypePartialRefund, 30.00, time.Hour)
	_ = store.Issue(note)

	now := time.Now()
	if _, err := redeemer.Redeem(store, "CN-USED", now); err != nil {
		t.Fatalf("first redeem should succeed: %v", err)
	}
	if _, err := redeemer.Redeem(store, "CN-USED", now.Add(time.Second)); err == nil {
		t.Fatal("expected error for already-used note, got nil")
	}
}

// ---------------------------------------------------------------------------
// ExpiryChecker tests
// ---------------------------------------------------------------------------

func TestExpiryCheckerExpireStaleCount(t *testing.T) {
	t.Parallel()
	store := creditnote.NewStore()
	checker := &creditnote.ExpiryChecker{}

	now := time.Now()

	// Two notes that are already expired.
	for _, id := range []string{"EXP-1", "EXP-2"} {
		n := creditnote.CreditNote{
			ID:         id,
			CustomerID: "CUST-H",
			Amount:     10.00,
			Type:       creditnote.TypeStoreCredit,
			Status:     creditnote.StatusActive,
			IssuedAt:   now.Add(-2 * time.Hour),
			ExpiresAt:  now.Add(-time.Hour), // expired
		}
		_ = store.Issue(n)
	}
	// One note still active.
	active := newNote("ACT-1", "CUST-H", creditnote.TypeStoreCredit, 10.00, time.Hour)
	_ = store.Issue(active)

	count := checker.ExpireStale(store, now)
	if count != 2 {
		t.Fatalf("want 2 expired notes, got %d", count)
	}

	// Confirm active note is unchanged.
	got, _ := store.Get("ACT-1")
	if got.Status != creditnote.StatusActive {
		t.Fatalf("active note should still be active, got %q", got.Status)
	}

	// Running again should not expire anything further.
	count2 := checker.ExpireStale(store, now)
	if count2 != 0 {
		t.Fatalf("second run should expire 0, got %d", count2)
	}
}
