package compliance

import (
	"errors"
	"testing"
)

// TestCompliance_BlocksRestrictedCategories is the EC-1-4 RED test.
// 20 fixture products spanning compliant + non-compliant categories
// must be classified correctly.
func TestCompliance_BlocksRestrictedCategories(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		product    Product
		wantPass   bool
		wantBlocks []string
	}{
		// Compliant products (should pass)
		{
			name:     "wireless earbuds 1688 compliant",
			product:  Product{ID: "p1", TenantID: "cylrl", Category: "electronics", SubCategory: "audio", Source: Source1688},
			wantPass: true,
		},
		{
			name:     "yoga mat 1688 compliant",
			product:  Product{ID: "p2", TenantID: "cylrl", Category: "fitness", SubCategory: "yoga", Source: Source1688},
			wantPass: true,
		},
		{
			name:     "kitchen blender taobao compliant",
			product:  Product{ID: "p3", TenantID: "cylrl", Category: "kitchen", SubCategory: "appliances", Source: SourceTaobao},
			wantPass: true,
		},
		{
			name:     "outdoor tent 1688 compliant",
			product:  Product{ID: "p4", TenantID: "cylrl", Category: "outdoor", SubCategory: "camping", Source: Source1688},
			wantPass: true,
		},
		{
			name:     "phone case 1688 compliant",
			product:  Product{ID: "p5", TenantID: "cylrl", Category: "accessories", SubCategory: "phone_case", Source: Source1688},
			wantPass: true,
		},
		// AU import restrictions (block on all platforms)
		{
			name:       "firearms blocked AU import",
			product:    Product{ID: "p6", TenantID: "cylrl", Category: "firearms", Source: Source1688},
			wantPass:   false,
			wantBlocks: []string{"all"},
		},
		{
			name:       "ammunition blocked AU import",
			product:    Product{ID: "p7", TenantID: "cylrl", Category: "ammunition", Source: Source1688},
			wantPass:   false,
			wantBlocks: []string{"all"},
		},
		{
			name:       "medical device blocked AU import",
			product:    Product{ID: "p8", TenantID: "cylrl", Category: "medical_device", Source: SourceTaobao},
			wantPass:   false,
			wantBlocks: []string{"all"},
		},
		{
			name:       "explosives blocked AU import",
			product:    Product{ID: "p9", TenantID: "cylrl", Category: "explosives", Source: Source1688},
			wantPass:   false,
			wantBlocks: []string{"all"},
		},
		{
			name:       "narcotics blocked AU import via subcategory",
			product:    Product{ID: "p10", TenantID: "cylrl", Category: "health", SubCategory: "narcotics", Source: Source1688},
			wantPass:   false,
			wantBlocks: []string{"all"},
		},
		// TikTok prohibited (compliant for AU import but blocked on platform)
		{
			name:       "vape blocked on tiktok and facebook",
			product:    Product{ID: "p11", TenantID: "cylrl", Category: "vape", Source: Source1688},
			wantPass:   false,
			wantBlocks: []string{string(SourceTikTok), string(SourceFacebook)},
		},
		{
			name:       "gambling product blocked on platforms",
			product:    Product{ID: "p12", TenantID: "cylrl", Category: "gambling", Source: SourceTaobao},
			wantPass:   false,
			wantBlocks: []string{string(SourceTikTok), string(SourceFacebook)},
		},
		{
			name:       "cbd blocked on platforms",
			product:    Product{ID: "p13", TenantID: "cylrl", Category: "cbd", Source: Source1688},
			wantPass:   false,
			wantBlocks: []string{string(SourceTikTok), string(SourceFacebook)},
		},
		{
			name:       "weight loss supplements blocked on platforms",
			product:    Product{ID: "p14", TenantID: "cylrl", Category: "weight_loss", Source: Source1688},
			wantPass:   false,
			wantBlocks: []string{string(SourceTikTok), string(SourceFacebook)},
		},
		{
			name:       "counterfeit goods blocked on platforms",
			product:    Product{ID: "p15", TenantID: "cylrl", Category: "counterfeit", Source: SourceTaobao},
			wantPass:   false,
			wantBlocks: []string{string(SourceTikTok), string(SourceFacebook)},
		},
		// TikTok-only categories
		{
			name:       "used cosmetics blocked on tiktok only",
			product:    Product{ID: "p16", TenantID: "cylrl", Category: "used_cosmetics", Source: Source1688},
			wantPass:   false,
			wantBlocks: []string{string(SourceTikTok)},
		},
		// Facebook-only category
		{
			name:       "weapons parts blocked on facebook only",
			product:    Product{ID: "p17", TenantID: "cylrl", Category: "weapons_parts", Source: Source1688},
			wantPass:   false,
			wantBlocks: []string{string(SourceFacebook)},
		},
		// Operator-flagged
		{
			name:       "operator flagged restricted",
			product:    Product{ID: "p18", TenantID: "cylrl", Category: "kitchen", Restricted: true, Source: Source1688},
			wantPass:   false,
			wantBlocks: []string{"all"},
		},
		// Edge cases
		{
			name:     "case insensitive category match",
			product:  Product{ID: "p19", TenantID: "cylrl", Category: "  Firearms  ", Source: Source1688},
			wantPass: false,
			// blocked due to AU import even with mixed case
		},
		{
			name:     "endangered species blocked AU import via subcategory",
			product:  Product{ID: "p20", TenantID: "cylrl", Category: "wildlife", SubCategory: "endangered", Source: Source1688},
			wantPass: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decision, err := Evaluate(tc.product)
			if tc.wantPass {
				if err != nil {
					t.Fatalf("expected pass, got error: %v", err)
				}
				if !decision.Pass {
					t.Fatalf("decision.Pass = false, want true: %+v", decision)
				}
				if len(decision.Reasons) > 0 {
					t.Fatalf("expected no reasons, got %v", decision.Reasons)
				}
				return
			}
			// Expect block
			if err == nil {
				t.Fatalf("expected ErrRestrictedCategory, got nil; decision=%+v", decision)
			}
			if !errors.Is(err, ErrRestrictedCategory) {
				t.Fatalf("error not wrapping ErrRestrictedCategory: %v", err)
			}
			if decision.Pass {
				t.Fatalf("decision.Pass = true, want false")
			}
			if decision.TenantID != tc.product.TenantID {
				t.Fatalf("tenant_id = %q, want %q", decision.TenantID, tc.product.TenantID)
			}
			for _, want := range tc.wantBlocks {
				if !contains(decision.BlockedFor, want) {
					t.Fatalf("BlockedFor = %v, want to include %q", decision.BlockedFor, want)
				}
			}
		})
	}
}

