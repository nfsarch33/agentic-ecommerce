// File scope: v3.9.1 EC-9-5 -- in-memory OperatorAlertRepository
// implementation used by tests + the cmd/* dev composition root.
//
// Production wires a Postgres-backed implementation that persists
// to the operator_alerts table (migration 0025). The in-memory
// implementation here mirrors the same lifecycle semantics so the
// handler tests cover the full state machine without spinning up
// testcontainers.
//
// Concurrency: the repository is goroutine-safe so the EC-9-5 sweeper
// can run alongside the handler.
package handler

import (
	"context"
	"sync"
	"time"
)

// InMemoryOperatorAlertRepository is the in-memory implementation
// of OperatorAlertRepository.
type InMemoryOperatorAlertRepository struct {
	mu     sync.Mutex
	alerts map[string]OperatorAlert
}

// NewInMemoryOperatorAlertRepository returns a fresh, empty
// repository.
func NewInMemoryOperatorAlertRepository() *InMemoryOperatorAlertRepository {
	return &InMemoryOperatorAlertRepository{alerts: map[string]OperatorAlert{}}
}

func alertKey(tenantID, alertID string) string {
	return tenantID + "/" + alertID
}

// Insert appends a new alert. Existing rows are overwritten so the
// caller can use Insert for both upsert + initial seed.
func (r *InMemoryOperatorAlertRepository) Insert(_ context.Context, alert OperatorAlert) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if alert.Status == "" {
		alert.Status = AlertStatusPending
	}
	if alert.Severity == "" {
		alert.Severity = AlertSeverityWarning
	}
	if alert.CreatedAt.IsZero() {
		alert.CreatedAt = time.Now().UTC()
	}
	if alert.ExpiresAt.IsZero() {
		alert.ExpiresAt = alert.CreatedAt.Add(DefaultAlertExpiryWindow)
	}
	r.alerts[alertKey(alert.TenantID, alert.AlertID)] = alert
	return nil
}

// List returns every alert for the tenant matching the supplied
// status. Status="" returns all rows.
func (r *InMemoryOperatorAlertRepository) List(_ context.Context, tenantID string, status AlertStatus) ([]OperatorAlert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]OperatorAlert, 0)
	for _, a := range r.alerts {
		if a.TenantID != tenantID {
			continue
		}
		if status != "" && a.Status != status {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// Get fetches a single alert by tenant + id.
func (r *InMemoryOperatorAlertRepository) Get(_ context.Context, tenantID, alertID string) (OperatorAlert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.alerts[alertKey(tenantID, alertID)]
	if !ok {
		return OperatorAlert{}, ErrAlertNotFound
	}
	return a, nil
}

// UpdateStatus transitions the alert state. action is recorded for
// resolved transitions and ignored otherwise.
func (r *InMemoryOperatorAlertRepository) UpdateStatus(_ context.Context, tenantID, alertID string, status AlertStatus, action string, occurredAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.alerts[alertKey(tenantID, alertID)]
	if !ok {
		return ErrAlertNotFound
	}
	a.Status = status
	if status == AlertStatusAcknowledged {
		a.AcknowledgedAt = occurredAt
	}
	if status == AlertStatusResolved {
		a.ResolvedAt = occurredAt
		a.ActionTaken = action
		if a.AcknowledgedAt.IsZero() {
			a.AcknowledgedAt = occurredAt
		}
	}
	r.alerts[alertKey(tenantID, alertID)] = a
	return nil
}

// ExpirePending walks pending+acknowledged rows whose ExpiresAt is
// before the supplied cutoff and flips them to expired. Returns the
// number of rows changed.
func (r *InMemoryOperatorAlertRepository) ExpirePending(_ context.Context, before time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := 0
	for k, a := range r.alerts {
		if a.Status != AlertStatusPending && a.Status != AlertStatusAcknowledged {
			continue
		}
		if a.ExpiresAt.Before(before) || a.ExpiresAt.Equal(before) {
			a.Status = AlertStatusExpired
			r.alerts[k] = a
			changed++
		}
	}
	return changed, nil
}
