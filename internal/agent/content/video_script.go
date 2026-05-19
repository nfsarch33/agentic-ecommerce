// File scope: v3.4.0 EC-5-1 AI video script writer.
//
// Generates a structured short-form video script (hook -> problem
// -> product demo -> CTA) per platform variant. The generator
// wraps port.AITextGenerator (IronClaw bridge / Bedrock-via-uiauto
// / minimax) so the cmd/* composition root chooses the LLM at
// startup. When the upstream LLM is unavailable the generator
// falls back to a deterministic template so the content pipeline
// never blocks the channel router.
//
// Platform variants (tone + duration enforced via templates +
// quality scorer time-budget check):
//   - TikTok: punchy / casual, 60s (~150-180 spoken words)
//   - RedNote: lifestyle storytelling, 60s
//   - Facebook: feature-forward, 60s
//   - Instagram Reels: aesthetic-first, 30s (~75-90 spoken words)
//
// Resilience pillar (v2.10 baseline):
//   - Implements lifecycle.Closer.
//   - No raw goroutines: the LLM call is synchronous; the only
//     concurrency concern is request-level cancellation (honoured
//     via ctx on every code path).
//   - All errors typed + %w-wrapped via this package's sentinels.
//   - Tenant awareness: every request carries the configured
//     TenantID + the request's product ID; metrics labels use
//     both.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4): split into Generate (envelope), tryLLMScript
// (parse + score), templateScript (deterministic fallback),
// scoreVideoScript (rubric). Per-function cyclomatic stays under 6.
//
// Cite skill: go-clean-architecture (port + adapter -- the
// VideoScriptGenerator depends on port.AITextGenerator).
package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// EC-5-1 typed sentinels.
var (
	// ErrVideoScriptGenUnconfigured is returned by
	// NewVideoScriptGenerator when a required dependency is missing.
	ErrVideoScriptGenUnconfigured = errors.New("content: video script generator unconfigured")

	// ErrVideoScriptGenClosed is returned by Generate after Close.
	ErrVideoScriptGenClosed = errors.New("content: video script generator closed")

	// ErrVideoScriptQualityBelowThreshold is returned when the LLM
	// output AND the template fallback both score below the
	// configured advisory threshold (0.75 by default per the plan
	// EC-5-1 acceptance criterion).
	ErrVideoScriptQualityBelowThreshold = errors.New("content: video script quality below threshold")
)

// VideoPlatform identifies the destination tone + duration target.
// Each platform pulls a different system prompt + word budget.
type VideoPlatform string

const (
	VideoPlatformTikTok        VideoPlatform = "tiktok"
	VideoPlatformRedNote       VideoPlatform = "rednote"
	VideoPlatformFacebook      VideoPlatform = "facebook"
	VideoPlatformInstagramReel VideoPlatform = "instagram-reels"
)

// VideoScriptSource captures whether the resulting script came from
// the LLM or the deterministic template fallback. Surfaced so the
// observability spine can chart the failover ratio.
type VideoScriptSource string

const (
	VideoScriptSourceLLM      VideoScriptSource = "llm"
	VideoScriptSourceTemplate VideoScriptSource = "template"
)

// VideoScriptDuration returns the target duration in seconds for a
// platform. Used by the scorer to enforce the time budget.
func VideoScriptDuration(platform VideoPlatform) int {
	if platform == VideoPlatformInstagramReel {
		return 30
	}
	return 60
}

// VideoScript is the structured output of the EC-5-1 generator.
// The four sections map directly to the Hook -> Problem -> Demo
// -> CTA narrative arc validated by the rubric.
type VideoScript struct {
	Hook         string   `json:"hook"`
	Problem      string   `json:"problem"`
	ProductDemo  string   `json:"product_demo"`
	CTA          string   `json:"cta"`
	Subtitles    []string `json:"subtitles,omitempty"`
	BrandingNote string   `json:"branding_note,omitempty"`
}

