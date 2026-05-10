package marketplace

import (
	"context"
	"testing"
	"time"
)

func TestVendor_CreateAndGet(t *testing.T) {
	t.Parallel()
	store := NewInMemoryVendorStore()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	v, err := NewVendor("v1", "t1", "Vendor Alpha", "alpha@example.com", 1500, now)
	if err != nil {
		t.Fatalf("NewVendor: %v", err)
	}
	if err := store.Create(context.Background(), v); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(context.Background(), "t1", "v1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Vendor Alpha" {
		t.Fatalf("name = %s, want Vendor Alpha", got.Name)
	}
	if got.CommissionRateBPS != 1500 {
		t.Fatalf("commission = %d, want 1500", got.CommissionRateBPS)
	}
	if got.Status != VendorStatusActive {
		t.Fatalf("status = %s, want active", got.Status)
	}
}

func TestVendor_ListByTenant(t *testing.T) {
	t.Parallel()
	store := NewInMemoryVendorStore()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	v1, _ := NewVendor("v1", "t1", "Vendor A", "a@x.com", 1000, now)
	v2, _ := NewVendor("v2", "t1", "Vendor B", "b@x.com", 2000, now)
	v3, _ := NewVendor("v3", "t2", "Vendor C", "c@x.com", 500, now)
	_ = store.Create(context.Background(), v1)
	_ = store.Create(context.Background(), v2)
	_ = store.Create(context.Background(), v3)

	t1Vendors, err := store.List(context.Background(), "t1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(t1Vendors) != 2 {
		t.Fatalf("t1 vendors = %d, want 2", len(t1Vendors))
	}

	t2Vendors, _ := store.List(context.Background(), "t2")
	if len(t2Vendors) != 1 {
		t.Fatalf("t2 vendors = %d, want 1", len(t2Vendors))
	}
}

func TestVendor_Deactivate(t *testing.T) {
	t.Parallel()
	store := NewInMemoryVendorStore()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	v, _ := NewVendor("v1", "t1", "Vendor A", "a@x.com", 1000, now)
	_ = store.Create(context.Background(), v)

	if err := store.Deactivate(context.Background(), "t1", "v1"); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	got, _ := store.Get(context.Background(), "t1", "v1")
	if got.Status != VendorStatusDeactivated {
		t.Fatalf("status = %s, want deactivated", got.Status)
	}
}

func TestVendor_ProductAssociation(t *testing.T) {
	t.Parallel()
	store := NewInMemoryVendorStore()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	v, _ := NewVendor("v1", "t1", "Vendor A", "a@x.com", 1000, now)
	_ = store.Create(context.Background(), v)

	got, _ := store.Get(context.Background(), "t1", "v1")
	if got.VendorID != "v1" {
		t.Fatalf("vendor_id = %s, want v1", got.VendorID)
	}
	if got.TenantID != "t1" {
		t.Fatalf("tenant_id = %s, want t1 (product isolation by tenant+vendor)", got.TenantID)
	}
}

func TestVendor_Isolation(t *testing.T) {
	t.Parallel()
	store := NewInMemoryVendorStore()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	v1, _ := NewVendor("v1", "t1", "Vendor A", "a@x.com", 1000, now)
	_ = store.Create(context.Background(), v1)

	_, err := store.Get(context.Background(), "t2", "v1")
	if err == nil {
		t.Fatal("expected error: vendor v1 belongs to t1, not t2 (tenant isolation)")
	}
}
