// File scope: v3.9.1 typed event payloads for the final v4 polish
// QA sprint -- Existing #10 onboarding wizard, EC-9-4 channel content
// analytics, EC-9-5 operator alert centre, and EC-4-4 IG/Pinterest
// stub adapters. Every payload follows the v3.5.0 / v3.8.0 / v3.9.0
// envelope pattern: typed Validate, typed asMap, typed constructor.
//
// Reuse evidence:
//   - Pattern mirrors v3.9.0 (v390_payloads.go) +
//     v3.8.0 (v380_payloads.go).
//   - Error sentinel + %w-wrap from the package convention.
package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// TenantOnboardedPayloadVersion is the schema version of
// TenantOnboardedPayload. Bump on breaking change.
const TenantOnboardedPayloadVersion = 1

// TenantOnboardedPayload is the v3.9.1 Existing #10 envelope. Emitted
// by the OnboardingWizard handler when a tenant successfully
// completes all four steps and the canonical tenant_onboarding
// workflow has been launched. Downstream agents (channel router,
// pricing agent, content calendar) subscribe to wire defaults.
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

// ErrTenantOnboardedInvalid is returned by Validate.
var ErrTenantOnboardedInvalid = errors.New("invalid tenant onboarded payload")

// Validate enforces required fields.
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

// NewTenantOnboardedEvent is the canonical constructor for Existing
// #10 onboarding completion.
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

// ChannelStatusNotYetImplementedPayloadVersion is the schema version
// of the EC-4-4 stub-channel signal payload.
const ChannelStatusNotYetImplementedPayloadVersion = 1

// ChannelStatusNotYetImplementedPayload is the v3.9.1 EC-4-4 envelope.
// Emitted by the channel router when an enriched product is routed to
// an Instagram or Pinterest stub adapter (production-ready facades
// pending v4.1.x integration). Downstream dashboards surface the
// "stub-routed but not delivered" signal so operators can pivot the
// fan-out plan without seeing it as a hard failure.
type ChannelStatusNotYetImplementedPayload struct {
	Version    int       `json:"version"`
	TenantID   string    `json:"tenant_id"`
	Channel    string    `json:"channel"`
	Op         string    `json:"op"`
	ProductID  string    `json:"product_id,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// ErrChannelStatusNotYetImplementedInvalid is returned by Validate.
var ErrChannelStatusNotYetImplementedInvalid = errors.New("invalid channel status not_yet_implemented payload")

// Validate enforces required fields.
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

// NewChannelStatusNotYetImplementedEvent is the canonical constructor
// for the EC-4-4 stub-channel signal.
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

// OperatorAlertResolvedPayloadVersion is the schema version of the
// EC-9-5 operator-alert-resolved payload.
const OperatorAlertResolvedPayloadVersion = 1

// OperatorAlertResolvedPayload is the v3.9.1 EC-9-5 envelope.
// Emitted by the operator alert centre when an operator resolves a
// pending alert (approve / deny). The downstream agents that produced
// the original alert subscribe so the source-side approval gate can
// honour the operator's decision.
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

// ErrOperatorAlertResolvedInvalid is returned by Validate.
var ErrOperatorAlertResolvedInvalid = errors.New("invalid operator alert resolved payload")

// Validate enforces required fields.
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

// NewOperatorAlertResolvedEvent is the canonical constructor for
// EC-9-5 alert resolution.
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
