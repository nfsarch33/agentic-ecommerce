package tenant_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/tenant"
)

func TestTenant_Create(t *testing.T) {
	t.Parallel()
	iso := tenant.NewIsolator()
	tn, err := iso.CreateTenant("T1", "Acme", "acme")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if tn.Slug != "acme" {
		t.Fatalf("expected slug acme, got %s", tn.Slug)
	}
}

func TestTenant_ResolveBySlug(t *testing.T) {
	t.Parallel()
	iso := tenant.NewIsolator()
	iso.CreateTenant("T2", "Beta Corp", "beta")
	tn, err := iso.ResolveTenant("beta")
	if err != nil {
		t.Fatalf("ResolveTenant: %v", err)
	}
	if tn.ID != "T2" {
		t.Fatalf("expected T2, got %s", tn.ID)
	}
}

func TestTenant_IsolateDataTagsTenant(t *testing.T) {
	t.Parallel()
	iso := tenant.NewIsolator()
	iso.CreateTenant("T3", "Gamma", "gamma")
	rec, err := iso.IsolateData("T3", map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("IsolateData: %v", err)
	}
	if rec.TenantID != "T3" {
		t.Fatalf("expected tenant T3 on record, got %s", rec.TenantID)
	}
}

func TestTenant_CrossTenantGuardBlocks(t *testing.T) {
	t.Parallel()
	iso := tenant.NewIsolator()
	if err := iso.CrossTenantGuard("T1", "T2"); err == nil {
		t.Fatal("expected cross-tenant error")
	}
}

func TestTenant_SameTenantGuardPasses(t *testing.T) {
	t.Parallel()
	iso := tenant.NewIsolator()
	if err := iso.CrossTenantGuard("T1", "T1"); err != nil {
		t.Fatalf("expected no error for same tenant, got %v", err)
	}
}

func TestTenant_DuplicateSlugError(t *testing.T) {
	t.Parallel()
	iso := tenant.NewIsolator()
	iso.CreateTenant("T4", "First", "dup")
	if _, err := iso.CreateTenant("T5", "Second", "dup"); err == nil {
		t.Fatal("expected duplicate slug error")
	}
}

func TestTenant_ResolveUnknownSlugError(t *testing.T) {
	t.Parallel()
	iso := tenant.NewIsolator()
	if _, err := iso.ResolveTenant("nobody"); err == nil {
		t.Fatal("expected tenant not found error")
	}
}
