// Package contentgen turns one source item into platform variants
// (the variant generator). Grounding is code, style is the model's:
// every claim token in a variant must come from the source corpus, and a
// variant with violations is emitted flagged, never silently accepted —
// the enrichment grounding rule (internal/agent/enrichment) applied to
// content instead of products.
package contentgen

import (
	"fmt"
	"regexp"
	"strings"
)

// Source is one citable item: the calendar entry, the product sheet, the
// launch note. Everything a variant may claim lives in the corpus:
// Title + Body + Facts.
type Source struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Body  string   `json:"body"`
	Facts []string `json:"facts"`
}

// Variant is one platform's generated post under grounding review.
type Variant struct {
	SourceID   string   `json:"source_id"`
	Platform   string   `json:"platform"`
	Text       string   `json:"text"`
	Slides     []string `json:"slides,omitempty"`
	Violations []string `json:"violations,omitempty"`
}

// Platform is one output shape and its deterministic limits (the
// format-validators contract, enforced here on generation so a
// validator breach is a code defect, not a surprise).
type Platform struct {
	Name       string
	MaxRunes   int // whole-post limit (0 = none at this layer)
	MaxSlides  int
	SlideLimit int // runes per slide (0 = none)
	Guidance   string
}

// Platforms is the six-shape set from the plan row.
var Platforms = []Platform{
	{Name: "facebook", MaxRunes: 2000, Guidance: "Warm community tone. One hook line, two short paragraphs, one question at the end. AU spelling."},
	{Name: "instagram_carousel", MaxSlides: 10, SlideLimit: 125, Guidance: "Carousel: slide 1 is the hook, slides 2-9 one idea each, last slide the call to action. AU spelling."},
	{Name: "x", MaxRunes: 280, Guidance: "One post under 250 characters, no thread. Plain claims, no hashtag spam (at most two). AU spelling."},
	{Name: "tiktok_script", MaxRunes: 2200, Guidance: "Spoken script: hook in the first 3 seconds, 30-45 seconds total, stage directions in brackets. AU spelling."},
	{Name: "youtube_short", MaxRunes: 1000, Guidance: "Short script under 60 seconds: title line first (max 60 runes), then the beat-by-beat voiceover. AU spelling."},
	{Name: "linkedin", MaxRunes: 1300, Guidance: "Professional, plain English, no emoji, one insight and one takeaway line. AU spelling."},
}

var (
	// A word boundary after the unit keeps "5 great reasons" from
	// reading as 5 g; % sits outside the boundary (its own edge).
	// (?i): the corpus is norm()ed to lowercase before its scan, so the
	// units must match case-blind on both sides ("2 L" and "2 l" agree).
	numUnitRe = regexp.MustCompile(`(?i)[0-9]+(?:\.[0-9]+)?\s*(?:%|(?:mm|cm|m|kg|g|ml|L|hours?|hrs?|mins?|minutes?|secs?|seconds?|days?|years?|W|V)\b)`)
	priceRe   = regexp.MustCompile(`\$[0-9]+(?:\.[0-9]{2})?`)
	// (?i): a capitalised property is still a claim.
	propRe = regexp.MustCompile(`(?i)\b(?:waterproof|wireless|rechargeable|organic|handmade|sustainable|biodegradable|recycled|adjustable|portable|lightweight|durable|hypoallergenic|vegan|cruelty-free|non-toxic|machine-washable|ergonomic|customisable|customizable)\b`)
)

// corpusText is every citable field joined, lowercased and
// whitespace-collapsed: phrase claims ("80 g", "$49.00") are substring
// checks against it, property words are word-boundary checks against it.
func (s Source) corpusText() string {
	parts := append([]string{s.Title, s.Body}, s.Facts...)
	return strings.Join(parts, " \n ")
}

var wsRe = regexp.MustCompile(`\s+`)

func norm(text string) string {
	return wsRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(text)), " ")
}

// CheckGrounding returns one violation string per claim token in the
// variant that the source corpus does not carry: numbers with units, price
// figures and strong property words are claims; anything else is style.
func CheckGrounding(v Variant, s Source) []string {
	// The corpus's OWN claim tokens, extracted with the same three
	// regexes: a variant claim is grounded only by an EXACT normalised
	// match, never by a substring - "Only $4" must not ride through on
	// "$49.00", nor "0 g" on "80 g".
	corpus := norm(s.corpusText())
	known := map[string]bool{}
	for _, m := range numUnitRe.FindAllString(corpus, -1) {
		known[norm(m)] = true
	}
	for _, m := range priceRe.FindAllString(corpus, -1) {
		known[norm(m)] = true
	}
	for _, m := range propRe.FindAllString(corpus, -1) {
		known[norm(m)] = true
	}
	var out []string
	scan := func(text, field string) {
		for _, m := range numUnitRe.FindAllString(text, -1) {
			if !known[norm(m)] {
				out = append(out, fmt.Sprintf("measure %q in %s", m, field))
			}
		}
		for _, m := range priceRe.FindAllString(text, -1) {
			if !known[norm(m)] {
				out = append(out, fmt.Sprintf("price %q in %s", m, field))
			}
		}
		for _, m := range propRe.FindAllString(text, -1) {
			if !known[norm(m)] {
				out = append(out, fmt.Sprintf("property %q in %s", m, field))
			}
		}
	}
	scan(v.Text, "text")
	for i, sl := range v.Slides {
		scan(sl, fmt.Sprintf("slide %d", i+1))
	}
	return out
}

// CheckFormat returns deterministic format breaches for the platform
// limits, so a validator failure is caught at generation.
func CheckFormat(v Variant, p Platform) []string {
	var out []string
	if p.MaxRunes > 0 && len([]rune(v.Text)) > p.MaxRunes {
		out = append(out, fmt.Sprintf("%s: %d runes over limit %d", p.Name, len([]rune(v.Text)), p.MaxRunes))
	}
	if p.MaxSlides > 0 && len(v.Slides) > p.MaxSlides {
		out = append(out, fmt.Sprintf("%s: %d slides over limit %d", p.Name, len(v.Slides), p.MaxSlides))
	}
	if p.SlideLimit > 0 {
		for i, sl := range v.Slides {
			if n := len([]rune(sl)); n > p.SlideLimit {
				out = append(out, fmt.Sprintf("%s slide %d: %d runes over limit %d", p.Name, i+1, n, p.SlideLimit))
			}
		}
	}
	return out
}

// slideLineRe requires a SEPARATOR after a leading number (a bar, a
// dot or a parenthesis): a bare number match ate the figures of
// unnumbered lines ("100% merino wool" became "% merino wool").
// Hoisted: the pattern compiled once per line before.
var slideLineRe = regexp.MustCompile(`^\s*(?:\d+[|.)\s]|[-•])\s*(.+)$`)

// ParseSlides pulls the model's slide lines ("1| ...", "1. ..." or a
// bullet) out of a raw completion into clean slide texts.
func ParseSlides(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := slideLineRe.FindStringSubmatch(line); m != nil {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}
