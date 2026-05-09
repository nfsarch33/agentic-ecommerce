// File scope: v3.4.1 EC-5-3 video assembler format validation.
//
// The plan calls out six scenarios:
//
//  1. 60s TikTok script (1080x1920) -> stub returns valid MP4 envelope
//  2. 60s RedNote script (1080x1080) -> stub returns valid MP4 envelope
//  3. 30s Instagram Reels script (1080x1920) -> stub returns valid MP4 envelope
//  4. Invalid script (no scenes)            -> ErrVideoScriptInvalid
//  5. LiveBridgeVideoAssembler with unset URL -> ErrVideoBridgeUnconfigured
//  6. Bridge URL wire-shape contract:        signed POST with X-Bridge-
//     Timestamp + X-Bridge-Sign
//
// The tests run table-driven so the v3.4.1 PR body can drop the
// scenario list straight into the evidence section. Live ffmpeg +
// 120s wall-clock acceptance is deferred to the live bridge sprint
// per the plan's note ("this QA validates stub format + bridge
// contract only").
//
// Cite skill: go-clean-architecture (port + adapter -- the test
// drives the StubVideoAssembler concrete impl + the BridgeVideoAssembler
// wire-shape helper without touching the pipeline composition root).
package media

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// videoFormatScenario is one row in the v3.4.1 stub format table.
// Pure data; no IO.
type videoFormatScenario struct {
	name              string
	platform          string
	format            VideoFormat
	durationSec       int
	subtitleLines     []string
	branding          string
	scenes            []string // optional; nil keeps v3.4.0 behaviour
	wantOutputBytes   bool
	wantErrSentinel   error
	expectMagic       bool
	expectFormatTag   string
	expectFingerprint bool
}

// stubFormatScenarios is the canonical v3.4.1 happy-path + error
// scenario set the QA report cites. Adding rows here keeps the PR
// body in sync with the test surface.
func stubFormatScenarios() []videoFormatScenario {
	return []videoFormatScenario{
		{
			name:              "tiktok_60s_vertical_valid_envelope",
			platform:          "tiktok",
			format:            VideoFormatVertical,
			durationSec:       60,
			subtitleLines:     []string{"00:00 hook", "00:07 problem", "00:18 demo", "00:50 cta"},
			branding:          "@tiktok-brand",
			scenes:            []string{"hook", "problem", "demo", "cta"},
			wantOutputBytes:   true,
			expectMagic:       true,
			expectFormatTag:   "1080x1920",
			expectFingerprint: true,
		},
		{
			name:              "rednote_60s_square_valid_envelope",
			platform:          "rednote",
			format:            VideoFormatSquare,
			durationSec:       60,
			subtitleLines:     []string{"00:00 ping", "00:15 lifestyle", "00:30 demo", "00:50 cta"},
			branding:          "@rednote-brand",
			scenes:            []string{"intro", "lifestyle", "demo", "cta"},
			wantOutputBytes:   true,
			expectMagic:       true,
			expectFormatTag:   "1080x1080",
			expectFingerprint: true,
		},
		{
			name:              "instagram_reels_30s_vertical_valid_envelope",
			platform:          "instagram",
			format:            VideoFormatVertical,
			durationSec:       30,
			subtitleLines:     []string{"00:00 hook", "00:08 demo", "00:24 cta"},
			branding:          "@reels-brand",
			scenes:            []string{"hook", "demo", "cta"},
			wantOutputBytes:   true,
			expectMagic:       true,
			expectFormatTag:   "1080x1920",
			expectFingerprint: true,
		},
		{
			name:            "invalid_script_no_scenes_returns_script_invalid",
			platform:        "tiktok",
			format:          VideoFormatVertical,
			durationSec:     60,
			scenes:          []string{},
			wantErrSentinel: ErrVideoScriptInvalid,
		},
	}
}

// TestVideoAssembler_StubFormatValidation_AllScenarios is the EC-5-3
// v3.4.1 acceptance ("StubVideoAssembler MP4 format validation").
// Decomposition: the per-row work runs through runFormatScenario
// so the top-level body stays a thin loop driver. Sentrux
// complex_fn guard: cyclomatic stays at 1.
func TestVideoAssembler_StubFormatValidation_AllScenarios(t *testing.T) {
	t.Parallel()

	for _, sc := range stubFormatScenarios() {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			runFormatScenario(t, sc)
		})
	}
}

