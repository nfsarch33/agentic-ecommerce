// Package pushnotif provides FCM/APNS-style push notification dispatch with topic subscriptions and badge counts.
package pushnotif

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrNoProvider is returned when no provider is configured.
var ErrNoProvider = errors.New("pushnotif: no provider configured")

// Platform constants.
const (
	PlatformFCM  = "fcm"
	PlatformAPNS = "apns"
)

// Notification holds the payload to deliver to a device.
type Notification struct {
	Token    string            // device push token
	Platform string            // "fcm" or "apns"
	Title    string
	Body     string
	Badge    int               // APNS badge count; ignored for FCM
	Data     map[string]string // custom key-value pairs
	Topic    string            // optional topic / channel
}

// Result holds the outcome of one push delivery.
type Result struct {
	Token   string
	Success bool
	Error   string
}

// Provider dispatches push notifications for one platform.
type Provider interface {
	Platform() string
	Send(ctx context.Context, n Notification) Result
}

// TopicStore manages per-topic device token subscriptions.
type TopicStore struct {
	mu     sync.RWMutex
	topics map[string]map[string]bool // topic -> set of tokens
}

// NewTopicStore returns an empty TopicStore.
func NewTopicStore() *TopicStore {
	return &TopicStore{topics: make(map[string]map[string]bool)}
}

// Subscribe adds a token to a topic.
func (s *TopicStore) Subscribe(topic, token string) {
	s.mu.Lock()
	if s.topics[topic] == nil {
		s.topics[topic] = make(map[string]bool)
	}
	s.topics[topic][token] = true
	s.mu.Unlock()
}

// Unsubscribe removes a token from a topic.
func (s *TopicStore) Unsubscribe(topic, token string) {
	s.mu.Lock()
	if s.topics[topic] != nil {
		delete(s.topics[topic], token)
	}
	s.mu.Unlock()
}

// Subscribers returns all tokens subscribed to a topic.
func (s *TopicStore) Subscribers(topic string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tokens := s.topics[topic]
	out := make([]string, 0, len(tokens))
	for t := range tokens {
		out = append(out, t)
	}
	return out
}

// Dispatcher routes notifications to the appropriate provider.
type Dispatcher struct {
	mu        sync.RWMutex
	providers map[string]Provider
	topics    *TopicStore
}

// NewDispatcher returns a Dispatcher with the given topic store.
func NewDispatcher(topics *TopicStore) *Dispatcher {
	return &Dispatcher{
		providers: make(map[string]Provider),
		topics:    topics,
	}
}

// Register adds a provider for its declared platform.
func (d *Dispatcher) Register(p Provider) {
	d.mu.Lock()
	d.providers[p.Platform()] = p
	d.mu.Unlock()
}

// Send dispatches a single notification to its platform provider.
func (d *Dispatcher) Send(ctx context.Context, n Notification) (Result, error) {
	d.mu.RLock()
	p, ok := d.providers[n.Platform]
	d.mu.RUnlock()
	if !ok {
		return Result{Token: n.Token, Success: false}, fmt.Errorf("pushnotif: no provider for platform %q", n.Platform)
	}
	result := p.Send(ctx, n)
	return result, nil
}

// Broadcast dispatches a notification to all tokens subscribed to the given topic.
func (d *Dispatcher) Broadcast(ctx context.Context, topic string, n Notification) []Result {
	tokens := d.topics.Subscribers(topic)
	results := make([]Result, 0, len(tokens))
	for _, token := range tokens {
		n.Token = token
		r, _ := d.Send(ctx, n)
		results = append(results, r)
	}
	return results
}

// BadgeManager tracks per-device badge counts.
type BadgeManager struct {
	mu     sync.RWMutex
	counts map[string]int
}

// NewBadgeManager returns an empty BadgeManager.
func NewBadgeManager() *BadgeManager {
	return &BadgeManager{counts: make(map[string]int)}
}

// Increment adds delta to the badge count for a device token and returns the new count.
func (m *BadgeManager) Increment(token string, delta int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[token] += delta
	return m.counts[token]
}

// Reset sets the badge count to zero for a token.
func (m *BadgeManager) Reset(token string) {
	m.mu.Lock()
	m.counts[token] = 0
	m.mu.Unlock()
}

// Count returns the current badge count for a token.
func (m *BadgeManager) Count(token string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.counts[token]
}

// StubProvider is a test double for a push platform.
type StubProvider struct {
	platform string
	Err      string
	Results  []Result
	mu       sync.Mutex
}

// NewStubProvider returns a named stub provider.
func NewStubProvider(platform string) *StubProvider { return &StubProvider{platform: platform} }

func (p *StubProvider) Platform() string { return p.platform }

func (p *StubProvider) Send(_ context.Context, n Notification) Result {
	r := Result{Token: n.Token, Success: p.Err == "", Error: p.Err}
	p.mu.Lock()
	p.Results = append(p.Results, r)
	p.mu.Unlock()
	return r
}

// Delivered returns count of successful sends.
func (p *StubProvider) Delivered() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, r := range p.Results {
		if r.Success {
			n++
		}
	}
	return n
}
