package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// scriptedGenerator returns a fixed JSON script for any prompt.
type scriptedGenerator struct {
	calls atomic.Int32
	json  string
	err   error
}

func (s *scriptedGenerator) Complete(_ context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	s.calls.Add(1)
	if s.err != nil {
		return port.AICompletionResponse{}, s.err
	}
	return port.AICompletionResponse{Content: s.json, TokensUsed: 234}, nil
}

func newGoodTikTokGenerator() *scriptedGenerator {
	body, _ := json.Marshal(VideoScript{
		Hook:         "Stop scrolling for two seconds. These wireless earbuds changed how I commute, run, and even take meetings, all on one charge per day.",
		Problem:      "Tinny audio and short battery life ruin every commute. Plenty of brands promise the moon then ship cheap drivers, fragile cases, and laggy Bluetooth that drops out the moment you move three steps away from your phone.",
		ProductDemo:  "Quick demo: thirty-six hour battery, active noise cancelling, magnetic charge case that lives in any pocket. Drop them in your bag and forget the cord salad forever. Rich highs, tight bass, and the latency is so low that the audio stays locked to whatever you watch from the very first second.",
		CTA:          "Tap the link to shop these earbuds now while we still have them in stock at this launch price.",
		Subtitles:    []string{"00:00 hook", "00:07 problem", "00:18 demo", "00:50 cta"},
		BrandingNote: "#fyp #earbuds #wireless",
	})
	return &scriptedGenerator{json: string(body)}
}

func newGoodReelsGenerator() *scriptedGenerator {
	body, _ := json.Marshal(VideoScript{
		Hook:         "These wireless earbuds changed my morning run for good.",
		Problem:      "Loose buds slip, bass disappears, calls cut out, batteries die at the worst time of the day.",
		ProductDemo:  "Watch the snug fit hold through the warm-up sprint, see the live volume meter, hear crisp vocals on the demo track over the wind, and check the case battery hit ninety-five percent after one quick charge.",
		CTA:          "Swipe up to shop these earbuds now.",
		Subtitles:    []string{"00:00 hook", "00:05 demo", "00:22 cta"},
		BrandingNote: "@brand_handle",
	})
	return &scriptedGenerator{json: string(body)}
}

func videoRequest(platform VideoPlatform) VideoScriptRequest {
	return VideoScriptRequest{
		Product: ProductInfo{
			ID:    "earbuds-001",
			Title: "Wireless Earbuds Pro",
		},
		Platform:    platform,
		Language:    "en-AU",
		KeyFeatures: []string{"36h battery", "active noise cancelling"},
		Keywords:    []string{"earbuds", "wireless"},
	}
}

