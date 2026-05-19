package outbound

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

type ServiceConfig struct {
	Store  Store
	Client *Client
	Logger *slog.Logger
}

type Service struct {
	store  Store
	client *Client
	log    *slog.Logger
}

func NewService(cfg ServiceConfig) *Service {
	store := cfg.Store
	if store == nil {
		store = NewInMemoryStore()
	}
	client := cfg.Client
	if client == nil {
		client = NewClient(ClientConfig{})
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, client: client, log: logger}
}

func (s *Service) Register(ctx context.Context, input CreateRegistrationInput) (Registration, error) {
	return s.store.CreateRegistration(ctx, input)
}

func (s *Service) List(ctx context.Context) ([]Registration, error) {
	return s.store.ListRegistrations(ctx)
}

func (s *Service) ListForTenant(ctx context.Context, tenantID string) ([]Registration, error) {
	tenantID = strings.TrimSpace(tenantID)
	registrations, err := s.store.ListRegistrations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Registration, 0, len(registrations))
	for _, reg := range registrations {
		if reg.TenantID == tenantID {
			out = append(out, reg)
		}
	}
	return out, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.DeleteRegistration(ctx, id)
}

func (s *Service) DeleteForTenant(ctx context.Context, id, tenantID string) error {
	if _, err := s.registrationForTenant(ctx, id, tenantID); err != nil {
		return err
	}
	return s.store.DeleteRegistration(ctx, id)
}

func (s *Service) DeliverEvent(ctx context.Context, event eventbus.Event) ([]DeliveryResult, error) {
	registrations, err := s.store.EnabledRegistrationsForEvent(ctx, event.Type)
	if err != nil {
		return nil, fmt.Errorf("list webhook registrations: %w", err)
	}
	results := make([]DeliveryResult, 0, len(registrations))
	for _, reg := range registrations {
		if event.TenantID != "" && reg.TenantID != "" && reg.TenantID != event.TenantID {
			continue
		}
		secret, err := s.store.SecretForRegistration(ctx, reg.ID)
		if err != nil {
			return results, fmt.Errorf("load webhook secret metadata: %w", err)
		}
		result := s.client.Deliver(ctx, DeliveryRequest{Registration: reg, Secret: secret, Event: event})
		recorded, err := s.store.RecordDelivery(ctx, result)
		if err != nil {
			return append(results, result), fmt.Errorf("record webhook delivery: %w", err)
		}
		results = append(results, recorded)
		if s.log != nil && !recorded.Success {
			s.log.WarnContext(ctx, "webhook.delivery_failed",
				"webhook_id", recorded.WebhookID,
				"event_id", recorded.EventID,
				"event_type", string(recorded.EventType),
				"status", recorded.Status,
				"attempts", recorded.Attempts,
				"error", recorded.Error,
			)
		}
	}
	return results, nil
}

func (s *Service) Test(ctx context.Context, id string, eventType eventbus.EventType) (DeliveryResult, error) {
	reg, err := s.store.GetRegistration(ctx, id)
	if err != nil {
		return DeliveryResult{}, err
	}
	if eventType == "" {
		if len(reg.EventTypes) == 0 {
			return DeliveryResult{}, fmt.Errorf("%w: missing event type", ErrInvalidInput)
		}
		eventType = reg.EventTypes[0]
	}
	if !IsSupportedEventType(eventType) || !registrationHasEventType(reg, eventType) {
		return DeliveryResult{}, fmt.Errorf("%w: unsupported event type", ErrInvalidInput)
	}
	secret, err := s.store.SecretForRegistration(ctx, reg.ID)
	if err != nil {
		return DeliveryResult{}, err
	}
	event := eventbus.Event{
		ID:        uuid.NewString(),
		Type:      eventType,
		TenantID:  "default",
		Payload:   map[string]any{"test": true},
		Timestamp: time.Now().UTC(),
		Source:    "webhook.test",
	}
	result := s.client.Deliver(ctx, DeliveryRequest{Registration: reg, Secret: secret, Event: event})
	recorded, err := s.store.RecordDelivery(ctx, result)
	if err != nil {
		return result, err
	}
	return recorded, nil
}

func (s *Service) TestForTenant(ctx context.Context, id, tenantID string, eventType eventbus.EventType) (DeliveryResult, error) {
	if _, err := s.registrationForTenant(ctx, id, tenantID); err != nil {
		return DeliveryResult{}, err
	}
	return s.Test(ctx, id, eventType)
}

func (s *Service) registrationForTenant(ctx context.Context, id, tenantID string) (Registration, error) {
	reg, err := s.store.GetRegistration(ctx, id)
	if err != nil {
		return Registration{}, err
	}
	if reg.TenantID != strings.TrimSpace(tenantID) {
		return Registration{}, ErrNotFound
	}
	return reg, nil
}

func (s *Service) Subscribe(ctx context.Context, consumer eventbus.Consumer, group string) error {
	if consumer == nil {
		return errors.New("nil event consumer")
	}
	if group == "" {
		group = "webhook-bridge"
	}
	return consumer.Subscribe(ctx, SupportedEventTypes(), group, func(ctx context.Context, event eventbus.Event) error {
		_, err := s.DeliverEvent(ctx, event)
		return err
	})
}

func registrationHasEventType(reg Registration, eventType eventbus.EventType) bool {
	for _, subscribed := range reg.EventTypes {
		if subscribed == eventType {
			return true
		}
	}
	return false
}
