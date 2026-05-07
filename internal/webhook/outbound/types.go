package outbound

import (
	"errors"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

var (
	ErrInvalidInput = errors.New("invalid webhook input")
	ErrNotFound     = errors.New("webhook not found")
)

type Registration struct {
	ID         string               `json:"id"`
	TenantID   string               `json:"tenant_id,omitempty"`
	URL        string               `json:"url"`
	EventTypes []eventbus.EventType `json:"event_types"`
	SecretRef  string               `json:"secret_ref,omitempty"`
	SecretHash string               `json:"secret_hash"`
	Enabled    bool                 `json:"enabled"`
	CreatedAt  time.Time            `json:"created_at"`
}

type CreateRegistrationInput struct {
	TenantID   string
	URL        string
	EventTypes []eventbus.EventType
	Secret     string
	SecretRef  string
	Enabled    *bool
}

type DeliveryResult struct {
	ID        string             `json:"id,omitempty"`
	WebhookID string             `json:"webhook_id"`
	EventID   string             `json:"event_id"`
	EventType eventbus.EventType `json:"event_type"`
	Success   bool               `json:"success"`
	Status    int                `json:"status"`
	Attempts  int                `json:"attempts"`
	Error     string             `json:"error,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
}

type EventPayload struct {
	ID        string             `json:"id"`
	Type      eventbus.EventType `json:"type"`
	TenantID  string             `json:"tenant_id,omitempty"`
	Payload   map[string]any     `json:"payload,omitempty"`
	Timestamp time.Time          `json:"timestamp"`
	Source    string             `json:"source,omitempty"`
}

func SupportedEventTypes() []eventbus.EventType {
	return []eventbus.EventType{
		eventbus.ProductCreated,
		eventbus.ProductUpdated,
		eventbus.OrderPlaced,
		eventbus.SyncCompleted,
		eventbus.AgentRunCompleted,
		eventbus.ComplianceChecked,
	}
}

func IsSupportedEventType(eventType eventbus.EventType) bool {
	for _, supported := range SupportedEventTypes() {
		if eventType == supported {
			return true
		}
	}
	return false
}

func cloneRegistration(reg Registration) Registration {
	reg.EventTypes = append([]eventbus.EventType(nil), reg.EventTypes...)
	return reg
}
