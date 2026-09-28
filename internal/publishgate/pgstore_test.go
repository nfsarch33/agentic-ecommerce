package publishgate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pgDSN returns the loopback test database, or "" to skip (skip with a
// message, never silently pass, per the design).
func pgDSN() string {
	if d := os.Getenv("ECOMMERCE_TEST_PG_DSN"); d != "" {
		return d
	}
	return "postgres://postgres:postgres@127.0.0.1:5432/ecommerce?sslmode=disable"
}

func pgStore(t *testing.T) *PGStore {
	t.Helper()
	dsn := pgDSN()
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("pg pool (%s): %v", "test dsn", err)
	}
	t.Cleanup(pool.Close)
	// Probe: a refused connection skips, never fails silently.
	var one int
	if err := pool.QueryRow(context.Background(), "SELECT 1").Scan(&one); err != nil {
		t.Skipf("pg not reachable: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "SELECT 1 FROM publish_approvals LIMIT 1"); err != nil {
		t.Skipf("publish gate tables missing (run migrations first): %v", err)
	}
	s := NewPGStore(pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM publish_ledger; DELETE FROM publish_approvals;")
	})
	return s
}

func TestPGRecordApprovalKeepsFirstDecision(t *testing.T) {
	s := pgStore(t)
	ctx := context.Background()
	_, inserted, err := s.RecordApproval(ctx, Decision{WorkflowID: "wf-t1", TenantID: "t1", ProductID: "00000000-0000-0000-0000-000000000001", Approved: true, Actor: "a", UpdateID: "k1"})
	if err != nil || !inserted {
		t.Fatalf("first: inserted=%v err=%v", inserted, err)
	}
	stored, inserted, err := s.RecordApproval(ctx, Decision{WorkflowID: "wf-t1", TenantID: "t1", ProductID: "00000000-0000-0000-0000-000000000001", Approved: false, Actor: "b", UpdateID: "k2"})
	if err != nil || inserted {
		t.Fatalf("second: inserted=%v err=%v", inserted, err)
	}
	if !stored.Approved {
		t.Fatal("first decision must win")
	}
}

func TestPGApprovalForMissing(t *testing.T) {
	s := pgStore(t)
	if _, err := s.ApprovalFor(context.Background(), "wf-none"); !errors.Is(err, ErrNoApproval) {
		t.Fatalf("err = %v, want ErrNoApproval", err)
	}
}

func TestPGClaimLeaseExpiryAndCompletion(t *testing.T) {
	s := pgStore(t)
	ctx := context.Background()
	if _, _, err := s.RecordApproval(ctx, Decision{WorkflowID: "wf-c", TenantID: "t1", ProductID: "00000000-0000-0000-0000-000000000002", Approved: true, UpdateID: "k"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim(ctx, "k1", "t1", "wf-c", "00000000-0000-0000-0000-000000000002", "S", "fp", "w-other", -time.Second); err != nil {
		t.Fatalf("claim expired-lease row: %v", err)
	}
	// Live foreign lease: refused (re-claim with a positive TTL first).
	if err := s.Claim(ctx, "k1", "t1", "wf-c", "00000000-0000-0000-0000-000000000002", "S", "fp", "w-other", time.Minute); err != nil {
		t.Fatalf("re-claim after expiry: %v", err)
	}
	if err := s.Claim(ctx, "k1", "t1", "wf-c", "00000000-0000-0000-0000-000000000002", "S", "fp", "w-me", time.Minute); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("live lease err = %v, want ErrLeaseHeld", err)
	}
	// Completed: short-circuit.
	if err := s.Complete(ctx, "k1", "completed", "create", "wp-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim(ctx, "k1", "t1", "wf-c", "00000000-0000-0000-0000-000000000002", "S", "fp", "w-me", time.Minute); !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("completed err = %v, want ErrAlreadyCompleted", err)
	}
	// Failed rows are reclaimable.
	if err := s.Claim(ctx, "k2", "t1", "wf-c", "00000000-0000-0000-0000-000000000002", "S2", "fp", "w-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.Fail(ctx, "k2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim(ctx, "k2", "t1", "wf-c", "00000000-0000-0000-0000-000000000002", "S2", "fp", "w-b", time.Minute); err != nil {
		t.Fatalf("failed row must be reclaimable: %v", err)
	}
}

// Design integration list: 20 goroutines claim one key; exactly 1 acquires.
func TestPGClaimRaceExactlyOneAcquires(t *testing.T) {
	s := pgStore(t)
	if _, _, err := s.RecordApproval(context.Background(), Decision{WorkflowID: "wf-r", TenantID: "t1", ProductID: "00000000-0000-0000-0000-000000000003", Approved: true, UpdateID: "k"}); err != nil {
		t.Fatal(err)
	}
	const n = 20
	var acquired int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			err := s.Claim(context.Background(), "race-key", "t1", "wf-r", "00000000-0000-0000-0000-000000000003", "S", "fp", fmt.Sprintf("w-%d", i), time.Minute)
			if err == nil {
				atomic.AddInt32(&acquired, 1)
			} else if !errors.Is(err, ErrLeaseHeld) {
				t.Errorf("unexpected claim error: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if got := atomic.LoadInt32(&acquired); got != 1 {
		t.Fatalf("acquired = %d, want exactly 1", got)
	}
}
