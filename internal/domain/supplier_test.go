package domain

import (
	"errors"
	"testing"
)

// TestSupplierScore_PenalisesHighMOQWithLongLeadTime is the canonical
// EC-1-5 RED test driving Supplier.Score().
func TestSupplierScore_PenalisesHighMOQWithLongLeadTime(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		supplier  Supplier
		wantPass  bool
		assertion func(t *testing.T, score float64)
	}{
		{
			name: "low MOQ short lead time gold supplier passes",
			supplier: Supplier{
				ID:                  "sup-001",
				TenantID:            "cylrl",
				MOQ:                 10,
				LeadTimeDays:        7,
				VerifiedGold:        true,
				PositiveReviewRatio: 0.9,
			},
			wantPass: true,
			assertion: func(t *testing.T, score float64) {
				t.Helper()
				if score < 0.95 {
					t.Fatalf("score = %v, want >= 0.95 (clamped near max)", score)
				}
			},
		},
		{
			name: "high MOQ long lead time fails floor",
			supplier: Supplier{
				ID:                  "sup-002",
				TenantID:            "cylrl",
				MOQ:                 700,
				LeadTimeDays:        100,
				VerifiedGold:        false,
				PositiveReviewRatio: 0.5,
			},
			wantPass: false,
			assertion: func(t *testing.T, score float64) {
				t.Helper()
				if score >= SupplierScoreFloor {
					t.Fatalf("score = %v, want < floor %v", score, SupplierScoreFloor)
				}
			},
		},
		{
			name: "MOQ at threshold stays neutral on MOQ axis",
			supplier: Supplier{
				ID:                  "sup-003",
				TenantID:            "cylrl",
				MOQ:                 50,
				LeadTimeDays:        15,
				VerifiedGold:        false,
				PositiveReviewRatio: 0.5,
			},
			wantPass: true,
			assertion: func(t *testing.T, score float64) {
				t.Helper()
				if score < 0.95 || score > 1.0 {
					t.Fatalf("score = %v, want exactly 1.0 at threshold", score)
				}
			},
		},
		{
			name: "lead time at threshold stays neutral on lead-time axis",
			supplier: Supplier{
				ID:                  "sup-004",
				TenantID:            "cylrl",
				MOQ:                 30,
				LeadTimeDays:        20,
				VerifiedGold:        false,
				PositiveReviewRatio: 0.7,
			},
			wantPass: true,
			assertion: func(t *testing.T, score float64) {
				t.Helper()
				if score != 1.0 {
					t.Fatalf("score = %v, want 1.0 with no penalties no bonuses", score)
				}
			},
		},
		{
			name: "review bonus exactly at threshold applies",
			supplier: Supplier{
				ID:                  "sup-005",
				TenantID:            "cylrl",
				MOQ:                 30,
				LeadTimeDays:        15,
				VerifiedGold:        false,
				PositiveReviewRatio: ReviewBonusThreshold,
			},
			wantPass: true,
			assertion: func(t *testing.T, score float64) {
				t.Helper()
				if score != 1.0 {
					t.Fatalf("score = %v, want 1.0 (review bonus clamped at max)", score)
				}
			},
		},
		{
			name: "MOQ very high but lead time short gold compensates",
			supplier: Supplier{
				ID:                  "sup-006",
				TenantID:            "cylrl",
				MOQ:                 500,
				LeadTimeDays:        5,
				VerifiedGold:        true,
				PositiveReviewRatio: 0.95,
			},
			wantPass: true,
			assertion: func(t *testing.T, score float64) {
				t.Helper()
				// 1.0 - 0.45 (MOQ cap) + 0.15 (gold) + 0.10 (review) = 0.80
				if score < 0.7 || score > 0.85 {
					t.Fatalf("score = %v, want around 0.80", score)
				}
			},
		},
		{
			name: "very long lead time alone fails floor",
			supplier: Supplier{
				ID:                  "sup-007",
				TenantID:            "cylrl",
				MOQ:                 30,
				LeadTimeDays:        90,
				VerifiedGold:        false,
				PositiveReviewRatio: 0.6,
			},
			wantPass: true, // 1.0 - 0.45 = 0.55, just above floor
			assertion: func(t *testing.T, score float64) {
				t.Helper()
				// LeadTime penalty caps at 0.45 so 1.0 - 0.45 = 0.55
				if score < 0.5 || score > 0.6 {
					t.Fatalf("score = %v, want around 0.55 (lead-time cap)", score)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			score, pass := tc.supplier.Score()
			if pass != tc.wantPass {
				t.Fatalf("pass = %v, want %v (score = %v)", pass, tc.wantPass, score)
			}
			tc.assertion(t, score)
		})
	}
}

