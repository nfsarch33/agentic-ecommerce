package preorder_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/preorder"
)

var testTime = time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)

func basePreOrder(id, productID string) preorder.PreOrder {
	return preorder.PreOrder{
		ID:          id,
		ProductID:   productID,
		CustomerID:  "cust-1",
		Deposit:     20.00,
		FullPrice:   100.00,
		Status:      preorder.StatusOpen,
		PlacedAt:    testTime,
		EstimatedAt: testTime.Add(30 * 24 * time.Hour),
	}
}

func TestStore_PlaceAndGet(t *testing.T) {
	t.Parallel()
	s := preorder.NewStore()
	po := basePreOrder("po1", "prod-A")
	if err := s.Place(po); err != nil {
		t.Fatalf("Place: %v", err)
	}
	got, err := s.Get("po1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want %q", got.CustomerID, "cust-1")
	}
}

func TestStore_GetNotFound(t *testing.T) {
	t.Parallel()
	s := preorder.NewStore()
	_, err := s.Get("missing")
	if err != preorder.ErrPreOrderNotFound {
		t.Errorf("err = %v, want ErrPreOrderNotFound", err)
	}
}

func TestStore_UpdateStatus(t *testing.T) {
	t.Parallel()
	s := preorder.NewStore()
	_ = s.Place(basePreOrder("po2", "prod-B"))
	if err := s.UpdateStatus("po2", preorder.StatusConfirmed); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := s.Get("po2")
	if got.Status != preorder.StatusConfirmed {
		t.Errorf("Status = %q, want %q", got.Status, preorder.StatusConfirmed)
	}
}

func TestStore_StatusTransitions(t *testing.T) {
	t.Parallel()
	s := preorder.NewStore()
	_ = s.Place(basePreOrder("po3", "prod-C"))

	for _, st := range []string{
		preorder.StatusConfirmed,
		preorder.StatusFulfilled,
	} {
		if err := s.UpdateStatus("po3", st); err != nil {
			t.Fatalf("UpdateStatus(%q): %v", st, err)
		}
	}
	got, _ := s.Get("po3")
	if got.Status != preorder.StatusFulfilled {
		t.Errorf("Status = %q, want %q", got.Status, preorder.StatusFulfilled)
	}
}

func TestStore_UpdateStatusNotFound(t *testing.T) {
	t.Parallel()
	s := preorder.NewStore()
	err := s.UpdateStatus("nope", preorder.StatusCancelled)
	if err != preorder.ErrPreOrderNotFound {
		t.Errorf("err = %v, want ErrPreOrderNotFound", err)
	}
}

func TestStore_List(t *testing.T) {
	t.Parallel()
	s := preorder.NewStore()
	_ = s.Place(basePreOrder("po-a1", "prod-X"))
	_ = s.Place(basePreOrder("po-a2", "prod-X"))
	_ = s.Place(basePreOrder("po-b1", "prod-Y"))

	got := s.List("prod-X")
	if len(got) != 2 {
		t.Errorf("List(prod-X) len = %d, want 2", len(got))
	}
}

func TestFulfillmentQueue_FIFO(t *testing.T) {
	t.Parallel()
	fq := preorder.NewFulfillmentQueue()
	fq.Enqueue("po1", "prod-A")
	fq.Enqueue("po2", "prod-A")
	fq.Enqueue("po3", "prod-A")

	for _, want := range []string{"po1", "po2", "po3"} {
		got, ok := fq.Dequeue("prod-A")
		if !ok || got != want {
			t.Errorf("Dequeue = (%q,%v), want (%q,true)", got, ok, want)
		}
	}

	_, ok := fq.Dequeue("prod-A")
	if ok {
		t.Error("expected empty queue after draining")
	}
}

func TestFulfillmentQueue_Depth(t *testing.T) {
	t.Parallel()
	fq := preorder.NewFulfillmentQueue()
	if fq.Depth("prod-Z") != 0 {
		t.Error("Depth of unknown product should be 0")
	}
	fq.Enqueue("po1", "prod-Z")
	fq.Enqueue("po2", "prod-Z")
	if fq.Depth("prod-Z") != 2 {
		t.Errorf("Depth = %d, want 2", fq.Depth("prod-Z"))
	}
}

func TestFulfillmentQueue_MultiProduct(t *testing.T) {
	t.Parallel()
	fq := preorder.NewFulfillmentQueue()
	fq.Enqueue("po-a", "prod-A")
	fq.Enqueue("po-b", "prod-B")

	got, _ := fq.Dequeue("prod-A")
	if got != "po-a" {
		t.Errorf("Dequeue(prod-A) = %q, want %q", got, "po-a")
	}
	got, _ = fq.Dequeue("prod-B")
	if got != "po-b" {
		t.Errorf("Dequeue(prod-B) = %q, want %q", got, "po-b")
	}
}

func TestNotificationDispatcher_Notify(t *testing.T) {
	t.Parallel()
	nd := preorder.NewNotificationDispatcher()
	po := basePreOrder("po1", "prod-A")
	now := testTime.Add(5 * time.Minute)
	nd.Notify(&po, now)

	if po.NotifiedAt == nil {
		t.Fatal("NotifiedAt should be set after Notify")
	}
	if !po.NotifiedAt.Equal(now) {
		t.Errorf("NotifiedAt = %v, want %v", *po.NotifiedAt, now)
	}
}

func TestNotificationDispatcher_Idempotent(t *testing.T) {
	t.Parallel()
	nd := preorder.NewNotificationDispatcher()
	po := basePreOrder("po2", "prod-A")
	first := testTime.Add(1 * time.Minute)
	nd.Notify(&po, first)
	second := testTime.Add(2 * time.Minute)
	nd.Notify(&po, second)

	if !po.NotifiedAt.Equal(first) {
		t.Errorf("NotifiedAt = %v, want first time %v", *po.NotifiedAt, first)
	}
}

func TestNotificationDispatcher_HasBeenNotified(t *testing.T) {
	t.Parallel()
	nd := preorder.NewNotificationDispatcher()
	po := basePreOrder("po3", "prod-A")

	if nd.HasBeenNotified(&po) {
		t.Error("HasBeenNotified should be false before Notify")
	}
	nd.Notify(&po, testTime)
	if !nd.HasBeenNotified(&po) {
		t.Error("HasBeenNotified should be true after Notify")
	}
}
