// System / operational domain payloads: tenant onboarding wizard
// completion, channel stub signals, and operator alert resolution.
//
// Consolidated from v391_payloads.go in v5.4.0.
package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// --- Tenant onboarded (v3.9.1 Existing #10) ---

const TenantOnboardedPayloadVersion = 1

type TenantOnboardedPayload struct {
	Version       int       `json:"version"`
	TenantID      string    `json:"tenant_id"`
	WizardID      string    `json:"wizard_id"`
	BusinessType  string    `json:"business_type"`
	Country       string    `json:"country"`
	Channels      []string  `json:"channels"`
	Compliance    []string  `json:"compliance"`
	SeedSource    string    `json:"seed_source,omitempty"`
	SeedItemCount int       `json:"seed_item_count,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
}

var ErrTenantOnboardedInvalid = errors.New("invalid tenant onboarded payload")

func (p TenantOnboardedPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrTenantOnboardedInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrTenantOnboardedInvalid)
	}
	if p.WizardID == "" {
		return fmt.Errorf("%w: wizard_id missing", ErrTenantOnboardedInvalid)
	}
	if p.BusinessType == "" {
		return fmt.Errorf("%w: business_type missing", ErrTenantOnboardedInvalid)
	}
	if p.Country == "" {
		return fmt.Errorf("%w: country missing", ErrTenantOnboardedInvalid)
	}
	return nil
}

func (p TenantOnboardedPayload) asMap() map[string]any {
	channels := make([]any, 0, len(p.Channels))
	for _, c := range p.Channels {
		channels = append(channels, c)
	}
	compliance := make([]any, 0, len(p.Compliance))
	for _, c := range p.Compliance {
		compliance = append(compliance, c)
	}
	return map[string]any{
		"version":         p.Version,
		"tenant_id":       p.TenantID,
		"wizard_id":       p.WizardID,
		"business_type":   p.BusinessType,
		"country":         p.Country,
		"channels":        channels,
		"compliance":      compliance,
		"seed_source":     p.SeedSource,
		"seed_item_count": p.SeedItemCount,
		"occurred_at":     p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

func NewTenantOnboardedEvent(source string, occurredAt time.Time, payload TenantOnboardedPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = TenantOnboardedPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "handler.onboarding"
	}
	return Event{
		Type:      TenantOnboarded,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}

// --- Channel status not-yet-implemented (v3.9.1 EC-4-4) ---

const ChannelStatusNotYetImplementedPayloadVersion = 1

type ChannelStatusNotYetImplementedPayload struct {
	Version    int       `json:"version"`
	TenantID   string    `json:"tenant_id"`
	Channel    string    `json:"channel"`
	Op         string    `json:"op"`
	ProductID  string    `json:"product_id,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

var ErrChannelStatusNotYetImplementedInvalid = errors.New("invalid channel status not_yet_implemented payload")

func (p ChannelStatusNotYetImplementedPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrChannelStatusNotYetImplementedInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrChannelStatusNotYetImplementedInvalid)
	}
	if p.Channel == "" {
		return fmt.Errorf("%w: channel missing", ErrChannelStatusNotYetImplementedInvalid)
	}
	if p.Op == "" {
		return fmt.Errorf("%w: op missing", ErrChannelStatusNotYetImplementedInvalid)
	}
	return nil
}

func (p ChannelStatusNotYetImplementedPayload) asMap() map[string]any {
	return map[string]any{
		"version":     p.Version,
		"tenant_id":   p.TenantID,
		"channel":     p.Channel,
		"op":          p.Op,
		"product_id":  p.ProductID,
		"reason":      p.Reason,
		"occurred_at": p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

func NewChannelStatusNotYetImplementedEvent(source string, occurredAt time.Time, payload ChannelStatusNotYetImplementedPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = ChannelStatusNotYetImplementedPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "channel.stub"
	}
	return Event{
		Type:      ChannelStatusNotYetImplemented,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}

// --- Operator alert resolved (v3.9.1 EC-9-5) ---

const OperatorAlertResolvedPayloadVersion = 1

type OperatorAlertResolvedPayload struct {
	Version       int       `json:"version"`
	TenantID      string    `json:"tenant_id"`
	AlertID       string    `json:"alert_id"`
	AlertType     string    `json:"alert_type"`
	Action        string    `json:"action"`
	OperatorEmail string    `json:"operator_email,omitempty"`
	Note          string    `json:"note,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
}

var ErrOperatorAlertResolvedInvalid = errors.New("invalid operator alert resolved payload")

func (p OperatorAlertResolvedPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrOperatorAlertResolvedInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrOperatorAlertResolvedInvalid)
	}
	if p.AlertID == "" {
		return fmt.Errorf("%w: alert_id missing", ErrOperatorAlertResolvedInvalid)
	}
	if p.AlertType == "" {
		return fmt.Errorf("%w: alert_type missing", ErrOperatorAlertResolvedInvalid)
	}
	if p.Action != "approve" && p.Action != "deny" {
		return fmt.Errorf("%w: action must be approve|deny", ErrOperatorAlertResolvedInvalid)
	}
	return nil
}

func (p OperatorAlertResolvedPayload) asMap() map[string]any {
	return map[string]any{
		"version":        p.Version,
		"tenant_id":      p.TenantID,
		"alert_id":       p.AlertID,
		"alert_type":     p.AlertType,
		"action":         p.Action,
		"operator_email": p.OperatorEmail,
		"note":           p.Note,
		"occurred_at":    p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

func NewOperatorAlertResolvedEvent(source string, occurredAt time.Time, payload OperatorAlertResolvedPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = OperatorAlertResolvedPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "handler.operator_alerts"
	}
	return Event{
		Type:      OperatorAlertResolved,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
