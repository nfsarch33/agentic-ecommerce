package db

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrNoReplicas      = errors.New("replica: no replicas available")
	ErrNoPrimary       = errors.New("replica: no primary available")
	ErrReplicaNotFound = errors.New("replica: not found")
)

type ReplicaConn struct {
	Name    string
	Primary bool
	Healthy bool
	Lag     time.Duration
}

// ReplicaRouter manages read/write routing with round-robin replica selection.
type ReplicaRouter struct {
	mu       sync.RWMutex
	conns    map[string]ReplicaConn
	replicas []string
	counter  atomic.Uint64
}

func NewReplicaRouter() *ReplicaRouter {
	return &ReplicaRouter{
		conns: make(map[string]ReplicaConn),
	}
}

func (r *ReplicaRouter) AddConn(c ReplicaConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conns[c.Name] = c
	if !c.Primary {
		r.replicas = append(r.replicas, c.Name)
	}
}

func (r *ReplicaRouter) RouteRead(_ context.Context) (ReplicaConn, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var healthy []string
	for _, name := range r.replicas {
		if c, ok := r.conns[name]; ok && c.Healthy {
			healthy = append(healthy, name)
		}
	}
	if len(healthy) == 0 {
		// Fallback to primary
		for _, c := range r.conns {
			if c.Primary && c.Healthy {
				return c, nil
			}
		}
		return ReplicaConn{}, ErrNoReplicas
	}
	idx := r.counter.Add(1) % uint64(len(healthy))
	return r.conns[healthy[idx]], nil
}

func (r *ReplicaRouter) RouteWrite(_ context.Context) (ReplicaConn, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.conns {
		if c.Primary && c.Healthy {
			return c, nil
		}
	}
	return ReplicaConn{}, ErrNoPrimary
}

func (r *ReplicaRouter) LagCheck(_ context.Context, replica string) (time.Duration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.conns[replica]
	if !ok {
		return 0, ErrReplicaNotFound
	}
	return c.Lag, nil
}

func (r *ReplicaRouter) Failover(_ context.Context, from, to string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.conns[from]
	if !ok {
		return ErrReplicaNotFound
	}
	newPrimary, ok := r.conns[to]
	if !ok {
		return ErrReplicaNotFound
	}
	old.Primary = false
	newPrimary.Primary = true
	r.conns[from] = old
	r.conns[to] = newPrimary
	return nil
}
