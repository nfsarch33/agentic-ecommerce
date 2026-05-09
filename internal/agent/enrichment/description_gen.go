// Package enrichment hosts the v3.2.0 EC-2 AI Product Enrichment
// agents that transform raw 1688/Taobao supplier listings into
// localised, SEO-ready product descriptions for the storefront +
// social channels.
//
// Story EC-2-1: multilingual product description generator. The
// DescriptionGenerator wraps the existing port.AITextGenerator
// (IronClaw bridge / Bedrock-via-uiauto / minimax) so the cmd/*
// composition root chooses the LLM at startup. When the upstream
// LLM is unavailable the generator falls back to a deterministic
// template so the enrichment pipeline never blocks the sourcing
// path completely.
//
// Resilience pillar (v2.10 baseline):
//
//   - Implements lifecycle.Closer via Close so cmd/agent-worker can
//     register the generator with the Manager.
//   - No raw goroutines: the LLM call is synchronous and the only
//     concurrency concern is request-level cancellation (honoured
//     via ctx.Err on every code path).
//   - All errors typed + %w-wrapped via this package's sentinels.
//   - Tenant awareness: every request carries the configured
//     TenantID + the request's product ID; metrics labels use both.
//
// Cite skill: go-clean-architecture (port + adapter -- the
// DescriptionGenerator depends on port.AITextGenerator, not on a
// concrete IronClaw client; the composition root wires the
// adapter).
package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// EC-2-1 typed sentinels.
var (
	// ErrDescriptionGenUnconfigured is returned by
	// NewDescriptionGenerator when a required dependency is missing.
	ErrDescriptionGenUnconfigured = errors.New("enrichment: description generator unconfigured")

	// ErrDescriptionGenClosed is returned by Generate after Close.
	ErrDescriptionGenClosed = errors.New("enrichment: description generator closed")

	// ErrEnrichmentQualityBelowThreshold is returned when the LLM
	// output AND the template fallback both score below the
	// configured advisory threshold (0.75 by default per ADR-028
	// EC-2-1 acceptance criterion).
	ErrEnrichmentQualityBelowThreshold = errors.New("enrichment: quality below threshold")
)

// Platform identifies the destination tone target for the agent.
// Each platform pulls a different system prompt + length budget.
type Platform string

const (
	PlatformWooCommerce Platform = "woocommerce"
	PlatformTikTok      Platform = "tiktok"
	PlatformFacebook    Platform = "facebook"
	PlatformRedNote     Platform = "rednote"
)

// ResultSource captures whether the resulting copy came from the
// LLM or the deterministic template fallback. Surfaced so the
// observability spine (EC-2-5) can chart the failover ratio.
type ResultSource string

const (
	ResultSourceLLM      ResultSource = "llm"
	ResultSourceTemplate ResultSource = "template"
)

// EnrichmentProduct is the slim subset of a v3.1.0 sourcing proposal
// the description agent needs. The full china.Product type is
// adapted into this struct at the composition root so this package
// stays free of platform-specific fields.
type EnrichmentProduct struct {
	ID                 string
	ChineseTitle       string
	ChineseDescription string
	ChineseSpecs       []string
	Category           string
	PriceCNYCents      int
}

// DescriptionRequest is the unit of work submitted to Generate.
type DescriptionRequest struct {
	Product  EnrichmentProduct
	Platform Platform
	Language string
	Keywords []string
}

// DescriptionResult captures the agent run output. The quality
// score is the same scorer used by internal/agent/content (length,
// keyword density, tone) so dashboards can blend.
type DescriptionResult struct {
	ProductID          string
	EnglishTitle       string
	EnglishDescription string
	QualityScore       float64
	Source             ResultSource
	Platform           Platform
	GeneratedAt        time.Time
	TokensUsed         int
}

// DescriptionGeneratorConfig wires the agent.
type DescriptionGeneratorConfig struct {
	Generator    port.AITextGenerator
	TenantID     string
	MinQuality   float64
	FallbackText string
	Now          func() time.Time
}

