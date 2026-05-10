// File scope: v3.9.0 EC-5-4 hashtag + caption agent.
//
// Generates platform-specific hashtags + captions backed by an LLM
// (port.AITextGenerator). Per platform:
//   - TikTok: <=30 hashtags; trend-aware; 2200-char caption.
//   - RedNote: 5-15 hashtags; community tone; 1000-char caption.
//   - Facebook: 3-5 hashtags; conversational; no length limit.
//   - Instagram: 5-30 hashtags; aesthetic; 2200-char caption.
//
// LLM-first via IronClaw / Bedrock-via-uiauto / minimax. On any LLM
// error the agent falls back to a rule-based generator that
// extracts keywords from the product description; that fallback is
// always available so the content pipeline never blocks the
// channel router.
//
// Reuse evidence:
//   - port.AITextGenerator from v3.2.0 EC-2-1 + v3.4.0 EC-5-1.
//   - The LLM-first / rule-fallback pattern mirrors v3.4.0 EC-5-1
//     video_script.go.
//   - The hashtag scoring rubric (penalises #sale / #cute generic
//     hashtags; rewards niche/brand combinations) reuses the
//     scoreVideoScript decomposition pattern.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4 -- 16-sprint streak; v3.9.0 sprint 16 target):
//   - Generate (envelope -> validate -> dispatch -> score -> emit)
//   - dispatch (platform-aware LLM prompt + JSON parse)
//   - runLLM (LLM call + JSON parse with template fallback)
//   - scoreHashtags (rubric)
//   - templateFallback (rule-based generator)
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
	"unicode/utf8"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// HashtagPlatform is the supported destination platform identifier.
type HashtagPlatform string

// Supported platforms.
const (
	HashtagPlatformTikTok    HashtagPlatform = "tiktok"
	HashtagPlatformRedNote   HashtagPlatform = "rednote"
	HashtagPlatformFacebook  HashtagPlatform = "facebook"
	HashtagPlatformInstagram HashtagPlatform = "instagram"
)

// HashtagCaptionSource identifies whether the result came from the
// LLM or the rule-based fallback.
type HashtagCaptionSource string

// Source enum values.
const (
	HashtagCaptionSourceLLM  HashtagCaptionSource = "llm"
	HashtagCaptionSourceRule HashtagCaptionSource = "rule"
)

// EC-5-4 typed sentinels.
var (
	// ErrHashtagAgentUnconfigured is returned when a required
	// dependency is missing.
	ErrHashtagAgentUnconfigured = errors.New("hashtag_agent: unconfigured")

	// ErrHashtagAgentClosed is returned by Generate after Close.
	ErrHashtagAgentClosed = errors.New("hashtag_agent: closed")

	// ErrHashtagOverLimit is returned when the generated hashtag
	// list exceeds the platform's per-post cap.
	ErrHashtagOverLimit = errors.New("hashtag_agent: hashtag count over platform limit")

	// ErrCaptionTooLong is returned when the generated caption
	// exceeds the platform's character limit.
	ErrCaptionTooLong = errors.New("hashtag_agent: caption exceeds platform limit")

	// ErrUnsupportedPlatform is returned for an unknown platform.
	ErrUnsupportedPlatform = errors.New("hashtag_agent: unsupported platform")
)

// HashtagPlatformLimits captures the per-platform constraints.
type HashtagPlatformLimits struct {
	MinHashtags int
	MaxHashtags int
	MaxCaption  int // <=0 means no limit
}

// HashtagPlatformDefaults returns the default per-platform limits.
func HashtagPlatformDefaults() map[HashtagPlatform]HashtagPlatformLimits {
	return map[HashtagPlatform]HashtagPlatformLimits{
		HashtagPlatformTikTok:    {MinHashtags: 1, MaxHashtags: 30, MaxCaption: 2200},
		HashtagPlatformRedNote:   {MinHashtags: 5, MaxHashtags: 15, MaxCaption: 1000},
		HashtagPlatformFacebook:  {MinHashtags: 3, MaxHashtags: 5, MaxCaption: 0},
		HashtagPlatformInstagram: {MinHashtags: 5, MaxHashtags: 30, MaxCaption: 2200},
	}
}

