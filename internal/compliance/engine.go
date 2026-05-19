package compliance

import (
	"context"
	"strings"

	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	"github.com/nfsarch33/helixon-ec/internal/media"
	"github.com/nfsarch33/helixon-ec/internal/seo"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

type ProductContent struct {
	Product         catalog.Product `json:"-"`
	Keywords        []string        `json:"keywords,omitempty"`
	SEOTitle        string          `json:"seo_title,omitempty"`
	Meta            string          `json:"meta_description,omitempty"`
	SEOScoreMin     int             `json:"seo_score_min,omitempty"`
	LegalDisclaimer string          `json:"legal_disclaimer,omitempty"`
}

type Rule interface {
	Descriptor() RuleDescriptor
	Evaluate(context.Context, ProductContent) RuleResult
}

type optionalRule interface {
	IsEnabled() bool
}

type RuleDescriptor struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Severity    Severity `json:"severity"`
}

type RuleResult struct {
	ID       string   `json:"id"`
	Pass     bool     `json:"pass"`
	Score    int      `json:"score"`
	Severity Severity `json:"severity"`
	Reasons  []string `json:"reasons"`
}

type Result struct {
	Pass     bool         `json:"pass"`
	Score    int          `json:"score"`
	Reasons  []string     `json:"reasons"`
	RuleIDs  []string     `json:"rule_ids"`
	Severity Severity     `json:"severity"`
	Results  []RuleResult `json:"results"`
}

type Engine struct {
	rules []Rule
}

func NewEngine(rules []Rule) Engine {
	return Engine{rules: append([]Rule(nil), rules...)}
}

func DefaultRules() []Rule {
	return []Rule{
		requiredTitleRule{},
		descriptionLengthRule{minRunes: 50, maxRunes: catalog.MaxDescriptionRunes},
		prohibitedWordsRule{phrases: []string{"miracle cure", "guaranteed cure", "risk-free", "no side effects"}},
		imageAltTextRule{processor: media.NewProcessor(media.DefaultConstraints())},
		seoMinimumScoreRule{optimizer: seo.NewOptimizer(), defaultMin: 70},
		legalDisclaimerRule{},
	}
}

func (e Engine) Rules() []RuleDescriptor {
	out := make([]RuleDescriptor, 0, len(e.rules))
	for _, rule := range e.rules {
		out = append(out, rule.Descriptor())
	}
	return out
}

func (e Engine) Evaluate(ctx context.Context, content ProductContent) Result {
	if err := ctx.Err(); err != nil {
		return Result{
			Pass:     false,
			Score:    0,
			Reasons:  []string{err.Error()},
			RuleIDs:  []string{"engine_context"},
			Severity: SeverityCritical,
			Results: []RuleResult{{
				ID:       "engine_context",
				Pass:     false,
				Score:    0,
				Severity: SeverityCritical,
				Reasons:  []string{err.Error()},
			}},
		}
	}
	results := make([]RuleResult, 0, len(e.rules))
	for _, rule := range e.rules {
		if optional, ok := rule.(optionalRule); ok && !optional.IsEnabled() {
			continue
		}
		results = append(results, rule.Evaluate(ctx, content))
	}
	return aggregate(results)
}

func aggregate(results []RuleResult) Result {
	if len(results) == 0 {
		return Result{Pass: true, Score: 100, Severity: SeverityInfo, Results: []RuleResult{}}
	}
	pass := true
	scoreTotal := 0
	reasons := make([]string, 0)
	ruleIDs := make([]string, 0)
	severity := SeverityInfo
	for _, result := range results {
		scoreTotal += result.Score
		if result.Pass {
			continue
		}
		pass = false
		ruleIDs = append(ruleIDs, result.ID)
		reasons = append(reasons, result.Reasons...)
		severity = maxSeverity(severity, result.Severity)
	}
	return Result{
		Pass:     pass,
		Score:    scoreTotal / len(results),
		Reasons:  reasons,
		RuleIDs:  ruleIDs,
		Severity: severity,
		Results:  results,
	}
}

type requiredTitleRule struct{}

func (requiredTitleRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{ID: "required_title", Description: "Product title must be present before publish.", Severity: SeverityCritical}
}

func (r requiredTitleRule) Evaluate(_ context.Context, content ProductContent) RuleResult {
	if strings.TrimSpace(content.Product.Title()) == "" {
		return fail(r.Descriptor(), "product title is required")
	}
	return pass(r.Descriptor())
}

type descriptionLengthRule struct {
	minRunes int
	maxRunes int
}

func (r descriptionLengthRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{ID: "description_length", Description: "Product description must be useful and within length limits.", Severity: SeverityError}
}

func (r descriptionLengthRule) Evaluate(_ context.Context, content ProductContent) RuleResult {
	description := strings.TrimSpace(content.Product.Description())
	length := len([]rune(description))
	switch {
	case length < r.minRunes:
		return fail(r.Descriptor(), "product description is too short")
	case length > r.maxRunes:
		return fail(r.Descriptor(), "product description is too long")
	default:
		return pass(r.Descriptor())
	}
}

