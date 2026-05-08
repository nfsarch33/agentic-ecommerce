package tenant

import (
	"context"
	"errors"
	"testing"
	"time"
)

// File scope: targeted negative-case coverage for the tenant Service so
// repository-side errors that are not ErrSettingsNotFound surface to
// callers, and zero-valued UpdatedAt is auto-stamped via the configured
// clock.

type stubTenantRepo struct {
	getErr   error
	settings Settings
	putCalls int
	putErr   error
	put      Settings
}

func (r *stubTenantRepo) GetSettings(_ context.Context, id ID) (Settings, error) {
	if r.getErr != nil {
		return Settings{}, r.getErr
	}
	settings := r.settings
	settings.TenantID = id
	return settings, nil
}

func (r *stubTenantRepo) PutSettings(_ context.Context, settings Settings) error {
	r.putCalls++
	r.put = settings
	return r.putErr
}

func TestServiceGetSettingsSurfaceUnexpectedRepositoryError(t *testing.T) {
	t.Parallel()

	want := errors.New("dial tcp: connection refused")
	service := NewService(&stubTenantRepo{getErr: want})
	if _, err := service.GetSettings(context.Background(), "tenant-x"); !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapped %v", err, want)
	}
}

func TestServicePutSettingsSurfacesRepositoryError(t *testing.T) {
	t.Parallel()

	want := errors.New("postgres timeout")
	repo := &stubTenantRepo{putErr: want}
	service := NewService(repo)

	err := service.PutSettings(context.Background(), Settings{TenantID: "tenant-x"})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapped %v", err, want)
	}
	if repo.putCalls != 1 {
		t.Fatalf("put calls = %d, want 1", repo.putCalls)
	}
}

func TestServicePutSettingsRequiresRepository(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	if err := service.PutSettings(context.Background(), Settings{TenantID: "tenant-x"}); err == nil {
		t.Fatal("PutSettings accepted nil repository")
	}
}

func TestServicePutSettingsAutoStampsUpdatedAtFromInjectedClock(t *testing.T) {
	t.Parallel()

	repo := &stubTenantRepo{}
	service := NewService(repo)
	frozen := time.Date(2026, 5, 8, 9, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return frozen }

	settings := Settings{TenantID: "tenant-x"}
	if err := service.PutSettings(context.Background(), settings); err != nil {
		t.Fatalf("PutSettings: %v", err)
	}
	if !repo.put.UpdatedAt.Equal(frozen) {
		t.Fatalf("UpdatedAt = %s, want %s (auto-stamped from injected clock)", repo.put.UpdatedAt, frozen)
	}
	if repo.put.TenantID != "tenant-x" {
		t.Fatalf("TenantID = %q", repo.put.TenantID)
	}
}

func TestServicePutSettingsPreservesProvidedUpdatedAt(t *testing.T) {
	t.Parallel()

	repo := &stubTenantRepo{}
	service := NewService(repo)
	when := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	settings := Settings{TenantID: "tenant-x", UpdatedAt: when}
	if err := service.PutSettings(context.Background(), settings); err != nil {
		t.Fatalf("PutSettings: %v", err)
	}
	if !repo.put.UpdatedAt.Equal(when) {
		t.Fatalf("UpdatedAt = %s, want %s (preserved)", repo.put.UpdatedAt, when)
	}
}

func TestRequireIDPreservesAlreadyTrimmedValue(t *testing.T) {
	t.Parallel()

	id, err := RequireID("tenant-x")
	if err != nil {
		t.Fatalf("RequireID: %v", err)
	}
	if id != "tenant-x" {
		t.Fatalf("id = %q, want tenant-x", id)
	}
}

func TestRequireIDRejectsTabAndNewlineWhitespace(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"\t", "\n", "  \t  \n", ""} {
		if _, err := RequireID(ID(raw)); !errors.Is(err, ErrTenantRequired) {
			t.Fatalf("RequireID(%q) = %v, want ErrTenantRequired", raw, err)
		}
	}
}

func TestInMemoryRepositoryEnforcesTenantOnRead(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryRepository()
	if _, err := repo.GetSettings(context.Background(), ""); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("GetSettings empty err = %v, want ErrTenantRequired", err)
	}
	if err := repo.PutSettings(context.Background(), Settings{}); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("PutSettings empty err = %v, want ErrTenantRequired", err)
	}
}

func TestInMemoryRepositoryReturnsNotFoundForUnknownTenant(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryRepository()
	if _, err := repo.GetSettings(context.Background(), "ghost"); !errors.Is(err, ErrSettingsNotFound) {
		t.Fatalf("err = %v, want ErrSettingsNotFound", err)
	}
}
