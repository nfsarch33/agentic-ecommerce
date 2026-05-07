package seo

import (
	"encoding/json"
	"os"
	"testing"
)

func TestOptimizerSuggestsDeterministicContentFromProductCopy(t *testing.T) {
	t.Parallel()

	optimizer := NewOptimizer()
	got := optimizer.Suggest(Input{
		Title:       "  Premium Resistance Band Set for Home Workouts  ",
		Description: "Premium resistance band set for strength training at home. This resistance band set supports warm ups, rehab, and full body workouts.",
		Keywords:    []string{"resistance band set", "home workouts"},
	})

	raw, err := os.ReadFile("testdata/suggestion.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var want Suggestion
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if got.Title != want.Title || got.MetaDescription != want.MetaDescription || got.Slug != want.Slug {
		t.Fatalf("suggestion = %#v, want %#v", got, want)
	}
	if got.Score != want.Score {
		t.Fatalf("score = %d, want %d", got.Score, want.Score)
	}
}

func TestValidateRejectsOverlongTitleAndMetaDescription(t *testing.T) {
	t.Parallel()

	optimizer := NewOptimizer()
	got := optimizer.Validate(Suggestion{
		Title:           "This SEO title is intentionally too long for a search result snippet",
		MetaDescription: "This meta description is intentionally much longer than the preferred search result snippet length because it keeps going with extra filler copy until it is no longer acceptable for normal ecommerce pages.",
		Slug:            "premium-resistance-band-set",
		KeywordDensity:  map[string]float64{"resistance band set": 1.8},
	})

	if got.Pass {
		t.Fatal("expected invalid suggestion")
	}
	if got.Score >= 80 {
		t.Fatalf("score = %d, want below 80", got.Score)
	}
	if len(got.Reasons) < 2 {
		t.Fatalf("reasons = %#v, want title and meta failures", got.Reasons)
	}
}

func TestKeywordDensityScoresExactPhraseDensity(t *testing.T) {
	t.Parallel()

	got := KeywordDensity("Resistance band set. Resistance band set for home workouts.", []string{"resistance band set", "home workouts"})

	if got["resistance band set"] != 22.22 {
		t.Fatalf("resistance density = %.2f, want 22.22", got["resistance band set"])
	}
	if got["home workouts"] != 11.11 {
		t.Fatalf("home density = %.2f, want 11.11", got["home workouts"])
	}
}

func TestSuggestTruncatesLongTitleAndMetaAtWordBoundaries(t *testing.T) {
	t.Parallel()

	got := NewOptimizer().Suggest(Input{
		Title:       "Ultimate Commercial Grade Resistance Band Set With Door Anchor Handles Carry Bag And Training Guide",
		Description: "This premium resistance band set is designed for home strength training, warm ups, mobility work, and travel workouts. It includes a carry bag and clear setup guidance for daily use.",
	})

	if runeLen(got.Title) > maxTitleRunes {
		t.Fatalf("title length = %d, want <= %d (%q)", runeLen(got.Title), maxTitleRunes, got.Title)
	}
	if runeLen(got.MetaDescription) > maxMetaRunes {
		t.Fatalf("meta length = %d, want <= %d (%q)", runeLen(got.MetaDescription), maxMetaRunes, got.MetaDescription)
	}
	if got.Slug != "ultimate-commercial-grade-resistance-band-set-with-door-anchor-handles-carry-bag-and-training-guide" {
		t.Fatalf("slug = %q", got.Slug)
	}
}

func TestValidateRejectsMissingSEOFields(t *testing.T) {
	t.Parallel()

	got := NewOptimizer().Validate(Suggestion{})

	if got.Pass {
		t.Fatal("missing SEO fields passed, want fail")
	}
	if got.Score != 20 {
		t.Fatalf("score = %d, want 20", got.Score)
	}
}

func TestKeywordDensityNormalizesDuplicateAndEmptyKeywords(t *testing.T) {
	t.Parallel()

	got := KeywordDensity("", []string{"", "  Home Workouts  ", "home workouts"})

	if len(got) != 1 {
		t.Fatalf("density keys = %#v, want one normalized keyword", got)
	}
	if got["home workouts"] != 0 {
		t.Fatalf("density = %.2f, want 0", got["home workouts"])
	}
}
