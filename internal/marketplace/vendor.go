// File scope: v4.8.0 Story 3 -- marketplace multi-vendor support.
//
// Vendor is the domain model for third-party sellers within a
// tenant's marketplace. Each vendor has an independent product
// catalogue, commission rate, and lifecycle.
//
// Decomposition discipline (HARD GATE: complex_fn=4):
//   - NewVendor       -> validate + construct (cyclomatic 3)
//   - Deactivate      -> state guard (cyclomatic 2)
//   - UpdateCommission -> validate + set (cyclomatic 2)
package marketplace

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrVendorNameEmpty       = errors.New("marketplace: vendor name empty")
	ErrVendorContactEmpty    = errors.New("marketplace: vendor contact empty")
	ErrVendorAlreadyInactive = errors.New("marketplace: vendor already deactivated")
	ErrVendorNotFound        = errors.New("marketplace: vendor not found")
	ErrInvalidCommissionRate = errors.New("marketplace: invalid commission rate")
)

type VendorStatus string

const (
	VendorStatusActive      VendorStatus = "active"
	VendorStatusSuspended   VendorStatus = "suspended"
	VendorStatusDeactivated VendorStatus = "deactivated"
)

type Vendor struct {
	VendorID          string       `json:"vendor_id"`
	TenantID          string       `json:"tenant_id"`
	Name              string       `json:"name"`
	ContactEmail      string       `json:"contact_email"`
	Status            VendorStatus `json:"status"`
	CommissionRateBPS int          `json:"commission_rate_bps"`
	JoinedAt          time.Time    `json:"joined_at"`
	DeactivatedAt     *time.Time   `json:"deactivated_at,omitempty"`
}

func NewVendor(vendorID, tenantID, name, contact string, commissionBPS int, now time.Time) (Vendor, error) {
	if name == "" {
		return Vendor{}, ErrVendorNameEmpty
	}
	if contact == "" {
		return Vendor{}, ErrVendorContactEmpty
	}
	if commissionBPS < 0 || commissionBPS > 10000 {
		return Vendor{}, fmt.Errorf("%w: %d bps", ErrInvalidCommissionRate, commissionBPS)
	}
	return Vendor{
		VendorID:          vendorID,
		TenantID:          tenantID,
		Name:              name,
		ContactEmail:      contact,
		Status:            VendorStatusActive,
		CommissionRateBPS: commissionBPS,
		JoinedAt:          now,
	}, nil
}

func (v *Vendor) Deactivate(now time.Time) error {
	if v.Status == VendorStatusDeactivated {
		return ErrVendorAlreadyInactive
	}
	v.Status = VendorStatusDeactivated
	v.DeactivatedAt = &now
	return nil
}

func (v *Vendor) UpdateCommission(bps int) error {
	if bps < 0 || bps > 10000 {
		return fmt.Errorf("%w: %d bps", ErrInvalidCommissionRate, bps)
	}
	v.CommissionRateBPS = bps
	return nil
}