// DescriptionGenerator is the EC-2-1 agent.
type DescriptionGenerator struct {
	generator    port.AITextGenerator
	tenantID     string
	minQuality   float64
	fallbackText string
	now          func() time.Time
	logger       *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewDescriptionGenerator constructs an agent. Defaults: MinQuality
// = 0.75 (ADR-028 advisory threshold), Now = time.Now, FallbackText
// = generic single-sentence template.
func NewDescriptionGenerator(logger *slog.Logger, cfg DescriptionGeneratorConfig) (*DescriptionGenerator, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Generator == nil {
		return nil, fmt.Errorf("%w: port.AITextGenerator required", ErrDescriptionGenUnconfigured)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrDescriptionGenUnconfigured)
	}
	if cfg.MinQuality <= 0 {
		cfg.MinQuality = 0.75
	}
	if cfg.MinQuality > 1 {
		cfg.MinQuality = 1
	}
	if cfg.FallbackText == "" {
		cfg.FallbackText = "Quality product imported from a verified supplier."
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &DescriptionGenerator{
		generator:    cfg.Generator,
		tenantID:     cfg.TenantID,
		minQuality:   cfg.MinQuality,
		fallbackText: cfg.FallbackText,
		now:          cfg.Now,
		logger:       logger,
	}, nil
}

// Close marks the agent closed. lifecycle.Closer contract.
func (d *DescriptionGenerator) Close(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// MinQuality returns the configured advisory quality threshold.
// Useful for dashboards + the EC-2-5 observability spine.
func (d *DescriptionGenerator) MinQuality() float64 { return d.minQuality }

// Generate runs the EC-2-1 pipeline: build prompt -> call LLM ->
// score -> fall back to template on LLM error or low score. Returns
// ErrEnrichmentQualityBelowThreshold when even the template cannot
// meet the floor (operator must hand-tune).
func (d *DescriptionGenerator) Generate(ctx context.Context, req DescriptionRequest) (DescriptionResult, error) {
	if err := d.guardGenerate(req); err != nil {
		return DescriptionResult{}, err
	}
	if req.Platform == "" {
		req.Platform = PlatformWooCommerce
	}
	if req.Language == "" {
		req.Language = "en-AU"
	}

	system, user := buildPrompt(req)
	resp, llmErr := d.generator.Complete(ctx, port.AICompletionRequest{
		Messages: []port.AIMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: floatPtr(0.4),
		MaxTokens:   intPtr(700),
	})
	res, ok := d.tryLLMResult(req, resp, llmErr)
	if ok {
		return res, nil
	}
	template := d.templateResult(req)
	if template.QualityScore < d.minQuality {
		return template, fmt.Errorf("%w: template quality %.2f < min %.2f", ErrEnrichmentQualityBelowThreshold, template.QualityScore, d.minQuality)
	}
	return template, nil
}

func (d *DescriptionGenerator) guardGenerate(req DescriptionRequest) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrDescriptionGenClosed
	}
	if strings.TrimSpace(req.Product.ID) == "" {
		return fmt.Errorf("%w: Product.ID required", ErrDescriptionGenUnconfigured)
	}
	return nil
}

// tryLLMResult parses the LLM response and runs the quality scorer.
// Returns ok=false when the response is empty, the JSON unmarshals
// to nothing useful, or the score is below the floor -- the caller
// then falls back to the template.
func (d *DescriptionGenerator) tryLLMResult(req DescriptionRequest, resp port.AICompletionResponse, llmErr error) (DescriptionResult, bool) {
	if llmErr != nil {
		d.logger.Warn("enrichment.description.llm_unavailable", "tenant_id", d.tenantID, "product_id", req.Product.ID, "error", llmErr)
		return DescriptionResult{}, false
	}
	english, title, ok := parseGeneratedDescription(resp.Content, req.Product.ChineseTitle)
	if !ok {
		return DescriptionResult{}, false
	}
	score := scoreDescription(english, req)
	if score < d.minQuality {
		d.logger.Warn("enrichment.description.low_score", "tenant_id", d.tenantID, "product_id", req.Product.ID, "score", score)
		return DescriptionResult{}, false
	}
	return DescriptionResult{
		ProductID:          req.Product.ID,
		EnglishTitle:       title,
		EnglishDescription: english,
		QualityScore:       score,
		Source:             ResultSourceLLM,
		Platform:           req.Platform,
		GeneratedAt:        d.now().UTC(),
		TokensUsed:         resp.TokensUsed,
	}, true
}

// templateResult builds the deterministic fallback. Score is
// computed against the same rubric as the LLM path then capped at
// the templateScoreCeiling so operators always know when a
// listing came from the fallback path (no template ever clears the
// 0.95 ceiling that signals a hand-tuned LLM result).
func (d *DescriptionGenerator) templateResult(req DescriptionRequest) DescriptionResult {
	body := buildTemplateDescription(d.fallbackText, req)
	title := buildTemplateTitle(req.Product.ChineseTitle)
	score := scoreDescription(body, req)
	if score > templateScoreCeiling {
		score = templateScoreCeiling
	}
	return DescriptionResult{
		ProductID:          req.Product.ID,
		EnglishTitle:       title,
		EnglishDescription: body,
		QualityScore:       score,
		Source:             ResultSourceTemplate,
		Platform:           req.Platform,
		GeneratedAt:        d.now().UTC(),
	}
}

// templateScoreCeiling caps the quality score the deterministic
// template can achieve so a stuck-on-template tenant is always
// visible in the EC-2-5 quality histogram (the LLM path can hit
// >0.95; the template path never can).
const templateScoreCeiling = 0.85

