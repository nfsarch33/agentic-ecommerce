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

	// v3.5.0 EC-6-1 supplier cost-change event. Carries the typed
	// SupplierCostChangedPayload; the EC-6-3 dynamic pricing agent
	// subscribes to it.
	SupplierCostChanged EventType = "supplier.cost.changed"

	// v3.5.0 EC-6-3 dynamic pricing approval gate event. Emitted
	// when the proposed price change exceeds the operator-configured
	// large-change threshold (default 15%).
	PriceChangePendingApproval EventType = "price.change.pending_approval"

	// v3.5.0 EC-6-3 dynamic pricing approved-and-applied event.
	// Emitted when the agent applied a price within guardrails (or
	// after operator approval cleared the gate).
	PriceChangeApplied EventType = "price.change.applied"

	// v3.5.0 EC-7-1 normalised cross-channel order event. Emitted
	// by the multi-channel order aggregator workflow once an order
	// has been deduped + normalised. EC-7-2 drop-ship agent
	// subscribes to it.
	OrderNormalised EventType = "order.normalised"

	// v3.5.0 EC-7-2 drop-ship pending-approval event for orders
	// that exceed the operator-configured large-order threshold
	// (default A$500).
	LargeDropshipOrderPendingApproval EventType = "dropship.order.pending_approval"

	// v3.5.0 EC-7-2 drop-ship placed event. Emitted after the
	// supplier order succeeded (primary or fallback adapter).
	DropshipOrderPlaced EventType = "dropship.order.placed"

	// v3.5.0 EC-7-2 drop-ship saga rollback event. Emitted when
	// every supplier adapter failed AND the customer-side
	// fulfillment trigger was rolled back.
	DropshipOrderRolledBack EventType = "dropship.order.rolled_back"

	// v3.8.0 EC-7-3 carrier label issued. Emitted by the shipping
	// label generator after a carrier-side CreateLabel succeeded.
	ShipmentLabelGenerated EventType = "shipment.label.generated"

	// v3.8.0 EC-7-4 carrier shipment status transitions. Emitted by
	// the AusPost / DHL webhook handlers after HMAC verify-then-parse.
	ShipmentStatusUpdated EventType = "shipment.status.updated"

	// v3.8.0 EC-7-4 final delivered transition. Distinct event type
	// so the EC-7-5 returns saga can subscribe directly without
	// filtering on payload status.
	OrderDelivered EventType = "order.delivered"

	// v3.8.0 EC-7-5 customer-initiated return. Carries
	// ReturnsSagaPayload.
	ReturnRequested EventType = "return.requested"

	// v3.8.0 EC-7-5 large refund operator approval gate event.
	// Emitted when refund_amount >= the configured threshold
	// (default A$50).
	LargeRefundPendingApproval EventType = "return.large_refund_pending_approval"

	// v3.8.0 EC-7-5 saga completion event. All saga steps ran.
	ReturnsSagaCompleted EventType = "return.saga.completed"

	// v3.8.0 EC-7-5 saga rollback event. Compensating activities ran.
	ReturnsSagaRolledBack EventType = "return.saga.rolled_back"

	// v3.9.0 EC-6-4 competitor price observation. Emitted by the
	// competitor scraper every time it observes a competitor's
	// price for one of the tenant's SKUs.
	CompetitorPriceObserved EventType = "competitor.price.observed"

	// v3.9.0 EC-6-4 competitor undercut detected. Emitted when the
	// observed competitor price is more than the configurable
	// delta threshold below the tenant's current price; the EC-6-3
	// dynamic pricing agent subscribes to it.
	CompetitorUndercut EventType = "competitor.price.undercut"

	// v3.9.0 EC-5-2 content calendar entry scheduled. Emitted when
	// the calendar agent accepts a new entry; the n8n scheduler
	// subscribes to it.
	ContentCalendarEntryScheduled EventType = "content.calendar.entry.scheduled"

	// v3.9.0 EC-5-2 content calendar entry published. Emitted when
	// the n8n scheduler reports a successful publish.
	ContentCalendarEntryPublished EventType = "content.calendar.entry.published"

	// v3.9.0 EC-5-2 content calendar entry failed. Emitted when
	// publishing exceeded the retry budget.
	ContentCalendarEntryFailed EventType = "content.calendar.entry.failed"

	// v3.9.0 EC-5-5 content performance EMA updated. Emitted by
	// the feedback loop after each engagement-metric ingestion;
	// the EC-5-4 hashtag/caption agent subscribes to bias future
	// generations.
	ContentEMAUpdated EventType = "content.ema.updated"

	// v3.9.1 Existing #10 -- AI onboarding wizard completion.
	// Emitted by the onboarding handler when a tenant finalises
	// all four wizard steps and the existing tenant_onboarding
	// Temporal workflow has been launched.
	TenantOnboarded EventType = "tenant.onboarded"

	// v3.9.1 EC-4-4 -- channel router fan-out signal indicating
	// the matched channel adapter is a production-ready stub
	// (Instagram / Pinterest). Treated as a non-failure outcome
	// so the dispatch metrics do not show false positives.
	ChannelStatusNotYetImplemented EventType = "channel.status.not_yet_implemented"

	// v3.9.1 EC-9-5 -- operator alert centre resolution event.
	// Emitted when an operator approves or denies a pending alert
	// via POST /api/v1/operator/alerts/{id}/resolve.
	OperatorAlertResolved EventType = "operator.alert.resolved"
)

type Event struct {
	ID        string         `json:"id"`
	Type      EventType      `json:"type"`
	TenantID  string         `json:"tenant_id"`
	Payload   map[string]any `json:"payload"`
	Timestamp time.Time      `json:"timestamp"`
	Source    string         `json:"source"`
}
