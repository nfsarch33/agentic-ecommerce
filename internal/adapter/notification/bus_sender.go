package notification

import (
	"context"
	"fmt"

	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// BusSender is a MembershipNotificationSender that publishes lifecycle
// events to the shared eventbus.Publisher. It is wired alongside the
// in-memory Recorder so the workflow path emits the same eventbus
// signals as the handler path.
type BusSender struct {
	publisher eventbus.Publisher
	source    string
}

// NewBusSender returns a BusSender that publishes to the supplied bus.
// Source defaults to "workflow.membership.notifier" when empty so audit
// trails show the producer.
func NewBusSender(publisher eventbus.Publisher, source string) *BusSender {
	if source == "" {
		source = "workflow.membership.notifier"
	}
	return &BusSender{publisher: publisher, source: source}
}

// SendMembershipEvent maps the notification port event onto the typed
// eventbus envelope and publishes it. Unrecognised transitions are
// dropped silently to keep the workflow path forgiving; production
// observability comes from the eventbus health check, not from a panic
// here.
func (b *BusSender) SendMembershipEvent(ctx context.Context, evt port.MembershipNotificationEvent) error {
	if b == nil || b.publisher == nil {
		return nil
	}
	eventType, ok := transitionToEventType(evt.Transition, evt.State)
	if !ok {
		return nil
	}
	payload := eventbus.MembershipPayload{
		TenantID:       evt.TenantID,
		SubscriptionID: evt.SubscriptionID.String(),
		MemberID:       evt.MemberID.String(),
		MemberEmail:    evt.MemberEmail,
		PlanID:         evt.PlanID.String(),
		PlanName:       evt.PlanName,
		State:          string(evt.State),
	}
	out, err := eventbus.NewMembershipEvent(eventType, b.source, evt.OccurredAt, payload)
	if err != nil {
		return fmt.Errorf("build membership event: %w", err)
	}
	return b.publisher.Publish(ctx, out)
}

// transitionToEventType maps the domain transition (and resulting state)
// onto the eventbus EventType. Returns ok=false when the combination
// does not produce a published event (e.g. activation while still in
// trial state, which is internal-only).
func transitionToEventType(transition membership.Transition, state membership.State) (eventbus.EventType, bool) {
	switch transition {
	case membership.TransitionActivate:
		// The first activation event is published as membership.created
		// when we leave the trial+initial-charge step. Subsequent
		// activations (e.g. after resume) are routed via TransitionResume.
		if state == membership.StateActive || state == membership.StateTrial {
			return eventbus.MembershipCreated, true
		}
		return "", false
	case membership.TransitionRenew:
		return eventbus.MembershipRenewed, true
	case membership.TransitionCancel:
		return eventbus.MembershipCancelled, true
	case membership.TransitionPause:
		return eventbus.MembershipPaused, true
	case membership.TransitionResume:
		return eventbus.MembershipResumed, true
	default:
		return "", false
	}
}
