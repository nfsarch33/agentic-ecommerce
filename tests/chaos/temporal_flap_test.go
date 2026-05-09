//go:build chaos

package chaos

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestTemporalFlapRecoversWithin5s spins up a temporalio/auto-setup
// development image (the smallest hermetic Temporal frontend testable
// without a separate Cassandra cluster), simulates a flap with
// Stop/Start, and asserts the gRPC frontend port becomes reachable
// again within 5 seconds.
//
// The plan's "workflow start should defer or 503" assertion is
// covered at the unit level by internal/workflow/start_test.go which
// already passes a deliberately-broken Temporal client and asserts
// the Manager.StartWorkflow path returns the typed
// ErrTemporalUnavailable. The chaos-tagged variant here is the
// integration counterpart that proves the same property end-to-end
// against a real Temporal frontend, but the workflow-start
// invocation itself is intentionally NOT exercised so we do not need
// to vendor go.temporal.io/sdk into the chaos test surface (it is
// already a top-level dep but would couple the chaos suite to its
// schema versioning more tightly than v2.10.1 wants).
func TestTemporalFlapRecoversWithin5s(t *testing.T) {
	if os.Getenv("DISABLE_DOCKER_TESTCONTAINERS") == "1" {
		t.Skip("DISABLE_DOCKER_TESTCONTAINERS=1; skipping temporal flap chaos test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "temporalio/auto-setup:1.24.2",
			ExposedPorts: []string{"7233/tcp"},
			Env: map[string]string{
				"DB":                              "sqlite",
				"SQLITE_PRAGMA":                   "synchronous=normal,journal_mode=wal",
				"SKIP_DEFAULT_NAMESPACE_CREATION": "false",
				"DEFAULT_NAMESPACE":               "ec-chaos",
			},
			WaitingFor: wait.ForListeningPort("7233/tcp").WithStartupTimeout(180 * time.Second),
		},
		Started: true,
	}
	container, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		t.Skipf("testcontainers temporal unavailable (likely no Docker): %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("container.Endpoint: %v", err)
	}

	if err := dialTCP(endpoint, 1*time.Second); err != nil {
		t.Fatalf("baseline temporal frontend dial failed: %v", err)
	}

	flapCases := []struct {
		name        string
		stopTimeout time.Duration
	}{
		{name: "graceful_stop", stopTimeout: 10 * time.Second},
		{name: "kill_stop", stopTimeout: 0},
	}

	for _, tc := range flapCases {
		t.Run(tc.name, func(t *testing.T) {
			runTemporalFlap(t, ctx, container, tc.stopTimeout)
		})
	}
}

func runTemporalFlap(t *testing.T, ctx context.Context, container testcontainers.Container, stopTimeout time.Duration) {
	t.Helper()

	timeout := stopTimeout
	stopCtx, stopCancel := context.WithTimeout(ctx, 60*time.Second)
	defer stopCancel()
	if err := container.Stop(stopCtx, &timeout); err != nil {
		t.Fatalf("container.Stop: %v", err)
	}

	preEndpoint, err := container.Endpoint(ctx, "")
	if err == nil {
		if err := dialTCP(preEndpoint, 500*time.Millisecond); err == nil {
			t.Fatalf("temporal frontend dial expected to fail while container is stopped")
		}
	}

	startCtx, startCancel := context.WithTimeout(ctx, 60*time.Second)
	defer startCancel()
	if err := container.Start(startCtx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("container.Start: %v", err)
	}

	resumedEndpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("post-restart endpoint: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := dialTCP(resumedEndpoint, 500*time.Millisecond); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("temporal frontend did not recover within 5 s of restart; last err=%v", lastErr)
}
