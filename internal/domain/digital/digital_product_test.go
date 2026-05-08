package digital

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func validProductInput() DigitalProductInput {
	return DigitalProductInput{
		TenantID:    "tenant-a",
		SKU:         "PDF-001",
		Name:        "Sample PDF",
		Description: "Lorem ipsum",
		FilePath:    "tenant-a/digital/pdf-001.pdf",
		FileSize:    1024,
		ContentType: "application/pdf",
		Checksum:    "sha256:abc",
		Version:     "1.0.0",
		Now:         time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
	}
}

func TestNewDigitalProductValidates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*DigitalProductInput)
		wantErr error
	}{
		{name: "ok", mutate: func(*DigitalProductInput) {}},
		{name: "missing tenant", mutate: func(in *DigitalProductInput) { in.TenantID = "  " }, wantErr: ErrTenantRequired},
		{name: "missing sku", mutate: func(in *DigitalProductInput) { in.SKU = "" }, wantErr: ErrSKURequired},
		{name: "missing name", mutate: func(in *DigitalProductInput) { in.Name = "  " }, wantErr: ErrNameRequired},
		{name: "missing file path", mutate: func(in *DigitalProductInput) { in.FilePath = "" }, wantErr: ErrFilePathRequired},
		{name: "non-positive file size", mutate: func(in *DigitalProductInput) { in.FileSize = 0 }, wantErr: ErrInvalidFileSize},
		{name: "missing version", mutate: func(in *DigitalProductInput) { in.Version = "" }, wantErr: ErrInvalidVersion},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := validProductInput()
			tc.mutate(&input)
			_, err := NewDigitalProduct(input)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestDigitalProductRoundTripFields(t *testing.T) {
	t.Parallel()
	input := validProductInput()
	p, err := NewDigitalProduct(input)
	if err != nil {
		t.Fatalf("NewDigitalProduct: %v", err)
	}
	if p.ID() == uuid.Nil {
		t.Fatal("ID should be assigned")
	}
	if p.TenantID() != "tenant-a" {
		t.Fatalf("TenantID = %q", p.TenantID())
	}
	if p.SKU() != "PDF-001" || p.Name() != "Sample PDF" {
		t.Fatalf("SKU/Name: %q/%q", p.SKU(), p.Name())
	}
	if p.FileSize() != 1024 {
		t.Fatalf("FileSize = %d", p.FileSize())
	}
	if !p.CreatedAt().Equal(input.Now) || !p.UpdatedAt().Equal(input.Now) {
		t.Fatalf("times = %s/%s, want %s", p.CreatedAt(), p.UpdatedAt(), input.Now)
	}
}

func TestDigitalProductUpdateAppliesPartialChanges(t *testing.T) {
	t.Parallel()
	input := validProductInput()
	p, err := NewDigitalProduct(input)
	if err != nil {
		t.Fatalf("NewDigitalProduct: %v", err)
	}
	later := input.Now.Add(time.Hour)
	if err := p.Update(DigitalProductInput{
		Name:    "Updated Name",
		Version: "1.0.1",
	}, later); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p.Name() != "Updated Name" || p.Version() != "1.0.1" {
		t.Fatalf("update did not apply: name=%q version=%q", p.Name(), p.Version())
	}
	if p.SKU() != "PDF-001" {
		t.Fatalf("SKU should not be changed: %q", p.SKU())
	}
	if !p.UpdatedAt().Equal(later) {
		t.Fatalf("UpdatedAt = %s, want %s", p.UpdatedAt(), later)
	}
}

func TestReconstructDigitalProductRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	rec := DigitalProductRecord{
		ID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		TenantID:  "tenant-a",
		SKU:       "PDF-002",
		Name:      "Hydrated",
		FilePath:  "k",
		FileSize:  1,
		Version:   "1",
		CreatedAt: now,
		UpdatedAt: now,
	}
	p := ReconstructDigitalProduct(rec)
	if p.ID() != rec.ID || p.TenantID() != rec.TenantID || p.Name() != rec.Name {
		t.Fatalf("hydration mismatch: %+v", p)
	}
}
