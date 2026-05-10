package eventbus

// EventType constants grouped by domain. All event type values are
// stable strings used in persisted event streams and subscriber
// routing -- never change a value without a migration.

type EventType string

// --- Product domain ---
const (
	EventProductCreated          EventType = "product.created"
	EventProductUpdated          EventType = "product.updated"
	EventProductEnriched         EventType = "product.enriched"
	EventProductSourcingProposed EventType = "product.sourcing.proposed"
)

// --- Order / fulfilment domain ---
const (
	EventOrderPlaced                      EventType = "order.placed"
	EventOrderReceived                    EventType = "order.received"
	EventOrderNormalised                  EventType = "order.normalised"
	EventOrderDelivered                   EventType = "order.delivered"
	EventDropshipOrderPendingApproval     EventType = "dropship.order.pending_approval"
	EventDropshipOrderPlaced              EventType = "dropship.order.placed"
	EventDropshipOrderRolledBack          EventType = "dropship.order.rolled_back"
	EventShipmentLabelGenerated           EventType = "shipment.label.generated"
	EventShipmentStatusUpdated            EventType = "shipment.status.updated"
	EventReturnRequested                  EventType = "return.requested"
	EventReturnLargeRefundPendingApproval EventType = "return.large_refund_pending_approval"
	EventReturnsSagaCompleted             EventType = "return.saga.completed"
	EventReturnsSagaRolledBack            EventType = "return.saga.rolled_back"
)

// --- Channel domain ---
const (
	EventTikTokListingRolledBack        EventType = "tiktok.listing.rolled_back"
	EventChannelStatusNotYetImplemented EventType = "channel.status.not_yet_implemented"
	EventSyncCompleted                  EventType = "sync.completed"
)

// --- Pricing / competitor domain ---
const (
	EventSupplierCostChanged        EventType = "supplier.cost.changed"
	EventPriceChangePendingApproval EventType = "price.change.pending_approval"
	EventPriceChangeApplied         EventType = "price.change.applied"
	EventCompetitorPriceObserved    EventType = "competitor.price.observed"
	EventCompetitorUndercut         EventType = "competitor.price.undercut"
)

// --- Content domain ---
const (
	EventContentCalendarEntryScheduled EventType = "content.calendar.entry.scheduled"
	EventContentCalendarEntryPublished EventType = "content.calendar.entry.published"
	EventContentCalendarEntryFailed    EventType = "content.calendar.entry.failed"
	EventContentEMAUpdated             EventType = "content.ema.updated"
)

// --- Payment domain ---
const (
	EventPaymentCompleted       EventType = "payment.completed"
	EventPaymentFailed          EventType = "payment.failed"
	EventPaymentRefundRequested EventType = "payment.refund.requested"
)

// --- Customer messaging domain ---
const (
	EventCustomerMessageReceived  EventType = "customer.message.received"
	EventCustomerMessageReplied   EventType = "customer.message.replied"
	EventCustomerMessageEscalated EventType = "customer.message.escalated_to_operator"
)

// --- Membership domain ---
const (
	EventMembershipCreated   EventType = "membership.created"
	EventMembershipRenewed   EventType = "membership.renewed"
	EventMembershipCancelled EventType = "membership.cancelled"
	EventMembershipPaused    EventType = "membership.paused"
	EventMembershipResumed   EventType = "membership.resumed"
)

// --- Digital goods domain ---
const (
	EventDigitalProductCreated EventType = "digital.product.created"
	EventDigitalProductUpdated EventType = "digital.product.updated"
	EventDigitalProductDeleted EventType = "digital.product.deleted"
	EventDigitalPurchased      EventType = "digital.purchased"
	EventDigitalDownloaded     EventType = "digital.downloaded"
	EventLicenseActivated      EventType = "license.activated"
	EventLicenseRevoked        EventType = "license.revoked"
	EventLicenseExpired        EventType = "license.expired"
)

// --- Billing domain ---
const (
	EventSubscriptionCreated  EventType = "subscription.created"
	EventSubscriptionUpdated  EventType = "subscription.updated"
	EventSubscriptionCanceled EventType = "subscription.canceled"
	EventInvoicePaid          EventType = "invoice.paid"
	EventInvoiceFailed        EventType = "invoice.failed"
)

