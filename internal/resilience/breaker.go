package resilience

import (
	"errors"
	"sync"
	"time"
)

var ErrBreakerOpen = errors.New("breaker: circuit is open")

type breakerState int

const (
	bClosed   breakerState = iota
	bOpen     breakerState = iota
	bHalfOpen breakerState = iota
)

// BreakerConfig controls thresholds and recovery.
type BreakerConfig struct {
	FailureThreshold int
	RecoveryTimeout  time.Duration
}

// Breaker is a simple three-state circuit breaker.
type Breaker struct {
	mu         sync.Mutex
	cfg        BreakerConfig
	state      breakerState
	failures   int
	lastOpen   time.Time
}

func NewBreaker(cfg BreakerConfig) *Breaker {
	return &Breaker{cfg: cfg, state: bClosed}
}

func (b *Breaker) Execute(fn func() error) error {
	b.mu.Lock()
	switch b.state {
	case bOpen:
		if time.Since(b.lastOpen) >= b.cfg.RecoveryTimeout {
			b.state = bHalfOpen
		} else {
			b.mu.Unlock()
			return ErrBreakerOpen
		}
	}
	state := b.state
	b.mu.Unlock()

	err := fn()

	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		b.failures++
		if state == bHalfOpen || b.failures >= b.cfg.FailureThreshold {
			b.state = bOpen
			b.lastOpen = time.Now()
		}
	} else {
		if state == bHalfOpen {
			b.state = bClosed
			b.failures = 0
		} else {
			b.failures = 0
		}
	}
	return err
}

func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case bOpen:
		return StateOpen
	case bHalfOpen:
		return StateHalfOpen
	default:
		return StateClosed
	}
}
