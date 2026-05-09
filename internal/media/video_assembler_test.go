package media

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestStubVideoAssembler_ProducesMP4WithSubtitlesAndBranding is
// the EC-5-3 RED acceptance test. Verifies the deterministic stub
// emits a recognisable MP4 envelope, embeds subtitles + branding
// markers, and produces a stable byte fingerprint across runs.
func TestStubVideoAssembler_ProducesMP4WithSubtitlesAndBranding(t *testing.T) {
	t.Parallel()

	asm := NewStubVideoAssembler()
	pipeline, err := NewVideoAssemblyPipeline(nil, VideoAssemblyPipelineConfig{
		Assembler: asm,
		TenantID:  "tenant-1",
		KeyPrefix: "tiktok",
		Now:       func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewVideoAssemblyPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })

	req := VideoAssemblyRequest{
		TenantID:        "tenant-1",
		ProductID:       "earbuds-001",
		Format:          VideoFormatVertical,
		Action:          VideoActionStubAssemble,
		HeroImageURLs:   []string{"https://cdn.example.com/img1.jpg"},
		VoiceoverScript: "Stop scrolling. Meet the wireless earbuds.",
		SubtitleLines:   []string{"00:00 hook", "00:07 problem", "00:18 demo"},
		BrandingOverlay: "@brand_handle",
		BackgroundMusic: "https://cdn.example.com/bg.mp3",
		DurationSec:     60,
	}
	res, err := pipeline.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if res.OutputContentType != "video/mp4" {
		t.Fatalf("OutputContentType = %q, want video/mp4", res.OutputContentType)
	}
	if len(res.OutputBytes) == 0 {
		t.Fatalf("OutputBytes empty")
	}
	// MP4 ftyp magic detection: bytes 4..12 should contain "ftypmp42".
	if !strings.Contains(string(res.OutputBytes), "ftypmp42") {
		t.Fatalf("missing ftypmp42 magic in output: %q", res.OutputBytes[:32])
	}
	if !strings.Contains(string(res.OutputBytes), "subtitles=00:00 hook|") {
		t.Fatalf("output missing subtitles marker")
	}
	if !strings.Contains(string(res.OutputBytes), "branding=@brand_handle") {
		t.Fatalf("output missing branding marker")
	}
	if !res.HasSubtitles || !res.HasBranding {
		t.Fatalf("HasSubtitles/HasBranding flags wrong: %+v", res)
	}
	if !res.Deterministic {
		t.Fatalf("expected stub to mark Deterministic=true")
	}
	wantKey := "tenants/tenant-1/tiktok/products/earbuds-001/video.mp4"
	if res.OutputKey != wantKey {
		t.Fatalf("OutputKey = %q, want %q", res.OutputKey, wantKey)
	}

	// Determinism: same inputs -> same bytes.
	res2, err := pipeline.Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("Assemble (second): %v", err)
	}
	if string(res.OutputBytes) != string(res2.OutputBytes) {
		t.Fatalf("stub output not deterministic across runs")
	}
}

func TestVideoAssemblyPipeline_GuardsInputs(t *testing.T) {
	t.Parallel()
	pipeline, err := NewVideoAssemblyPipeline(nil, VideoAssemblyPipelineConfig{
		Assembler: NewStubVideoAssembler(),
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewVideoAssemblyPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })
	cases := []VideoAssemblyRequest{
		{},
		{ProductID: "p"},
	}
	for i, req := range cases {
		req := req
		t.Run([]string{"missing-product", "missing-voiceover"}[i], func(t *testing.T) {
			t.Parallel()
			_, err := pipeline.Assemble(context.Background(), req)
			if !errors.Is(err, ErrVideoAssemblerUnconfigured) {
				t.Fatalf("err = %v, want ErrVideoAssemblerUnconfigured", err)
			}
		})
	}
}