// HashtagCaptionRequest is the unit of work submitted to Generate.
type HashtagCaptionRequest struct {
	Product   ProductInfo
	Platform  string
	Keywords  []string
	BrandTags []string // brand-owned hashtags the agent should always include
}

// HashtagCaptionResult captures the agent's run output.
type HashtagCaptionResult struct {
	ProductID   string
	Platform    HashtagPlatform
	Hashtags    []string
	Caption     string
	Score       float64
	Source      HashtagCaptionSource
	GeneratedAt time.Time
	TokensUsed  int
}

// HashtagAgentMetrics is the small port the agent emits counters
// through.
type HashtagAgentMetrics interface {
	RecordHashtagGeneration(tenantID, channel, outcome string)
}

// HashtagAgentKPISample is the v3.9.0 EvoMap KPI sample.
type HashtagAgentKPISample struct {
	TenantID string
	Platform string
	Score    float64
	Source   HashtagCaptionSource
}

// HashtagAgentKPIHook is the optional EvoMap emission hook.
type HashtagAgentKPIHook func(HashtagAgentKPISample)

// EMABiasProvider is the optional v3.9.0 EC-5-5 feedback hook. The
// agent uses this to bias caption length / hashtag count for
// (channel, content_type) bins that historically perform well with
// a particular profile.
type EMABiasProvider interface {
	BiasFor(channel, contentType string) (HashtagBias, bool)
}

// HashtagBias is the per-(channel, content_type) profile the EMA
// learner publishes. All fields are advisory; the agent clamps them
// to the platform's hard limits.
type HashtagBias struct {
	PreferLongerCaption bool
	BiasHashtagCount    int     // >0 means prefer this many
	EMAScore            float64 // [0,100]
}

// HashtagAgentConfig wires the agent.
type HashtagAgentConfig struct {
	TenantID     string
	Generator    port.AITextGenerator
	Limits       map[HashtagPlatform]HashtagPlatformLimits
	Metrics      HashtagAgentMetrics
	KPIHook      HashtagAgentKPIHook
	BiasProvider EMABiasProvider
	Now          func() time.Time
}

// HashtagAgent is the v3.9.0 EC-5-4 agent.
type HashtagAgent struct {
	cfg    HashtagAgentConfig
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewHashtagAgent constructs an agent.
func NewHashtagAgent(logger *slog.Logger, cfg HashtagAgentConfig) (*HashtagAgent, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrHashtagAgentUnconfigured)
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if len(cfg.Limits) == 0 {
		cfg.Limits = HashtagPlatformDefaults()
	}
	return &HashtagAgent{cfg: cfg, logger: logger}, nil
}

