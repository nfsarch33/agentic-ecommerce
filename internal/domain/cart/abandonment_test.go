package cart_test

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/domain/cart"
)

func TestAbandonmentDetector_NoAbandoned(t *testing.T) {
	t.Parallel()
	store := cart.NewMemoryCartStore()
	active := cart.Cart{ID: "c1", CustomerID: "u1", UpdatedAt: time.Now()}
	store.Save(active)

	detector := cart.NewAbandonmentDetector(store)
	abandoned := detector.DetectAbandoned(context.Background(), 30*time.Minute)
	if len(abandoned) != 0 {
		t.Fatalf("expected 0 abandoned, got %d", len(abandoned))
	}
}

func TestAbandonmentDetector_DetectsAbandoned(t *testing.T) {
	t.Parallel()
	store := cart.NewMemoryCartStore()
	old := cart.Cart{ID: "c1", CustomerID: "u1", UpdatedAt: time.Now().Add(-2 * time.Hour)}
	recent := cart.Cart{ID: "c2", CustomerID: "u2", UpdatedAt: time.Now()}
	store.Save(old)
	store.Save(recent)

	detector := cart.NewAbandonmentDetector(store)
	abandoned := detector.DetectAbandoned(context.Background(), 30*time.Minute)
	if len(abandoned) != 1 {
		t.Fatalf("expected 1 abandoned, got %d", len(abandoned))
	}
	if abandoned[0].ID != "c1" {
		t.Fatalf("wrong cart abandoned: %s", abandoned[0].ID)
	}
}

func TestRecoveryWorkflow_TriggersRecovery(t *testing.T) {
	t.Parallel()
	var notified []string
	notifier := func(c cart.Cart) error {
		notified = append(notified, c.ID)
		return nil
	}

	store := cart.NewMemoryCartStore()
	old := cart.Cart{ID: "c1", CustomerID: "u1", UpdatedAt: time.Now().Add(-2 * time.Hour)}
	store.Save(old)

	wf := cart.NewRecoveryWorkflow(store, notifier, 30*time.Minute)
	count, err := wf.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 recovery, got %d", count)
	}
	if len(notified) != 1 || notified[0] != "c1" {
		t.Fatalf("wrong notifications: %v", notified)
	}
}

func TestRecoveryWorkflow_IdempotentRecover(t *testing.T) {
	t.Parallel()
	callCount := 0
	notifier := func(c cart.Cart) error {
		callCount++
		return nil
	}

	store := cart.NewMemoryCartStore()
	old := cart.Cart{ID: "c1", CustomerID: "u1", UpdatedAt: time.Now().Add(-2 * time.Hour)}
	store.Save(old)

	wf := cart.NewRecoveryWorkflow(store, notifier, 30*time.Minute)
	wf.RunOnce(context.Background())
	wf.RunOnce(context.Background())

	if callCount != 1 {
		t.Fatalf("expected idempotent: notifier called %d times, want 1", callCount)
	}
}

func TestAbandonmentDetector_EmptyStore(t *testing.T) {
	t.Parallel()
	store := cart.NewMemoryCartStore()
	detector := cart.NewAbandonmentDetector(store)
	abandoned := detector.DetectAbandoned(context.Background(), 30*time.Minute)
	if abandoned == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(abandoned) != 0 {
		t.Fatalf("expected 0, got %d", len(abandoned))
	}
}
