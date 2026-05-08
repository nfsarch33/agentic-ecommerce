package registration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/tenant"
)

type fakeTenantProvisioner struct {
	created      tenant.Tenant
	createErr    error
	activated    tenant.Tenant
	activErr     error
	createdInput tenant.CreateTenantInput
}

func (f *fakeTenantProvisioner) Create(_ context.Context, in tenant.CreateTenantInput) (tenant.Tenant, error) {
	f.createdInput = in
	if f.createErr != nil {
		return tenant.Tenant{}, f.createErr
	}
	now := time.Now().UTC()
	t := tenant.Tenant{
		ID: tenant.ID(in.Slug), Slug: in.Slug, Name: in.Name, Plan: in.Plan,
		Status: tenant.StatusProvisioning, CreatedAt: now, UpdatedAt: now,
	}
	f.created = t
	return t, nil
}

func (f *fakeTenantProvisioner) Activate(_ context.Context, id tenant.ID) (tenant.Tenant, error) {
	if f.activErr != nil {
		return tenant.Tenant{}, f.activErr
	}
	t := f.created
	t.Status = tenant.StatusActive
	f.activated = t
	return t, nil
}

func newTestService(t *testing.T) (*Service, *InMemoryRepository, *Recorder, *fakeTenantProvisioner) {
	t.Helper()
	issuer, err := NewIssuer([]byte(testRegistrationSecret))
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	repo := NewInMemoryRepository()
	rec := NewRecorder()
	prov := &fakeTenantProvisioner{}
	svc, err := NewService(ServiceConfig{
		Repository: repo,
		Issuer:     issuer,
		Tenants:    prov,
		Notifier:   rec,
		TokenTTL:   time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, repo, rec, prov
}

func TestServiceSubmit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, rec, _ := newTestService(t)
	out, err := svc.Submit(ctx, SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if out.Token == "" {
		t.Fatalf("Submit returned empty token")
	}
	if got, _ := repo.Get(ctx, out.Request.ID); got.Status != StatusPendingEmailVerification {
		t.Fatalf("status = %s", got.Status)
	}
	events := rec.Events()
	if len(events) != 1 || events[0].Kind != NotificationRequested {
		t.Fatalf("recorder events = %+v", events)
	}
}

func TestServiceSubmitInvalid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _, _ := newTestService(t)
	if _, err := svc.Submit(ctx, SubmitInput{Email: "", SlugRequested: "tenant-a"}); !errors.Is(err, ErrEmailRequired) {
		t.Fatalf("expected ErrEmailRequired, got %v", err)
	}
}

func TestServiceVerify(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _, _ := newTestService(t)
	out, _ := svc.Submit(ctx, SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"})
	verified, err := svc.Verify(ctx, out.Token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified.Status != StatusEmailVerified {
		t.Fatalf("status = %s", verified.Status)
	}
}

func TestServiceVerifyTampered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _, _ := newTestService(t)
	out, _ := svc.Submit(ctx, SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"})
	tampered := out.Token[:len(out.Token)-1] + "x"
	if _, err := svc.Verify(ctx, tampered); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestServiceCompleteOnboarding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _, prov := newTestService(t)
	out, _ := svc.Submit(ctx, SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"})
	if _, err := svc.Verify(ctx, out.Token); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	final, t1, err := svc.CompleteOnboarding(ctx, out.Request.ID, "Acme Co", "starter")
	if err != nil {
		t.Fatalf("CompleteOnboarding: %v", err)
	}
	if final.Status != StatusActive {
		t.Fatalf("status = %s", final.Status)
	}
	if string(t1.ID) != "tenant-a" {
		t.Fatalf("tenant id = %s", t1.ID)
	}
	if prov.createdInput.Plan != "starter" {
		t.Fatalf("provisioner plan = %s", prov.createdInput.Plan)
	}
}

func TestServiceCompleteOnboardingRequiresVerification(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _, _ := newTestService(t)
	out, _ := svc.Submit(ctx, SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"})
	_, _, err := svc.CompleteOnboarding(ctx, out.Request.ID, "Acme", "free")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition before verify, got %v", err)
	}
}

func TestServiceCompleteOnboardingSlugTaken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _, prov := newTestService(t)
	prov.createErr = tenant.ErrTenantSlugAlreadyExists
	out, _ := svc.Submit(ctx, SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"})
	if _, err := svc.Verify(ctx, out.Token); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	_, _, err := svc.CompleteOnboarding(ctx, out.Request.ID, "Acme", "free")
	if !errors.Is(err, ErrSlugTaken) {
		t.Fatalf("expected ErrSlugTaken, got %v", err)
	}
}

func TestNewServiceValidation(t *testing.T) {
	t.Parallel()
	if _, err := NewService(ServiceConfig{}); err == nil {
		t.Fatalf("expected error for empty config")
	}
	if _, err := NewService(ServiceConfig{Repository: NewInMemoryRepository()}); err == nil {
		t.Fatalf("expected error without issuer")
	}
}

func TestServiceGet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _, _ := newTestService(t)
	out, _ := svc.Submit(ctx, SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"})
	got, err := svc.Get(ctx, out.Request.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Email != out.Request.Email {
		t.Fatalf("email = %s", got.Email)
	}
}
