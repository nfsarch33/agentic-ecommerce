// File scope: v3.6.0 EC-8-3 typed event payloads for the inbound
// customer messaging pipeline.
//
// Three events surface from the messaging webhook layer:
//
//   - CustomerMessageReceived            -- fires after HMAC verify +
//     idempotency check + envelope decode (the inbound side has
//     definitively accepted the message).
//   - CustomerMessageReplied             -- fires after the EC-8-2
//     responder produced an auto_replied or suggested response and
//     the channel adapter SendMessage call succeeded.
//   - CustomerMessageEscalatedToOperator -- fires when the responder
//     routed to the operator queue (low confidence, no FAQ match,
//     or the channel send failed).
//
// All three events follow the v3.5.0 typed-payload envelope pattern
// (Validate + asMap + canonical constructor).
package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// EC-8-3 event types. Declared additive to the existing block in
// event.go so prior consumers stay source-compatible.
const (
	// CustomerMessageReceived fires once the inbound webhook
	// definitively accepted a message (HMAC verified + idempotency
	// check passed + envelope decoded).
	CustomerMessageReceived EventType = "customer.message.received"

	// CustomerMessageReplied fires once the EC-8-2 responder + the
	// channel adapter SendMessage succeeded.
	CustomerMessageReplied EventType = "customer.message.replied"

	// CustomerMessageEscalatedToOperator fires when the responder
	// routed the inbound message to the operator queue.
	CustomerMessageEscalatedToOperator EventType = "customer.message.escalated_to_operator"
)

// CustomerMessagePayloadVersion is the schema version of the
// customer message payload family. Bump on breaking change.
const CustomerMessagePayloadVersion = 1

// CustomerMessagePayload is the EC-8-3 envelope shipped inside
// Event.Payload for every customer.message.* event.
type CustomerMessagePayload struct {
	Version           int       `json:"version"`
	TenantID          string    `json:"tenant_id"`
	MessageID         string    `json:"message_id"`
	Channel           string    `json:"channel"`
	ThreadID          string    `json:"thread_id"`
	BuyerID           string    `json:"buyer_id,omitempty"`
	Intent            string    `json:"intent,omitempty"`
	Sentiment         string    `json:"sentiment,omitempty"`
	Language          string    `json:"language,omitempty"`
	ConfidenceScore   float64   `json:"confidence_score,omitempty"`
	Outcome           string    `json:"outcome,omitempty"`
	ReplyText         string    `json:"reply_text,omitempty"`
	ProviderMessageID string    `json:"provider_message_id,omitempty"`
	Reason            string    `json:"reason,omitempty"`
	OccurredAt        time.Time `json:"occurred_at"`
}

// ErrCustomerMessagePayloadInvalid is the sentinel for missing fields.
var ErrCustomerMessagePayloadInvalid = errors.New("invalid customer message payload")

// Validate enforces required identity fields.
func (p CustomerMessagePayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrCustomerMessagePayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrCustomerMessagePayloadInvalid)
	}
	if p.MessageID == "" {
		return fmt.Errorf("%w: message_id missing", ErrCustomerMessagePayloadInvalid)
	}
	if p.Channel == "" {
		return fmt.Errorf("%w: channel missing", ErrCustomerMessagePayloadInvalid)
	}
	return nil
}

func (p CustomerMessagePayload) asMap() map[string]any {
	return map[string]any{
		"version":             p.Version,
		"tenant_id":           p.TenantID,
		"message_id":          p.MessageID,
		"channel":             p.Channel,
		"thread_id":           p.ThreadID,
		"buyer_id":            p.BuyerID,
		"intent":              p.Intent,
		"sentiment":           p.Sentiment,
		"language":            p.Language,
		"confidence_score":    p.ConfidenceScore,
		"outcome":             p.Outcome,
		"reply_text":          p.ReplyText,
		"provider_message_id": p.ProviderMessageID,
		"reason":              p.Reason,
		"occurred_at":         p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

// NewCustomerMessageReceivedEvent is the canonical constructor for
// the inbound-accepted event.
func NewCustomerMessageReceivedEvent(source string, occurredAt time.Time, payload CustomerMessagePayload) (Event, error) {
	return newCustomerMessageEvent(CustomerMessageReceived, source, occurredAt, payload, "webhook.messaging.received")
}

// NewCustomerMessageRepliedEvent is the canonical constructor for
// the auto-reply success event.
func NewCustomerMessageRepliedEvent(source string, occurredAt time.Time, payload CustomerMessagePayload) (Event, error) {
	return newCustomerMessageEvent(CustomerMessageReplied, source, occurredAt, payload, "webhook.messaging.replied")
}

// NewCustomerMessageEscalatedEvent is the canonical constructor for
// the operator-escalation event.
func NewCustomerMessageEscalatedEvent(source string, occurredAt time.Time, payload CustomerMessagePayload) (Event, error) {
	return newCustomerMessageEvent(CustomerMessageEscalatedToOperator, source, occurredAt, payload, "webhook.messaging.escalated")
}

func newCustomerMessageEvent(kind EventType, source string, occurredAt time.Time, payload CustomerMessagePayload, defaultSource string) (Event, error) {
	if payload.Version == 0 {
		payload.Version = CustomerMessagePayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = defaultSource
	}
	return Event{
		Type:      kind,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
