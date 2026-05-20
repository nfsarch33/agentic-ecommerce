package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/db"
)

func TestReplica_ReadRoutesToReplica(t *testing.T) {
	t.Parallel()
	r := db.NewReplicaRouter()
	r.AddConn(db.ReplicaConn{Name: "primary", Primary: true, Healthy: true})
	r.AddConn(db.ReplicaConn{Name: "replica1", Primary: false, Healthy: true})
	c, err := r.RouteRead(context.Background())
	if err != nil {
		t.Fatalf("route read failed: %v", err)
	}
	if c.Primary {
		t.Fatal("expected read to route to replica, not primary")
	}
}

func TestReplica_WriteRoutesToPrimary(t *testing.T) {
	t.Parallel()
	r := db.NewReplicaRouter()
	r.AddConn(db.ReplicaConn{Name: "primary", Primary: true, Healthy: true})
	r.AddConn(db.ReplicaConn{Name: "replica1", Primary: false, Healthy: true})
	c, err := r.RouteWrite(context.Background())
	if err != nil {
		t.Fatalf("route write failed: %v", err)
	}
	if !c.Primary {
		t.Fatal("expected write to route to primary")
	}
}

func TestReplica_RoundRobinDistributes(t *testing.T) {
	t.Parallel()
	r := db.NewReplicaRouter()
	r.AddConn(db.ReplicaConn{Name: "primary", Primary: true, Healthy: true})
	r.AddConn(db.ReplicaConn{Name: "replica1", Primary: false, Healthy: true})
	r.AddConn(db.ReplicaConn{Name: "replica2", Primary: false, Healthy: true})
	names := make(map[string]int)
	for i := 0; i < 10; i++ {
		c, _ := r.RouteRead(context.Background())
		names[c.Name]++
	}
	if len(names) < 2 {
		t.Fatalf("expected round-robin across replicas, got %v", names)
	}
}

func TestReplica_LagCheckReturnsDuration(t *testing.T) {
	t.Parallel()
	r := db.NewReplicaRouter()
	r.AddConn(db.ReplicaConn{Name: "replica1", Primary: false, Healthy: true, Lag: 5 * time.Millisecond})
	lag, err := r.LagCheck(context.Background(), "replica1")
	if err != nil {
		t.Fatalf("lag check failed: %v", err)
	}
	if lag != 5*time.Millisecond {
		t.Fatalf("expected 5ms lag, got %v", lag)
	}
}

func TestReplica_FailoverPromotes(t *testing.T) {
	t.Parallel()
	r := db.NewReplicaRouter()
	r.AddConn(db.ReplicaConn{Name: "primary", Primary: true, Healthy: true})
	r.AddConn(db.ReplicaConn{Name: "replica1", Primary: false, Healthy: true})
	if err := r.Failover(context.Background(), "primary", "replica1"); err != nil {
		t.Fatalf("failover failed: %v", err)
	}
	c, err := r.RouteWrite(context.Background())
	if err != nil {
		t.Fatalf("route write after failover failed: %v", err)
	}
	if c.Name != "replica1" {
		t.Fatalf("expected replica1 as new primary, got %s", c.Name)
	}
}

func TestReplica_FallbackToPrimaryWhenNoReplicas(t *testing.T) {
	t.Parallel()
	r := db.NewReplicaRouter()
	r.AddConn(db.ReplicaConn{Name: "primary", Primary: true, Healthy: true})
	// No replicas added
	c, err := r.RouteRead(context.Background())
	if err != nil {
		t.Fatalf("expected fallback to primary, got %v", err)
	}
	if !c.Primary {
		t.Fatal("expected primary as fallback")
	}
}
