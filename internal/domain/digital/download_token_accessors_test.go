// File scope: v6.1.0 coverage backfill -- DownloadToken accessors +
// ReconstructDownloadToken were uncovered in the post-v6.0.0 baseline.
package digital

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReconstructDownloadTokenAndAccessors(t *testing.T) {
	t.Parallel()
	rec := DownloadTokenRecord{
		ID:           uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		TenantID:     "tenant-tok",
		LicenseID:    uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		Signature:    "sig-xyz",
		ExpiresAt:    time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		UsesAllowed:  3,
		UsesSoFar:    1,
		CreatedAt:    time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC),
		LastIssuedAt: time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC),
	}
	tok := ReconstructDownloadToken(rec)

	if tok.ID() != rec.ID {
		t.Errorf("ID() = %s, want %s", tok.ID(), rec.ID)
	}
	if tok.TenantID() != rec.TenantID {
		t.Errorf("TenantID() = %q, want %q", tok.TenantID(), rec.TenantID)
	}
	if tok.LicenseID() != rec.LicenseID {
		t.Errorf("LicenseID() = %s, want %s", tok.LicenseID(), rec.LicenseID)
	}
	if tok.Signature() != rec.Signature {
		t.Errorf("Signature() = %q, want %q", tok.Signature(), rec.Signature)
	}
	if !tok.ExpiresAt().Equal(rec.ExpiresAt) {
		t.Errorf("ExpiresAt() = %v, want %v", tok.ExpiresAt(), rec.ExpiresAt)
	}
	if tok.UsesAllowed() != rec.UsesAllowed {
		t.Errorf("UsesAllowed() = %d, want %d", tok.UsesAllowed(), rec.UsesAllowed)
	}
	if tok.UsesSoFar() != rec.UsesSoFar {
		t.Errorf("UsesSoFar() = %d, want %d", tok.UsesSoFar(), rec.UsesSoFar)
	}
	if !tok.CreatedAt().Equal(rec.CreatedAt) {
		t.Errorf("CreatedAt() = %v, want %v", tok.CreatedAt(), rec.CreatedAt)
	}
	if !tok.LastIssuedAt().Equal(rec.LastIssuedAt) {
		t.Errorf("LastIssuedAt() = %v, want %v", tok.LastIssuedAt(), rec.LastIssuedAt)
	}
}
