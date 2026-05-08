package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// MembershipPayloadVersion is the schema version of MembershipPayload.
// Bump only on a breaking change to the field set; subscribers gate on
// this value before consuming the payload.
const MembershipPayloadVersion = 1

// MembershipPayload is the typed envelope shipped inside Event.Payload
// for every membership.* event. The TenantID also lives at Event.TenantID;
// it is duplicated here so downstream consumers reading only the payload
// (e.g. webhook bridges) still get tenant scoping without drilling into
// the envelope.
type MembershipPayload struct {
	Version        int    `json:"version"`
	TenantID       string `json:"tenant_id"`
	SubscriptionID string `json:"subscription_id"`
	MemberID       string `json:"member_id"`
	MemberEmail    string `json:"member_email"`
	PlanID         string `json:"plan_id"`
	PlanName       string `json:"plan_name"`
	State          string `json:"state"`
}

// Validate returns an error when required fields are missing or empty.
// Returned errors wrap ErrMembershipPayloadInvalid so callers can use
// errors.Is checks.
func (p MembershipPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version is zero", ErrMembershipPayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrMembershipPayloadInvalid)
	}
	if p.SubscriptionID == "" {
		return fmt.Errorf("%w: subscription_id missing", ErrMembershipPayloadInvalid)
	}
	if p.PlanID == "" {
		return fmt.Errorf("%w: plan_id missing", ErrMembershipPayloadInvalid)
	}
	if p.State == "" {
		return fmt.Errorf("%w: state missing", ErrMembershipPayloadInvalid)
	}
	return nil
}

// ErrMembershipPayloadInvalid is the sentinel returned by
// MembershipPayload.Validate when required fields are absent.
var ErrMembershipPayloadInvalid = errors.New("invalid membership payload")

// IsMembershipEvent reports whether the EventType belongs to the
// membership bounded context.
func IsMembershipEvent(t EventType) bool {
	switch t {
	case MembershipCreated, MembershipRenewed, MembershipCancelled,
		MembershipPaused, MembershipResumed:
		return true
	default:
		return false
	}
}

// asMap renders the typed payload as the generic map[string]any the
// in-memory bus stores in Event.Payload. Keep field names in sync with
// MembershipPayload JSON tags so downstream Streams/JSON consumers see a
// single canonical shape.
func (p MembershipPayload) asMap() map[string]any {
	return map[string]any{
		"version":         p.Version,
		"tenant_id":       p.TenantID,
		"subscription_id": p.SubscriptionID,
		"member_id":       p.MemberID,
		"member_email":    p.MemberEmail,
		"plan_id":         p.PlanID,
		"plan_name":       p.PlanName,
		"state":           p.State,
	}
}

// NewMembershipEvent is the canonical constructor every membership
// publisher path goes through. It defaults Version when zero, stamps
// the timestamp when missing, and validates the payload.
//
// Source is a free-form attribution string ("mc-api.membership",
// "workflow.membership.notifier", ...). When empty, "mc-api.membership"
// is used so audit trails always show a real producer.
func NewMembershipEvent(eventType EventType, source string, occurredAt time.Time, payload MembershipPayload) (Event, error) {
	if !IsMembershipEvent(eventType) {
		return Event{}, fmt.Errorf("%w: %s is not a membership event", ErrMembershipPayloadInvalid, eventType)
	}
	if payload.Version == 0 {
		payload.Version = MembershipPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "mc-api.membership"
	}
	return Event{
		Type:      eventType,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