func runFormatScenario(t *testing.T, sc videoFormatScenario) {
	t.Helper()
	pipeline := newFormatPipeline(t, sc.platform)
	req := VideoAssemblyRequest{
		TenantID:        "tenant-v341",
		ProductID:       "p-" + sc.name,
		Action:          VideoActionStubAssemble,
		Format:          sc.format,
		HeroImageURLs:   []string{"https://cdn.example.com/" + sc.name + ".jpg"},
		VoiceoverScript: "Stop scrolling. Meet the product.",
		SubtitleLines:   sc.subtitleLines,
		BrandingOverlay: sc.branding,
		BackgroundMusic: "https://cdn.example.com/bg-" + sc.platform + ".mp3",
		DurationSec:     sc.durationSec,
		Scenes:          sc.scenes,
	}
	res, err := pipeline.Assemble(context.Background(), req)
	if sc.wantErrSentinel != nil {
		if !errors.Is(err, sc.wantErrSentinel) {
			t.Fatalf("err = %v, want %v", err, sc.wantErrSentinel)
		}
		return
	}
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	assertStubFormatEnvelope(t, sc, res)
	t.Logf("v3.4.1 EC-5-3 stub format -- platform=%s format=%s duration=%ds bytes=%d magic_ok=%v fingerprint_ok=%v",
		sc.platform, sc.format, sc.durationSec, len(res.OutputBytes), sc.expectMagic, sc.expectFingerprint)
}

// newFormatPipeline wires a fresh pipeline per row so per-row
// assertions never collide.
func newFormatPipeline(t *testing.T, platform string) *VideoAssemblyPipeline {
	t.Helper()
	pipeline, err := NewVideoAssemblyPipeline(nil, VideoAssemblyPipelineConfig{
		Assembler: NewStubVideoAssembler(),
		TenantID:  "tenant-v341",
		KeyPrefix: platform,
		Now:       func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewVideoAssemblyPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })
	return pipeline
}

// assertStubFormatEnvelope validates the deterministic stub MP4
// envelope shape. Pulled out so runFormatScenario stays small.
func assertStubFormatEnvelope(t *testing.T, sc videoFormatScenario, res VideoAssemblyResult) {
	t.Helper()
	if res.OutputContentType != "video/mp4" {
		t.Fatalf("OutputContentType = %q, want video/mp4", res.OutputContentType)
	}
	if !sc.wantOutputBytes && len(res.OutputBytes) != 0 {
		t.Fatalf("OutputBytes len = %d, want 0", len(res.OutputBytes))
	}
	if sc.wantOutputBytes && len(res.OutputBytes) == 0 {
		t.Fatalf("OutputBytes empty")
	}
	body := string(res.OutputBytes)
	if sc.expectMagic && !strings.Contains(body, "ftypmp42") {
		t.Fatalf("missing ftypmp42 magic in output (first 32 bytes %q)", res.OutputBytes[:32])
	}
	if sc.expectFormatTag != "" && !strings.Contains(body, "format="+sc.expectFormatTag) {
		t.Fatalf("output missing format=%s tag (got body=%q)", sc.expectFormatTag, body)
	}
	if sc.expectFingerprint && !strings.Contains(body, "voiceover_sha256=") {
		t.Fatalf("output missing voiceover_sha256 fingerprint")
	}
	if !res.Deterministic {
		t.Fatalf("expected stub to mark Deterministic=true")
	}
}

// TestVideoAssembler_LiveBridgeUnconfiguredAlwaysFails covers
// scenario 5 ("LiveBridgeVideoAssembler with unset URL ->
// ErrVideoBridgeUnconfigured") explicitly. Splits constructor +
// runtime checks into two table rows so reviewers can see both
// gates fire.
func TestVideoAssembler_LiveBridgeUnconfiguredAlwaysFails(t *testing.T) {
	t.Parallel()
	cases := map[string]BridgeVideoAssemblerConfig{
		"missing url":    {BridgeSecret: validVideoBridgeSecret(), Timeout: 10 * time.Second},
		"missing secret": {BridgeURL: "video-bridge-node-a", Timeout: 10 * time.Second},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewBridgeVideoAssembler(cfg)
			if !errors.Is(err, ErrVideoBridgeUnconfigured) {
				t.Fatalf("err = %v, want ErrVideoBridgeUnconfigured", err)
			}
		})
	}
	configured, err := NewBridgeVideoAssembler(BridgeVideoAssemblerConfig{
		BridgeURL:    "https://bridge.invalid/video/assemble",
		BridgeSecret: validVideoBridgeSecret(),
		Timeout:      10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewBridgeVideoAssembler: %v", err)
	}
	_, err = configured.Assemble(context.Background(), VideoAssemblyRequest{
		ProductID:       "p-bridge",
		VoiceoverScript: "v",
		Action:          VideoActionLiveAssemble,
	})
	if !errors.Is(err, ErrVideoBridgeUnconfigured) {
		t.Fatalf("Assemble err = %v, want ErrVideoBridgeUnconfigured", err)
	}
}

