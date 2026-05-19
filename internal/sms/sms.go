// Package sms provides SMS dispatch with rate limiting, delivery status, and provider fallback.
package sms

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrRateLimited is returned when the per-user rate limit is exceeded.
var ErrRateLimited = errors.New("sms: rate limited")

// ErrNoProvider is returned when all providers fail.
var ErrNoProvider = errors.New("sms: no provider available")

// Status values.
const (
	StatusQueued    = "queued"
	StatusSent      = "sent"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
	StatusOptedOut  = "opted_out"
)

// Message holds an outbound SMS.
type Message struct {
	To      string
	From    string
	Body    string
	Ref     string // correlation reference
}

// DeliveryStatus represents the status of a sent SMS from the carrier.
type DeliveryStatus struct {
	MessageID string
	Status    string
	Timestamp time.Time
	Raw       map[string]string
}

// Provider is the interface a carrier adapter must implement.
type Provider interface {
	Name() string
	Send(ctx context.Context, msg Message) (DeliveryStatus, error)
}

// RateLimiter enforces per-user per-window message limits.
type RateLimiter struct {
	mu       sync.Mutex
	windows  map[string][]time.Time
	limit    int
	window   time.Duration
	now      func() time.Time
}

// NewRateLimiter returns a RateLimiter with the given limit and sliding window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		windows: make(map[string][]time.Time),
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

// WithClock replaces the internal clock (for testing).
func (r *RateLimiter) WithClock(fn func() time.Time) { r.now = fn }

// Allow returns nil if the user is within limits, or ErrRateLimited.
func (r *RateLimiter) Allow(userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := r.now().Add(-r.window)
	ts := r.windows[userID]
	// Evict expired entries.
	var kept []time.Time
	for _, t := range ts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= r.limit {
		r.windows[userID] = kept
		return ErrRateLimited
	}
	r.windows[userID] = append(kept, r.now())
	return nil
}

// OptOutStore tracks opted-out phone numbers.
type OptOutStore struct {
	mu      sync.RWMutex
	numbers map[string]bool
}

// NewOptOutStore returns an empty store.
func NewOptOutStore() *OptOutStore {
	return &OptOutStore{numbers: make(map[string]bool)}
}

// OptOut records a number as opted out.
func (s *OptOutStore) OptOut(number string) {
	s.mu.Lock()
	s.numbers[number] = true
	s.mu.Unlock()
}

// IsOptedOut returns true if the number has opted out.
func (s *OptOutStore) IsOptedOut(number string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.numbers[number]
}

// Service coordinates SMS sending across providers with rate limiting and opt-out enforcement.
type Service struct {
	providers []Provider
	limiter   *RateLimiter
	optOuts   *OptOutStore
}

// NewService returns a Service with ordered provider fallback.
func NewService(providers []Provider, limiter *RateLimiter, optOuts *OptOutStore) *Service {
	return &Service{providers: providers, limiter: limiter, optOuts: optOuts}
}

// Send dispatches the SMS through the first available provider.
func (s *Service) Send(ctx context.Context, msg Message) (DeliveryStatus, error) {
	if s.optOuts != nil && s.optOuts.IsOptedOut(msg.To) {
		return DeliveryStatus{Status: StatusOptedOut}, nil
	}
	if s.limiter != nil {
		if err := s.limiter.Allow(msg.To); err != nil {
			return DeliveryStatus{}, err
		}
	}
	var lastErr error
	for _, p := range s.providers {
		status, err := p.Send(ctx, msg)
		if err == nil {
			return status, nil
		}
		lastErr = fmt.Errorf("sms: provider %s: %w", p.Name(), err)
	}
	if lastErr != nil {
		return DeliveryStatus{}, lastErr
	}
	return DeliveryStatus{}, ErrNoProvider
}

// StubProvider is a test-only provider.
type StubProvider struct {
	name string
	Err  error
	Sent []Message
	mu   sync.Mutex
}

// NewStubProvider returns a named stub provider.
func NewStubProvider(name string) *StubProvider { return &StubProvider{name: name} }

func (p *StubProvider) Name() string { return p.name }

func (p *StubProvider) Send(_ context.Context, msg Message) (DeliveryStatus, error) {
	p.mu.Lock()
	p.Sent = append(p.Sent, msg)
	p.mu.Unlock()
	if p.Err != nil {
		return DeliveryStatus{}, p.Err
	}
	return DeliveryStatus{
		MessageID: "stub-" + p.name,
		Status:    StatusSent,
		Timestamp: time.Now(),
	}, nil
}

// SentMessages returns a snapshot of messages sent through this provider.
func (p *StubProvider) SentMessages() []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Message, len(p.Sent))
	copy(out, p.Sent)
	return out
}
