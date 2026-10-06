// Package contentfmt validates social post payloads against the per-platform
// format limits the content engine must honour before anything is queued for
// publishing. The numbers are the business contract of record, captured from
// each platform's public limits when the plan was written; changing one is a
// content decision, not a refactor.
//
// The package is deliberately a leaf: no I/O, no clocks, no dependencies —
// validation is pure so the calendar, the publishers, and the QA gates all
// see the same answer.
package contentfmt

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Platform identifies a publishing destination.
type Platform string

const (
	Instagram Platform = "instagram"
	X         Platform = "x"
	YouTube   Platform = "youtube"
	LinkedIn  Platform = "linkedin"
)

// Media is one slide or attachment. Format is the container/extension of the
// asset as the publisher will upload it (for example "jpg", "png", "mp4").
type Media struct {
	Format string
}

// Post is the subset of a content draft the format rules can judge.
// Caption is the post text; Title and Description are the YouTube-only
// fields and are ignored by the other platforms.
type Post struct {
	Platform    Platform
	Caption     string
	Hashtags    []string
	Media       []Media
	Title       string
	Description string
}

// Violation is one broken rule. Rule names are stable identifiers (the
// contract the UI and tests quote), not prose.
type Violation struct {
	Rule   string
	Detail string
}

// xLinkRunes is the wrapped length the platform counts every link as, no
// matter its literal length.
const xLinkRunes = 23

// Validate returns one Violation per broken rule, in a stable order:
// platform recognition first, then that platform's rules in the order they
// are documented. An empty return means the post is format-clean; it says
// nothing about content quality or scheduling.
func Validate(p Post) []Violation {
	var vs []Violation
	switch p.Platform {
	case Instagram:
		vs = append(vs, instagram(p)...)
	case X:
		vs = append(vs, xRules(p)...)
	case YouTube:
		vs = append(vs, youtube(p)...)
	case LinkedIn:
		vs = append(vs, linkedin(p)...)
	default:
		vs = append(vs, Violation{Rule: "platform", Detail: fmt.Sprintf("unknown platform %q", p.Platform)})
	}
	return vs
}

// instagram: still-image slides must be JPEG, at most ten of them, with the
// caption and hashtag counts inside the platform caps.
func instagram(p Post) []Violation {
	var vs []Violation
	for i, m := range p.Media {
		// Upload tooling emits "jpg", "jpeg" and both cases; all are JPEG.
		if !strings.EqualFold(m.Format, "jpg") && !strings.EqualFold(m.Format, "jpeg") {
			vs = append(vs, Violation{Rule: "instagram.media-jpeg", Detail: fmt.Sprintf("slide %d is %q; instagram slides must be JPEG (jpg or jpeg)", i, m.Format)})
		}
	}
	if len(p.Media) > 10 {
		vs = append(vs, Violation{Rule: "instagram.slides", Detail: fmt.Sprintf("%d slides; the maximum is 10", len(p.Media))})
	}
	if utf8.RuneCountInString(p.Caption) > 2200 {
		vs = append(vs, Violation{Rule: "instagram.caption", Detail: fmt.Sprintf("caption is %d runes; the maximum is 2200", utf8.RuneCountInString(p.Caption))})
	}
	if len(p.Hashtags) > 30 {
		vs = append(vs, Violation{Rule: "instagram.hashtags", Detail: fmt.Sprintf("%d hashtags; the maximum is 30", len(p.Hashtags))})
	}
	return vs
}

// xRules: the caption must fit the post cap once every link is counted at
// its wrapped length. Counting runs per token so a link that is literally
// shorter or longer than the wrap still counts as exactly the wrap, and the
// separators between tokens are added back so the total is the platform's
// own count of the rendered post.
func xRules(p Post) []Violation {
	var vs []Violation
	// The caption is counted AS TYPED (X counts what it renders): every
	// rune of the caption, with each link token's runes replaced by the
	// fixed 23 — one pass, no separator arithmetic.
	n := utf8.RuneCountInString(p.Caption)
	for _, tok := range strings.Fields(p.Caption) {
		if strings.HasPrefix(tok, "http://") || strings.HasPrefix(tok, "https://") {
			n += xLinkRunes - utf8.RuneCountInString(tok)
		}
	}
	if n > 280 {
		vs = append(vs, Violation{Rule: "x.length", Detail: fmt.Sprintf("caption counts as %d runes (links at %d); the maximum is 280", n, xLinkRunes)})
	}
	return vs
}

// youtube: title and description caps; video uploads themselves are the
// publisher's job, not the format contract's.
func youtube(p Post) []Violation {
	var vs []Violation
	if utf8.RuneCountInString(p.Title) > 100 {
		vs = append(vs, Violation{Rule: "youtube.title", Detail: fmt.Sprintf("title is %d runes; the maximum is 100", utf8.RuneCountInString(p.Title))})
	}
	if utf8.RuneCountInString(p.Description) > 5000 {
		vs = append(vs, Violation{Rule: "youtube.description", Detail: fmt.Sprintf("description is %d runes; the maximum is 5000", utf8.RuneCountInString(p.Description))})
	}
	return vs
}

// linkedin: one body-text cap.
func linkedin(p Post) []Violation {
	var vs []Violation
	if utf8.RuneCountInString(p.Caption) > 3000 {
		vs = append(vs, Violation{Rule: "linkedin.length", Detail: fmt.Sprintf("caption is %d runes; the maximum is 3000", utf8.RuneCountInString(p.Caption))})
	}
	return vs
}
