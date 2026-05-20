package mesh_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/mesh"
)

func TestMesh_RegisterAndDiscover(t *testing.T) {
	t.Parallel()
	r := mesh.NewRegistry()
	svc := mesh.ServiceInfo{Name: "cart", Version: "v1"}
	r.Register(svc, mesh.Endpoint{Address: "cart:8080", Healthy: true})
	endpoints, err := r.Discover("cart")
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
}

func TestMesh_LoadBalanceRoundRobin(t *testing.T) {
	t.Parallel()
	r := mesh.NewRegistry()
	eps := []mesh.Endpoint{
		{Address: "svc:8081", Healthy: true},
		{Address: "svc:8082", Healthy: true},
		{Address: "svc:8083", Healthy: true},
	}
	seen := make(map[string]int)
	for i := 0; i < 9; i++ {
		e, err := r.LoadBalance(eps, "round-robin")
		if err != nil {
			t.Fatalf("load balance failed: %v", err)
		}
		seen[e.Address]++
	}
	if len(seen) < 2 {
		t.Fatalf("expected distribution across endpoints, got %v", seen)
	}
}

func TestMesh_LoadBalanceLeastConn(t *testing.T) {
	t.Parallel()
	r := mesh.NewRegistry()
	eps := []mesh.Endpoint{
		{Address: "svc:8081", ActiveConns: 10},
		{Address: "svc:8082", ActiveConns: 2},
		{Address: "svc:8083", ActiveConns: 5},
	}
	e, err := r.LoadBalance(eps, "least-conn")
	if err != nil {
		t.Fatalf("load balance failed: %v", err)
	}
	if e.Address != "svc:8082" {
		t.Fatalf("expected least-conn endpoint svc:8082, got %s", e.Address)
	}
}

func TestMesh_RetrySucceedsOnSecondAttempt(t *testing.T) {
	t.Parallel()
	attempts := 0
	err := mesh.Retry(func() error {
		attempts++
		if attempts < 2 {
			return errors.New("temporary error")
		}
		return nil
	}, 3, time.Millisecond)
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestMesh_RetryExhaustedReturnsError(t *testing.T) {
	t.Parallel()
	err := mesh.Retry(func() error {
		return errors.New("always fails")
	}, 2, time.Millisecond)
	if err != mesh.ErrRetryExhausted {
		t.Fatalf("expected ErrRetryExhausted, got %v", err)
	}
}

func TestMesh_TracePropagatesContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	newCtx, span := mesh.Trace(ctx, "test-span")
	if span.Name != "test-span" {
		t.Fatalf("expected span name test-span, got %s", span.Name)
	}
	if newCtx == nil {
		t.Fatal("expected non-nil context")
	}
	span.End()
}
