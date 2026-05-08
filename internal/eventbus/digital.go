package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// DigitalPayloadVersion is the schema version of DigitalPayload. Bump
// only on a breaking change to the field set; subscribers gate on this
// value before consuming the payload.
const DigitalPayloadVersion = 1

// DigitalPayload is the typed envelope shipped inside Event.Payload
// for every digital.* and license.* event. Tenant scoping is also
// duplicated at Event.TenantID so downstream consumers reading only
// the payload (e.g. webhook bridges) still get tenant scoping.
type DigitalPayload struct {
	Version    int    `json:"version"`
	TenantID   string `json:"tenant_id"`
	ProductID  string `json:"product_id,omitempty"`
	ProductSKU string `json:"product_sku,omitempty"`
	LicenseID  string `json:"license_id,omitempty"`
	CustomerID string `json:"customer_id,omitempty"`
	State      string `json:"state,omitempty"`
	Source     string `json:"source,omitempty"`
}

// ErrDigitalPayloadInvalid is the sentinel returned by
// DigitalPayload.Validate when required fields are absent.
var ErrDigitalPayloadInvalid = errors.New("invalid digital payload")

// Validate returns an error when required fields are missing.
func (p DigitalPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version is zero", ErrDigitalPayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrDigitalPayloadInvalid)
	}
	if p.ProductID == "" && p.LicenseID == "" {
		return fmt.Errorf("%w: at least one of product_id or license_id required", ErrDigitalPayloadInvalid)
	}
	return nil
}

// IsDigitalEvent reports whether the EventType belongs to the digital
// bounded context.
func IsDigitalEvent(t EventType) bool {
	switch t {
	case DigitalProductCreated, DigitalProductUpdated, DigitalProductDeleted,
		DigitalPurchased, DigitalDownloaded,
		LicenseActivated, LicenseRevoked, LicenseExpired:
		return true
	default:
		return false
	}
}

// asMap renders the typed payload as the generic map[string]any the
// in-memory bus stores in Event.Payload.
func (p DigitalPayload) asMap() map[string]any {
	return map[string]any{
		"version":     p.Version,
		"tenant_id":   p.TenantID,
		"product_id":  p.ProductID,
		"product_sku": p.ProductSKU,
		"license_id":  p.LicenseID,
		"customer_id": p.CustomerID,
		"state":       p.State,
		"source":      p.Source,
	}
}

// NewDigitalEvent is the canonical constructor every digital publisher
// path goes through. Defaults Version when zero, stamps timestamp when
// missing, and validates the payload.
func NewDigitalEvent(eventType EventType, source string, occurredAt time.Time, payload DigitalPayload) (Event, error) {
	if !IsDigitalEvent(eventType) {
		return Event{}, fmt.Errorf("%w: %s is not a digital event", ErrDigitalPayloadInvalid, eventType)
	}
	if payload.Version == 0 {
		payload.Version = DigitalPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "mc-api.digital"
	}
	return Event{
		Type:      eventType,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