// TestVideoAssembler_BridgeWireShapeContract is scenario 6
// ("Bridge URL wire-shape contract: signed POST with
// X-Bridge-Timestamp + X-Bridge-Sign"). Verifies the wire shape
// the live bridge POST will use without performing the network
// round-trip.
func TestVideoAssembler_BridgeWireShapeContract(t *testing.T) {
	t.Parallel()
	bridge, err := NewBridgeVideoAssembler(BridgeVideoAssemblerConfig{
		BridgeURL:    "https://video-bridge.example.com" + VideoBridgePostPath,
		BridgeSecret: validVideoBridgeSecret(),
		Timeout:      30 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewBridgeVideoAssembler: %v", err)
	}
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	req := VideoAssemblyRequest{
		TenantID:        "tenant-v341",
		ProductID:       "p-bridge-wire",
		Action:          VideoActionLiveAssemble,
		Format:          VideoFormatVertical,
		VoiceoverScript: "Stop scrolling.",
		Scenes:          []string{"hook", "demo", "cta"},
		DurationSec:     60,
	}
	httpReq, err := bridge.BuildSignedRequest(context.Background(), req, now)
	if err != nil {
		t.Fatalf("BuildSignedRequest: %v", err)
	}
	if httpReq.Method != "POST" {
		t.Fatalf("Method = %q, want POST", httpReq.Method)
	}
	if got := httpReq.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := httpReq.Header.Get("X-Bridge-Timestamp"); got == "" {
		t.Fatal("X-Bridge-Timestamp header missing")
	}
	if got := httpReq.Header.Get("X-Bridge-Sign"); got == "" {
		t.Fatal("X-Bridge-Sign header missing")
	}
	if got := httpReq.Header.Get("X-Bridge-Tenant"); got != "tenant-v341" {
		t.Fatalf("X-Bridge-Tenant = %q, want tenant-v341", got)
	}
	if got := httpReq.Header.Get("X-Bridge-Platform"); got != VideoBridgePlatform {
		t.Fatalf("X-Bridge-Platform = %q, want %q", got, VideoBridgePlatform)
	}
	if !strings.HasSuffix(httpReq.URL.String(), VideoBridgePostPath) {
		t.Fatalf("URL = %q, want suffix %q", httpReq.URL.String(), VideoBridgePostPath)
	}
}

// TestVideoAssembler_BridgeSignatureChangesPerRequest verifies
// signature determinism: two distinct request bodies MUST produce
// two distinct signatures so a replay of an earlier signed payload
// cannot impersonate a later request.
func TestVideoAssembler_BridgeSignatureChangesPerRequest(t *testing.T) {
	t.Parallel()
	bridge, err := NewBridgeVideoAssembler(BridgeVideoAssemblerConfig{
		BridgeURL:    "https://video-bridge.example.com" + VideoBridgePostPath,
		BridgeSecret: validVideoBridgeSecret(),
		Timeout:      30 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewBridgeVideoAssembler: %v", err)
	}
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	reqA := VideoAssemblyRequest{TenantID: "t", ProductID: "a", VoiceoverScript: "v", Action: VideoActionLiveAssemble, Scenes: []string{"a"}}
	reqB := VideoAssemblyRequest{TenantID: "t", ProductID: "b", VoiceoverScript: "v", Action: VideoActionLiveAssemble, Scenes: []string{"b"}}
	httpA, err := bridge.BuildSignedRequest(context.Background(), reqA, now)
	if err != nil {
		t.Fatalf("BuildSignedRequest A: %v", err)
	}
	httpB, err := bridge.BuildSignedRequest(context.Background(), reqB, now)
	if err != nil {
		t.Fatalf("BuildSignedRequest B: %v", err)
	}
	if sigA, sigB := httpA.Header.Get("X-Bridge-Sign"), httpB.Header.Get("X-Bridge-Sign"); sigA == sigB {
		t.Fatalf("X-Bridge-Sign matches for distinct payloads: %s == %s", sigA, sigB)
	}
}

// TestVideoAssembler_BridgeConfigSurface returns the resolved
// config (sans secret) so admin surfaces can introspect.
func TestVideoAssembler_BridgeConfigSurface(t *testing.T) {
	t.Parallel()
	bridge, err := NewBridgeVideoAssembler(BridgeVideoAssemblerConfig{
		BridgeURL:    "https://video-bridge.example.com" + VideoBridgePostPath,
		BridgeSecret: validVideoBridgeSecret(),
	})
	if err != nil {
		t.Fatalf("NewBridgeVideoAssembler: %v", err)
	}
	cfg := bridge.Config()
	if cfg.Timeout != 120*time.Second {
		t.Fatalf("Timeout default = %v, want 120s", cfg.Timeout)
	}
	if cfg.BridgeURL == "" {
		t.Fatal("BridgeURL missing in Config snapshot")
	}
	if len(cfg.BridgeSecret) != 0 {
		t.Fatalf("Config().BridgeSecret should not leak: got %d bytes", len(cfg.BridgeSecret))
	}
}

// validVideoBridgeSecret returns a 32-byte secret matching the
// social.MinTikTokSecretBytes minimum so the constructor never
// returns ErrVideoBridgeUnconfigured for the secret-length check.
func validVideoBridgeSecret() []byte {
	return []byte("v341-video-bridge-secret-32byte!!")
}