// WordCount returns the cumulative word count across the four
// narrative sections (subtitles + branding note excluded since
// those are visual aids, not spoken script).
func (s VideoScript) WordCount() int {
	return countWords(s.Hook) + countWords(s.Problem) + countWords(s.ProductDemo) + countWords(s.CTA)
}

// VideoScriptRequest is the unit of work submitted to Generate.
type VideoScriptRequest struct {
	Product     ProductInfo
	Platform    VideoPlatform
	Language    string
	KeyFeatures []string
	Keywords    []string
	BrandVoice  string // optional override hint for the LLM prompt
}

// VideoScriptResult captures the agent run output.
type VideoScriptResult struct {
	ProductID    string
	Script       VideoScript
	Platform     VideoPlatform
	DurationSec  int
	WordCount    int
	QualityScore float64
	Source       VideoScriptSource
	GeneratedAt  time.Time
	TokensUsed   int
}

// VideoScriptMetrics is the small port the EC-5-1 generator emits
// counters + histograms through.
type VideoScriptMetrics interface {
	RecordVideoScript(tenantID, platform, source string)
	ObserveVideoScriptQuality(platform string, score float64)
}

// VideoScriptGeneratorConfig wires the generator.
type VideoScriptGeneratorConfig struct {
	Generator  port.AITextGenerator
	TenantID   string
	MinQuality float64
	Now        func() time.Time
	Metrics    VideoScriptMetrics
}

// VideoScriptGenerator is the EC-5-1 agent.
type VideoScriptGenerator struct {
	generator  port.AITextGenerator
	tenantID   string
	minQuality float64
	now        func() time.Time
	logger     *slog.Logger
	metrics    VideoScriptMetrics

	mu     sync.Mutex
	closed bool
}

// NewVideoScriptGenerator constructs an agent. Defaults: MinQuality
// = 0.75 (advisory threshold), Now = time.Now.
func NewVideoScriptGenerator(logger *slog.Logger, cfg VideoScriptGeneratorConfig) (*VideoScriptGenerator, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Generator == nil {
		return nil, fmt.Errorf("%w: port.AITextGenerator required", ErrVideoScriptGenUnconfigured)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrVideoScriptGenUnconfigured)
	}
	if cfg.MinQuality <= 0 {
		cfg.MinQuality = 0.75
	}
	if cfg.MinQuality > 1 {
		cfg.MinQuality = 1
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &VideoScriptGenerator{
		generator:  cfg.Generator,
		tenantID:   cfg.TenantID,
		minQuality: cfg.MinQuality,
		now:        cfg.Now,
		logger:     logger,
		metrics:    cfg.Metrics,
	}, nil
}

// Close marks the agent closed. lifecycle.Closer contract.
func (g *VideoScriptGenerator) Close(_ context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	return nil
}

// MinQuality returns the configured advisory quality threshold.
func (g *VideoScriptGenerator) MinQuality() float64 { return g.minQuality }

// Generate runs the EC-5-1 pipeline: build platform-specific
// prompt -> call LLM -> score -> fall back to template on LLM
// error or low score.
func (g *VideoScriptGenerator) Generate(ctx context.Context, req VideoScriptRequest) (VideoScriptResult, error) {
	if err := g.guardGenerate(req); err != nil {
		return VideoScriptResult{}, err
	}
	if req.Platform == "" {
		req.Platform = VideoPlatformTikTok
	}
	if req.Language == "" {
		req.Language = "en-AU"
	}
	system, user := buildVideoScriptPrompt(req)
	resp, llmErr := g.generator.Complete(ctx, port.AICompletionRequest{
		Messages: []port.AIMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: floatPtrLocal(0.5),
		MaxTokens:   intPtrLocal(900),
	})
	res, ok := g.tryLLMScript(req, resp, llmErr)
	if ok {
		g.recordSource(req.Platform, VideoScriptSourceLLM, res.QualityScore)
		return res, nil
	}
	template := g.templateScript(req)
	if template.QualityScore < g.minQuality {
		g.recordSource(req.Platform, VideoScriptSourceTemplate, template.QualityScore)
		return template, fmt.Errorf("%w: template quality %.2f < min %.2f", ErrVideoScriptQualityBelowThreshold, template.QualityScore, g.minQuality)
	}
	g.recordSource(req.Platform, VideoScriptSourceTemplate, template.QualityScore)
	return template, nil
}

