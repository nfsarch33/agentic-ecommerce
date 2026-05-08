package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/registration"
)

func TestRegistrationRepositoryList(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	pool := &fakePool{
		row: fakeRow{values: []any{1}},
		rows: &fakeRows{rows: [][]any{{
			"reg_1", "alice@example.com", "tenant-a", "free", string(registration.StatusEmailVerified),
			"", "", now, timePtr(now), nil, nil, now.Add(time.Hour),
		}}},
	}
	repo := &RegistrationRepository{pool: pool}
	rows, total, err := repo.List(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d", total)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
}

func TestRegistrationRepositoryGetByEmail(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	r, _ := registration.NewRequest(registration.SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a", Now: now}, time.Hour)
	pool := &fakePool{row: fakeRegistrationRow(r)}
	repo := &RegistrationRepository{pool: pool}
	got, err := repo.GetByEmail(context.Background(), r.Email)
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got.Email != r.Email {
		t.Fatalf("email = %s", got.Email)
	}
}

func TestRegistrationRepositoryListCountErr(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: errors.New("boom")}}
	repo := &RegistrationRepository{pool: pool}
	if _, _, err := repo.List(context.Background(), 1, 20); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRegistrationConstructors(t *testing.T) {
	t.Parallel()
	if NewBillingRepository(nil) == nil {
		t.Fatalf("NewBillingRepository nil")
	}
	if NewRegistrationRepository(nil) == nil {
		t.Fatalf("NewRegistrationRepository nil")
	}
}
