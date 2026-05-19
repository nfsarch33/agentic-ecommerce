package db

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrPoolExhausted = errors.New("pool: no connections available")
	ErrConnBad       = errors.New("pool: connection is bad")
)

// Conn represents a database connection.
type Conn struct {
	ID      int
	Healthy bool
}

// Pool manages a fixed-size connection pool.
type Pool struct {
	mu      sync.Mutex
	conns   chan Conn
	all     []Conn
	maxSize int
	seq     int
	healthy func(Conn) bool
}

func NewPool(maxSize int, healthFn func(Conn) bool) *Pool {
	if healthFn == nil {
		healthFn = func(c Conn) bool { return c.Healthy }
	}
	p := &Pool{
		conns:   make(chan Conn, maxSize),
		maxSize: maxSize,
		healthy: healthFn,
	}
	for i := 0; i < maxSize; i++ {
		p.seq++
		c := Conn{ID: p.seq, Healthy: true}
		p.all = append(p.all, c)
		p.conns <- c
	}
	return p
}

func (p *Pool) Acquire(ctx context.Context) (Conn, error) {
	select {
	case c := <-p.conns:
		return c, nil
	case <-ctx.Done():
		return Conn{}, ErrPoolExhausted
	}
}

func (p *Pool) Release(c Conn) error {
	select {
	case p.conns <- c:
		return nil
	default:
		return ErrPoolExhausted
	}
}

func (p *Pool) HealthCheck(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Drain and refill, removing unhealthy connections
	var healthy []Conn
	drain:
	for {
		select {
		case c := <-p.conns:
			if p.healthy(c) {
				healthy = append(healthy, c)
			}
		default:
			break drain
		}
	}
	for _, c := range healthy {
		p.conns <- c
	}
	return nil
}

func (p *Pool) Size() int {
	return len(p.conns)
}
