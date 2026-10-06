package contentfmt

import (
	"fmt"
	"strings"
	"testing"
)

// The contract of record for v18870-3. Every rule the plan names has both a
// pass case and a fail case on the other side of the same branch, so the
// table doubles as the branch-coverage map: 100% of Validate's branches are
// exercised by rows here, and go tool cover -func must report 100.0%.
func TestValidateContract(t *testing.T) {
	tt := []struct {
		name string
		post Post
		want []string // Rule ids in order; empty means format-clean
	}{
		{"instagram clean at the caps", Post{
			Platform: Instagram,
			Caption:  strings.Repeat("a", 2200),
			Hashtags: fill(30, "#tag"),
			Media:    fill(10, Media{Format: "jpg"}),
		}, nil},
		{"instagram png slide", Post{
			Platform: Instagram,
			Media:    []Media{{Format: "jpg"}, {Format: "png"}},
		}, []string{"instagram.media-jpeg"}},
		{"instagram eleven slides", Post{
			Platform: Instagram,
			Media:    fill(11, Media{Format: "jpg"}),
		}, []string{"instagram.slides"}},
		{"instagram caption over", Post{
			Platform: Instagram,
			Caption:  strings.Repeat("a", 2201),
		}, []string{"instagram.caption"}},
		{"instagram hashtags over", Post{
			Platform: Instagram,
			Hashtags: fill(31, "#tag"),
		}, []string{"instagram.hashtags"}},
		{"instagram every rule broken at once", Post{
			Platform: Instagram,
			Media:    []Media{{Format: "png"}, {Format: "png"}, {Format: "png"}},
			Hashtags: fill(31, "#tag"),
			Caption:  strings.Repeat("a", 2201),
		}, []string{"instagram.media-jpeg", "instagram.media-jpeg", "instagram.media-jpeg", "instagram.caption", "instagram.hashtags"}},

		{"x clean at the cap with a wrapped link", Post{
			Platform: X,
			Caption:  strings.Repeat("a", 256) + " https://example.com/a/very/long/path/that/wraps", // 256 + separator + 23 = 280
		}, nil},
		{"x over the cap only because the link wraps", Post{
			Platform: X,
			Caption:  strings.Repeat("a", 257) + " https://example.com/a/very/long/path/that/wraps", // 257 + separator + 23 = 281
		}, []string{"x.length"}},
		{"x separators count: 140 two-rune words is 419 counted, not 280", Post{
			Platform: X,
			Caption:  strings.Repeat("aa ", 139) + "aa", // tokens 280, separators 139
		}, []string{"x.length"}},
		{"x clean just under with short link", Post{
			Platform: X,
			Caption:  strings.Repeat("b", 256) + " http://x.example",
		}, nil},
		{"x http link also wraps to 23", Post{
			Platform: X,
			Caption:  strings.Repeat("b", 257) + " http://x.example",
		}, []string{"x.length"}},

		{"youtube clean at the caps", Post{
			Platform:    YouTube,
			Title:       strings.Repeat("t", 100),
			Description: strings.Repeat("d", 5000),
		}, nil},
		{"youtube title over", Post{
			Platform: YouTube,
			Title:    strings.Repeat("t", 101),
		}, []string{"youtube.title"}},
		{"youtube description over", Post{
			Platform:    YouTube,
			Description: strings.Repeat("d", 5001),
		}, []string{"youtube.description"}},
		{"youtube both over, title reported first", Post{
			Platform:    YouTube,
			Title:       strings.Repeat("t", 101),
			Description: strings.Repeat("d", 5001),
		}, []string{"youtube.title", "youtube.description"}},

		{"linkedin clean at the cap", Post{
			Platform: LinkedIn,
			Caption:  strings.Repeat("l", 3000),
		}, nil},
		{"linkedin over", Post{
			Platform: LinkedIn,
			Caption:  strings.Repeat("l", 3001),
		}, []string{"linkedin.length"}},

		{"unicode counts runes not bytes", Post{
			Platform: LinkedIn,
			Caption:  strings.Repeat("世", 3001), // 9003 bytes, 3001 runes
		}, []string{"linkedin.length"}},

		{"unknown platform fails closed", Post{
			Platform: "tiktok",
		}, []string{"platform"}},

		{"empty platform fails closed", Post{
			Platform: "",
		}, []string{"platform"}},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got := Validate(tc.post)
			var rules []string
			for _, v := range got {
				rules = append(rules, v.Rule)
			}
			if fmt.Sprint(rules) != fmt.Sprint(tc.want) {
				t.Fatalf("Validate rules = %v, want %v", rules, tc.want)
			}
		})
	}
}

func fill[T any](n int, v T) []T {
	out := make([]T, n)
	for i := range out {
		out[i] = v
	}
	return out
}
