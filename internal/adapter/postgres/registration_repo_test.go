package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/agentic-ecommerce/internal/registration"
)

func fakeRegistrationRow(req registration.Request) fakeRow {
	var ver, onb, act *time.Time
	if !req.VerifiedAt.IsZero() {
		t := req.VerifiedAt
		ver = &t
	}
	if !req.OnboardedAt.IsZero() {
		t := req.OnboardedAt
		onb = &t
	}
	if !req.ActivatedAt.IsZero() {
		t := req.ActivatedAt
		act = &t
	}
	return fakeRow{values: []any{
		req.ID, req.Email, req.SlugRequested, req.PlanRequested,
		string(req.Status), req.TenantID, req.CompanyName,
		req.CreatedAt, ver, onb, act, req.ExpiresAt,
	}}
}

func TestRegistrationRepositoryCreate(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &RegistrationRepository{pool: pool}
	r, _ := registration.NewRequest(registration.SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	if err := repo.Create(context.Background(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestRegistrationRepositoryCreateUnique(t *testing.T) {
	t.Parallel()
	pool := &fakePool{execErr: errors.New("ERROR: 23505 duplicate key")}
	repo := &RegistrationRepository{pool: pool}
	r, _ := registration.NewRequest(registration.SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	if err := repo.Create(context.Background(), r); !errors.Is(err, registration.ErrRequestAlreadyExists) {
		t.Fatalf("expected ErrRequestAlreadyExists, got %v", err)
	}
}

func TestRegistrationRepositoryGet(t *testing.T) {
	t.Parallel()
	r, _ := registration.NewRequest(registration.SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	pool := &fakePool{row: fakeRegistrationRow(r)}
	repo := &RegistrationRepository{pool: pool}
	got, err := repo.Get(context.Background(), r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SlugRequested != "tenant-a" {
		t.Fatalf("slug = %s", got.SlugRequested)
	}
}

func TestRegistrationRepositoryGetNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &RegistrationRepository{pool: pool}
	if _, err := repo.Get(context.Background(), "x"); !errors.Is(err, registration.ErrRequestNotFound) {
		t.Fatalf("expected ErrRequestNotFound, got %v", err)
	}
	if _, err := repo.GetByEmail(context.Background(), "x@x.com"); !errors.Is(err, registration.ErrRequestNotFound) {
		t.Fatalf("expected ErrRequestNotFound, got %v", err)
	}
}

func TestRegistrationRepositorySaveNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 0")}
	repo := &RegistrationRepository{pool: pool}
	r, _ := registration.NewRequest(registration.SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	if err := repo.Save(context.Background(), r); !errors.Is(err, registration.ErrRequestNotFound) {
		t.Fatalf("expected ErrRequestNotFound, got %v", err)
	}
}
