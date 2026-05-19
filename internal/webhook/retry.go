package webhook

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

var (
	ErrMaxRetriesExhausted = errors.New("max retries exhausted: moved to dead-letter queue")
	ErrAlreadyDelivered    = errors.New("delivery already succeeded")
)

// WebhookDelivery describes a pending webhook delivery.
type WebhookDelivery struct {
	ID             string
	URL            string
	AttemptCount   int
	LastStatusCode int
	LastError      string
}

// DeliveryAttempt is the result of scheduling a retry.
type DeliveryAttempt struct {
	AttemptNumber int
	NextRetryAt   time.Time
	LastError     string
	StatusCode    int
}

// RetryConfig controls the backoff behaviour.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
}

// DeadLetterQueue holds deliveries that exceeded max retries.
type DeadLetterQueue struct {
	mu    sync.Mutex
	items []WebhookDelivery
}

func (q *DeadLetterQueue) add(d WebhookDelivery) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, d)
}

func (q *DeadLetterQueue) Items() []WebhookDelivery {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]WebhookDelivery(nil), q.items...)
}

// RetryScheduler computes the next retry time for a webhook delivery.
type RetryScheduler struct {
	cfg RetryConfig
	dlq *DeadLetterQueue
}

func NewRetryScheduler(cfg RetryConfig) *RetryScheduler {
	return &RetryScheduler{cfg: cfg, dlq: &DeadLetterQueue{}}
}

// DeadLetterQueue returns the dead-letter queue associated with this scheduler.
func (s *RetryScheduler) DeadLetterQueue() *DeadLetterQueue { return s.dlq }

// Schedule computes the next DeliveryAttempt or returns an error when exhausted.
func (s *RetryScheduler) Schedule(d WebhookDelivery) (DeliveryAttempt, error) {
	if d.LastStatusCode >= 200 && d.LastStatusCode < 300 {
		return DeliveryAttempt{}, ErrAlreadyDelivered
	}
	if d.AttemptCount >= s.cfg.MaxRetries {
		s.dlq.add(d)
		return DeliveryAttempt{}, ErrMaxRetriesExhausted
	}
	delay := s.backoffDelay(d.AttemptCount)
	return DeliveryAttempt{
		AttemptNumber: d.AttemptCount + 1,
		NextRetryAt:   time.Now().Add(delay),
		LastError:     d.LastError,
		StatusCode:    d.LastStatusCode,
	}, nil
}

// backoffDelay computes base * 2^attempt + jitter.
func (s *RetryScheduler) backoffDelay(attempt int) time.Duration {
	base := s.cfg.BaseDelay * (1 << uint(attempt))
	// add up to 25% jitter using crypto/rand
	jitter := cryptoJitter(base / 4)
	return base + jitter
}

// cryptoJitter returns a random duration in [0, max).
func cryptoJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	var b [8]byte
	rand.Read(b[:])
	n := binary.BigEndian.Uint64(b[:])
	return time.Duration(n % uint64(max))
}