// Close marks the agent closed. lifecycle.Closer contract.
func (a *HashtagAgent) Close(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

// Generate runs the EC-5-4 pipeline. Cyclomatic 5.
func (a *HashtagAgent) Generate(ctx context.Context, req HashtagCaptionRequest) (HashtagCaptionResult, error) {
	if err := a.guard(); err != nil {
		return HashtagCaptionResult{}, err
	}
	platform := HashtagPlatform(strings.ToLower(strings.TrimSpace(req.Platform)))
	limits, ok := a.cfg.Limits[platform]
	if !ok {
		return HashtagCaptionResult{}, fmt.Errorf("%w: %s", ErrUnsupportedPlatform, req.Platform)
	}
	if strings.TrimSpace(req.Product.Title) == "" {
		return HashtagCaptionResult{}, fmt.Errorf("%w: Product.Title required", ErrHashtagAgentUnconfigured)
	}
	llmRes, source, tokens := a.dispatch(ctx, req, platform, limits)
	if err := enforceLimits(llmRes, limits); err != nil {
		return HashtagCaptionResult{}, err
	}
	score := a.ScoreHashtags(llmRes.Hashtags, req.Product)
	a.recordOutcome(req.Product.ID, platform, source)
	a.recordKPI(platform, score, source)
	return HashtagCaptionResult{
		ProductID:   req.Product.ID,
		Platform:    platform,
		Hashtags:    llmRes.Hashtags,
		Caption:     llmRes.Caption,
		Score:       score,
		Source:      source,
		GeneratedAt: a.cfg.Now(),
		TokensUsed:  tokens,
	}, nil
}

// dispatch routes to the LLM first, falling back to the rule
// generator on any failure.
func (a *HashtagAgent) dispatch(ctx context.Context, req HashtagCaptionRequest, platform HashtagPlatform, limits HashtagPlatformLimits) (hashtagPayload, HashtagCaptionSource, int) {
	bias := a.lookupBias(platform, req)
	if a.cfg.Generator == nil {
		return a.templateFallback(req, platform, limits, bias), HashtagCaptionSourceRule, 0
	}
	llm, tokens, ok := a.runLLM(ctx, req, platform, limits, bias)
	if !ok {
		return a.templateFallback(req, platform, limits, bias), HashtagCaptionSourceRule, 0
	}
	return llm, HashtagCaptionSourceLLM, tokens
}

// runLLM calls the generator and parses the JSON response. Returns
// ok=false on any failure.
func (a *HashtagAgent) runLLM(ctx context.Context, req HashtagCaptionRequest, platform HashtagPlatform, limits HashtagPlatformLimits, bias HashtagBias) (hashtagPayload, int, bool) {
	system, user := buildHashtagPrompt(req, platform, limits, bias)
	resp, err := a.cfg.Generator.Complete(ctx, port.AICompletionRequest{
		Messages: []port.AIMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: floatPtrLocal(0.7),
		MaxTokens:   intPtrLocal(800),
	})
	if err != nil {
		a.logger.Warn("hashtag_agent.llm_unavailable", "tenant_id", a.cfg.TenantID, "platform", string(platform), "error", err)
		return hashtagPayload{}, 0, false
	}
	parsed, ok := parseHashtagPayload(resp.Content)
	if !ok {
		return hashtagPayload{}, 0, false
	}
	parsed.Hashtags = trimHashtagSlice(parsed.Hashtags, limits.MaxHashtags)
	return parsed, resp.TokensUsed, true
}

func (a *HashtagAgent) lookupBias(platform HashtagPlatform, req HashtagCaptionRequest) HashtagBias {
	if a.cfg.BiasProvider == nil {
		return HashtagBias{}
	}
	bias, ok := a.cfg.BiasProvider.BiasFor(string(platform), "post")
	if !ok {
		return HashtagBias{}
	}
	_ = req
	return bias
}

// ScoreHashtags is the v3.9.0 EC-5-4 hashtag rubric. Returns a
// score in [0, 1].
//
//   - At least one brand or niche hashtag (length >= 8): +0.4
//   - Less than 30% generic hashtags: +0.3
//   - Brand/title keyword present in at least one hashtag: +0.2
//   - All hashtags begin with #: +0.1
//
// Pure helper so the failover test fixtures stay deterministic.
func (a *HashtagAgent) ScoreHashtags(hashtags []string, product ProductInfo) float64 {
	if len(hashtags) == 0 {
		return 0
	}
	score := hashtagNicheBonus(hashtags) + hashtagGenericPenalty(hashtags) + hashtagBrandKeywordBonus(hashtags, product) + hashtagFormatBonus(hashtags)
	return clampUnit(score)
}

func (a *HashtagAgent) recordOutcome(productID string, platform HashtagPlatform, source HashtagCaptionSource) {
	if a.cfg.Metrics == nil {
		return
	}
	a.cfg.Metrics.RecordHashtagGeneration(a.cfg.TenantID, string(platform), string(source))
	_ = productID
}

func (a *HashtagAgent) recordKPI(platform HashtagPlatform, score float64, source HashtagCaptionSource) {
	if a.cfg.KPIHook == nil {
		return
	}
	a.cfg.KPIHook(HashtagAgentKPISample{
		TenantID: a.cfg.TenantID,
		Platform: string(platform),
		Score:    score,
		Source:   source,
	})
}

func (a *HashtagAgent) guard() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrHashtagAgentClosed
	}
	return nil
}