func TestVideoAssemblyPipeline_RejectsAfterClose(t *testing.T) {
	t.Parallel()
	pipeline, err := NewVideoAssemblyPipeline(nil, VideoAssemblyPipelineConfig{
		Assembler: NewStubVideoAssembler(),
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewVideoAssemblyPipeline: %v", err)
	}
	_ = pipeline.Close(context.Background())
	_, err = pipeline.Assemble(context.Background(), VideoAssemblyRequest{ProductID: "p", VoiceoverScript: "v"})
	if !errors.Is(err, ErrVideoAssemblerClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewVideoAssemblyPipeline_ConfigValidation(t *testing.T) {
	t.Parallel()
	cases := map[string]VideoAssemblyPipelineConfig{
		"missing assembler": {TenantID: "t"},
		"missing tenant":    {Assembler: NewStubVideoAssembler()},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewVideoAssemblyPipeline(nil, cfg)
			if !errors.Is(err, ErrVideoAssemblerUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestBridgeVideoAssembler_AlwaysUnconfiguredInV340(t *testing.T) {
	t.Parallel()
	asm, err := NewBridgeVideoAssembler(BridgeVideoAssemblerConfig{
		BridgeURL:    "video-bridge-node-a", // runx alias
		BridgeSecret: []byte("ignored-because-not-implemented-yet"),
	})
	if err != nil {
		t.Fatalf("NewBridgeVideoAssembler: %v", err)
	}
	_, err = asm.Assemble(context.Background(), VideoAssemblyRequest{ProductID: "p", VoiceoverScript: "v", Action: VideoActionLiveAssemble})
	if !errors.Is(err, ErrVideoBridgeUnconfigured) {
		t.Fatalf("err = %v, want ErrVideoBridgeUnconfigured", err)
	}
}

func TestNewBridgeVideoAssembler_ValidatesRequiredFields(t *testing.T) {
	t.Parallel()
	cases := map[string]BridgeVideoAssemblerConfig{
		"missing url":    {BridgeSecret: []byte("s")},
		"missing secret": {BridgeURL: "alias"},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewBridgeVideoAssembler(cfg)
			if !errors.Is(err, ErrVideoBridgeUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestStubVideoAssembler_DefaultsApplied(t *testing.T) {
	t.Parallel()
	pipeline, err := NewVideoAssemblyPipeline(nil, VideoAssemblyPipelineConfig{
		Assembler: NewStubVideoAssembler(),
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewVideoAssemblyPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })
	res, err := pipeline.Assemble(context.Background(), VideoAssemblyRequest{
		TenantID:        "tenant-1",
		ProductID:       "p-defaults",
		VoiceoverScript: "v",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if res.Format != VideoFormatVertical {
		t.Fatalf("Format default = %q", res.Format)
	}
	if res.Action != VideoActionStubAssemble {
		t.Fatalf("Action default = %q", res.Action)
	}
}

func TestVideoAssemblyMetricsHookFires(t *testing.T) {
	t.Parallel()
	hits := 0
	pipeline, err := NewVideoAssemblyPipeline(nil, VideoAssemblyPipelineConfig{
		Assembler:   NewStubVideoAssembler(),
		TenantID:    "tenant-1",
		MetricsHook: func(_ VideoAssemblyAction, _ string, _ time.Duration, _ int) { hits++ },
	})
	if err != nil {
		t.Fatalf("NewVideoAssemblyPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })
	_, err = pipeline.Assemble(context.Background(), VideoAssemblyRequest{
		TenantID:        "tenant-1",
		ProductID:       "p",
		VoiceoverScript: "v",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if hits != 1 {
		t.Fatalf("metrics hook hits = %d, want 1", hits)
	}
}

func TestVideoAssemblyPipeline_FailedAssemblerWraps(t *testing.T) {
	t.Parallel()
	pipeline, err := NewVideoAssemblyPipeline(nil, VideoAssemblyPipelineConfig{
		Assembler: &failingAssembler{err: errors.New("boom")},
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewVideoAssemblyPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })
	_, err = pipeline.Assemble(context.Background(), VideoAssemblyRequest{ProductID: "p", VoiceoverScript: "v"})
	if !errors.Is(err, ErrVideoAssemblyFailed) {
		t.Fatalf("err = %v", err)
	}
}

type failingAssembler struct {
	err error
}

func (f *failingAssembler) Assemble(_ context.Context, _ VideoAssemblyRequest) (VideoAssemblyResult, error) {
	return VideoAssemblyResult{}, f.err
}
