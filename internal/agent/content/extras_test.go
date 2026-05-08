package content

import (
	"strings"
	"testing"
)

// File scope: small targeted tests for previously-uncovered helpers in
// the content agent: SortClaimChecks (0%), styleSystemPrompt branches
// (50%), and roundFactConfidence (60%).

func TestSortClaimChecksOrdersByText(t *testing.T) {
	t.Parallel()

	checks := []ClaimCheck{
		{Text: "zeta claim"},
		{Text: "alpha claim"},
		{Text: "mu claim"},
	}
	SortClaimChecks(checks)
	if checks[0].Text != "alpha claim" || checks[1].Text != "mu claim" || checks[2].Text != "zeta claim" {
		t.Fatalf("sort order = %+v", checks)
	}
}

func TestSortClaimChecksHandlesEmptyAndSingleEntry(t *testing.T) {
	t.Parallel()

	SortClaimChecks(nil)
	SortClaimChecks([]ClaimCheck{{Text: "lonely"}})
}

func TestStyleSystemPromptCoversAllStylesAndDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		style    Style
		fragment string
	}{
		{name: "professional", style: StyleProfessional, fragment: "professional, confident"},
		{name: "casual", style: StyleCasual, fragment: "warm, casual"},
		{name: "luxury", style: StyleLuxury, fragment: "exclusivity"},
		{name: "technical", style: StyleTechnical, fragment: "specification-focused"},
		{name: "unknown defaults", style: Style("imaginary"), fragment: "compelling product descriptions"},
		{name: "empty defaults", style: "", fragment: "compelling product descriptions"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := styleSystemPrompt(tc.style)
			if !strings.Contains(strings.ToLower(got), tc.fragment) {
				t.Fatalf("prompt for style %q missing %q; got=%q", tc.style, tc.fragment, got)
			}
		})
	}
}

func TestRoundFactConfidenceClampsToZeroToOneRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input, want float64
	}{
		{input: -0.4, want: 0},
		{input: 0, want: 0},
		{input: 0.5, want: 0.5},
		{input: 0.756, want: 0.76},
		{input: 1.0, want: 1.0},
		{input: 1.42, want: 1.0},
	}
	for _, tc := range tests {
		tc := tc
		if got := roundFactConfidence(tc.input); got != tc.want {
			t.Fatalf("roundFactConfidence(%v) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