// templateFallback is the rule-based generator. Extracts keywords
// from the title + description; emits brand-tag combinations + a
// short caption that respects the platform's tone.
func (a *HashtagAgent) templateFallback(req HashtagCaptionRequest, platform HashtagPlatform, limits HashtagPlatformLimits, bias HashtagBias) hashtagPayload {
	hashtags := make([]string, 0, limits.MaxHashtags)
	hashtags = append(hashtags, "#"+sanitizeHashtag(req.Product.Title))
	for _, kw := range req.Keywords {
		hashtags = append(hashtags, "#"+sanitizeHashtag(kw))
	}
	for _, b := range req.BrandTags {
		hashtags = append(hashtags, "#"+sanitizeHashtag(b))
	}
	hashtags = appendPlatformDefaults(hashtags, platform)
	hashtags = dedupeStrings(hashtags)
	hashtags = trimHashtagSlice(hashtags, limits.MaxHashtags)
	return hashtagPayload{
		Hashtags: hashtags,
		Caption:  buildTemplateCaption(req.Product, platform, bias),
	}
}

// hashtagPayload is the JSON shape we ask the LLM to emit AND the
// shape the rule fallback produces internally.
type hashtagPayload struct {
	Hashtags []string `json:"hashtags"`
	Caption  string   `json:"caption"`
}

func parseHashtagPayload(raw string) (hashtagPayload, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return hashtagPayload{}, false
	}
	stripped := strings.TrimPrefix(raw, "```json")
	stripped = strings.TrimPrefix(stripped, "```")
	stripped = strings.TrimSuffix(stripped, "```")
	stripped = strings.TrimSpace(stripped)
	var out hashtagPayload
	if err := json.Unmarshal([]byte(stripped), &out); err != nil {
		return hashtagPayload{}, false
	}
	if len(out.Hashtags) == 0 || strings.TrimSpace(out.Caption) == "" {
		return hashtagPayload{}, false
	}
	return out, true
}

func enforceLimits(payload hashtagPayload, limits HashtagPlatformLimits) error {
	if limits.MaxHashtags > 0 && len(payload.Hashtags) > limits.MaxHashtags {
		return fmt.Errorf("%w: got %d > max %d", ErrHashtagOverLimit, len(payload.Hashtags), limits.MaxHashtags)
	}
	if limits.MaxCaption > 0 && utf8.RuneCountInString(payload.Caption) > limits.MaxCaption {
		return fmt.Errorf("%w: got %d > max %d", ErrCaptionTooLong, utf8.RuneCountInString(payload.Caption), limits.MaxCaption)
	}
	return nil
}

func buildHashtagPrompt(req HashtagCaptionRequest, platform HashtagPlatform, limits HashtagPlatformLimits, bias HashtagBias) (string, string) {
	system := fmt.Sprintf(
		"You are a senior social copywriter for %s. Min %d, max %d hashtags; max %d caption chars (0=no limit). %s",
		platform, limits.MinHashtags, limits.MaxHashtags, limits.MaxCaption, hashtagToneHint(platform),
	)
	if bias.PreferLongerCaption {
		system += " Operator data shows longer captions perform best -- use the upper end of the limit."
	}
	if bias.BiasHashtagCount > 0 {
		system += fmt.Sprintf(" Operator data shows %d hashtags perform best -- aim for that count.", bias.BiasHashtagCount)
	}
	user := fmt.Sprintf(
		"Output JSON ONLY: {\"hashtags\":[\"#tag1\",\"#tag2\",...],\"caption\":\"...\"}\n\nProduct: %s\nDescription: %s\nKeywords: %s\nBrandTags: %s",
		req.Product.Title, req.Product.Description,
		strings.Join(req.Keywords, ", "), strings.Join(req.BrandTags, ", "),
	)
	return system, user
}