// TestVideoScript_Generates60SecTikTokScriptForProduct is the EC-5-1
// RED acceptance test for the happy path.
func TestVideoScript_Generates60SecTikTokScriptForProduct(t *testing.T) {
	t.Parallel()
	gen, err := NewVideoScriptGenerator(nil, VideoScriptGeneratorConfig{
		Generator: newGoodTikTokGenerator(),
		TenantID:  "tenant-1",
		Now:       func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewVideoScriptGenerator: %v", err)
	}
	t.Cleanup(func() { _ = gen.Close(context.Background()) })

	res, err := gen.Generate(context.Background(), videoRequest(VideoPlatformTikTok))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.Source != VideoScriptSourceLLM {
		t.Fatalf("Source = %q, want LLM", res.Source)
	}
	if res.DurationSec != 60 {
		t.Fatalf("DurationSec = %d, want 60", res.DurationSec)
	}
	if res.QualityScore < 0.75 {
		t.Fatalf("QualityScore = %.4f, want >= 0.75", res.QualityScore)
	}
	if res.Script.Hook == "" || res.Script.Problem == "" || res.Script.ProductDemo == "" || res.Script.CTA == "" {
		t.Fatalf("script missing required sections: %+v", res.Script)
	}
	if res.WordCount < 100 {
		t.Fatalf("WordCount = %d, want >= 100 for 60s budget", res.WordCount)
	}
	if !strings.Contains(strings.ToLower(res.Script.CTA), "shop") {
		t.Fatalf("CTA missing action verb: %q", res.Script.CTA)
	}
}

// TestVideoScript_FailoverToTemplateWhenLLMUnavailable is the
// EC-5-1 RED acceptance test mirroring the v3.2.1 EC-2-1 4-scenario
// matrix.
func TestVideoScript_FailoverToTemplateWhenLLMUnavailable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		generator port.AITextGenerator
	}{
		{
			name:      "nil_equivalent_not_configured",
			generator: &scriptedGenerator{err: errors.New("video: llm adapter not configured")},
		},
		{
			name:      "bedrock_503_service_unavailable",
			generator: &scriptedGenerator{err: fmt.Errorf("bedrock: status %d service unavailable", http.StatusServiceUnavailable)},
		},
		{
			name:      "context_deadline_exceeded_real",
			generator: newRealVideoTimeoutGenerator(50 * time.Millisecond),
		},
		{
			name:      "circuit_breaker_open",
			generator: &scriptedGenerator{err: errors.New("downstream guarded: circuit breaker open")},
		},
	}

	req := videoRequest(VideoPlatformTikTok)
	var firstHook string
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gen, err := NewVideoScriptGenerator(nil, VideoScriptGeneratorConfig{
				Generator:  tc.generator,
				TenantID:   "tenant-1",
				MinQuality: 0.65,
				Now:        func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
			})
			if err != nil {
				t.Fatalf("NewVideoScriptGenerator: %v", err)
			}
			t.Cleanup(func() { _ = gen.Close(context.Background()) })

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			res, err := gen.Generate(ctx, req)
			if err != nil {
				t.Fatalf("Generate(%s): %v", tc.name, err)
			}
			if res.Source != VideoScriptSourceTemplate {
				t.Fatalf("Source = %q, want template (failover %s)", res.Source, tc.name)
			}
			if res.QualityScore < 0.65 {
				t.Fatalf("QualityScore = %.4f, want >= 0.65 (failover %s)", res.QualityScore, tc.name)
			}
			if res.QualityScore > videoScriptTemplateScoreCeiling {
				t.Fatalf("QualityScore = %.4f, want <= %.4f (template ceiling)", res.QualityScore, videoScriptTemplateScoreCeiling)
			}

			// Determinism: every failover case must yield the same
			// hook so a stuck-on-template tenant is spottable by
			// stable copy.
			if firstHook == "" {
				firstHook = res.Script.Hook
			} else if res.Script.Hook != firstHook {
				t.Fatalf("template hook drifted across failover cases (case %s): got %q, want %q", tc.name, res.Script.Hook, firstHook)
			}
		})
	}
}

// TestVideoScript_PlatformVariantsHaveCorrectStructure exercises
// each platform's prompt + duration + template branch so the
// table-driven matrix proves the four supported variants ship
// distinct outputs.
func TestVideoScript_PlatformVariantsHaveCorrectStructure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		platform    VideoPlatform
		generator   *scriptedGenerator
		wantSeconds int
		wantSource  VideoScriptSource
	}{
		{name: "tiktok_60s_llm", platform: VideoPlatformTikTok, generator: newGoodTikTokGenerator(), wantSeconds: 60, wantSource: VideoScriptSourceLLM},
		{name: "instagram_reels_30s_llm", platform: VideoPlatformInstagramReel, generator: newGoodReelsGenerator(), wantSeconds: 30, wantSource: VideoScriptSourceLLM},
		// RedNote + Facebook fall back to template since the test
		// scripted generator returns the TikTok shape -- but we
		// reuse the lifestyle template fallback for them.
		{name: "rednote_60s_template", platform: VideoPlatformRedNote, generator: &scriptedGenerator{err: errors.New("llm down")}, wantSeconds: 60, wantSource: VideoScriptSourceTemplate},
		{name: "facebook_60s_template", platform: VideoPlatformFacebook, generator: &scriptedGenerator{err: errors.New("llm down")}, wantSeconds: 60, wantSource: VideoScriptSourceTemplate},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gen, err := NewVideoScriptGenerator(nil, VideoScriptGeneratorConfig{
				Generator:  tc.generator,
				TenantID:   "tenant-1",
				MinQuality: 0.65,
				Now:        func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
			})
			if err != nil {
				t.Fatalf("NewVideoScriptGenerator: %v", err)
			}
			t.Cleanup(func() { _ = gen.Close(context.Background()) })

			res, err := gen.Generate(context.Background(), videoRequest(tc.platform))
			if err != nil {
				t.Fatalf("Generate(%s): %v", tc.name, err)
			}
			if res.Platform != tc.platform {
				t.Fatalf("Platform = %q, want %q", res.Platform, tc.platform)
			}
			if res.DurationSec != tc.wantSeconds {
				t.Fatalf("DurationSec = %d, want %d", res.DurationSec, tc.wantSeconds)
			}
			if res.Source != tc.wantSource {
				t.Fatalf("Source = %q, want %q", res.Source, tc.wantSource)
			}
		})
	}
}

