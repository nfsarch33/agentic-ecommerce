package billing

// BillingPayloadVersion is the schema version of BillingPayload.
// Mirrors eventbus.BillingPayloadVersion so adapters that bridge to
// the bus can validate without round-tripping through the bus.
const BillingPayloadVersion = 1

// BillingPayload is the typed payload the billing Service emits via
// EventPublisher. It is intentionally a billing-package value type so
// the Service contract does not import the eventbus package; the
// BusEventPublisher in event_publisher.go bridges this to the
// eventbus envelope.
type BillingPayload struct {
	Version              int    `json:"version"`
	TenantID             string `json:"tenant_id"`
	SubscriptionID       string `json:"subscription_id"`
	PlanID               string `json:"plan_id,omitempty"`
	StripeSubscriptionID string `json:"stripe_subscription_id,omitempty"`
	State                string `json:"state,omitempty"`
	InvoiceID            string `json:"invoice_id,omitempty"`
	Amount               int    `json:"amount,omitempty"`
	Currency             string `json:"currency,omitempty"`
}
