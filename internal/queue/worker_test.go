package queue_test

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/queue"
)

func TestQueue_EnqueueAndDequeue(t *testing.T) {
	t.Parallel()
	m := queue.NewManager()
	ctx := context.Background()
	id, err := m.Enqueue(ctx, "orders", []byte("payload"))
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	job, err := m.Dequeue(ctx, "orders")
	if err != nil {
		t.Fatalf("dequeue failed: %v", err)
	}
	if job.ID != id {
		t.Fatalf("expected job id %s, got %s", id, job.ID)
	}
}

func TestQueue_AckRemovesFromProcessing(t *testing.T) {
	t.Parallel()
	m := queue.NewManager()
	ctx := context.Background()
	id, _ := m.Enqueue(ctx, "q", []byte("data"))
	m.Dequeue(ctx, "q")
	if err := m.Ack(ctx, id); err != nil {
		t.Fatalf("ack failed: %v", err)
	}
	if err := m.Ack(ctx, id); err != queue.ErrJobNotFound {
		t.Fatal("expected ErrJobNotFound on second ack")
	}
}

func TestQueue_RetryIncrementsAttempt(t *testing.T) {
	t.Parallel()
	m := queue.NewManager()
	ctx := context.Background()
	id, _ := m.Enqueue(ctx, "q", []byte("data"))
	m.Dequeue(ctx, "q") // take it out of queue but leave in jobs map
	m.Retry(ctx, id)
	// dequeue again to get the retried job
	job, err := m.Dequeue(ctx, "q")
	if err != nil {
		t.Fatalf("dequeue after retry failed: %v", err)
	}
	if job.Attempts != 1 {
		t.Fatalf("expected attempts=1, got %d", job.Attempts)
	}
}

func TestQueue_DeadLetterMovesJob(t *testing.T) {
	t.Parallel()
	m := queue.NewManager()
	ctx := context.Background()
	id, _ := m.Enqueue(ctx, "q", []byte("data"))
	m.Dequeue(ctx, "q")
	m.DeadLetter(ctx, id, "too many retries")
	if !m.IsDeadLettered(id) {
		t.Fatal("expected job to be in dead letter queue")
	}
}

func TestQueue_DequeueBlocksOnEmpty(t *testing.T) {
	t.Parallel()
	m := queue.NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := m.Dequeue(ctx, "empty")
	if err == nil {
		t.Fatal("expected error when context cancelled on empty queue")
	}
}

func TestQueue_CancelUnblocksDequeue(t *testing.T) {
	t.Parallel()
	m := queue.NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := m.Dequeue(ctx, "q")
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error after cancel")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("dequeue did not unblock after cancel")
	}
}

func TestQueue_ConcurrentEnqueueOrdering(t *testing.T) {
	t.Parallel()
	m := queue.NewManager()
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		m.Enqueue(ctx, "q", []byte("p"))
	}
	count := 0
	for i := 0; i < 10; i++ {
		dCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		_, err := m.Dequeue(dCtx, "q")
		cancel()
		if err == nil {
			count++
		}
	}
	if count != 10 {
		t.Fatalf("expected 10 dequeued, got %d", count)
	}
}
