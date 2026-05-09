//go:build chaos

package chaos

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestPostgresFlapRecoversWithin5s spins up a real Postgres
// container, hard-stops the container to simulate a flap, then
// re-starts it and asserts that a fresh pgxpool acquire succeeds
// within the 5-second post-recovery budget called out in the v2.10.1
// chaos plan.
//
// The plan asks for a -STOP / -CONT signal pattern. testcontainers-go
// 0.34's stable Container API exposes Stop+Start (full container
// lifecycle) but does not expose Pause/Resume on the public
// interface. Stop/Start delivers the same correctness property under
// test ("the pool returns ErrAcquire while the DB is gone, then
// recovers cleanly when the DB returns") with a slightly heavier
// failure mode -- the container loses its in-memory state, but our
// migrations are idempotent so the resumed instance is functionally
// identical from the pool's perspective.
//
// All Docker-dependent assertions self-skip when Docker is missing or
// DISABLE_DOCKER_TESTCONTAINERS=1 is set. The full test takes 30-60s
// when Docker is present.
func TestPostgresFlapRecoversWithin5s(t *testing.T) {
	if os.Getenv("DISABLE_DOCKER_TESTCONTAINERS") == "1" {
		t.Skip("DISABLE_DOCKER_TESTCONTAINERS=1; skipping postgres flap chaos test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(
		ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("ec_chaos"),
		tcpostgres.WithUsername("ec"),
		tcpostgres.WithPassword("ec"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("testcontainers postgres unavailable (likely no Docker): %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("baseline ping failed: %v", err)
	}

	flapCases := []struct {
		name        string
		stopTimeout time.Duration
	}{
		{name: "graceful_stop", stopTimeout: 5 * time.Second},
		{name: "kill_stop", stopTimeout: 0},
	}

	for _, tc := range flapCases {
		t.Run(tc.name, func(t *testing.T) {
			runPostgresFlap(t, ctx, container, pool, tc.stopTimeout)
		})
	}
}

func runPostgresFlap(t *testing.T, ctx context.Context, container testcontainers.Container, pool *pgxpool.Pool, stopTimeout time.Duration) {
	t.Helper()

	timeout := stopTimeout
	stopCtx, stopCancel := context.WithTimeout(ctx, 30*time.Second)
	defer stopCancel()
	if err := container.Stop(stopCtx, &timeout); err != nil {
		t.Fatalf("container.Stop: %v", err)
	}

	probeCtx, probeCancel := context.WithTimeout(ctx, 3*time.Second)
	defer probeCancel()
	if err := pool.Ping(probeCtx); err == nil {
		t.Fatalf("pool ping expected to fail while postgres is stopped")
	}

	startCtx, startCancel := context.WithTimeout(ctx, 30*time.Second)
	defer startCancel()
	if err := container.Start(startCtx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("container.Start: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		recoverCtx, c := context.WithTimeout(ctx, 500*time.Millisecond)
		err := pool.Ping(recoverCtx)
		c()
		if err == nil {
			return
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("pool did not recover within 5 s of postgres restart; last err=%v", lastErr)
}
