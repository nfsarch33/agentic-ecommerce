package contentgen

import (
	"strings"
	"testing"
)

func fixtureSource() Source {
	return Source{
		ID:    "s1",
		Title: "Merino wool beanie",
		Body:  "A soft merino beanie for cold mornings. One size fits most.",
		Facts: []string{"100% merino wool", "machine-washable", "price $49.00", "weighs 80 g"},
	}
}

func TestGroundingCatchesInventedClaims(t *testing.T) {
	s := fixtureSource()
	v := Variant{SourceID: "s1", Platform: "facebook", Text: "A waterproof beanie for $39.00 that weighs 200 g — fully organic!"}
	got := CheckGrounding(v, s)
	joined := strings.Join(got, "; ")
	for _, want := range []string{`property "waterproof"`, `price "$39.00"`, `measure "200 g"`, `property "organic"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("violations missing %s: %v", want, got)
		}
	}
}

func TestGroundingPassesSourceClaims(t *testing.T) {
	s := fixtureSource()
	v := Variant{SourceID: "s1", Platform: "x", Text: "The 100% merino wool beanie — $49.00, machine-washable, 80 g."}
	if got := CheckGrounding(v, s); len(got) > 0 {
		t.Fatalf("source-grounded claims must pass, got: %v", got)
	}
}

func TestFormatLimitsEnforced(t *testing.T) {
	v := Variant{Platform: "x", Text: strings.Repeat("a", 281)}
	if got := CheckFormat(v, platformByName("x")); len(got) != 1 || !strings.Contains(got[0], "over limit 280") {
		t.Fatalf("x over-limit not caught: %v", got)
	}
	slides := make([]string, 11)
	for i := range slides {
		slides[i] = "ok"
	}
	v2 := Variant{Platform: "instagram_carousel", Slides: slides}
	if got := CheckFormat(v2, platformByName("instagram_carousel")); len(got) == 0 || !strings.Contains(got[0], "11 slides over limit 10") {
		t.Fatalf("carousel slide count not caught: %v", got)
	}
	long := Variant{Platform: "instagram_carousel", Slides: []string{strings.Repeat("b", 126)}}
	if got := CheckFormat(long, platformByName("instagram_carousel")); len(got) == 0 || !strings.Contains(got[0], "over limit 125") {
		t.Fatalf("slide length not caught: %v", got)
	}
}

func TestParseSlides(t *testing.T) {
	raw := "1| Hook line\n2. Second idea\n3 third slide\nrandom aside"
	got := ParseSlides(raw)
	want := []string{"Hook line", "Second idea", "third slide"}
	if len(got) != len(want) {
		t.Fatalf("slides = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slide %d = %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestPlatformSetShape(t *testing.T) {
	if len(Platforms) != 6 {
		t.Fatalf("platform set = %d shapes, want 6", len(Platforms))
	}
	for _, p := range Platforms {
		if p.Name == "" || p.Guidance == "" {
			t.Fatalf("platform %q missing name or guidance", p.Name)
		}
	}
}

func platformByName(name string) Platform {
	for _, p := range Platforms {
		if p.Name == name {
			return p
		}
	}
	panic("unknown platform " + name)
}

// The round-1 probes: a capitalised property is a claim; substring
// matches must not carry a shorter figure through on a longer corpus
// figure; a number before a word is not a measure; an unnumbered slide
// line keeps its figures.
func TestGroundingRoundOneProbes(t *testing.T) {
	s := Source{
		ID:    "s1",
		Title: "Merino wool beanie",
		Body:  "One size fits most.",
		Facts: []string{"100% merino wool", "machine-washable", "price $49.00", "weighs 80 g"},
	}

	got := CheckGrounding(Variant{SourceID: "s1", Platform: "x", Text: "Waterproof and warm."}, s)
	if len(got) != 1 || !strings.Contains(got[0], `property "Waterproof"`) {
		t.Fatalf("capitalised property must be caught: %v", got)
	}

	got = CheckGrounding(Variant{SourceID: "s1", Platform: "x", Text: "Only $4 today."}, s)
	if len(got) != 1 || !strings.Contains(got[0], `price "$4"`) {
		t.Fatalf("substring price must be caught: %v", got)
	}

	got = CheckGrounding(Variant{SourceID: "s1", Platform: "x", Text: "Just 0 g of fuss."}, s)
	if len(got) != 1 || !strings.Contains(got[0], `measure "0 g"`) {
		t.Fatalf("substring measure must be caught: %v", got)
	}

	got = CheckGrounding(Variant{SourceID: "s1", Platform: "x", Text: "5 great reasons to love it."}, s)
	if len(got) != 0 {
		t.Fatalf("a number before a word is not a measure: %v", got)
	}
}

func TestParseSlidesNumberedOnly(t *testing.T) {
	// A figure-leading unnumbered line is NOT a slide mangled into
	// "% merino wool" — the number match requires a separator, so the
	// line is left out entirely (slides are numbered; asides are asides).
	got := ParseSlides("1| Hook\n100% merino wool, really")
	if len(got) != 1 || got[0] != "Hook" {
		t.Fatalf("numbered-only contract: %v", got)
	}
}

// Round 3: the unit alternation carried case-sensitive L, W and V while
// the corpus is norm()ed to lowercase before its own scan, so the two
// scans disagreed: a lowercase invented unit rode through clean and a
// capitalised source claim false-flagged as ungrounded.
func TestGroundingCaseBlindUnits(t *testing.T) {
	s := Source{ID: "s1", Title: "Camping lantern", Body: "", Facts: []string{"60 W bulb", "holds 2 L", "12 V battery"}}

	got := CheckGrounding(Variant{SourceID: "s1", Platform: "x", Text: "A 60 W bulb."}, s)
	if len(got) != 0 {
		t.Fatalf("the source's own capitalised claim must pass: %v", got)
	}

	got = CheckGrounding(Variant{SourceID: "s1", Platform: "x", Text: "Holds 5 l of water."}, s)
	if len(got) != 1 || !strings.Contains(got[0], `measure "5 l"`) {
		t.Fatalf("a lowercase invented measure must be caught: %v", got)
	}
}

// Round 2: a percentage claim carries its number. The bare-% top-level
// alternative extracted every percent as the token "%", so any invented
// percentage passed whenever the source held any percentage at all.
func TestGroundingPercentKeepsItsNumber(t *testing.T) {
	s := Source{ID: "s1", Title: "Merino wool beanie", Body: "", Facts: []string{"100% merino wool"}}
	got := CheckGrounding(Variant{SourceID: "s1", Platform: "x", Text: "Now 50% off."}, s)
	if len(got) != 1 || !strings.Contains(got[0], `measure "50%"`) {
		t.Fatalf("invented percentage must be caught: %v", got)
	}
	if got := CheckGrounding(Variant{SourceID: "s1", Platform: "x", Text: "Truly 100% merino."}, s); len(got) != 0 {
		t.Fatalf("the source's own percentage must pass: %v", got)
	}
}
