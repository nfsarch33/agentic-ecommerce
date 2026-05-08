package notification

import (
	"context"
	"errors"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// MultiSender fans out a MembershipNotificationEvent to multiple
// downstream senders. Errors are joined with errors.Join so callers see
// every failure in a single error value (Go 1.20+).
type MultiSender struct {
	senders []port.MembershipNotificationSender
}

// NewMultiSender returns a sender that fans out to the supplied
// senders. Nil senders are skipped so callers can pass optional
// adapters without nil checks at call time.
func NewMultiSender(senders ...port.MembershipNotificationSender) *MultiSender {
	filtered := make([]port.MembershipNotificationSender, 0, len(senders))
	for _, s := range senders {
		if s == nil {
			continue
		}
		filtered = append(filtered, s)
	}
	return &MultiSender{senders: filtered}
}

// SendMembershipEvent invokes every sender in registration order. The
// first failure does not short-circuit so every sink sees the event.
func (m *MultiSender) SendMembershipEvent(ctx context.Context, evt port.MembershipNotificationEvent) error {
	if m == nil || len(m.senders) == 0 {
		return nil
	}
	errs := make([]error, 0, len(m.senders))
	for _, s := range m.senders {
		if err := s.SendMembershipEvent(ctx, evt); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
