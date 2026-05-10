// File scope: v3.9.1 Existing #10 -- in-memory OnboardingRepository
// implementation used by tests + the cmd/* dev composition root.
//
// Production wires a Postgres-backed implementation that persists to
// the onboarding_wizards table (migration 0023). The in-memory
// implementation here mirrors the same Get/Update semantics so the
// handler tests cover the full wizard state machine without spinning
// up testcontainers.
package handler

import (
	"context"
	"sync"
)

// InMemoryOnboardingRepository is the in-memory implementation of
// OnboardingRepository.
type InMemoryOnboardingRepository struct {
	mu      sync.Mutex
	wizards map[string]OnboardingWizard
}

// NewInMemoryOnboardingRepository returns a fresh, empty repository.
func NewInMemoryOnboardingRepository() *InMemoryOnboardingRepository {
	return &InMemoryOnboardingRepository{wizards: map[string]OnboardingWizard{}}
}

func wizardKey(tenantID, wizardID string) string {
	return tenantID + "/" + wizardID
}

// Create inserts a new wizard. Returns no error for duplicate keys
// so the cmd/* composition root can replay-safe insert; tests assert
// on the resulting Get state.
func (r *InMemoryOnboardingRepository) Create(_ context.Context, w OnboardingWizard) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wizards[wizardKey(w.TenantID, w.WizardID)] = w
	return nil
}

// Get returns the wizard for the supplied tenant + id.
func (r *InMemoryOnboardingRepository) Get(_ context.Context, tenantID, wizardID string) (OnboardingWizard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.wizards[wizardKey(tenantID, wizardID)]
	if !ok {
		return OnboardingWizard{}, ErrWizardNotFound
	}
	return w, nil
}

// Update writes the wizard state. Returns ErrWizardNotFound when
// the row does not exist.
func (r *InMemoryOnboardingRepository) Update(_ context.Context, w OnboardingWizard) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.wizards[wizardKey(w.TenantID, w.WizardID)]; !ok {
		return ErrWizardNotFound
	}
	r.wizards[wizardKey(w.TenantID, w.WizardID)] = w
	return nil
}
