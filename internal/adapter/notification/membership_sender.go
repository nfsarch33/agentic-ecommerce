// Package notification provides the in-process stub adapter for the
// membership bounded context's NotificationSender port. Production
// adapters dispatch to email/SMS/n8n; the stub records events for tests
// and the dev compose stack.
package notification

import (
	"context"
	"sync"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// MembershipNotificationRecorder is an in-process MembershipNotificationSender
// that captures every event for inspection.
type MembershipNotificationRecorder struct {
	mu     sync.RWMutex
	events []port.MembershipNotificationEvent
}

// NewMembershipNotificationRecorder returns a fresh recorder.
func NewMembershipNotificationRecorder() *MembershipNotificationRecorder {
	return &MembershipNotificationRecorder{}
}

// SendMembershipEvent appends the event to the recorder.
func (r *MembershipNotificationRecorder) SendMembershipEvent(_ context.Context, evt port.MembershipNotificationEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, evt)
	return nil
}

// Events returns a defensive copy of the recorded events.
func (r *MembershipNotificationRecorder) Events() []port.MembershipNotificationEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]port.MembershipNotificationEvent, len(r.events))
	copy(out, r.events)
	return out
}
