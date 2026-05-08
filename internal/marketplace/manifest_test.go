package marketplace

import (
	"errors"
	"testing"
)

func TestManifestValidate(t *testing.T) {
	t.Parallel()
	base := Manifest{
		Slug:    "stripe-payments",
		Name:    "Stripe Payments",
		Version: "1.2.3",
		Vendor:  "Agentic Labs",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("base manifest should be valid: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*Manifest)
		wantErr error
	}{
		{"slug invalid", func(m *Manifest) { m.Slug = "Stripe!" }, ErrSlugInvalid},
		{"slug uppercase", func(m *Manifest) { m.Slug = "Stripe-Payments" }, ErrSlugInvalid},
		{"slug single char", func(m *Manifest) { m.Slug = "x" }, ErrSlugInvalid},
		{"name empty", func(m *Manifest) { m.Name = "  " }, ErrManifestInvalid},
		{"vendor empty", func(m *Manifest) { m.Vendor = "" }, ErrManifestInvalid},
		{"version invalid", func(m *Manifest) { m.Version = "1.2" }, ErrSemverInvalid},
		{"version with prerelease", func(m *Manifest) { m.Version = "1.2.3-beta" }, ErrSemverInvalid},
		{"self dependency", func(m *Manifest) {
			m.Dependencies = []DependencyRef{{Slug: m.Slug, Constraint: "^1.0.0"}}
		}, ErrManifestInvalid},
		{"duplicate dep", func(m *Manifest) {
			m.Dependencies = []DependencyRef{
				{Slug: "ses-email", Constraint: "^1.0.0"},
				{Slug: "ses-email", Constraint: "^1.1.0"},
			}
		}, ErrManifestInvalid},
		{"invalid constraint", func(m *Manifest) {
			m.Dependencies = []DependencyRef{{Slug: "ses-email", Constraint: ">=1.0.0"}}
		}, ErrSemverInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := base
			tc.mutate(&m)
			err := m.Validate()
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestConstraintSatisfied(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		constraint string
		candidate  string
		want       bool
	}{
		{"empty constraint always ok", "", "1.0.0", true},
		{"caret minor up ok", "^1.2.0", "1.3.0", true},
		{"caret patch up ok", "^1.2.3", "1.2.4", true},
		{"caret major up rejected", "^1.0.0", "2.0.0", false},
		{"caret minor down rejected", "^1.2.0", "1.1.0", false},
		{"caret patch down rejected", "^1.2.3", "1.2.2", false},
		{"exact match", "=1.2.3", "1.2.3", true},
		{"exact mismatch", "=1.2.3", "1.2.4", false},
		{"bare treated as caret", "1.2.0", "1.2.5", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, err := ConstraintSatisfied(tc.constraint, tc.candidate)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tc.want {
				t.Fatalf("constraint=%q candidate=%q got %v want %v",
					tc.constraint, tc.candidate, ok, tc.want)
			}
		})
	}
}

func TestParseSemverErrors(t *testing.T) {
	t.Parallel()
	if _, err := ParseSemver("1"); !errors.Is(err, ErrSemverInvalid) {
		t.Fatalf("expected ErrSemverInvalid, got %v", err)
	}
	if _, err := ParseSemver("1.2.x"); !errors.Is(err, ErrSemverInvalid) {
		t.Fatalf("expected ErrSemverInvalid, got %v", err)
	}
}

func TestConstraintSatisfiedErrors(t *testing.T) {
	t.Parallel()
	if _, err := ConstraintSatisfied(">=1.0.0", "1.0.0"); !errors.Is(err, ErrSemverInvalid) {
		t.Fatalf("expected ErrSemverInvalid for >= constraint, got %v", err)
	}
	if _, err := ConstraintSatisfied("^1.0.0", "garbage"); !errors.Is(err, ErrSemverInvalid) {
		t.Fatalf("expected ErrSemverInvalid for garbage candidate, got %v", err)
	}
}