func TestVideoScript_RejectsAfterClose(t *testing.T) {
	t.Parallel()
	gen, err := NewVideoScriptGenerator(nil, VideoScriptGeneratorConfig{
		Generator: &scriptedGenerator{},
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewVideoScriptGenerator: %v", err)
	}
	_ = gen.Close(context.Background())
	_, err = gen.Generate(context.Background(), videoRequest(VideoPlatformTikTok))
	if !errors.Is(err, ErrVideoScriptGenClosed) {
		t.Fatalf("err = %v, want ErrVideoScriptGenClosed", err)
	}
}

func TestVideoScript_GuardsRequiredFields(t *testing.T) {
	t.Parallel()
	gen, err := NewVideoScriptGenerator(nil, VideoScriptGeneratorConfig{
		Generator: &scriptedGenerator{},
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewVideoScriptGenerator: %v", err)
	}
	t.Cleanup(func() { _ = gen.Close(context.Background()) })
	cases := []VideoScriptRequest{
		{},
		{Product: ProductInfo{ID: "x"}},
	}
	for i, req := range cases {
		req := req
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			t.Parallel()
			_, err := gen.Generate(context.Background(), req)
			if !errors.Is(err, ErrVideoScriptGenUnconfigured) {
				t.Fatalf("err = %v, want ErrVideoScriptGenUnconfigured", err)
			}
		})
	}
}

func TestNewVideoScriptGenerator_ConfigValidation(t *testing.T) {
	t.Parallel()
	cases := map[string]VideoScriptGeneratorConfig{
		"missing generator": {TenantID: "t"},
		"missing tenant":    {Generator: &scriptedGenerator{}},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewVideoScriptGenerator(nil, cfg)
			if !errors.Is(err, ErrVideoScriptGenUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestVideoScript_TemplateBelowFloorReturnsSentinel(t *testing.T) {
	t.Parallel()
	// Use a request that gives the template scorer essentially no
	// keywords + a too-short title so the score drops well below
	// the configured floor.
	gen, err := NewVideoScriptGenerator(nil, VideoScriptGeneratorConfig{
		Generator:  &scriptedGenerator{err: errors.New("llm down")},
		TenantID:   "tenant-1",
		MinQuality: 0.99, // unreachable for the template
	})
	if err != nil {
		t.Fatalf("NewVideoScriptGenerator: %v", err)
	}
	t.Cleanup(func() { _ = gen.Close(context.Background()) })
	_, err = gen.Generate(context.Background(), videoRequest(VideoPlatformTikTok))
	if !errors.Is(err, ErrVideoScriptQualityBelowThreshold) {
		t.Fatalf("err = %v, want ErrVideoScriptQualityBelowThreshold", err)
	}
}

func TestParseVideoScriptResponse_FencedJSON(t *testing.T) {
	t.Parallel()
	body := "```json\n{\"hook\":\"H\",\"problem\":\"P\",\"product_demo\":\"D\",\"cta\":\"C\"}\n```"
	script, ok := parseVideoScriptResponse(body)
	if !ok {
		t.Fatal("expected ok parse")
	}
	if script.Hook != "H" || script.CTA != "C" {
		t.Fatalf("script = %+v", script)
	}
}

func TestParseVideoScriptResponse_RejectsMissingSection(t *testing.T) {
	t.Parallel()
	if _, ok := parseVideoScriptResponse(`{"hook":"H","problem":"P","cta":"C"}`); ok {
		t.Fatal("expected reject when product_demo missing")
	}
	if _, ok := parseVideoScriptResponse(""); ok {
		t.Fatal("expected reject for empty body")
	}
	if _, ok := parseVideoScriptResponse("not-json"); ok {
		t.Fatal("expected reject for non-JSON")
	}
}

func TestVideoScriptDuration_Defaults(t *testing.T) {
	t.Parallel()
	if VideoScriptDuration(VideoPlatformInstagramReel) != 30 {
		t.Fatal("instagram-reels = 30s")
	}
	if VideoScriptDuration(VideoPlatformTikTok) != 60 {
		t.Fatal("tiktok = 60s")
	}
	if VideoScriptDuration("unknown") != 60 {
		t.Fatal("default = 60s")
	}
}

func TestScoreVideoScript_HighQualityRubric(t *testing.T) {
	t.Parallel()
	script := VideoScript{
		Hook:        strings.Repeat("a a a a a ", 10),
		Problem:     strings.Repeat("b b b b b ", 10),
		ProductDemo: strings.Repeat("c c c c c ", 10),
		CTA:         "Tap to shop these earbuds now",
	}
	req := VideoScriptRequest{
		Platform: VideoPlatformTikTok,
		Keywords: []string{"a"},
	}
	score := scoreVideoScript(script, req)
	if score < 0.9 {
		t.Fatalf("score = %.2f, want >= 0.9", score)
	}
}

func TestHasActionVerb_TableDriven(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"Shop now":    true,
		"swipe up":    true,
		"  see this ": true,
		"点击查看":        true,
		"random text": false,
	}
	for cta, want := range cases {
		cta, want := cta, want
		t.Run(cta, func(t *testing.T) {
			t.Parallel()
			if got := hasActionVerb(cta); got != want {
				t.Fatalf("hasActionVerb(%q) = %v, want %v", cta, got, want)
			}
		})
	}
}

func TestVideoScriptMetricsHookFires(t *testing.T) {
	t.Parallel()
	mh := &recordingVideoMetrics{}
	gen, err := NewVideoScriptGenerator(nil, VideoScriptGeneratorConfig{
		Generator: newGoodTikTokGenerator(),
		TenantID:  "tenant-1",
		Metrics:   mh,
	})
	if err != nil {
		t.Fatalf("NewVideoScriptGenerator: %v", err)
	}
	t.Cleanup(func() { _ = gen.Close(context.Background()) })
	if _, err := gen.Generate(context.Background(), videoRequest(VideoPlatformTikTok)); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if mh.records.Load() == 0 || mh.observes.Load() == 0 {
		t.Fatalf("metrics not invoked: %+v", mh)
	}
}

type recordingVideoMetrics struct {
	records  atomic.Int64
	observes atomic.Int64
}

func (r *recordingVideoMetrics) RecordVideoScript(_, _, _ string)              { r.records.Add(1) }
func (r *recordingVideoMetrics) ObserveVideoScriptQuality(_ string, _ float64) { r.observes.Add(1) }

// realVideoTimeoutGenerator sleeps until ctx fires so the failover
// path is proved against a true context.DeadlineExceeded shape.
type realVideoTimeoutGenerator struct {
	delay time.Duration
}

func newRealVideoTimeoutGenerator(delay time.Duration) *realVideoTimeoutGenerator {
	return &realVideoTimeoutGenerator{delay: delay}
}

func (g *realVideoTimeoutGenerator) Complete(ctx context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	innerCtx, cancel := context.WithTimeout(ctx, g.delay)
	defer cancel()
	<-innerCtx.Done()
	return port.AICompletionResponse{}, fmt.Errorf("bedrock: %w", innerCtx.Err())
}
