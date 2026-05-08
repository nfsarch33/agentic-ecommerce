package registration

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInMemoryRepositoryRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	r, _ := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	if err := repo.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, r); !errors.Is(err, ErrRequestAlreadyExists) {
		t.Fatalf("expected duplicate id rejection, got %v", err)
	}
	got, err := repo.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Email != r.Email {
		t.Fatalf("email = %s", got.Email)
	}
	gotEmail, err := repo.GetByEmail(ctx, r.Email)
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if gotEmail.ID != r.ID {
		t.Fatalf("email lookup id mismatch")
	}
}

func TestInMemoryRepositoryNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	if _, err := repo.Get(ctx, "missing"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("expected ErrRequestNotFound, got %v", err)
	}
	if _, err := repo.GetByEmail(ctx, "missing@example.com"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("expected ErrRequestNotFound, got %v", err)
	}
	if err := repo.Save(ctx, Request{ID: "missing"}); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("expected ErrRequestNotFound on Save, got %v", err)
	}
}

func TestInMemoryRepositoryReregisterPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	r1, _ := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	if err := repo.Create(ctx, r1); err != nil {
		t.Fatalf("Create r1: %v", err)
	}
	r2, _ := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	if err := repo.Create(ctx, r2); err != nil {
		t.Fatalf("Create r2 (pending allowed): %v", err)
	}
}

func TestInMemoryRepositoryActiveBlocksReregister(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	r1, _ := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	r1.Status = StatusActive
	if err := repo.Create(ctx, r1); err != nil {
		t.Fatalf("Create r1: %v", err)
	}
	r2, _ := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-b"}, time.Hour)
	if err := repo.Create(ctx, r2); !errors.Is(err, ErrRequestAlreadyExists) {
		t.Fatalf("expected ErrRequestAlreadyExists, got %v", err)
	}
}

func TestInMemoryRepositoryListPaginates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	for i := 0; i < 3; i++ {
		r, _ := NewRequest(SubmitInput{Email: makeEmail(i), SlugRequested: makeSlug(i)}, time.Hour)
		if err := repo.Create(ctx, r); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	rows, total, err := repo.List(ctx, 1, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d", total)
	}
	if len(rows) != 2 {
		t.Fatalf("page size = %d", len(rows))
	}
}

func makeEmail(i int) string {
	return string(rune('a'+i)) + "@example.com"
}

func makeSlug(i int) string {
	return "tenant-" + string(rune('a'+i))
}