type prohibitedWordsRule struct {
	phrases []string
}

func (r prohibitedWordsRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{ID: "prohibited_words", Description: "Product copy must not contain prohibited medical or deceptive claims.", Severity: SeverityCritical}
}

func (r prohibitedWordsRule) Evaluate(_ context.Context, content ProductContent) RuleResult {
	text := strings.ToLower(strings.Join([]string{content.Product.Title(), content.Product.Description(), content.SEOTitle, content.Meta}, " "))
	for _, phrase := range r.phrases {
		if strings.Contains(text, phrase) {
			return fail(r.Descriptor(), "prohibited phrase present: "+phrase)
		}
	}
	return pass(r.Descriptor())
}

type imageAltTextRule struct {
	processor media.Processor
}

func (r imageAltTextRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{ID: "image_alt_text", Description: "Every product image must include useful alt text.", Severity: SeverityError}
}

func (r imageAltTextRule) Evaluate(_ context.Context, content ProductContent) RuleResult {
	images := content.Product.Images()
	if len(images) == 0 {
		return fail(r.Descriptor(), "at least one product image is required")
	}
	reasons := make([]string, 0)
	for _, image := range images {
		result := r.processor.Validate(media.ImageMetadata{
			URL:         image.URL,
			AltText:     image.Alt,
			ProductName: content.Product.Title(),
		})
		if !result.Pass {
			for _, reason := range result.Reasons {
				reasons = append(reasons, reason.Message)
			}
		}
	}
	if len(reasons) > 0 {
		desc := r.Descriptor()
		return RuleResult{ID: desc.ID, Pass: false, Score: 0, Severity: desc.Severity, Reasons: reasons}
	}
	return pass(r.Descriptor())
}

type seoMinimumScoreRule struct {
	optimizer  seo.Optimizer
	defaultMin int
}

func (r seoMinimumScoreRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{ID: "seo_minimum_score", Description: "SEO title, meta description, slug, and keyword density must meet minimum quality.", Severity: SeverityError}
}

func (r seoMinimumScoreRule) Evaluate(_ context.Context, content ProductContent) RuleResult {
	minScore := content.SEOScoreMin
	if minScore == 0 {
		minScore = r.defaultMin
	}
	suggestion := r.optimizer.Validate(seo.Suggestion{
		Title:           firstNonEmpty(content.SEOTitle, content.Product.Title()),
		MetaDescription: firstNonEmpty(content.Meta, content.Product.Description()),
		Slug:            content.Product.Slug(),
		KeywordDensity:  seo.KeywordDensity(content.Product.Description()+" "+content.SEOTitle+" "+content.Meta, content.Keywords),
	})
	if suggestion.Score < minScore || !suggestion.Pass {
		reasons := append([]string{}, suggestion.Reasons...)
		reasons = append(reasons, "seo score below minimum")
		desc := r.Descriptor()
		return RuleResult{ID: desc.ID, Pass: false, Score: suggestion.Score, Severity: desc.Severity, Reasons: reasons}
	}
	return RuleResult{ID: r.Descriptor().ID, Pass: true, Score: suggestion.Score, Severity: r.Descriptor().Severity, Reasons: []string{}}
}

type legalDisclaimerRule struct{}

func (legalDisclaimerRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{ID: "legal_disclaimer", Description: "Regulated product claims must include an appropriate disclaimer.", Severity: SeverityCritical}
}

func (r legalDisclaimerRule) Evaluate(_ context.Context, content ProductContent) RuleResult {
	if !requiresDisclaimer(content) {
		return pass(r.Descriptor())
	}
	text := strings.ToLower(strings.Join([]string{content.Product.Description(), content.Meta, content.LegalDisclaimer}, " "))
	for _, marker := range []string{"not medical advice", "consult a professional", "consult your doctor", "disclaimer"} {
		if strings.Contains(text, marker) {
			return pass(r.Descriptor())
		}
	}
	return fail(r.Descriptor(), "legal disclaimer is required for regulated claims")
}

func requiresDisclaimer(content ProductContent) bool {
	text := strings.ToLower(strings.Join([]string{content.Product.Title(), content.Product.Description(), content.Meta}, " "))
	for _, marker := range []string{"cure", "treat disease", "medical", "supplement", "therapeutic"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func pass(desc RuleDescriptor) RuleResult {
	return RuleResult{ID: desc.ID, Pass: true, Score: 100, Severity: desc.Severity, Reasons: []string{}}
}

func fail(desc RuleDescriptor, reason string) RuleResult {
	return RuleResult{ID: desc.ID, Pass: false, Score: 0, Severity: desc.Severity, Reasons: []string{reason}}
}

func maxSeverity(a, b Severity) Severity {
	if severityRank(b) > severityRank(a) {
		return b
	}
	return a
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 4
	case SeverityError:
		return 3
	case SeverityWarning:
		return 2
	default:
		return 1
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