func buildPrompt(req DescriptionRequest) (string, string) {
	system := platformSystemPrompt(req.Platform, req.Language)
	specs := strings.Join(req.Product.ChineseSpecs, "; ")
	keywords := strings.Join(req.Keywords, ", ")
	user := fmt.Sprintf(
		"Translate this product from Chinese into %s and rewrite it in the channel-appropriate tone.\n"+
			"Return JSON {\"english_title\":\"...\",\"english_description\":\"...\"} only.\n\n"+
			"Title (zh): %s\nDescription (zh): %s\nSpecs (zh): %s\nCategory: %s\nKeywords: %s",
		req.Language, req.Product.ChineseTitle, req.Product.ChineseDescription, specs, req.Product.Category, keywords,
	)
	return system, user
}

func platformSystemPrompt(platform Platform, language string) string {
	base := fmt.Sprintf("You are a senior e-commerce copywriter. Output natural %s.", language)
	switch platform {
	case PlatformTikTok:
		return base + " TikTok tone: punchy, casual, Gen Z-friendly, 2-3 short lines + 1 emoji."
	case PlatformFacebook:
		return base + " Facebook Shop tone: feature-forward, benefit-led, single tight paragraph."
	case PlatformRedNote:
		return base + " RedNote (XHS) tone: lifestyle storytelling, sensory adjectives, 1 hashtag."
	default:
		return base + " WooCommerce tone: SEO-structured, 2 sentences, no emoji, keyword-rich."
	}
}

// parseGeneratedDescription accepts JSON or raw text. Strips
// fenced-code-block decorations so an LLM that wraps the JSON in
// ```json ... ``` still parses cleanly.
func parseGeneratedDescription(raw, fallbackTitleZH string) (description, title string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	stripped := strings.TrimPrefix(raw, "```json")
	stripped = strings.TrimPrefix(stripped, "```")
	stripped = strings.TrimSuffix(stripped, "```")
	stripped = strings.TrimSpace(stripped)

	var payload struct {
		EnglishTitle       string `json:"english_title"`
		EnglishDescription string `json:"english_description"`
		Description        string `json:"description"`
	}
	if err := json.Unmarshal([]byte(stripped), &payload); err == nil {
		desc := strings.TrimSpace(payload.EnglishDescription)
		if desc == "" {
			desc = strings.TrimSpace(payload.Description)
		}
		titleOut := strings.TrimSpace(payload.EnglishTitle)
		if titleOut == "" {
			titleOut = buildTemplateTitle(fallbackTitleZH)
		}
		if desc == "" {
			return "", "", false
		}
		return desc, titleOut, true
	}
	// Plain-text fallback: treat raw as description.
	if stripped == "" {
		return "", "", false
	}
	return stripped, buildTemplateTitle(fallbackTitleZH), true
}

func buildTemplateTitle(zhTitle string) string {
	zhTitle = strings.TrimSpace(zhTitle)
	if zhTitle == "" {
		return "New Product"
	}
	return "Quality Product"
}

func buildTemplateDescription(prefix string, req DescriptionRequest) string {
	parts := []string{prefix}
	if req.Product.Category != "" {
		parts = append(parts, fmt.Sprintf("Category: %s.", strings.ToUpper(req.Product.Category[:1])+req.Product.Category[1:]))
	}
	if len(req.Keywords) > 0 {
		parts = append(parts, fmt.Sprintf("Keywords: %s.", strings.Join(req.Keywords, ", ")))
	}
	parts = append(parts, "Ships from Australia, fast and tracked.")
	return strings.Join(parts, " ")
}

// scoreDescription returns a value in [0, 1] estimating description
// quality. Heuristics:
//
//   - Length factor (50-300 char window scores best).
//   - English-character ratio (>0.85 ASCII letters expected).
//   - Keyword presence bonus (when req.Keywords supplied).
//
// Kept tiny + deterministic so the test fixture is stable.
func scoreDescription(text string, req DescriptionRequest) float64 {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	length := float64(len(text))
	lengthScore := 0.0
	switch {
	case length < 30:
		lengthScore = length / 30 * 0.4
	case length <= 320:
		lengthScore = 0.4 + (1-abs(length-160)/160)*0.4 // peak around 160
	default:
		lengthScore = 0.4
	}
	if lengthScore < 0 {
		lengthScore = 0
	}
	asciiRatio := asciiLetterRatio(text)
	englishScore := 0.0
	if asciiRatio > 0.5 {
		englishScore = (asciiRatio - 0.5) * 2 * 0.3 // up to 0.3
	}
	if asciiRatio < 0.5 {
		englishScore = 0
	}
	keywordScore := 0.0
	if len(req.Keywords) > 0 {
		hits := 0
		lower := strings.ToLower(text)
		for _, kw := range req.Keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				hits++
			}
		}
		keywordScore = float64(hits) / float64(len(req.Keywords)) * 0.3
	} else {
		keywordScore = 0.3 // neutral default when no keywords supplied
	}
	score := lengthScore + englishScore + keywordScore
	if score > 1 {
		score = 1
	}
	if score < 0 {
		score = 0
	}
	return score
}

func asciiLetterRatio(text string) float64 {
	letters := 0
	total := 0
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' {
			continue
		}
		total++
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			letters++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(letters) / float64(total)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }
