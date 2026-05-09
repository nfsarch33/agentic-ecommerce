package enrichment

import (
	"strings"
	"testing"
)

// TestPlatformSystemPromptCoversAllPlatforms exercises the
// per-platform tone branches so the prompt-construction path stays
// at high coverage as new platforms are added.
func TestPlatformSystemPromptCoversAllPlatforms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		platform Platform
		marker   string
	}{
		{platform: PlatformTikTok, marker: "tiktok"},
		{platform: PlatformFacebook, marker: "facebook"},
		{platform: PlatformRedNote, marker: "rednote"},
		{platform: PlatformWooCommerce, marker: "woocommerce"},
		{platform: Platform("instagram"), marker: "woocommerce"}, // default branch
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.platform), func(t *testing.T) {
			t.Parallel()
			got := strings.ToLower(platformSystemPrompt(tc.platform, "en-AU"))
			if !strings.Contains(got, tc.marker) {
				t.Fatalf("platform %q prompt missing marker %q: %s", tc.platform, tc.marker, got)
			}
		})
	}
}

func TestScoreDescriptionEdgeCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		text   string
		req    DescriptionRequest
		minWnt float64
		maxWnt float64
	}{
		{name: "empty text", text: "", req: DescriptionRequest{}, minWnt: 0, maxWnt: 0},
		{name: "very short", text: "Hi", req: DescriptionRequest{}, minWnt: 0, maxWnt: 0.7},
		{name: "sweet spot", text: strings.Repeat("a ", 80), req: DescriptionRequest{}, minWnt: 0.4, maxWnt: 1.0},
		{name: "very long", text: strings.Repeat("a ", 500), req: DescriptionRequest{}, minWnt: 0.3, maxWnt: 1.0},
		{name: "with keywords matched", text: "premium wireless earbuds with great battery", req: DescriptionRequest{Keywords: []string{"earbuds", "premium"}}, minWnt: 0.5, maxWnt: 1.0},
		{name: "with keywords missed", text: "totally unrelated text content", req: DescriptionRequest{Keywords: []string{"earbuds", "battery"}}, minWnt: 0, maxWnt: 0.95},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := scoreDescription(tc.text, tc.req)
			if got < tc.minWnt || got > tc.maxWnt {
				t.Fatalf("scoreDescription = %.4f, want in [%.4f, %.4f]", got, tc.minWnt, tc.maxWnt)
			}
		})
	}
}

func TestBuildTemplateTitleAndDescription(t *testing.T) {
	t.Parallel()

	if got := buildTemplateTitle(""); got != "New Product" {
		t.Fatalf("empty zh title: got %q, want New Product", got)
	}
	if got := buildTemplateTitle("无线耳机"); got != "Quality Product" {
		t.Fatalf("non-empty zh: got %q, want Quality Product", got)
	}
	body := buildTemplateDescription("Quality product.", DescriptionRequest{
		Product:  EnrichmentProduct{Category: "electronics"},
		Keywords: []string{"earbuds"},
	})
	if !strings.Contains(body, "Electronics") || !strings.Contains(body, "earbuds") {
		t.Fatalf("template body missing tokens: %q", body)
	}
}

func TestParseGeneratedDescriptionFallbacks(t *testing.T) {
	t.Parallel()

	desc, title, ok := parseGeneratedDescription("```json\n{\"english_title\":\"\",\"english_description\":\"foo\"}\n```", "无线")
	if !ok || desc != "foo" || title != "Quality Product" {
		t.Fatalf("fenced parse: ok=%v desc=%q title=%q", ok, desc, title)
	}
	desc, _, ok = parseGeneratedDescription("plain text body", "title")
	if !ok || desc != "plain text body" {
		t.Fatalf("plain fallback: ok=%v desc=%q", ok, desc)
	}
	if _, _, ok := parseGeneratedDescription("```\n```", "x"); ok {
		t.Fatal("empty fenced should fail")
	}
}

func TestAbsAndAccessor(t *testing.T) {
	t.Parallel()

	if got := abs(-3.5); got != 3.5 {
		t.Fatalf("abs(-3.5) = %v, want 3.5", got)
	}
	if got := abs(3.5); got != 3.5 {
		t.Fatalf("abs(3.5) = %v, want 3.5", got)
	}
}

func TestMinQualityAccessor(t *testing.T) {
	t.Parallel()

	gen := &fakeAITextGenerator{}
	d, err := NewDescriptionGenerator(nil, DescriptionGeneratorConfig{
		Generator:  gen,
		TenantID:   "cylrl",
		MinQuality: 0.8,
	})
	if err != nil {
		t.Fatalf("NewDescriptionGenerator: %v", err)
	}
	if got := d.MinQuality(); got != 0.8 {
		t.Fatalf("MinQuality = %v, want 0.8", got)
	}
	// Default + clamping
	d2, _ := NewDescriptionGenerator(nil, DescriptionGeneratorConfig{
		Generator: gen,
		TenantID:  "cylrl",
	})
	if d2.MinQuality() != 0.75 {
		t.Fatalf("default MinQuality = %v, want 0.75", d2.MinQuality())
	}
	d3, _ := NewDescriptionGenerator(nil, DescriptionGeneratorConfig{
		Generator:  gen,
		TenantID:   "cylrl",
		MinQuality: 2.0,
	})
	if d3.MinQuality() != 1.0 {
		t.Fatalf("clamped MinQuality = %v, want 1.0", d3.MinQuality())
	}
}
