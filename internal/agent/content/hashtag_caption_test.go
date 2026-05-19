// File scope: v3.9.0 EC-5-4 hashtag + caption agent RED tests.
package content

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

type stubHashtagLLM struct {
	response port.AICompletionResponse
	err      error
	mu       sync.Mutex
	calls    int
	last     port.AICompletionRequest
}

func (s *stubHashtagLLM) Complete(_ context.Context, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.last = req
	if s.err != nil {
		return port.AICompletionResponse{}, s.err
	}
	return s.response, nil
}

func newHashtagAgentHarness(t *testing.T, llm *stubHashtagLLM) *HashtagAgent {
	t.Helper()
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	a, err := NewHashtagAgent(nil, HashtagAgentConfig{
		TenantID:  "tenant-1",
		Generator: llm,
		Now:       func() time.Time { return clk },
	})
	if err != nil {
		t.Fatalf("NewHashtagAgent: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	return a
}

func TestHashtagAgent_TikTokGenerates30Hashtags(t *testing.T) {
	t.Parallel()
	hashtags := []string{}
	for i := 0; i < 30; i++ {
		hashtags = append(hashtags, "#brandtag"+string(rune('A'+i%26)))
	}
	llm := &stubHashtagLLM{
		response: port.AICompletionResponse{
			Content: jsonResponse(hashtags, "Premium audio at last."),
		},
	}
	a := newHashtagAgentHarness(t, llm)
	res, err := a.Generate(context.Background(), HashtagCaptionRequest{
		Product:  ProductInfo{ID: "p-1", Title: "Wireless Earbuds", Description: "Compact ANC earbuds"},
		Platform: "tiktok",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Hashtags) != 30 {
		t.Fatalf("expected 30 hashtags, got %d", len(res.Hashtags))
	}
	if res.Caption == "" {
		t.Fatalf("expected non-empty caption")
	}
}

func TestHashtagAgent_RedNote5to15Hashtags(t *testing.T) {
	t.Parallel()
	llm := &stubHashtagLLM{
		response: port.AICompletionResponse{
			Content: jsonResponse([]string{"#好物", "#种草", "#新品", "#推荐", "#日常", "#生活", "#分享", "#美好", "#精选", "#优选"}, "种草分享 ✨"),
		},
	}
	a := newHashtagAgentHarness(t, llm)
	res, err := a.Generate(context.Background(), HashtagCaptionRequest{
		Product:  ProductInfo{ID: "p-1", Title: "新款蓝牙耳机", Description: "高音质降噪"},
		Platform: "rednote",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Hashtags) < 5 || len(res.Hashtags) > 15 {
		t.Fatalf("rednote hashtag count outside [5,15]: got %d", len(res.Hashtags))
	}
}

func TestHashtagAgent_PenalizesGenericHashtags(t *testing.T) {
	t.Parallel()
	a := newHashtagAgentHarness(t, &stubHashtagLLM{
		response: port.AICompletionResponse{Content: ""}, // force template fallback
	})
	scoreGeneric := a.ScoreHashtags([]string{"#sale", "#cute", "#fyp", "#shopping"}, ProductInfo{Title: "Earbuds"})
	scoreNiche := a.ScoreHashtags([]string{"#wirelessearbuds", "#anc", "#audiophile", "#earbudreview"}, ProductInfo{Title: "Earbuds"})
	if scoreNiche <= scoreGeneric {
		t.Fatalf("niche %.2f should beat generic %.2f", scoreNiche, scoreGeneric)
	}
}

func TestHashtagAgent_CaptionRespectLengthLimits(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("Premium product. ", 200) // > 2200 chars
	llm := &stubHashtagLLM{
		response: port.AICompletionResponse{Content: jsonResponse([]string{"#a", "#b", "#c"}, long)},
	}
	a := newHashtagAgentHarness(t, llm)
	_, err := a.Generate(context.Background(), HashtagCaptionRequest{
		Product:  ProductInfo{ID: "p-1", Title: "Earbuds"},
		Platform: "tiktok",
	})
	if !errors.Is(err, ErrCaptionTooLong) {
		t.Fatalf("expected ErrCaptionTooLong, got %v", err)
	}
}

func TestHashtagAgent_LLMFailoverToRules(t *testing.T) {
	t.Parallel()
	llm := &stubHashtagLLM{err: errors.New("llm down")}
	a := newHashtagAgentHarness(t, llm)
	res, err := a.Generate(context.Background(), HashtagCaptionRequest{
		Product:  ProductInfo{ID: "p-1", Title: "Wireless Earbuds", Description: "Compact ANC earbuds with bluetooth audio"},
		Platform: "tiktok",
		Keywords: []string{"earbuds", "anc"},
	})
	if err != nil {
		t.Fatalf("Generate (expected fallback): %v", err)
	}
	if res.Source != HashtagCaptionSourceRule {
		t.Fatalf("expected rule fallback, got %s", res.Source)
	}
	if len(res.Hashtags) == 0 {
		t.Fatalf("rule fallback should generate at least one hashtag")
	}
	if res.Caption == "" {
		t.Fatalf("rule fallback should generate a caption")
	}
}

func TestHashtagAgent_PlatformSpecificTone(t *testing.T) {
	t.Parallel()
	platforms := []string{"tiktok", "rednote", "facebook", "instagram"}
	captions := map[string]string{
		"tiktok":    "🔥 Game-changing earbuds! 🎧",
		"rednote":   "种草这款耳机～音质绝了 ✨",
		"facebook":  "Discover the all-new wireless earbuds. Quality you can trust.",
		"instagram": "Sleek. Wireless. Yours. 🎧✨",
	}
	for _, plat := range platforms {
		t.Run(plat, func(t *testing.T) {
			llm := &stubHashtagLLM{
				response: port.AICompletionResponse{
					Content: jsonResponse([]string{"#tagA", "#tagB", "#tagC", "#tagD", "#tagE"}, captions[plat]),
				},
			}
			a := newHashtagAgentHarness(t, llm)
			res, err := a.Generate(context.Background(), HashtagCaptionRequest{
				Product:  ProductInfo{ID: "p-1", Title: "Earbuds"},
				Platform: plat,
			})
			if err != nil {
				t.Fatalf("Generate %s: %v", plat, err)
			}
			if res.Caption != captions[plat] {
				t.Fatalf("%s caption mismatch got=%q want=%q", plat, res.Caption, captions[plat])
			}
			if !strings.Contains(llm.last.Messages[0].Content, plat) {
				t.Fatalf("%s system prompt missing platform tag: %q", plat, llm.last.Messages[0].Content)
			}
		})
	}
}

func TestHashtagAgent_UnsupportedPlatform(t *testing.T) {
	t.Parallel()
	a := newHashtagAgentHarness(t, &stubHashtagLLM{})
	_, err := a.Generate(context.Background(), HashtagCaptionRequest{
		Product:  ProductInfo{ID: "p-1", Title: "Earbuds"},
		Platform: "twitter",
	})
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("expected ErrUnsupportedPlatform, got %v", err)
	}
}

func TestHashtagAgent_AppendPlatformDefaults(t *testing.T) {
	t.Parallel()
	for _, plat := range []HashtagPlatform{
		HashtagPlatformTikTok, HashtagPlatformRedNote, HashtagPlatformFacebook, HashtagPlatformInstagram,
	} {
		out := appendPlatformDefaults([]string{"#brand"}, plat)
		if len(out) <= 1 {
			t.Errorf("%s: expected platform-default tags appended, got %v", plat, out)
		}
	}
}

func TestHashtagAgent_TemplateFallbackOnNilLLM(t *testing.T) {
	t.Parallel()
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	a, err := NewHashtagAgent(nil, HashtagAgentConfig{TenantID: "tenant-1", Now: func() time.Time { return clk }})
	if err != nil {
		t.Fatalf("NewHashtagAgent: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	res, err := a.Generate(context.Background(), HashtagCaptionRequest{
		Product:   ProductInfo{ID: "p-1", Title: "Wireless Earbuds", Description: "Compact ANC earbuds"},
		Platform:  "rednote",
		Keywords:  []string{"earbuds"},
		BrandTags: []string{"acmebrand"},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.Source != HashtagCaptionSourceRule {
		t.Fatalf("expected rule source when LLM nil, got %s", res.Source)
	}
}

func TestHashtagAgent_BiasProviderInfluence(t *testing.T) {
	t.Parallel()
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	bias := stubBiasProvider{bias: HashtagBias{PreferLongerCaption: true, BiasHashtagCount: 12, EMAScore: 80}}
	llm := &stubHashtagLLM{response: port.AICompletionResponse{Content: jsonResponse([]string{"#a", "#b", "#c"}, "Caption")}}
	a, err := NewHashtagAgent(nil, HashtagAgentConfig{
		TenantID:     "tenant-1",
		Generator:    llm,
		BiasProvider: bias,
		Now:          func() time.Time { return clk },
	})
	if err != nil {
		t.Fatalf("NewHashtagAgent: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	if _, err := a.Generate(context.Background(), HashtagCaptionRequest{
		Product:  ProductInfo{ID: "p-1", Title: "Earbuds"},
		Platform: "tiktok",
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(llm.last.Messages[0].Content, "longer captions") {
		t.Fatalf("expected bias hint in system prompt: %q", llm.last.Messages[0].Content)
	}
}

type stubBiasProvider struct {
	bias HashtagBias
}

func (s stubBiasProvider) BiasFor(_, _ string) (HashtagBias, bool) { return s.bias, true }

func TestHashtagAgent_ClosedReturnsError(t *testing.T) {
	t.Parallel()
	a := newHashtagAgentHarness(t, &stubHashtagLLM{})
	if err := a.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := a.Generate(context.Background(), HashtagCaptionRequest{Product: ProductInfo{Title: "x"}, Platform: "tiktok"})
	if !errors.Is(err, ErrHashtagAgentClosed) {
		t.Fatalf("expected ErrHashtagAgentClosed, got %v", err)
	}
}

// jsonResponse builds the LLM JSON response shape the agent parses.
func jsonResponse(hashtags []string, caption string) string {
	var sb strings.Builder
	sb.WriteString(`{"hashtags":[`)
	for i, h := range hashtags {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"` + h + `"`)
	}
	sb.WriteString(`],"caption":"`)
	sb.WriteString(strings.ReplaceAll(caption, `"`, `\"`))
	sb.WriteString(`"}`)
	return sb.String()
}
