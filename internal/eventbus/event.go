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

	// v3.1.0 EC-1-3 China Sourcing Agent. Fired by the sourcing agent
	// every time it produces a `ProductSourcingProposal` for a tenant.
	// Carries the typed SourcingProposalPayload via Event.Payload (as
	// map[string]any for in-memory bus compatibility).
	ProductSourcingProposed EventType = "product.sourcing.proposed"

	// v3.2.0 EC-2 enrichment + v3.3.0 EC-3 channel publish bounded
	// context. Carries the typed ProductEnrichedPayload (see
	// product_enriched.go).
	ProductEnriched EventType = "product.enriched"

	// v3.3.0 EC-3-2 TikTok listing rollback signal. Emitted by the
	// EC-3-2 channel agent when the publish path failed and the
	// compensating action ran. Carries TikTokListingRollbackPayload.
	TikTokListingRolledBack EventType = "tiktok.listing.rolled_back"

	// v3.3.0 EC-3-3 inbound TikTok Shop order webhook event. Carries
	// the typed OrderReceivedPayload (see order_received.go) so the
	// downstream fulfilment + inventory pipelines stay tenant-scoped
	// and idempotency-keyed.
	OrderReceived EventType = "order.received"
)

type Event struct {
	ID        string         `json:"id"`
	Type      EventType      `json:"type"`
	TenantID  string         `json:"tenant_id"`
	Payload   map[string]any `json:"payload"`
	Timestamp time.Time      `json:"timestamp"`
	Source    string         `json:"source"`
}