func TestSupplierScore_BoundsAreEnforced(t *testing.T) {
	t.Parallel()

	huge := Supplier{
		ID:                  "sup-huge",
		TenantID:            "cylrl",
		MOQ:                 100_000,
		LeadTimeDays:        365,
		VerifiedGold:        false,
		PositiveReviewRatio: 0,
	}
	score, pass := huge.Score()
	if score < SupplierScoreMin || score > SupplierScoreMax {
		t.Fatalf("score = %v, want clamped to [%v, %v]", score, SupplierScoreMin, SupplierScoreMax)
	}
	if pass {
		t.Fatalf("expected pass=false for grossly bad supplier")
	}
}

func TestSupplierValidate_RequiresIDAndTenant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		s       Supplier
		wantErr bool
	}{
		{name: "missing ID", s: Supplier{TenantID: "cylrl"}, wantErr: true},
		{name: "missing tenant", s: Supplier{ID: "sup-x"}, wantErr: true},
		{name: "ok", s: Supplier{ID: "sup-x", TenantID: "cylrl"}, wantErr: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.s.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidSupplier) {
				t.Fatalf("error not wrapping ErrInvalidSupplier: %v", err)
			}
		})
	}
}

func TestFilterByScore_DropsBelowFloor(t *testing.T) {
	t.Parallel()

	suppliers := []Supplier{
		{ID: "good", TenantID: "cylrl", MOQ: 20, LeadTimeDays: 10, VerifiedGold: true, PositiveReviewRatio: 0.9},
		{ID: "bad", TenantID: "cylrl", MOQ: 1000, LeadTimeDays: 90, VerifiedGold: false, PositiveReviewRatio: 0.3},
		{ID: "ok", TenantID: "cylrl", MOQ: 60, LeadTimeDays: 22, VerifiedGold: false, PositiveReviewRatio: 0.6},
	}
	filtered := FilterByScore(suppliers, SupplierScoreFloor)
	if len(filtered) < 1 {
		t.Fatalf("filtered = %d, want at least 1 (good supplier)", len(filtered))
	}
	for _, s := range filtered {
		if score, _ := s.Score(); score < SupplierScoreFloor {
			t.Fatalf("filtered supplier %s has score %v below floor", s.ID, score)
		}
	}
}

func TestFilterByScore_NegativeFloorFallsBackToDefault(t *testing.T) {
	t.Parallel()

	suppliers := []Supplier{
		{ID: "good", TenantID: "cylrl", MOQ: 20, LeadTimeDays: 10, VerifiedGold: true, PositiveReviewRatio: 0.9},
	}
	filtered := FilterByScore(suppliers, -1.0)
	if len(filtered) != 1 {
		t.Fatalf("filtered = %d, want 1 (negative floor falls back to default)", len(filtered))
	}
}

func TestFilterByScore_EmptyInput(t *testing.T) {
	t.Parallel()
	if got := FilterByScore(nil, SupplierScoreFloor); len(got) != 0 {
		t.Fatalf("got %d, want 0", len(got))
	}
}

func TestClampHandlesAllBranches(t *testing.T) {
	t.Parallel()
	if clamp(-1, 0, 1) != 0 {
		t.Fatal("below lo not clamped")
	}
	if clamp(2, 0, 1) != 1 {
		t.Fatal("above hi not clamped")
	}
	if clamp(0.5, 0, 1) != 0.5 {
		t.Fatal("in-range value altered")
	}
}
