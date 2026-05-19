package testing_test

import (
	"context"
	"sync"
	"testing"
	"time"

	lt "github.com/nfsarch33/helixon-ec/internal/testing"
)

func TestChaos_InjectCreatesExperiment(t *testing.T) {
	t.Parallel()
	engine := lt.NewChaosEngine()
	fault := lt.NetworkPartition(context.Background(), "svc-a", time.Second)
	id, err := engine.Inject(context.Background(), fault)
	if err != nil {
		t.Fatalf("inject failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty experiment ID")
	}
}

func TestChaos_NetworkPartitionIsolatesTarget(t *testing.T) {
	t.Parallel()
	fault := lt.NetworkPartition(context.Background(), "payments", 5*time.Second)
	if fault.Type != "network_partition" {
		t.Fatalf("expected network_partition type, got %s", fault.Type)
	}
	if fault.Target != "payments" {
		t.Fatalf("expected target payments, got %s", fault.Target)
	}
}

func TestChaos_LatencySpikeAddsDelay(t *testing.T) {
	t.Parallel()
	fault := lt.LatencySpike(context.Background(), "inventory", 200*time.Millisecond)
	if fault.Type != "latency_spike" {
		t.Fatalf("expected latency_spike, got %s", fault.Type)
	}
	if fault.Delay != 200*time.Millisecond {
		t.Fatalf("expected 200ms delay, got %v", fault.Delay)
	}
}

func TestChaos_RecoveryCleansUp(t *testing.T) {
	t.Parallel()
	engine := lt.NewChaosEngine()
	fault := lt.NetworkPartition(context.Background(), "svc", time.Second)
	id, _ := engine.Inject(context.Background(), fault)
	if err := engine.Recovery(context.Background(), id); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if engine.IsActive(id) {
		t.Fatal("expected experiment to be inactive after recovery")
	}
}

func TestChaos_InjectValidatesTarget(t *testing.T) {
	t.Parallel()
	engine := lt.NewChaosEngine()
	_, err := engine.Inject(context.Background(), lt.Fault{Type: "latency", Target: ""})
	if err != lt.ErrInvalidTarget {
		t.Fatalf("expected ErrInvalidTarget, got %v", err)
	}
}

func TestChaos_ConcurrentExperimentsIsolated(t *testing.T) {
	t.Parallel()
	engine := lt.NewChaosEngine()
	var wg sync.WaitGroup
	ids := make([]lt.ExperimentID, 10)
	mu := sync.Mutex{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fault := lt.Fault{Type: "latency", Target: "svc"}
			id, _ := engine.Inject(context.Background(), fault)
			mu.Lock()
			ids[i] = id
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	seen := make(map[lt.ExperimentID]bool)
	for _, id := range ids {
		if seen[id] {
			t.Fatal("duplicate experiment IDs in concurrent inject")
		}
		seen[id] = true
	}
}
