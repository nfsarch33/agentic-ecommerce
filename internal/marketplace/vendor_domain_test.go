// File scope: v6.1.0 coverage backfill -- the marketplace.Vendor
// domain object had its Deactivate and UpdateCommission methods at
// 0% (only the repository-side TestVendor_* tests in vendor_test.go
// exist, and they exercise the repo not the domain methods).
package marketplace

import (
	"errors"
	"testing"
	"time"
)

func TestVendorDomainNewVendorValidationMatrix(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name           string
		vendorID       string
		tenantID       string
		vendorName     string
		contact        string
		commissionBPS  int
		wantErr        error
		wantErrMessage bool
	}{
		{name: "ok", vendorID: "v-1", tenantID: "t-1", vendorName: "Acme", contact: "ops@acme.test", commissionBPS: 1500},
		{name: "empty name", vendorID: "v-2", tenantID: "t-1", vendorName: "", contact: "ops@acme.test", commissionBPS: 1500, wantErr: ErrVendorNameEmpty},
		{name: "empty contact", vendorID: "v-3", tenantID: "t-1", vendorName: "Acme", contact: "", commissionBPS: 1500, wantErr: ErrVendorContactEmpty},
		{name: "negative bps", vendorID: "v-4", tenantID: "t-1", vendorName: "Acme", contact: "ops@acme.test", commissionBPS: -1, wantErr: ErrInvalidCommissionRate},
		{name: "bps above max", vendorID: "v-5", tenantID: "t-1", vendorName: "Acme", contact: "ops@acme.test", commissionBPS: 10001, wantErr: ErrInvalidCommissionRate},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v, err := NewVendor(tc.vendorID, tc.tenantID, tc.vendorName, tc.contact, tc.commissionBPS, now)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if v.Status != VendorStatusActive {
				t.Fatalf("Status = %q, want active", v.Status)
			}
			if v.CommissionRateBPS != tc.commissionBPS {
				t.Fatalf("CommissionRateBPS = %d, want %d", v.CommissionRateBPS, tc.commissionBPS)
			}
			if !v.JoinedAt.Equal(now) {
				t.Fatalf("JoinedAt = %v, want %v", v.JoinedAt, now)
			}
		})
	}
}

func TestVendorDomainDeactivateOnceThenReject(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	v, err := NewVendor("v-d", "t-1", "Acme", "ops@acme.test", 1500, now)
	if err != nil {
		t.Fatalf("NewVendor: %v", err)
	}
	deactivatedAt := now.Add(time.Hour)
	if err := v.Deactivate(deactivatedAt); err != nil {
		t.Fatalf("Deactivate first: %v", err)
	}
	if v.Status != VendorStatusDeactivated {
		t.Fatalf("Status = %q, want deactivated", v.Status)
	}
	if v.DeactivatedAt == nil || !v.DeactivatedAt.Equal(deactivatedAt) {
		t.Fatalf("DeactivatedAt = %v, want %v", v.DeactivatedAt, deactivatedAt)
	}
	if err := v.Deactivate(deactivatedAt.Add(time.Hour)); !errors.Is(err, ErrVendorAlreadyInactive) {
		t.Fatalf("Deactivate second: err = %v, want ErrVendorAlreadyInactive", err)
	}
}

func TestVendorDomainUpdateCommission(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	v, err := NewVendor("v-c", "t-1", "Acme", "ops@acme.test", 1500, now)
	if err != nil {
		t.Fatalf("NewVendor: %v", err)
	}
	if err := v.UpdateCommission(2500); err != nil {
		t.Fatalf("UpdateCommission ok: %v", err)
	}
	if v.CommissionRateBPS != 2500 {
		t.Fatalf("CommissionRateBPS = %d, want 2500", v.CommissionRateBPS)
	}
	if err := v.UpdateCommission(-5); !errors.Is(err, ErrInvalidCommissionRate) {
		t.Fatalf("UpdateCommission negative: err = %v, want ErrInvalidCommissionRate", err)
	}
	if err := v.UpdateCommission(10001); !errors.Is(err, ErrInvalidCommissionRate) {
		t.Fatalf("UpdateCommission too-high: err = %v, want ErrInvalidCommissionRate", err)
	}
}
