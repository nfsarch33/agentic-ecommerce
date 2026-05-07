package seo

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	maxTitleRunes       = 60
	preferredTitleRunes = 45
	maxMetaRunes        = 155
)

var (
	spaceRe = regexp.MustCompile(`\s+`)
	slugRe  = regexp.MustCompile(`[^a-z0-9-]+`)
)

type Input struct {
	Title       string
	Description string
	Keywords    []string
}

type Suggestion struct {
	Title           string             `json:"title"`
	MetaDescription string             `json:"meta_description"`
	Slug            string             `json:"slug"`
	Score           int                `json:"score"`
	KeywordDensity  map[string]float64 `json:"keyword_density"`
	Pass            bool               `json:"pass"`
	Reasons         []string           `json:"reasons"`
}

type Optimizer struct{}

func NewOptimizer() Optimizer {
	return Optimizer{}
}

func (Optimizer) Suggest(input Input) Suggestion {
	title := normalizeSpaces(input.Title)
	meta := normalizeSpaces(input.Description)
	if meta == "" {
		meta = title
	}
	meta = truncateSentence(meta, maxMetaRunes)
	suggestion := Suggestion{
		Title:           truncateSentence(title, maxTitleRunes),
		MetaDescription: meta,
		Slug:            OptimizeSlug(title),
		KeywordDensity:  KeywordDensity(strings.Join([]string{title, meta}, " "), input.Keywords),
	}
	return NewOptimizer().Validate(suggestion)
}

func (Optimizer) Validate(s Suggestion) Suggestion {
	s.Title = normalizeSpaces(s.Title)
	s.MetaDescription = normalizeSpaces(s.MetaDescription)
	s.Slug = OptimizeSlug(s.Slug)
	if s.KeywordDensity == nil {
		s.KeywordDensity = map[string]float64{}
	}

	score := 100
	reasons := make([]string, 0)
	titleLen := runeLen(s.Title)
	switch {
	case titleLen == 0:
		score -= 35
		reasons = append(reasons, "seo title is required")
	case titleLen > maxTitleRunes:
		score -= 25
		reasons = append(reasons, "seo title exceeds 60 characters")
	case titleLen > preferredTitleRunes:
		score -= 7
	}

	metaLen := runeLen(s.MetaDescription)
	switch {
	case metaLen == 0:
		score -= 30
		reasons = append(reasons, "meta description is required")
	case metaLen > maxMetaRunes:
		score -= 25
		reasons = append(reasons, "meta description exceeds 155 characters")
	case metaLen < 50:
		score -= 8
	}

	if s.Slug == "" {
		score -= 15
		reasons = append(reasons, "slug is required")
	}
	if score < 0 {
		score = 0
	}
	s.Score = score
	s.Pass = score >= 80
	s.Reasons = reasons
	return s
}

func OptimizeSlug(value string) string {
	s := strings.ToLower(normalizeSpaces(value))
	s = strings.ReplaceAll(s, " ", "-")
	s = slugRe.ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

func KeywordDensity(text string, keywords []string) map[string]float64 {
	out := make(map[string]float64, len(keywords))
	words := wordsOf(text)
	if len(words) == 0 {
		for _, keyword := range normalizedKeywords(keywords) {
			out[keyword] = 0
		}
		return out
	}
	lower := strings.ToLower(text)
	for _, keyword := range normalizedKeywords(keywords) {
		out[keyword] = round2(float64(strings.Count(lower, keyword)) / float64(len(words)) * 100)
	}
	return out
}

func normalizedKeywords(keywords []string) []string {
	seen := make(map[string]struct{}, len(keywords))
	out := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		key := strings.ToLower(normalizeSpaces(keyword))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func truncateSentence(value string, maxRunes int) string {
	value = normalizeSpaces(value)
	if runeLen(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	cut := strings.TrimSpace(string(runes[:maxRunes]))
	if idx := strings.LastIndexAny(cut, ".!?"); idx >= 40 {
		return strings.TrimSpace(cut[:idx+1])
	}
	if idx := strings.LastIndex(cut, " "); idx >= 30 {
		cut = strings.TrimSpace(cut[:idx])
	}
	return strings.TrimRight(cut, ".,;:-")
}

func normalizeSpaces(value string) string {
	return spaceRe.ReplaceAllString(strings.TrimSpace(value), " ")
}

func wordsOf(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	words := fields[:0]
	for _, field := range fields {
		if field != "" {
			words = append(words, field)
		}
	}
	return words
}

func runeLen(value string) int {
	return len([]rune(value))
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
