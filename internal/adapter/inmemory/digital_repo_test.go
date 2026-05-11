package inmemory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/digital"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

func newProductHelper(t *testing.T, tenant string) digital.DigitalProduct {
	t.Helper()
	p, err := digital.NewDigitalProduct(digital.DigitalProductInput{
		TenantID: tenant,
		SKU:      "PDF-x",
		Name:     "Sample",
		FilePath: "tenant/digital/x.pdf",
		FileSize: 1024,
		Version:  "1",
		Now:      time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("new product: %v", err)
	}
	return p
}

func TestDigitalProductRepoCRUD(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewDigitalProductRepository()
	ctx := context.Background()
	p := newProductHelper(t, "tenant-a")
	if err := repo.Create(ctx, "tenant-a", p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(ctx, "tenant-a", p.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != p.ID() {
		t.Fatalf("got id = %v, want %v", got.ID(), p.ID())
	}
	// Tenant isolation: another tenant cannot read.
	if _, err := repo.Get(ctx, "tenant-b", p.ID()); !errors.Is(err, port.ErrDigitalProductNotFound) {
		t.Fatalf("cross-tenant Get err = %v, want ErrDigitalProductNotFound", err)
	}
	// Update.
	if err := got.Update(digital.DigitalProductInput{Name: "Renamed"}, time.Now().UTC()); err != nil {
		t.Fatalf("update domain: %v", err)
	}
	if err := repo.Update(ctx, "tenant-a", got); err != nil {
		t.Fatalf("repo Update: %v", err)
	}
	again, _ := repo.Get(ctx, "tenant-a", p.ID())
	if again.Name() != "Renamed" {
		t.Fatalf("name = %q, want Renamed", again.Name())
	}
	// List.
	list, err := repo.List(ctx, "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("total = %d, want 1", list.Total)
	}
	// Delete.
	if err := repo.Delete(ctx, "tenant-a", p.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, "tenant-a", p.ID()); !errors.Is(err, port.ErrDigitalProductNotFound) {
		t.Fatalf("post-delete Get: %v", err)
	}
	// Update missing.
	if err := repo.Update(ctx, "tenant-a", got); !errors.Is(err, port.ErrDigitalProductNotFound) {
		t.Fatalf("Update missing: %v", err)
	}
	// Delete missing.
	if err := repo.Delete(ctx, "tenant-a", p.ID()); !errors.Is(err, port.ErrDigitalProductNotFound) {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestLicenseRepoStateMachineSafe(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewLicenseRepository()
	ctx := context.Background()
	lic, err := digital.NewLicense(digital.LicenseInput{
		TenantID:   "tenant-a",
		ProductID:  uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		CustomerID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		Key:        "AAAAA-BBBBB-CCCCC-DDDDD-EEEEEEEE",
	})
	if err != nil {
		t.Fatalf("NewLicense: %v", err)
	}
	if err := repo.Create(ctx, "tenant-a", lic); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Tenant isolation.
	if _, err := repo.Get(ctx, "tenant-b", lic.ID()); !errors.Is(err, port.ErrLicenseNotFound) {
		t.Fatalf("cross-tenant Get: %v", err)
	}
	// Apply revoke and persist.
	if err := lic.Apply(digital.TransitionRevoke, time.Now()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := repo.SaveState(ctx, "tenant-a", lic); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	stored, _ := repo.Get(ctx, "tenant-a", lic.ID())
	if stored.State() != digital.StateRevoked {
		t.Fatalf("state = %q, want revoked", stored.State())
	}
	// SaveState on missing licence is an error.
	other, _ := digital.NewLicense(digital.LicenseInput{
		TenantID:   "tenant-c",
		ProductID:  uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		CustomerID: uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"),
		Key:        "ZZZZZ-ZZZZZ-ZZZZZ-ZZZZZ-ZZZZZZZZ",
	})
	if err := repo.SaveState(ctx, "tenant-c", other); !errors.Is(err, port.ErrLicenseNotFound) {
		t.Fatalf("missing SaveState: %v", err)
	}
	// ListByCustomer.
	page, _ := repo.ListByCustomer(ctx, "tenant-a", lic.CustomerID(), 1, 10)
	if page.Total != 1 {
		t.Fatalf("ListByCustomer total = %d, want 1", page.Total)
	}
	all, err := repo.List(ctx, "tenant-a", 0, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if all.Total != 1 || len(all.Licenses) != 1 {
		t.Fatalf("List total=%d len=%d, want 1/1", all.Total, len(all.Licenses))
	}
	empty, err := repo.List(ctx, "tenant-a", 99, 1)
	if err != nil {
		t.Fatalf("List empty page: %v", err)
	}
	if empty.Total != 1 || len(empty.Licenses) != 0 {
		t.Fatalf("List empty total=%d len=%d, want 1/0", empty.Total, len(empty.Licenses))
	}
}

func TestAccessGrantRepoUpsertIdempotent(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewAccessGrantRepository()
	ctx := context.Background()
	customerID := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	productID := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	g1, err := digital.NewAccessGrant(digital.AccessGrantInput{
		TenantID: "tenant-a", CustomerID: customerID, ProductID: productID,
		LicenseID: uuid.New(), Source: digital.SourcePurchase,
	})
	if err != nil {
		t.Fatalf("NewAccessGrant: %v", err)
	}
	if err := repo.Upsert(ctx, "tenant-a", g1); err != nil {
		t.Fatalf("Upsert 1: %v", err)
	}
	// Upsert with the same (customer, product) replaces.
	g2, _ := digital.NewAccessGrant(digital.AccessGrantInput{
		TenantID: "tenant-a", CustomerID: customerID, ProductID: productID,
		LicenseID: uuid.New(), Source: digital.SourceGift,
	})
	if err := repo.Upsert(ctx, "tenant-a", g2); err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}
	got, err := repo.GetByCustomerProduct(ctx, "tenant-a", customerID, productID)
	if err != nil {
		t.Fatalf("GetByCustomerProduct: %v", err)
	}
	if got.LicenseID() != g2.LicenseID() {
		t.Fatalf("upsert did not replace")
	}
	if got.Source() != digital.SourceGift {
		t.Fatalf("source not updated, got %q", got.Source())
	}
	byID, err := repo.Get(ctx, "tenant-a", got.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if byID.ProductID() != productID {
		t.Fatalf("Get product=%v, want %v", byID.ProductID(), productID)
	}
	if _, err := repo.Get(ctx, "tenant-a", uuid.New()); !errors.Is(err, port.ErrAccessGrantNotFound) {
		t.Fatalf("missing Get: %v", err)
	}
	// Tenant isolation.
	if _, err := repo.GetByCustomerProduct(ctx, "tenant-b", customerID, productID); !errors.Is(err, port.ErrAccessGrantNotFound) {
		t.Fatalf("cross-tenant grant: %v", err)
	}
	// ListByCustomer.
	page, _ := repo.ListByCustomer(ctx, "tenant-a", customerID, 1, 10)
	if page.Total != 1 {
		t.Fatalf("ListByCustomer total = %d, want 1", page.Total)
	}
	empty, err := repo.ListByCustomer(ctx, "tenant-a", customerID, 99, 1)
	if err != nil {
		t.Fatalf("ListByCustomer empty page: %v", err)
	}
	if empty.Total != 1 || len(empty.Grants) != 0 {
		t.Fatalf("ListByCustomer empty total=%d len=%d, want 1/0", empty.Total, len(empty.Grants))
	}
}
