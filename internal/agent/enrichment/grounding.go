// Grounding for enrichment drafts: a draft may cite only attributes the
// product actually has. The check is deterministic code, not an LLM
// judgement — the LLM judges style; grounding is code. A claim token (a
// colour, a material, a size, a number with a unit, a strong property word,
// or a price figure) that does not appear in the product's facts is a named
// violation, and a draft with violations never reaches the approval gate.
package enrichment

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Facts is the attribute surface a draft may cite, read from the store row.
type Facts struct {
	SKU         string
	Name        string
	Currency    string
	Price       string // the store's formatted price string ("" = none)
	Categories  []string
	Tags        []string
	AttrOptions map[string][]string // attribute name -> its offered values
}

// Violation is one ungrounded claim.
type Violation struct {
	Kind  string // colour | material | size | measure | property | price
	Token string // what the draft claimed
	Field string // which draft field carried it
}

func (v Violation) String() string {
	return fmt.Sprintf("%s %q in %s", v.Kind, v.Token, v.Field)
}

// Draft is the four-field enrichment output under grounding review.
type Draft struct {
	Description     string
	SEOTitle        string
	MetaDescription string
	AltText         string
}

// The claim lexicons are deliberately small and deterministic: a longer
// lexicon means more false refusals, and a refusal is cheap (one re-draft)
// while a hallucinated attribute in a published description is not.
var (
	colourWords = []string{
		"terracotta", "sandstone", "obsidian", "ivory", "emerald", "sapphire",
		"crimson", "amber", "lavender", "cobalt", "olive", "coral",
		"burgundy", "turquoise", "charcoal", "beige", "azure", "mahogany",
	}
	materialWords = []string{
		"aluminium", "bamboo", "cotton", "leather", "steel", "ceramic",
		"linen", "walnut", "rattan", "brass", "copper", "jute", "denim", "marble",
		"cashmere", "terracotta clay", "recycled plastic", "stainless steel",
	}
	sizeWords      = []string{"XS", "S", "M", "L", "XL", "XXL"}
	unitRe         = regexp.MustCompile(`\b(\d+(?:\.\d+)?)\s?(ml|L|cm|mm|g|kg|GB|W|m)\b`)
	priceRe        = regexp.MustCompile(`(?:\$|AUD\s?|USD\s?)(\d+(?:\.\d{2})?)`)
	propertyClaims = []string{
		"waterproof", "water-resistant", "machine-washable", "dishwasher-safe",
		"hypoallergenic", "organic", "BPA-free", "wireless", "rechargeable",
	}
)

// corpus is every fact string a claim token may match against, lower-cased.
func (f Facts) corpus() []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(strings.ToLower(s))
		if s != "" {
			out = append(out, s)
		}
	}
	add(f.Name)
	for _, c := range f.Categories {
		add(c)
	}
	for _, t := range f.Tags {
		add(t)
	}
	for _, opts := range f.AttrOptions {
		for _, o := range opts {
			add(o)
		}
	}
	return out
}

func (f Facts) hasToken(tok string) bool {
	tok = strings.ToLower(tok)
	for _, s := range f.corpus() {
		if strings.Contains(s, tok) {
			return true
		}
	}
	return false
}

// Check returns every ungrounded claim across the four draft fields. Empty
// means grounded.
func Check(d Draft, f Facts) []Violation {
	var vs []Violation
	field := func(name, text string) {
		for _, v := range checkText(text, f) {
			vs = append(vs, Violation{Kind: v.Kind, Token: v.Token, Field: name})
		}
	}
	field("description", d.Description)
	field("seo_title", d.SEOTitle)
	field("meta_description", d.MetaDescription)
	field("alt_text", d.AltText)
	return vs
}

func checkText(text string, f Facts) []Violation {
	var vs []Violation
	low := strings.ToLower(text)

	for _, c := range colourWords {
		if strings.Contains(low, c) && !f.hasToken(c) {
			vs = append(vs, Violation{Kind: "colour", Token: c})
		}
	}
	for _, m := range materialWords {
		if strings.Contains(low, strings.ToLower(m)) && !f.hasToken(m) {
			vs = append(vs, Violation{Kind: "material", Token: m})
		}
	}
	for _, s := range sizeWords {
		re := regexp.MustCompile(`\b` + s + `\b`)
		if re.MatchString(text) && !f.hasToken(s) {
			vs = append(vs, Violation{Kind: "size", Token: s})
		}
	}
	for _, m := range unitRe.FindAllStringSubmatch(text, -1) {
		tok := m[1] + " " + m[2]
		if !f.hasToken(tok) {
			vs = append(vs, Violation{Kind: "measure", Token: tok})
		}
	}
	for _, p := range propertyClaims {
		if strings.Contains(low, strings.ToLower(p)) && !f.hasToken(p) {
			vs = append(vs, Violation{Kind: "property", Token: p})
		}
	}
	// A price in the draft must restate the store's price exactly, or name
	// no price at all: a wrong price is the worst hallucination in commerce.
	for _, m := range priceRe.FindAllStringSubmatch(text, -1) {
		if f.Price == "" {
			vs = append(vs, Violation{Kind: "price", Token: m[1] + " (product has no price)"})
			continue
		}
		claimed, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		stored, err := strconv.ParseFloat(strings.TrimPrefix(f.Price, "$"), 64)
		if err != nil {
			continue // unreadable store price: refuse the comparison, not the draft
		}
		if claimed != stored {
			vs = append(vs, Violation{Kind: "price", Token: fmt.Sprintf("%s != store %s", m[1], f.Price)})
		}
	}
	return vs
}