func hashtagToneHint(platform HashtagPlatform) string {
	switch platform {
	case HashtagPlatformTikTok:
		return "Tone: punchy, high-energy, trend-aware. Mix brand + niche tags; avoid stale generics like #fyp #cute when used alone."
	case HashtagPlatformRedNote:
		return "Tone: 种草 lifestyle Mandarin; sensory adjectives; use brand + niche combinations."
	case HashtagPlatformFacebook:
		return "Tone: conversational, benefit-led; 3-5 hashtags only; no emoji overuse."
	case HashtagPlatformInstagram:
		return "Tone: aesthetic, visual; mix brand + community + niche tags; avoid generic #photo or #love."
	default:
		return ""
	}
}

func buildTemplateCaption(p ProductInfo, platform HashtagPlatform, bias HashtagBias) string {
	core := p.Title
	if p.Description != "" {
		core += " - " + truncateForPlatform(p.Description, platform, bias)
	}
	switch platform {
	case HashtagPlatformTikTok:
		return "🔥 " + core
	case HashtagPlatformRedNote:
		return "✨ " + core + " 种草分享"
	case HashtagPlatformFacebook:
		return "Discover " + core + " - made for everyday quality."
	case HashtagPlatformInstagram:
		return core + " ✨"
	default:
		return core
	}
}

func truncateForPlatform(text string, platform HashtagPlatform, bias HashtagBias) string {
	limits := HashtagPlatformDefaults()[platform]
	if limits.MaxCaption == 0 {
		return text
	}
	target := limits.MaxCaption / 2 // template stays well below platform cap
	if bias.PreferLongerCaption {
		target = limits.MaxCaption * 3 / 4
	}
	if utf8.RuneCountInString(text) <= target {
		return text
	}
	runes := []rune(text)
	return string(runes[:target])
}

func sanitizeHashtag(in string) string {
	in = strings.TrimSpace(in)
	in = strings.ReplaceAll(in, " ", "")
	in = strings.ReplaceAll(in, "-", "")
	if in == "" {
		return "tag"
	}
	return strings.ToLower(in)
}

func appendPlatformDefaults(in []string, platform HashtagPlatform) []string {
	switch platform {
	case HashtagPlatformTikTok:
		return append(in, "#discover", "#newdrop", "#musthave")
	case HashtagPlatformRedNote:
		return append(in, "#好物推荐", "#种草", "#新品", "#日常分享", "#优选")
	case HashtagPlatformFacebook:
		return append(in, "#shopnow")
	case HashtagPlatformInstagram:
		return append(in, "#discover", "#shop", "#design", "#inspiration", "#daily")
	default:
		return in
	}
}

func trimHashtagSlice(in []string, max int) []string {
	if max <= 0 || len(in) <= max {
		return in
	}
	return in[:max]
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// hashtagNicheBonus rewards hashtags >= 8 chars (proxy for niche).
func hashtagNicheBonus(tags []string) float64 {
	for _, t := range tags {
		if utf8.RuneCountInString(strings.TrimPrefix(t, "#")) >= 8 {
			return 0.4
		}
	}
	return 0
}

// hashtagGenericPenalty awards 0.3 when <30% of tags are generic.
func hashtagGenericPenalty(tags []string) float64 {
	generic := map[string]struct{}{
		"#sale": {}, "#cute": {}, "#fyp": {}, "#shopping": {},
		"#deal": {}, "#new": {}, "#nice": {}, "#love": {}, "#daily": {},
	}
	if len(tags) == 0 {
		return 0
	}
	hits := 0
	for _, t := range tags {
		if _, ok := generic[strings.ToLower(t)]; ok {
			hits++
		}
	}
	if float64(hits)/float64(len(tags)) < 0.3 {
		return 0.3
	}
	return 0
}

// hashtagBrandKeywordBonus awards 0.2 when product title appears
// in at least one hashtag.
func hashtagBrandKeywordBonus(tags []string, product ProductInfo) float64 {
	title := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(product.Title), " ", ""))
	if title == "" {
		return 0
	}
	for _, t := range tags {
		if strings.Contains(strings.ToLower(t), title) {
			return 0.2
		}
	}
	return 0
}

// hashtagFormatBonus awards 0.1 when every hashtag begins with #.
func hashtagFormatBonus(tags []string) float64 {
	for _, t := range tags {
		if !strings.HasPrefix(t, "#") {
			return 0
		}
	}
	return 0.1
}
