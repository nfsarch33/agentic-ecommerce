// Customer messaging domain payloads.
//
// Consolidated from customer_message.go in v5.4.0. The original file
// is retained as a thin backward-compat import target (no payload
// definitions remain there).
package eventbus

import (
	"errors"
	"fmt"
	"time"
)

const CustomerMessagePayloadVersion = 1

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

var ErrCustomerMessagePayloadInvalid = errors.New("invalid customer message payload")

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

func NewCustomerMessageReceivedEvent(source string, occurredAt time.Time, payload CustomerMessagePayload) (Event, error) {
	return newCustomerMessageEvent(CustomerMessageReceived, source, occurredAt, payload, "webhook.messaging.received")
}

func NewCustomerMessageRepliedEvent(source string, occurredAt time.Time, payload CustomerMessagePayload) (Event, error) {
	return newCustomerMessageEvent(CustomerMessageReplied, source, occurredAt, payload, "webhook.messaging.replied")
}

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
