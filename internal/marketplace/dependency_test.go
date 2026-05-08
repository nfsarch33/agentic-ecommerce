package marketplace

import (
	"errors"
	"reflect"
	"testing"
)

func TestResolveOrderTopological(t *testing.T) {
	t.Parallel()
	manifests := []Manifest{
		{Slug: "klaviyo-marketing", Name: "K", Version: "0.4.1", Vendor: "Klaviyo",
			Dependencies: []DependencyRef{{Slug: "ses-email", Constraint: "^1.0.0"}}},
		{Slug: "ses-email", Name: "SES", Version: "1.0.0", Vendor: "AWS"},
		{Slug: "stripe-payments", Name: "Stripe", Version: "1.2.0", Vendor: "Stripe"},
	}
	out, err := ResolveOrder(manifests)
	if err != nil {
		t.Fatalf("ResolveOrder: %v", err)
	}
	gotSlugs := make([]string, len(out))
	for i, m := range out {
		gotSlugs[i] = m.Slug
	}
	// ses-email and stripe-payments have indegree 0 and sort
	// alphabetically: ses-email, stripe-payments. klaviyo-marketing
	// follows.
	wantSlugs := []string{"ses-email", "stripe-payments", "klaviyo-marketing"}
	if !reflect.DeepEqual(gotSlugs, wantSlugs) {
		t.Fatalf("ResolveOrder slugs = %v, want %v", gotSlugs, wantSlugs)
	}
}

func TestResolveOrderCycleRejected(t *testing.T) {
	t.Parallel()
	manifests := []Manifest{
		{Slug: "alpha", Name: "A", Version: "1.0.0", Vendor: "Acme",
			Dependencies: []DependencyRef{{Slug: "beta"}}},
		{Slug: "beta", Name: "B", Version: "1.0.0", Vendor: "Acme",
			Dependencies: []DependencyRef{{Slug: "alpha"}}},
	}
	_, err := ResolveOrder(manifests)
	if !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("expected ErrDependencyCycle, got %v", err)
	}
}

func TestResolveOrderEmpty(t *testing.T) {
	t.Parallel()
	out, err := ResolveOrder(nil)
	if err != nil {
		t.Fatalf("nil manifests should not error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty result, got %v", out)
	}
}

func TestResolveOrderDuplicateSlugs(t *testing.T) {
	t.Parallel()
	manifests := []Manifest{
		{Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe"},
		{Slug: "stripe-payments", Name: "S", Version: "1.0.1", Vendor: "Stripe"},
	}
	_, err := ResolveOrder(manifests)
	if !errors.Is(err, ErrSlugAlreadyExists) {
		t.Fatalf("expected ErrSlugAlreadyExists, got %v", err)
	}
}

func TestVerifyDependencySemver(t *testing.T) {
	t.Parallel()
	installed := []Manifest{
		{Slug: "ses-email", Version: "1.5.0"},
	}
	ok := Manifest{
		Slug: "klaviyo-marketing", Name: "K", Version: "0.4.0", Vendor: "K",
		Dependencies: []DependencyRef{{Slug: "ses-email", Constraint: "^1.0.0"}},
	}
	if err := VerifyDependencySemver(ok, installed); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}

	missing := Manifest{
		Slug: "klaviyo-marketing", Name: "K", Version: "0.4.0", Vendor: "K",
		Dependencies: []DependencyRef{{Slug: "twilio-sms", Constraint: "^1.0.0"}},
	}
	if err := VerifyDependencySemver(missing, installed); !errors.Is(err, ErrSemverConflict) {
		t.Fatalf("expected ErrSemverConflict, got %v", err)
	}

	majorMismatch := Manifest{
		Slug: "klaviyo-marketing", Name: "K", Version: "0.4.0", Vendor: "K",
		Dependencies: []DependencyRef{{Slug: "ses-email", Constraint: "^2.0.0"}},
	}
	if err := VerifyDependencySemver(majorMismatch, installed); !errors.Is(err, ErrSemverConflict) {
		t.Fatalf("expected ErrSemverConflict for major mismatch, got %v", err)
	}
}
