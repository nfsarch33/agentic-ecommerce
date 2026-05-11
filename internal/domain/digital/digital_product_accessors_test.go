// File scope: v6.1.0 coverage backfill -- DigitalProduct
// Description/FilePath/ContentType/Checksum accessors were
// uncovered in the post-v6.0.0 baseline.
package digital

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDigitalProductAccessorsCoverAllFields(t *testing.T) {
	t.Parallel()
	rec := DigitalProductRecord{
		ID:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		TenantID:    "tenant-acc",
		SKU:         "DIG-ACC-1",
		Name:        "Course Bundle",
		Description: "Premium course bundle",
		FilePath:    "/store/bundle.zip",
		FileSize:    4096,
		ContentType: "application/zip",
		Checksum:    "sha256:abc",
		Version:     "v1.0",
		CreatedAt:   time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 5, 11, 11, 0, 0, 0, time.UTC),
	}
	p := ReconstructDigitalProduct(rec)

	if p.Description() != rec.Description {
		t.Errorf("Description() = %q, want %q", p.Description(), rec.Description)
	}
	if p.FilePath() != rec.FilePath {
		t.Errorf("FilePath() = %q, want %q", p.FilePath(), rec.FilePath)
	}
	if p.ContentType() != rec.ContentType {
		t.Errorf("ContentType() = %q, want %q", p.ContentType(), rec.ContentType)
	}
	if p.Checksum() != rec.Checksum {
		t.Errorf("Checksum() = %q, want %q", p.Checksum(), rec.Checksum)
	}
}
