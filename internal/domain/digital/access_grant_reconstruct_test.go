// File scope: v6.1.0 coverage backfill -- AccessGrant accessors +
// ReconstructAccessGrant were previously uncovered (0%) per the
// post-v6.0.0 baseline. Backfilling them closes a trivial gap that
// counts against the 85% overall coverage gate.
package digital

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReconstructAccessGrantRoundTrip(t *testing.T) {
	t.Parallel()
	rec := AccessGrantRecord{
		ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		TenantID:   "tenant-r",
		CustomerID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		ProductID:  uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		LicenseID:  uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		GrantedAt:  time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		Source:     SourceGift,
	}
	got := ReconstructAccessGrant(rec)

	if got.ID() != rec.ID {
		t.Errorf("ID() = %s, want %s", got.ID(), rec.ID)
	}
	if got.TenantID() != rec.TenantID {
		t.Errorf("TenantID() = %q, want %q", got.TenantID(), rec.TenantID)
	}
	if got.CustomerID() != rec.CustomerID {
		t.Errorf("CustomerID() = %s, want %s", got.CustomerID(), rec.CustomerID)
	}
	if got.ProductID() != rec.ProductID {
		t.Errorf("ProductID() = %s, want %s", got.ProductID(), rec.ProductID)
	}
	if got.LicenseID() != rec.LicenseID {
		t.Errorf("LicenseID() = %s, want %s", got.LicenseID(), rec.LicenseID)
	}
	if !got.GrantedAt().Equal(rec.GrantedAt) {
		t.Errorf("GrantedAt() = %v, want %v", got.GrantedAt(), rec.GrantedAt)
	}
	if got.Source() != rec.Source {
		t.Errorf("Source() = %q, want %q", got.Source(), rec.Source)
	}
}
