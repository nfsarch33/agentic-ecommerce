package mesh

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrServiceNotFound   = errors.New("mesh: service not found")
	ErrNoEndpoints       = errors.New("mesh: no healthy endpoints")
	ErrRetryExhausted    = errors.New("mesh: retry exhausted")
	ErrUnknownStrategy   = errors.New("mesh: unknown load balance strategy")
)

type ServiceInfo struct {
	Name    string
	Version string
}

type Endpoint struct {
	Address     string
	Healthy     bool
	ActiveConns int32
}

type SpanHandle struct {
	Name string
	End  func()
}

type Registry struct {
	mu        sync.RWMutex
	services  map[string]ServiceInfo
	endpoints map[string][]Endpoint
	counters  map[string]*atomic.Uint64
}

func NewRegistry() *Registry {
	return &Registry{
		services:  make(map[string]ServiceInfo),
		endpoints: make(map[string][]Endpoint),
		counters:  make(map[string]*atomic.Uint64),
	}
}

func (r *Registry) Register(svc ServiceInfo, endpoints ...Endpoint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[svc.Name] = svc
	r.endpoints[svc.Name] = append(r.endpoints[svc.Name], endpoints...)
	if r.counters[svc.Name] == nil {
		r.counters[svc.Name] = new(atomic.Uint64)
	}
	return nil
}

func (r *Registry) Discover(name string) ([]Endpoint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	eps, ok := r.endpoints[name]
	if !ok {
		return nil, ErrServiceNotFound
	}
	var healthy []Endpoint
	for _, e := range eps {
		if e.Healthy {
			healthy = append(healthy, e)
		}
	}
	if len(healthy) == 0 {
		return nil, ErrNoEndpoints
	}
	return healthy, nil
}

func (r *Registry) LoadBalance(endpoints []Endpoint, strategy string) (Endpoint, error) {
	if len(endpoints) == 0 {
		return Endpoint{}, ErrNoEndpoints
	}
	switch strategy {
	case "round-robin":
		// Use a package-level counter for simplicity in tests
		return endpoints[int(globalRR.Add(1)-1)%len(endpoints)], nil
	case "least-conn":
		best := endpoints[0]
		for _, e := range endpoints[1:] {
			if e.ActiveConns < best.ActiveConns {
				best = e
			}
		}
		return best, nil
	default:
		return Endpoint{}, ErrUnknownStrategy
	}
}

var globalRR atomic.Uint64

func Retry(fn func() error, maxRetries int, backoff time.Duration) error {
	var err error
	for i := 0; i <= maxRetries; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		if i < maxRetries {
			time.Sleep(backoff)
		}
	}
	return ErrRetryExhausted
}

func Trace(ctx context.Context, spanName string) (context.Context, SpanHandle) {
	type spanKey struct{}
	ctx = context.WithValue(ctx, spanKey{}, spanName)
	return ctx, SpanHandle{
		Name: spanName,
		End:  func() {},
	}
}
