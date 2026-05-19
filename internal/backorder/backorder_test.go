package backorder_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/backorder"
)

var baseTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func makeBO(id, productID string, priority int, placedAt time.Time) backorder.BackOrder {
	return backorder.BackOrder{
		ID:        id,
		ProductID: productID,
		CustomerID: "cust-1",
		Quantity:  1,
		Priority:  priority,
		PlacedAt:  placedAt,
		Status:    backorder.StatusWaiting,
	}
}

func TestStore_AddGet(t *testing.T) {
	t.Parallel()
	s := backorder.NewStore()
	bo := makeBO("bo-1", "prod-1", 5, baseTime)
	s.Add(bo)

	got, err := s.Get("bo-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "bo-1" {
		t.Errorf("want ID bo-1, got %s", got.ID)
	}
}

func TestStore_GetNotFound(t *testing.T) {
	t.Parallel()
	s := backorder.NewStore()
	_, err := s.Get("missing")
	if err != backorder.ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestStore_UpdateETA(t *testing.T) {
	t.Parallel()
	s := backorder.NewStore()
	s.Add(makeBO("bo-2", "prod-1", 1, baseTime))

	eta := baseTime.AddDate(0, 0, 7)
	if err := s.UpdateETA("bo-2", eta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := s.Get("bo-2")
	if got.EstimatedAt == nil || !got.EstimatedAt.Equal(eta) {
		t.Errorf("want ETA %v, got %v", eta, got.EstimatedAt)
	}
}

func TestStore_UpdateETA_NotFound(t *testing.T) {
	t.Parallel()
	s := backorder.NewStore()
	if err := s.UpdateETA("missing", baseTime); err != backorder.ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestStore_UpdateStatus(t *testing.T) {
	t.Parallel()
	s := backorder.NewStore()
	s.Add(makeBO("bo-3", "prod-1", 1, baseTime))

	if err := s.UpdateStatus("bo-3", backorder.StatusAllocated); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := s.Get("bo-3")
	if got.Status != backorder.StatusAllocated {
		t.Errorf("want %s, got %s", backorder.StatusAllocated, got.Status)
	}
}

func TestStore_UpdateStatus_Invalid(t *testing.T) {
	t.Parallel()
	s := backorder.NewStore()
	s.Add(makeBO("bo-4", "prod-1", 1, baseTime))
	if err := s.UpdateStatus("bo-4", "bogus"); err != backorder.ErrInvalidStatus {
		t.Errorf("want ErrInvalidStatus, got %v", err)
	}
}

func TestStore_UpdateStatus_NotFound(t *testing.T) {
	t.Parallel()
	s := backorder.NewStore()
	if err := s.UpdateStatus("missing", backorder.StatusFulfilled); err != backorder.ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestStore_SetNotified(t *testing.T) {
	t.Parallel()
	s := backorder.NewStore()
	s.Add(makeBO("bo-5", "prod-1", 1, baseTime))
	if err := s.SetNotified("bo-5", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := s.Get("bo-5")
	if !got.Notified {
		t.Error("want Notified=true")
	}
}

// PriorityQueue tests

func TestPriorityQueue_DequeueHighestFirst(t *testing.T) {
	t.Parallel()
	pq := backorder.NewPriorityQueue()

	pq.Enqueue(makeBO("low", "prod-x", 1, baseTime))
	pq.Enqueue(makeBO("high", "prod-x", 10, baseTime.Add(time.Hour)))
	pq.Enqueue(makeBO("mid", "prod-x", 5, baseTime.Add(2*time.Hour)))

	first, ok := pq.Dequeue("prod-x")
	if !ok || first.ID != "high" {
		t.Errorf("want high, got %v (ok=%v)", first, ok)
	}
	second, _ := pq.Dequeue("prod-x")
	if second.ID != "mid" {
		t.Errorf("want mid, got %s", second.ID)
	}
}

func TestPriorityQueue_TieBreakByPlacedAt(t *testing.T) {
	t.Parallel()
	pq := backorder.NewPriorityQueue()

	earlier := makeBO("earlier", "prod-y", 5, baseTime)
	later := makeBO("later", "prod-y", 5, baseTime.Add(time.Hour))
	pq.Enqueue(later)
	pq.Enqueue(earlier)

	first, _ := pq.Dequeue("prod-y")
	if first.ID != "earlier" {
		t.Errorf("want earlier (placed first), got %s", first.ID)
	}
}

func TestPriorityQueue_DequeueEmpty(t *testing.T) {
	t.Parallel()
	pq := backorder.NewPriorityQueue()
	_, ok := pq.Dequeue("no-product")
	if ok {
		t.Error("want ok=false for empty queue")
	}
}

func TestPriorityQueue_Peek(t *testing.T) {
	t.Parallel()
	pq := backorder.NewPriorityQueue()
	pq.Enqueue(makeBO("p1", "prod-z", 3, baseTime))

	got, ok := pq.Peek("prod-z")
	if !ok || got.ID != "p1" {
		t.Errorf("want p1, got %v (ok=%v)", got, ok)
	}
	// Peek does not remove
	if pq.Depth("prod-z") != 1 {
		t.Errorf("want depth 1 after Peek, got %d", pq.Depth("prod-z"))
	}
}

func TestPriorityQueue_Depth(t *testing.T) {
	t.Parallel()
	pq := backorder.NewPriorityQueue()
	if pq.Depth("prod-a") != 0 {
		t.Error("want 0 depth for unknown product")
	}
	pq.Enqueue(makeBO("d1", "prod-a", 1, baseTime))
	pq.Enqueue(makeBO("d2", "prod-a", 2, baseTime))
	if pq.Depth("prod-a") != 2 {
		t.Errorf("want depth 2, got %d", pq.Depth("prod-a"))
	}
}

// ETACalculator tests

func TestETACalculator_Basic(t *testing.T) {
	t.Parallel()
	calc := backorder.ETACalculator{}
	base := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	got := calc.EstimateETA("prod-1", base, 3, 2) // 3 * 2 = 6 days
	want := base.AddDate(0, 0, 6)
	if !got.Equal(want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestETACalculator_ZeroDepth(t *testing.T) {
	t.Parallel()
	calc := backorder.ETACalculator{}
	base := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	got := calc.EstimateETA("prod-1", base, 0, 5)
	if !got.Equal(base) {
		t.Errorf("want baseDate unchanged, got %v", got)
	}
}

func TestETACalculator_LargeQueue(t *testing.T) {
	t.Parallel()
	calc := backorder.ETACalculator{}
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	got := calc.EstimateETA("prod-2", base, 10, 3) // 30 days
	want := base.AddDate(0, 0, 30)
	if !got.Equal(want) {
		t.Errorf("want %v, got %v", want, got)
	}
}
