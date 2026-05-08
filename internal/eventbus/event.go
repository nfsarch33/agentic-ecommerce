package eventbus

import "time"

type EventType string

const (
	ProductCreated    EventType = "product.created"
	ProductUpdated    EventType = "product.updated"
	OrderPlaced       EventType = "order.placed"
	SyncCompleted     EventType = "sync.completed"
	AgentRunCompleted EventType = "agent.run.completed"
	ComplianceChecked EventType = "compliance.checked"

	// Membership bounded context events. The lifecycle reflects the
	// subscription state machine in internal/domain/membership/state.go.
	MembershipCreated   EventType = "membership.created"
	MembershipRenewed   EventType = "membership.renewed"
	MembershipCancelled EventType = "membership.cancelled"
	MembershipPaused    EventType = "membership.paused"
	MembershipResumed   EventType = "membership.resumed"

	// Digital goods bounded context events. The lifecycle reflects the
	// licence state machine in internal/domain/digital/state.go.
	DigitalProductCreated EventType = "digital.product.created"
	DigitalProductUpdated EventType = "digital.product.updated"
	DigitalProductDeleted EventType = "digital.product.deleted"
	DigitalPurchased      EventType = "digital.purchased"
	DigitalDownloaded     EventType = "digital.downloaded"
	LicenseActivated      EventType = "license.activated"
	LicenseRevoked        EventType = "license.revoked"
	LicenseExpired        EventType = "license.expired"
)

type Event struct {
	ID        string         `json:"id"`
	Type      EventType      `json:"type"`
	TenantID  string         `json:"tenant_id"`
	Payload   map[string]any `json:"payload"`
	Timestamp time.Time      `json:"timestamp"`
	Source    string         `json:"source"`
}