func (g *VideoScriptGenerator) guardGenerate(req VideoScriptRequest) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return ErrVideoScriptGenClosed
	}
	if strings.TrimSpace(req.Product.ID) == "" {
		return fmt.Errorf("%w: Product.ID required", ErrVideoScriptGenUnconfigured)
	}
	if strings.TrimSpace(req.Product.Title) == "" {
		return fmt.Errorf("%w: Product.Title required", ErrVideoScriptGenUnconfigured)
	}
	return nil
}

// tryLLMScript parses the LLM response and runs the video-script
// quality scorer. Returns ok=false when the response is empty,
// the JSON unmarshals to nothing useful, or the score is below
// the floor -- the caller then falls back to the template.
func (g *VideoScriptGenerator) tryLLMScript(req VideoScriptRequest, resp port.AICompletionResponse, llmErr error) (VideoScriptResult, bool) {
	if llmErr != nil {
		g.logger.Warn("content.video_script.llm_unavailable", "tenant_id", g.tenantID, "product_id", req.Product.ID, "error", llmErr)
		return VideoScriptResult{}, false
	}
	script, ok := parseVideoScriptResponse(resp.Content)
	if !ok {
		return VideoScriptResult{}, false
	}
	score := scoreVideoScript(script, req)
	if score < g.minQuality {
		g.logger.Warn("content.video_script.low_score", "tenant_id", g.tenantID, "product_id", req.Product.ID, "platform", string(req.Platform), "score", score)
		return VideoScriptResult{}, false
	}
	return VideoScriptResult{
		ProductID:    req.Product.ID,
		Script:       script,
		Platform:     req.Platform,
		DurationSec:  VideoScriptDuration(req.Platform),
		WordCount:    script.WordCount(),
		QualityScore: score,
		Source:       VideoScriptSourceLLM,
		GeneratedAt:  g.now().UTC(),
		TokensUsed:   resp.TokensUsed,
	}, true
}

// templateScript builds the deterministic fallback. Score is
// computed against the same rubric as the LLM path then capped at
// videoScriptTemplateScoreCeiling so operators always know when
// the template path won.
func (g *VideoScriptGenerator) templateScript(req VideoScriptRequest) VideoScriptResult {
	script := buildTemplateVideoScript(req)
	score := scoreVideoScript(script, req)
	if score > videoScriptTemplateScoreCeiling {
		score = videoScriptTemplateScoreCeiling
	}
	return VideoScriptResult{
		ProductID:    req.Product.ID,
		Script:       script,
		Platform:     req.Platform,
		DurationSec:  VideoScriptDuration(req.Platform),
		WordCount:    script.WordCount(),
		QualityScore: score,
		Source:       VideoScriptSourceTemplate,
		GeneratedAt:  g.now().UTC(),
	}
}

func (g *VideoScriptGenerator) recordSource(platform VideoPlatform, source VideoScriptSource, score float64) {
	if g.metrics == nil {
		return
	}
	g.metrics.RecordVideoScript(g.tenantID, string(platform), string(source))
	g.metrics.ObserveVideoScriptQuality(string(platform), score)
}

// videoScriptTemplateScoreCeiling caps the quality score the
// deterministic template can achieve so a stuck-on-template tenant
// stays visible in the EC-2-5-style histogram.
const videoScriptTemplateScoreCeiling = 0.85

