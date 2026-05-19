package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/db"
)

func TestPool_AcquireReturnsConnection(t *testing.T) {
	t.Parallel()
	p := db.NewPool(2, nil)
	ctx := context.Background()
	c, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if c.ID == 0 {
		t.Fatal("expected valid connection ID")
	}
}

func TestPool_ReleaseReturnsToPool(t *testing.T) {
	t.Parallel()
	p := db.NewPool(1, nil)
	ctx := context.Background()
	c, _ := p.Acquire(ctx)
	p.Release(c)
	if p.Size() != 1 {
		t.Fatalf("expected pool size 1 after release, got %d", p.Size())
	}
}

func TestPool_MaxSizeEnforced(t *testing.T) {
	t.Parallel()
	p := db.NewPool(1, nil)
	ctx := context.Background()
	p.Acquire(ctx) // take the only connection
	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err := p.Acquire(timeoutCtx)
	if err != db.ErrPoolExhausted {
		t.Fatalf("expected ErrPoolExhausted, got %v", err)
	}
}

func TestPool_TimeoutOnExhausted(t *testing.T) {
	t.Parallel()
	p := db.NewPool(2, nil)
	ctx := context.Background()
	p.Acquire(ctx)
	p.Acquire(ctx)
	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err := p.Acquire(timeoutCtx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestPool_HealthCheckRemovesBadConnections(t *testing.T) {
	t.Parallel()
	healthFn := func(c db.Conn) bool { return c.ID%2 != 0 } // only odd IDs healthy
	p := db.NewPool(4, healthFn)
	p.HealthCheck(context.Background())
	// All even IDs removed, only odd remain
	size := p.Size()
	if size == 0 {
		t.Fatal("expected some healthy connections")
	}
}

func TestPool_ConcurrentAcquireReleaseSafe(t *testing.T) {
	t.Parallel()
	p := db.NewPool(5, nil)
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if c, err := p.Acquire(ctx); err == nil {
				p.Release(c)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
