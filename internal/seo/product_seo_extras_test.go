package seo

import (
	"strings"
	"testing"
)

func TestSanitiseKeywordsDedupesAndTrims(t *testing.T) {
	t.Parallel()

	got := sanitiseKeywords([]string{"  Earbuds  ", "earbuds", "WIRELESS EARBUDS", "", "wireless earbuds"})
	if len(got) != 2 {
		t.Fatalf("got %d keywords, want 2: %v", len(got), got)
	}
	// Longer keyword first (length-desc stable sort).
	if got[0] != "wireless earbuds" || got[1] != "earbuds" {
		t.Fatalf("ordering wrong: %v", got)
	}
}

func TestComposeTitleRespects60RuneCeiling(t *testing.T) {
	t.Parallel()

	p := &ProductSEO{}
	tooLong := strings.Repeat("k", 100)
	got := p.composeTitle("My Title", []string{tooLong})
	if got != "My Title" {
		t.Fatalf("composeTitle should not exceed ceiling; got %q", got)
	}
	got = p.composeTitle("My Title", nil)
	if got != "My Title" {
		t.Fatalf("composeTitle with no keywords should return title; got %q", got)
	}
	// Keyword already in title -> returned unchanged.
	got = p.composeTitle("My Wireless Title", []string{"wireless"})
	if got != "My Wireless Title" {
		t.Fatalf("composeTitle should detect existing keyword; got %q", got)
	}
}

func TestInjectKeywordsIntoMetaCapsAt155(t *testing.T) {
	t.Parallel()

	p := &ProductSEO{}
	long := strings.Repeat("a", 200)
	s := Suggestion{MetaDescription: long}
	got := p.injectKeywordsIntoMeta(s, []string{"keyword"})
	if runeLen(got.MetaDescription) > 155 {
		t.Fatalf("meta exceeds 155: %d", runeLen(got.MetaDescription))
	}
	// Existing meta ending without . should still get separator.
	s2 := Suggestion{MetaDescription: "no period"}
	got2 := p.injectKeywordsIntoMeta(s2, []string{"x"})
	if !strings.Contains(got2.MetaDescription, "x.") {
		t.Fatalf("meta missing keyword: %q", got2.MetaDescription)
	}
	// Existing meta ending with ! handles period correctly.
	s3 := Suggestion{MetaDescription: "wow!"}
	got3 := p.injectKeywordsIntoMeta(s3, []string{"x"})
	if !strings.HasSuffix(got3.MetaDescription, "x.") {
		t.Fatalf("meta missing trailing period: %q", got3.MetaDescription)
	}
	// Empty meta + keyword.
	s4 := Suggestion{MetaDescription: ""}
	got4 := p.injectKeywordsIntoMeta(s4, []string{"y"})
	if got4.MetaDescription != "y." {
		t.Fatalf("empty meta + keyword: got %q", got4.MetaDescription)
	}
	// No keywords => unchanged.
	s5 := Suggestion{MetaDescription: "stable"}
	got5 := p.injectKeywordsIntoMeta(s5, nil)
	if got5.MetaDescription != "stable" {
		t.Fatalf("no keywords: got %q", got5.MetaDescription)
	}
}