// buildVideoScriptPrompt returns the system + user prompt for a
// platform-specific video script.
func buildVideoScriptPrompt(req VideoScriptRequest) (string, string) {
	durationSec := VideoScriptDuration(req.Platform)
	system := videoSystemPrompt(req.Platform, req.Language, durationSec)
	keywords := strings.Join(req.Keywords, ", ")
	features := strings.Join(req.KeyFeatures, "; ")
	user := fmt.Sprintf(
		"Write a %d-second %s video script for the following product. "+
			"Return JSON {\"hook\":\"...\",\"problem\":\"...\",\"product_demo\":\"...\",\"cta\":\"...\",\"subtitles\":[\"...\"],\"branding_note\":\"...\"} only.\n\n"+
			"Product: %s\nDescription: %s\nKey features: %s\nKeywords: %s\nBrand voice override: %s",
		durationSec, req.Platform, req.Product.Title, req.Product.Description, features, keywords, req.BrandVoice,
	)
	return system, user
}

// videoSystemPrompt selects the platform-specific tone preamble.
func videoSystemPrompt(platform VideoPlatform, language string, durationSec int) string {
	base := fmt.Sprintf("You are a senior social-video copywriter. Output natural %s. Target duration: %d seconds (~%d-%d spoken words).", language, durationSec, durationSec*5/2, durationSec*3)
	switch platform {
	case VideoPlatformTikTok:
		return base + " TikTok tone: punchy, casual, high-energy hook in the first 2 seconds. End with a clear CTA."
	case VideoPlatformRedNote:
		return base + " RedNote (XHS) tone: lifestyle storytelling, sensory adjectives, soft-sell CTA. Include 1 hashtag in the branding_note."
	case VideoPlatformFacebook:
		return base + " Facebook tone: feature-forward, benefit-led, single tight narrative. CTA = 'Shop now'."
	case VideoPlatformInstagramReel:
		return base + " Instagram Reels tone: aesthetic-first, visual cues in subtitles, fast cuts. CTA = swipe up."
	default:
		return base + " Generic tone: professional, structured, balanced."
	}
}

// parseVideoScriptResponse accepts JSON or fenced-code-block JSON
// and decodes it into a VideoScript. Returns ok=false on any
// missing required section.
func parseVideoScriptResponse(raw string) (VideoScript, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return VideoScript{}, false
	}
	stripped := strings.TrimPrefix(raw, "```json")
	stripped = strings.TrimPrefix(stripped, "```")
	stripped = strings.TrimSuffix(stripped, "```")
	stripped = strings.TrimSpace(stripped)

	var script VideoScript
	if err := json.Unmarshal([]byte(stripped), &script); err != nil {
		return VideoScript{}, false
	}
	if script.Hook == "" || script.Problem == "" || script.ProductDemo == "" || script.CTA == "" {
		return VideoScript{}, false
	}
	return script, true
}

// buildTemplateVideoScript constructs the deterministic fallback
// script for a platform.
func buildTemplateVideoScript(req VideoScriptRequest) VideoScript {
	feature := "the standout feature"
	if len(req.KeyFeatures) > 0 {
		feature = req.KeyFeatures[0]
	}
	cta := "Tap to shop now."
	if req.Platform == VideoPlatformInstagramReel {
		cta = "Swipe up to shop."
	}
	if req.Platform == VideoPlatformRedNote {
		cta = "点击链接，看种草清单。"
	}
	return VideoScript{
		Hook:         fmt.Sprintf("Stop scrolling -- meet the %s.", req.Product.Title),
		Problem:      "Tired of products that promise the moon and deliver less? We hear you.",
		ProductDemo:  fmt.Sprintf("Quick demo: %s. Notice %s in action -- crisp, reliable, ready for your routine.", req.Product.Title, feature),
		CTA:          cta,
		Subtitles:    templateSubtitles(req),
		BrandingNote: templateBrandingNote(req.Platform),
	}
}

func templateSubtitles(req VideoScriptRequest) []string {
	subs := []string{
		"00:00 - HOOK: " + req.Product.Title,
		"00:08 - PROBLEM",
		"00:18 - DEMO",
		"00:48 - CTA",
	}
	if req.Platform == VideoPlatformInstagramReel {
		return []string{
			"00:00 - HOOK: " + req.Product.Title,
			"00:05 - DEMO",
			"00:22 - CTA",
		}
	}
	return subs
}

