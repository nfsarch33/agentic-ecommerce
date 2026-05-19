package outbound

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

type Store interface {
	CreateRegistration(ctx context.Context, input CreateRegistrationInput) (Registration, error)
	ListRegistrations(ctx context.Context) ([]Registration, error)
	GetRegistration(ctx context.Context, id string) (Registration, error)
	DeleteRegistration(ctx context.Context, id string) error
	EnabledRegistrationsForEvent(ctx context.Context, eventType eventbus.EventType) ([]Registration, error)
	SecretForRegistration(ctx context.Context, id string) (string, error)
	RecordDelivery(ctx context.Context, result DeliveryResult) (DeliveryResult, error)
	ListDeliveries(ctx context.Context, webhookID string) ([]DeliveryResult, error)
}

type InMemoryStore struct {
	mu            sync.RWMutex
	registrations map[string]registrationRecord
	deliveries    map[string][]DeliveryResult
	now           func() time.Time
}

type registrationRecord struct {
	registration Registration
	secret       string
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		registrations: make(map[string]registrationRecord),
		deliveries:    make(map[string][]DeliveryResult),
		now:           time.Now,
	}
}

func (s *InMemoryStore) CreateRegistration(_ context.Context, input CreateRegistrationInput) (Registration, error) {
	if err := validateRegistrationInput(input); err != nil {
		return Registration{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	reg := Registration{
		ID:         uuid.NewString(),
		TenantID:   strings.TrimSpace(input.TenantID),
		URL:        strings.TrimSpace(input.URL),
		EventTypes: append([]eventbus.EventType(nil), input.EventTypes...),
		SecretRef:  strings.TrimSpace(input.SecretRef),
		SecretHash: hashSecret(input.Secret),
		Enabled:    enabled,
		CreatedAt:  s.now().UTC(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrations[reg.ID] = registrationRecord{registration: reg, secret: input.Secret}
	return cloneRegistration(reg), nil
}

func (s *InMemoryStore) ListRegistrations(_ context.Context) ([]Registration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Registration, 0, len(s.registrations))
	for _, record := range s.registrations {
		out = append(out, cloneRegistration(record.registration))
	}
	return out, nil
}

func (s *InMemoryStore) GetRegistration(_ context.Context, id string) (Registration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.registrations[id]
	if !ok {
		return Registration{}, ErrNotFound
	}
	return cloneRegistration(record.registration), nil
}

func (s *InMemoryStore) DeleteRegistration(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.registrations[id]; !ok {
		return ErrNotFound
	}
	delete(s.registrations, id)
	delete(s.deliveries, id)
	return nil
}

func (s *InMemoryStore) EnabledRegistrationsForEvent(_ context.Context, eventType eventbus.EventType) ([]Registration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := []Registration{}
	for _, record := range s.registrations {
		reg := record.registration
		if !reg.Enabled {
			continue
		}
		for _, subscribed := range reg.EventTypes {
			if subscribed == eventType {
				out = append(out, cloneRegistration(reg))
				break
			}
		}
	}
	return out, nil
}

func (s *InMemoryStore) SecretForRegistration(_ context.Context, id string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.registrations[id]
	if !ok {
		return "", ErrNotFound
	}
	return record.secret, nil
}

func (s *InMemoryStore) RecordDelivery(_ context.Context, result DeliveryResult) (DeliveryResult, error) {
	if result.WebhookID == "" || result.EventID == "" || result.EventType == "" {
		return DeliveryResult{}, fmt.Errorf("%w: missing delivery identity", ErrInvalidInput)
	}
	if result.ID == "" {
		result.ID = uuid.NewString()
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = s.now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveries[result.WebhookID] = append(s.deliveries[result.WebhookID], result)
	return result, nil
}

func (s *InMemoryStore) ListDeliveries(_ context.Context, webhookID string) ([]DeliveryResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]DeliveryResult(nil), s.deliveries[webhookID]...)
	return out, nil
}

func validateRegistrationInput(input CreateRegistrationInput) error {
	parsed, err := url.Parse(strings.TrimSpace(input.URL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: invalid url", ErrInvalidInput)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: unsupported url scheme", ErrInvalidInput)
	}
	if len(input.EventTypes) == 0 {
		return fmt.Errorf("%w: missing event types", ErrInvalidInput)
	}
	seen := make(map[eventbus.EventType]bool, len(input.EventTypes))
	for _, eventType := range input.EventTypes {
		if !IsSupportedEventType(eventType) {
			return fmt.Errorf("%w: unsupported event type", ErrInvalidInput)
		}
		if seen[eventType] {
			return fmt.Errorf("%w: duplicate event type", ErrInvalidInput)
		}
		seen[eventType] = true
	}
	if strings.TrimSpace(input.Secret) == "" {
		return fmt.Errorf("%w: missing secret", ErrInvalidInput)
	}
	return nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return "sha256:" + hex.EncodeToString(sum[:])
}
