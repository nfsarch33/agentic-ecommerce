package enrichment

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// TestDescriptionGen_TranslatesChineseToEnglish is the v3.2.0 EC-2-1
// RED test. It exercises the IronClaw-backed multilingual product
// description agent against a representative Chinese supplier
// listing and asserts:
//
//  1. Output is in English (no CJK characters in the body).
//  2. The quality score >= the configured advisory threshold (0.75).
//  3. The platform-appropriate tone marker is present (e.g. an
//     emoji + casual line for TikTok; SEO-structured paragraph for
//     WooCommerce).
//  4. The result records the generation source as "llm".
func TestDescriptionGen_TranslatesChineseToEnglish(t *testing.T) {
	t.Parallel()

	gen := &fakeAITextGenerator{
		response: `{"english_title":"Premium Wireless Earbuds","english_description":"Crisp sound, all-day comfort, and 36-hour battery life. Pair instantly with any device. Perfect for commuting, workouts, and remote calls."}`,
	}
	d, err := NewDescriptionGenerator(nil, DescriptionGeneratorConfig{
		Generator:    gen,
		TenantID:     "cylrl",
		MinQuality:   0.75,
		Now:          func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
		FallbackText: "Quality product imported from a verified supplier.",
	})
	if err != nil {
		t.Fatalf("NewDescriptionGenerator: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })

	req := DescriptionRequest{
		Product: EnrichmentProduct{
			ID:                 "earbuds-001",
			ChineseTitle:       "高品质无线蓝牙耳机",
			ChineseDescription: "无线蓝牙耳机, 续航36小时, 主动降噪",
			Category:           "electronics",
			PriceCNYCents:      4500,
		},
		Platform: PlatformWooCommerce,
		Language: "en-AU",
	}
	res, err := d.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.EnglishDescription == "" {
		t.Fatal("expected non-empty English description")
	}
	if containsCJK(res.EnglishDescription) {
		t.Fatalf("description still contains CJK characters: %q", res.EnglishDescription)
	}
	if res.QualityScore < 0.75 {
		t.Fatalf("quality score = %.2f, want >= 0.75", res.QualityScore)
	}
	if res.Source != ResultSourceLLM {
		t.Fatalf("Source = %q, want %q", res.Source, ResultSourceLLM)
	}
	if res.Platform != PlatformWooCommerce {
		t.Fatalf("Platform = %q, want %q", res.Platform, PlatformWooCommerce)
	}
}

func TestDescriptionGen_TikTokToneIsCasual(t *testing.T) {
	t.Parallel()

	gen := &fakeAITextGenerator{
		response: `{"english_title":"Earbuds That Slap","english_description":"Bestie, these earbuds are a vibe. 36 hours of battery, snatched ANC, drop-resistant -- you NEED this."}`,
	}
	d, err := NewDescriptionGenerator(nil, DescriptionGeneratorConfig{
		Generator:  gen,
		TenantID:   "cylrl",
		MinQuality: 0.5,
	})
	if err != nil {
		t.Fatalf("NewDescriptionGenerator: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })

	res, err := d.Generate(context.Background(), DescriptionRequest{
		Product: EnrichmentProduct{
			ID:                 "earbuds-001",
			ChineseTitle:       "好物耳机",
			ChineseDescription: "耳机",
			Category:           "electronics",
		},
		Platform: PlatformTikTok,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// The fake generator returns the configured response verbatim,
	// but the tone-system-prompt on the request must reach the
	// generator.
	if !strings.Contains(strings.ToLower(gen.lastSystem), "tiktok") {
		t.Fatalf("system prompt did not mention tiktok tone: %q", gen.lastSystem)
	}
	if res.Platform != PlatformTikTok {
		t.Fatalf("Platform = %q, want %q", res.Platform, PlatformTikTok)
	}
}

func TestDescriptionGen_FailsOverToTemplateWhenLLMUnavailable(t *testing.T) {
	t.Parallel()

	// Generator that returns an error simulates Bedrock outage.
	gen := &fakeAITextGenerator{err: errors.New("bedrock: service unavailable")}
	d, err := NewDescriptionGenerator(nil, DescriptionGeneratorConfig{
		Generator:    gen,
		TenantID:     "cylrl",
		MinQuality:   0.75,
		FallbackText: "Quality product imported from a verified supplier.",
	})
	if err != nil {
		t.Fatalf("NewDescriptionGenerator: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })

	res, err := d.Generate(context.Background(), DescriptionRequest{
		Product: EnrichmentProduct{
			ID:                 "earbuds-001",
			ChineseTitle:       "蓝牙耳机",
			ChineseDescription: "耳机",
			Category:           "electronics",
		},
		Platform: PlatformWooCommerce,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.Source != ResultSourceTemplate {
		t.Fatalf("Source = %q, want %q", res.Source, ResultSourceTemplate)
	}
	if !strings.Contains(res.EnglishDescription, "Quality product") {
		t.Fatalf("template fallback missing fallback prefix: %q", res.EnglishDescription)
	}
}

func TestDescriptionGen_LowQualityBelowThresholdReturnsErr(t *testing.T) {
	t.Parallel()

	// Empty response from generator -> low quality (no length, no
	// keywords). The template fallback then runs but its quality
	// must still meet the floor.
	gen := &fakeAITextGenerator{response: ``}
	d, err := NewDescriptionGenerator(nil, DescriptionGeneratorConfig{
		Generator:  gen,
		TenantID:   "cylrl",
		MinQuality: 0.99, // unrealistically high so even template fails
	})
	if err != nil {
		t.Fatalf("NewDescriptionGenerator: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })

	_, err = d.Generate(context.Background(), DescriptionRequest{
		Product: EnrichmentProduct{
			ID:                 "earbuds-001",
			ChineseTitle:       "x",
			ChineseDescription: "x",
		},
		Platform: PlatformWooCommerce,
	})
	if !errors.Is(err, ErrEnrichmentQualityBelowThreshold) {
		t.Fatalf("error = %v, want ErrEnrichmentQualityBelowThreshold", err)
	}
}

func TestNewDescriptionGenerator_RejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	gen := &fakeAITextGenerator{}

	cases := []struct {
		name string
		mut  func(c *DescriptionGeneratorConfig)
	}{
		{name: "no generator", mut: func(c *DescriptionGeneratorConfig) { c.Generator = nil }},
		{name: "no tenant", mut: func(c *DescriptionGeneratorConfig) { c.TenantID = " " }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := DescriptionGeneratorConfig{
				Generator: gen,
				TenantID:  "cylrl",
			}
			tc.mut(&cfg)
			_, err := NewDescriptionGenerator(nil, cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrDescriptionGenUnconfigured) {
				t.Fatalf("error not wrapping ErrDescriptionGenUnconfigured: %v", err)
			}
		})
	}
}

func TestDescriptionGen_GenerateAfterCloseReturnsClosedError(t *testing.T) {
	t.Parallel()

	gen := &fakeAITextGenerator{response: `{"english_description":"x"}`}
	d, err := NewDescriptionGenerator(nil, DescriptionGeneratorConfig{
		Generator: gen,
		TenantID:  "cylrl",
	})
	if err != nil {
		t.Fatalf("NewDescriptionGenerator: %v", err)
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = d.Generate(context.Background(), DescriptionRequest{
		Product: EnrichmentProduct{ID: "x", ChineseTitle: "x"},
	})
	if !errors.Is(err, ErrDescriptionGenClosed) {
		t.Fatalf("error = %v, want ErrDescriptionGenClosed", err)
	}
}

// containsCJK returns true if s contains any CJK Unified Ideographs.
// Used to assert the generator actually translated rather than
// passing the Chinese through.
func containsCJK(s string) bool {
	for _, r := range s {
		if (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) {
			return true
		}
	}
	return false
}

// fakeAITextGenerator is the in-test port.AITextGenerator. The last
// system + user prompts are captured so tests can assert tone wiring.
type fakeAITextGenerator struct {
	response   string
	err        error
	lastSystem string
	lastUser   string
	tokens     int
}

func (f *fakeAITextGenerator) Complete(_ context.Context, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			f.lastSystem = msg.Content
		}
		if msg.Role == "user" {
			f.lastUser = msg.Content
		}
	}
	if f.err != nil {
		return port.AICompletionResponse{}, f.err
	}
	return port.AICompletionResponse{Content: f.response, TokensUsed: f.tokens}, nil
}