func templateBrandingNote(platform VideoPlatform) string {
	switch platform {
	case VideoPlatformTikTok:
		return "#fyp #shopping"
	case VideoPlatformRedNote:
		return "#好物推荐"
	case VideoPlatformFacebook:
		return "Available now in our store"
	case VideoPlatformInstagramReel:
		return "@brand_handle"
	default:
		return ""
	}
}

// scoreVideoScript returns a value in [0, 1] estimating script
// quality. Heuristics:
//
//   - All four sections present (Hook + Problem + ProductDemo + CTA): +0.4
//   - Word count within +/- 30% of target word budget: +0.3
//   - At least one keyword present (or no keywords required): +0.15
//   - CTA contains an action verb: +0.15
//
// Decomposition: each rubric dimension is its own helper so this
// composer stays well under the v3.1.0 sentrux complex_fn ceiling.
// Kept tiny + deterministic so the failover test fixture is stable.
func scoreVideoScript(script VideoScript, req VideoScriptRequest) float64 {
	score := videoStructureBonus(script) + videoDurationBonus(script, req.Platform) + videoKeywordBonus(script, req.Keywords) + videoCTABonus(script.CTA)
	return clampUnit(score)
}

// videoStructureBonus awards 0.4 when all four narrative sections
// are non-empty.
func videoStructureBonus(script VideoScript) float64 {
	if script.Hook != "" && script.Problem != "" && script.ProductDemo != "" && script.CTA != "" {
		return 0.4
	}
	return 0
}

// videoDurationBonus awards 0.3 when the script word count is
// within +/- 30% of the platform's target spoken-word budget.
func videoDurationBonus(script VideoScript, platform VideoPlatform) float64 {
	target := wordsForDuration(VideoScriptDuration(platform))
	if withinPct(script.WordCount(), target, 30) {
		return 0.3
	}
	return 0
}

// videoKeywordBonus awards 0.15 when at least one keyword is
// present (or no keywords were configured).
func videoKeywordBonus(script VideoScript, keywords []string) float64 {
	if hasKeyword(script, keywords) {
		return 0.15
	}
	return 0
}

// videoCTABonus awards 0.15 when the CTA contains an action verb.
func videoCTABonus(cta string) float64 {
	if hasActionVerb(cta) {
		return 0.15
	}
	return 0
}

// clampUnit clamps a score to the [0, 1] interval.
func clampUnit(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// wordsForDuration returns the rough spoken-word target for a
// given duration in seconds (~2.7 words/second).
func wordsForDuration(durationSec int) int {
	return durationSec * 27 / 10
}

// withinPct reports whether got is within pct% of target.
func withinPct(got, target, pct int) bool {
	if target <= 0 {
		return false
	}
	delta := got - target
	if delta < 0 {
		delta = -delta
	}
	return delta*100 <= target*pct
}

// hasKeyword reports whether at least one of req.Keywords appears
// in any narrative section. When req.Keywords is empty the check
// passes (no keyword constraint configured).
func hasKeyword(script VideoScript, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	body := strings.ToLower(strings.Join([]string{script.Hook, script.Problem, script.ProductDemo, script.CTA}, " "))
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw == "" {
			continue
		}
		if strings.Contains(body, kw) {
			return true
		}
	}
	return false
}

// hasActionVerb returns true when the CTA contains at least one
// canonical English action verb. RedNote Chinese CTA is treated
// as action-verb-bearing by virtue of containing "点击" or "看".
func hasActionVerb(cta string) bool {
	lower := strings.ToLower(cta)
	for _, verb := range []string{"shop", "tap", "swipe", "click", "buy", "get", "see", "watch", "discover", "try"} {
		if strings.Contains(lower, verb) {
			return true
		}
	}
	for _, zh := range []string{"点击", "看", "购买", "了解"} {
		if strings.Contains(cta, zh) {
			return true
		}
	}
	return false
}

func countWords(text string) int {
	words := strings.Fields(strings.TrimSpace(text))
	return len(words)
}

func floatPtrLocal(v float64) *float64 { return &v }
func intPtrLocal(v int) *int           { return &v }
