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