func TestEvaluate_RejectsEmptyProduct(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		p    Product
	}{
		{name: "missing tenant", p: Product{Category: "x"}},
		{name: "missing category", p: Product{TenantID: "cylrl"}},
		{name: "both missing", p: Product{}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Evaluate(tc.p)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrEmptyProduct) {
				t.Fatalf("error not wrapping ErrEmptyProduct: %v", err)
			}
		})
	}
}

func TestEvaluateBatch_PartitionsApprovedAndRejected(t *testing.T) {
	t.Parallel()

	products := []Product{
		{ID: "ok-1", TenantID: "cylrl", Category: "electronics", Source: Source1688},
		{ID: "bad-1", TenantID: "cylrl", Category: "firearms", Source: Source1688},
		{ID: "ok-2", TenantID: "cylrl", Category: "kitchen", Source: SourceTaobao},
		{ID: "bad-2", TenantID: "cylrl", Category: "vape", Source: Source1688},
	}
	approved, rejected := EvaluateBatch(products)
	if len(approved) != 2 {
		t.Fatalf("approved = %d, want 2", len(approved))
	}
	if len(rejected) != 2 {
		t.Fatalf("rejected = %d, want 2", len(rejected))
	}
	for _, d := range approved {
		if !d.Pass {
			t.Fatalf("approved decision has Pass=false: %+v", d)
		}
	}
	for _, d := range rejected {
		if d.Pass {
			t.Fatalf("rejected decision has Pass=true: %+v", d)
		}
	}
}

func TestDecision_PlatformsBlockedReason(t *testing.T) {
	t.Parallel()

	pass := Decision{Pass: true}
	if got := pass.PlatformsBlockedReason(); got != "" {
		t.Fatalf("Pass=true reason = %q, want empty", got)
	}

	fail := Decision{
		Pass:       false,
		Reasons:    []string{"category x prohibited"},
		BlockedFor: []string{"tiktok", "facebook"},
	}
	if got := fail.PlatformsBlockedReason(); got != "blocked: tiktok,facebook" {
		t.Fatalf("PlatformsBlockedReason = %q", got)
	}

	noBlocks := Decision{
		Pass:    false,
		Reasons: []string{"operator flag"},
	}
	if got := noBlocks.PlatformsBlockedReason(); got != "operator flag" {
		t.Fatalf("PlatformsBlockedReason fallback = %q", got)
	}
}

func TestNormaliseCategoryHandlesWhitespaceAndCase(t *testing.T) {
	t.Parallel()
	if got := normaliseCategory("  Firearms  "); got != "firearms" {
		t.Fatalf("normalise = %q", got)
	}
}

func TestMatchesPlatformProhibitionUnknownPlatformReturnsFalse(t *testing.T) {
	t.Parallel()
	if matchesPlatformProhibition(SourceUnknown, "vape", "") {
		t.Fatal("unknown platform should not block")
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
