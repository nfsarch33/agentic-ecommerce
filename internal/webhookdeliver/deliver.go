// Package webhookdeliver provides reliable webhook delivery with retry, DLQ, and HMAC-SHA256 signing.
package webhookdeliver

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ErrDLQ is returned when a delivery exhausts all retries and is moved to the DLQ.
var ErrDLQ = errors.New("webhookdeliver: moved to dead-letter queue")

// ErrSignatureInvalid is returned when HMAC verification fails.
var ErrSignatureInvalid = errors.New("webhookdeliver: invalid signature")

// Status values for delivery records.
const (
	StatusPending   = "pending"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
	StatusDLQ       = "dlq"
)

// Delivery holds the state of one outbound webhook call.
type Delivery struct {
	ID          string
	URL         string
	EventType   string
	Payload     json.RawMessage
	Status      string
	Attempts    int
	MaxAttempts int
	NextRetryAt time.Time
	CreatedAt   time.Time
}

// DLQEntry represents a delivery that has exhausted all retries.
type DLQEntry struct {
	Delivery  Delivery
	Reason    string
	ArchivedAt time.Time
}

// Config controls retry and signing behaviour.
type Config struct {
	Secret      []byte
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	HTTPClient  HTTPClient
	Now         func() time.Time // replaceable for tests
}

// SetClock replaces the clock on the dispatcher (useful in tests).
func (d *Dispatcher) SetClock(fn func() time.Time) {
	d.mu.Lock()
	d.cfg.Now = fn
	d.mu.Unlock()
}

// HTTPClient abstracts *http.Client for testing.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Dispatcher manages in-memory webhook deliveries, retries, and DLQ.
type Dispatcher struct {
	cfg Config
	mu  sync.Mutex
	q   []*Delivery
	dlq []*DLQEntry
}

// NewDispatcher returns a Dispatcher with the given config.
func NewDispatcher(cfg Config) *Dispatcher {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 1 * time.Second
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 5 * time.Minute
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Dispatcher{cfg: cfg}
}

// Enqueue adds a delivery to the pending queue.
func (d *Dispatcher) Enqueue(deliveryURL, eventType string, payload interface{}) (*Delivery, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("webhookdeliver: marshal payload: %w", err)
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	del := &Delivery{
		ID:          id,
		URL:         deliveryURL,
		EventType:   eventType,
		Payload:     raw,
		Status:      StatusPending,
		MaxAttempts: d.cfg.MaxAttempts,
		CreatedAt:   d.cfg.Now(),
	}
	d.mu.Lock()
	d.q = append(d.q, del)
	d.mu.Unlock()
	return del, nil
}

// Process attempts all pending deliveries that are ready.
func (d *Dispatcher) Process(ctx context.Context) error {
	d.mu.Lock()
	pending := make([]*Delivery, len(d.q))
	copy(pending, d.q)
	d.mu.Unlock()

	now := d.cfg.Now()
	for _, del := range pending {
		if del.Status != StatusPending {
			continue
		}
		if del.Attempts > 0 && del.NextRetryAt.After(now) {
			continue
		}
		d.attempt(ctx, del)
	}
	return nil
}

func (d *Dispatcher) attempt(ctx context.Context, del *Delivery) {
	del.Attempts++
	err := d.send(ctx, del)
	if err == nil {
		d.mu.Lock()
		del.Status = StatusDelivered
		d.mu.Unlock()
		return
	}

	if del.Attempts >= del.MaxAttempts {
		d.mu.Lock()
		del.Status = StatusDLQ
		d.dlq = append(d.dlq, &DLQEntry{
			Delivery:   *del,
			Reason:     err.Error(),
			ArchivedAt: d.cfg.Now(),
		})
		d.mu.Unlock()
		return
	}

	delay := d.cfg.BaseDelay * (1 << uint(del.Attempts-1))
	if delay > d.cfg.MaxDelay {
		delay = d.cfg.MaxDelay
	}
	d.mu.Lock()
	del.NextRetryAt = d.cfg.Now().Add(delay)
	d.mu.Unlock()
}

func (d *Dispatcher) send(ctx context.Context, del *Delivery) error {
	sig := Sign(d.cfg.Secret, del.Payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, del.URL, bytes.NewReader(del.Payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", del.EventType)
	req.Header.Set("X-Webhook-Signature", "sha256="+sig)
	req.Header.Set("X-Webhook-ID", del.ID)

	resp, err := d.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhookdeliver: HTTP %d", resp.StatusCode)
	}
	return nil
}

// DLQEntries returns a snapshot of the dead-letter queue.
func (d *Dispatcher) DLQEntries() []DLQEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]DLQEntry, len(d.dlq))
	for i, e := range d.dlq {
		out[i] = *e
	}
	return out
}

// PendingCount returns the number of deliveries in StatusPending.
func (d *Dispatcher) PendingCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, del := range d.q {
		if del.Status == StatusPending {
			n++
		}
	}
	return n
}

// Sign returns the hex-encoded HMAC-SHA256 of payload using secret.
func Sign(secret, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify checks an "sha256=<hex>" header against payload.
func Verify(secret []byte, header string, payload []byte) error {
	const prefix = "sha256="
	if len(header) <= len(prefix) {
		return ErrSignatureInvalid
	}
	got := header[len(prefix):]
	want := Sign(secret, payload)
	if !hmac.Equal([]byte(got), []byte(want)) {
		return ErrSignatureInvalid
	}
	return nil
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("webhookdeliver: generate ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}
