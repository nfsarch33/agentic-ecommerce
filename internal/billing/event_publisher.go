package billing

import (
	"context"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

// BusEventPublisher is the production EventPublisher that bridges
// Service onto the in-process eventbus.Publisher port. It wraps the
// raw publisher with the canonical NewBillingEvent constructor so
// every billing event has a validated typed payload.
type BusEventPublisher struct {
	bus eventbus.Publisher
}

// NewBusEventPublisher returns a BusEventPublisher bound to the bus.
func NewBusEventPublisher(bus eventbus.Publisher) *BusEventPublisher {
	return &BusEventPublisher{bus: bus}
}

// PublishBilling implements EventPublisher.
func (p *BusEventPublisher) PublishBilling(ctx context.Context, eventType, source string, occurredAt time.Time, payload BillingPayload) error {
	if p == nil || p.bus == nil {
		return nil
	}
	evt, err := eventbus.NewBillingEvent(
		eventbus.EventType(eventType),
		source,
		occurredAt,
		eventbus.BillingPayload{
			Version:              payload.Version,
			TenantID:             payload.TenantID,
			SubscriptionID:       payload.SubscriptionID,
			PlanID:               payload.PlanID,
			StripeSubscriptionID: payload.StripeSubscriptionID,
			State:                payload.State,
			InvoiceID:            payload.InvoiceID,
			Amount:               payload.Amount,
			Currency:             payload.Currency,
		},
	)
	if err != nil {
		return err
	}
	return p.bus.Publish(ctx, evt)
}

// NoopPublisher is a stub EventPublisher for tests and dev mode.
type NoopPublisher struct{}

// PublishBilling implements EventPublisher.
func (NoopPublisher) PublishBilling(_ context.Context, _, _ string, _ time.Time, _ BillingPayload) error {
	return nil
}
