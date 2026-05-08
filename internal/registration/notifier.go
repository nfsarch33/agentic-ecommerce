package registration

import (
	"context"
	"sync"
	"time"
)

// NotificationKind classifies recorder events.
type NotificationKind string

const (
	// NotificationRequested is emitted by Service.Submit.
	NotificationRequested NotificationKind = "registration.requested"
	// NotificationVerified is emitted by Service.Verify.
	NotificationVerified NotificationKind = "registration.verified"
)

// NotificationEvent is the captured shape recorded by Recorder.
type NotificationEvent struct {
	Kind        NotificationKind
	RequestID   string
	Email       string
	SlugRequest string
	Token       string
	OccurredAt  time.Time
}

// Recorder is a goroutine-safe in-process Notifier that captures
// every notification for inspection. Tests use Events() to assert
// behaviour; production wiring fans out to the email/n8n adapter.
type Recorder struct {
	mu     sync.RWMutex
	events []NotificationEvent
	now    func() time.Time
}

// NewRecorder returns a fresh recorder.
func NewRecorder() *Recorder {
	return &Recorder{now: func() time.Time { return time.Now().UTC() }}
}

// NotifyRegistrationRequested implements Notifier.
func (r *Recorder) NotifyRegistrationRequested(_ context.Context, req Request, token string) error {
	r.append(NotificationEvent{
		Kind:        NotificationRequested,
		RequestID:   req.ID,
		Email:       req.Email,
		SlugRequest: req.SlugRequested,
		Token:       token,
		OccurredAt:  r.now(),
	})
	return nil
}

// NotifyRegistrationVerified implements Notifier.
func (r *Recorder) NotifyRegistrationVerified(_ context.Context, req Request) error {
	r.append(NotificationEvent{
		Kind:        NotificationVerified,
		RequestID:   req.ID,
		Email:       req.Email,
		SlugRequest: req.SlugRequested,
		OccurredAt:  r.now(),
	})
	return nil
}

func (r *Recorder) append(evt NotificationEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, evt)
}

// Events returns a defensive copy of the captured events.
func (r *Recorder) Events() []NotificationEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NotificationEvent, len(r.events))
	copy(out, r.events)
	return out
}
