package residency

import (
	"context"
	"errors"
	"testing"
)

type stubResolver struct {
	regions map[string]RegionCode
}

func (s *stubResolver) TenantRegion(_ context.Context, tenantID string) (RegionCode, error) {
	r, ok := s.regions[tenantID]
	if !ok {
		return "", ErrTenantNotFound
	}
	return r, nil
}

type stubPool struct{ region RegionCode }

func (p *stubPool) Region() RegionCode { return p.region }

// RED Scenario 1: AU tenant data stays in AU.
func TestValidator_AUTenantAUWrite_Passes(t *testing.T) {
	t.Parallel()
	resolver := &stubResolver{regions: map[string]RegionCode{"t-au": RegionAU}}
	v := NewValidator(resolver)
	err := v.Validate(context.Background(), "t-au", RegionAU)
	if err != nil {
		t.Fatalf("AU→AU should pass, got: %v", err)
	}
}

// RED Scenario 2: CN tenant data stays in CN.
func TestValidator_CNTenantCNWrite_Passes(t *testing.T) {
	t.Parallel()
	resolver := &stubResolver{regions: map[string]RegionCode{"t-cn": RegionCN}}
	v := NewValidator(resolver)
	err := v.Validate(context.Background(), "t-cn", RegionCN)
	if err != nil {
		t.Fatalf("CN→CN should pass, got: %v", err)
	}
}

// RED Scenario 3: cross-region write rejected.
func TestValidator_CrossRegionWrite_Rejected(t *testing.T) {
	t.Parallel()
	resolver := &stubResolver{regions: map[string]RegionCode{"t-au": RegionAU}}
	v := NewValidator(resolver)
	err := v.Validate(context.Background(), "t-au", RegionEU)
	if !errors.Is(err, ErrResidencyViolation) {
		t.Fatalf("AU→EU should be ErrResidencyViolation, got: %v", err)
	}
}

// RED Scenario 4: default to AU when tenant has AU set.
func TestDefaultRegion_IsAU(t *testing.T) {
	t.Parallel()
	if DefaultRegion() != RegionAU {
		t.Fatalf("default region = %s, want AU", DefaultRegion())
	}
}

// RED Scenario 5: region migration (CN→EU) updates validation.
func TestValidator_RegionMigration(t *testing.T) {
	t.Parallel()
	resolver := &stubResolver{regions: map[string]RegionCode{"t-cn": RegionCN}}
	v := NewValidator(resolver)

	err := v.Validate(context.Background(), "t-cn", RegionEU)
	if !errors.Is(err, ErrResidencyViolation) {
		t.Fatalf("pre-migration CN→EU should fail, got: %v", err)
	}

	resolver.regions["t-cn"] = RegionEU
	err = v.Validate(context.Background(), "t-cn", RegionEU)
	if err != nil {
		t.Fatalf("post-migration EU→EU should pass, got: %v", err)
	}
}

func TestNewPolicy_ValidInputs(t *testing.T) {
	t.Parallel()
	p, err := NewPolicy("t1", RegionAU)
	if err != nil {
		t.Fatalf("NewPolicy failed: %v", err)
	}
	if p.TenantID != "t1" || p.DataRegion != RegionAU {
		t.Fatalf("unexpected policy: %+v", p)
	}
}

func TestNewPolicy_InvalidRegion(t *testing.T) {
	t.Parallel()
	_, err := NewPolicy("t1", "XX")
	if !errors.Is(err, ErrUnknownRegion) {
		t.Fatalf("expected ErrUnknownRegion, got: %v", err)
	}
}

func TestRouter_RouteWrite_KnownRegion(t *testing.T) {
	t.Parallel()
	auPool := &stubPool{region: RegionAU}
	cnPool := &stubPool{region: RegionCN}
	pools := map[RegionCode]ConnectionPool{RegionAU: auPool, RegionCN: cnPool}
	r := NewRouter(pools, auPool)

	got, err := r.RouteWrite(RegionCN)
	if err != nil {
		t.Fatalf("RouteWrite CN: %v", err)
	}
	if got.Region() != RegionCN {
		t.Fatalf("got region %s, want CN", got.Region())
	}
}

func TestRouter_RouteWrite_FallbackToDefault(t *testing.T) {
	t.Parallel()
	auPool := &stubPool{region: RegionAU}
	pools := map[RegionCode]ConnectionPool{RegionAU: auPool}
	r := NewRouter(pools, auPool)

	got, err := r.RouteWrite(RegionEU)
	if err != nil {
		t.Fatalf("RouteWrite EU fallback: %v", err)
	}
	if got.Region() != RegionAU {
		t.Fatalf("got region %s, want AU (default)", got.Region())
	}
}

func TestRegionCode_GCPZone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		region RegionCode
		zone   string
	}{
		{RegionAU, "australia-southeast1"},
		{RegionCN, "asia-east2"},
		{RegionEU, "europe-west1"},
		{RegionUS, "us-central1"},
	}
	for _, tc := range tests {
		if tc.region.GCPZone() != tc.zone {
			t.Errorf("%s.GCPZone() = %s, want %s", tc.region, tc.region.GCPZone(), tc.zone)
		}
	}
}