// --- System / operational domain ---
const (
	EventAgentRunCompleted            EventType = "agent.run.completed"
	EventComplianceChecked            EventType = "compliance.checked"
	EventTenantOnboarded              EventType = "tenant.onboarded"
	EventOperatorAlertResolved        EventType = "operator.alert.resolved"
	EventCoordinationDecisionResolved EventType = "coordination.decision.resolved"
)

// Backward-compatible aliases. Existing code references these names;
// the aliases point at the canonical Event* constants above so both
// old and new usage compiles. These will be removed in v6.0.0.
const (
	ProductCreated    = EventProductCreated
	ProductUpdated    = EventProductUpdated
	OrderPlaced       = EventOrderPlaced
	SyncCompleted     = EventSyncCompleted
	AgentRunCompleted = EventAgentRunCompleted
	ComplianceChecked = EventComplianceChecked

	MembershipCreated   = EventMembershipCreated
	MembershipRenewed   = EventMembershipRenewed
	MembershipCancelled = EventMembershipCancelled
	MembershipPaused    = EventMembershipPaused
	MembershipResumed   = EventMembershipResumed

	DigitalProductCreated = EventDigitalProductCreated
	DigitalProductUpdated = EventDigitalProductUpdated
	DigitalProductDeleted = EventDigitalProductDeleted
	DigitalPurchased      = EventDigitalPurchased
	DigitalDownloaded     = EventDigitalDownloaded
	LicenseActivated      = EventLicenseActivated
	LicenseRevoked        = EventLicenseRevoked
	LicenseExpired        = EventLicenseExpired

	ProductSourcingProposed = EventProductSourcingProposed
	ProductEnriched         = EventProductEnriched
	TikTokListingRolledBack = EventTikTokListingRolledBack
	OrderReceived           = EventOrderReceived

	SupplierCostChanged        = EventSupplierCostChanged
	PriceChangePendingApproval = EventPriceChangePendingApproval
	PriceChangeApplied         = EventPriceChangeApplied

	OrderNormalised                   = EventOrderNormalised
	LargeDropshipOrderPendingApproval = EventDropshipOrderPendingApproval
	DropshipOrderPlaced               = EventDropshipOrderPlaced
	DropshipOrderRolledBack           = EventDropshipOrderRolledBack

	ShipmentLabelGenerated     = EventShipmentLabelGenerated
	ShipmentStatusUpdated      = EventShipmentStatusUpdated
	OrderDelivered             = EventOrderDelivered
	ReturnRequested            = EventReturnRequested
	LargeRefundPendingApproval = EventReturnLargeRefundPendingApproval
	ReturnsSagaCompleted       = EventReturnsSagaCompleted
	ReturnsSagaRolledBack      = EventReturnsSagaRolledBack

	CompetitorPriceObserved       = EventCompetitorPriceObserved
	CompetitorUndercut            = EventCompetitorUndercut
	ContentCalendarEntryScheduled = EventContentCalendarEntryScheduled
	ContentCalendarEntryPublished = EventContentCalendarEntryPublished
	ContentCalendarEntryFailed    = EventContentCalendarEntryFailed
	ContentEMAUpdated             = EventContentEMAUpdated

	TenantOnboarded                = EventTenantOnboarded
	ChannelStatusNotYetImplemented = EventChannelStatusNotYetImplemented
	OperatorAlertResolved          = EventOperatorAlertResolved

	PaymentCompleted       = EventPaymentCompleted
	PaymentFailed          = EventPaymentFailed
	PaymentRefundRequested = EventPaymentRefundRequested

	CoordinationDecisionResolved = EventCoordinationDecisionResolved

	SubscriptionCreated  = EventSubscriptionCreated
	SubscriptionUpdated  = EventSubscriptionUpdated
	SubscriptionCanceled = EventSubscriptionCanceled
	InvoicePaid          = EventInvoicePaid
	InvoiceFailed        = EventInvoiceFailed

	CustomerMessageReceived            = EventCustomerMessageReceived
	CustomerMessageReplied             = EventCustomerMessageReplied
	CustomerMessageEscalatedToOperator = EventCustomerMessageEscalated
)
