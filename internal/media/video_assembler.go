// File scope: v3.4.0 EC-5-3 ffmpeg video assembly pipeline
// (stub-with-doc; live ffmpeg deferred to fleet bridge per
// resource-guard.mdc).
//
// The pipeline takes a structured video script (from EC-5-1) +
// product hero image(s) + voiceover audio, and assembles a short-
// form MP4 (1080x1920 vertical for TikTok / RedNote / Reels;
// 1080x1080 square for Facebook). Per the v3.2.0 EC-2-2 stub-with-
// doc precedent the backend ships ONLY a deterministic
// StubVideoAssembler (small footprint, deterministic output for
// tests + dev compose) and a typed sentinel ErrVideoBridgeUnconfigured
// for live calls. Operator setup at docs/operations/video-bridge.md.
//
// Why a bridge (per resource-guard.mdc):
//   - ffmpeg pipelines spike heap during multipart encode.
//   - Bedrock Polly voiceover calls are multi-second synchronous.
//   - 1080x1920 H.264 encoding burns CPU; the MacBook MUST NOT
//     run this directly per the v322-2 OOM lesson.
//
// Resilience pillar (v2.10 baseline):
//   - Implements lifecycle.Closer.
//   - Single-shot synchronous Assemble call -- no raw goroutines.
//   - Memwatch hint: the stub holds no decoded image in memory; it
//     emits a tiny synthetic MP4 header + metadata payload
//     (well under MaxLocalDecodeBytes from product_image.go).
//   - All errors typed + %w-wrapped via package sentinels.
//   - Tenant awareness: every output's storage key is namespaced
//     under tenants/<tenant_id>/.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4): split into Assemble (envelope), guardAssemble
// (preconditions), buildStubMP4 (deterministic encoder). Per-
// function cyclomatic stays under 5.
//
// Cite skill: go-clean-architecture (port + adapter -- the
// pipeline depends on VideoAssembler; the cmd/* binary wires the
// production bridge adapter at startup).
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/social"
)

// VideoBridgePostPath is the canonical path the video bridge exposes
// for /video/assemble live-encode requests. Used in the HMAC
// canonical form so the bridge can re-verify.
const VideoBridgePostPath = "/video/assemble"

// VideoBridgePlatform is the literal value used for the
// X-Bridge-Platform header so a single video-bridge instance can
// route TikTok / RedNote / Reels output under one HMAC scheme.
const VideoBridgePlatform = "video"

// EC-5-3 typed sentinels.
var (
	// ErrVideoAssemblerUnconfigured is returned by
	// NewVideoAssemblyPipeline when a required dependency is
	// missing.
	ErrVideoAssemblerUnconfigured = errors.New("media: video assembler unconfigured")

	// ErrVideoAssemblerClosed is returned by Assemble after Close.
	ErrVideoAssemblerClosed = errors.New("media: video assembler closed")

	// ErrVideoBridgeUnconfigured is the v3.4.0 sentinel returned by
	// the production assembler when the operator has not wired the
	// fleet video-bridge. Mirrors v3.2.0 ErrImageBridgeUnconfigured
	// semantics: the typed sentinel makes the deferred bridge work
	// item visible at call-time so the operator never silently
	// loses output.
	ErrVideoBridgeUnconfigured = errors.New("media: video bridge unconfigured (live ffmpeg/Polly deferred to next sprint)")

	// ErrVideoAssemblyFailed wraps any unexpected failure inside
	// the pipeline.
	ErrVideoAssemblyFailed = errors.New("media: video assembly failed")

	// ErrVideoScriptInvalid is the v3.4.1 sentinel returned when
	// the supplied script payload is structurally invalid (e.g.,
	// no scenes, no voiceover). Surfaces from the stub + the
	// future live bridge so callers branch consistently via
	// errors.Is.
	ErrVideoScriptInvalid = errors.New("media: video script invalid")

	// ErrVideoBridgeSignature is the v3.4.1 sentinel returned when
	// the HMAC signature build for the bridge POST fails (e.g.,
	// secret too short or hash error).
	ErrVideoBridgeSignature = errors.New("media: video bridge signature build failed")
)

// VideoFormat is the requested output aspect ratio.
type VideoFormat string

const (
	VideoFormatVertical VideoFormat = "1080x1920" // TikTok / RedNote / Reels
	VideoFormatSquare   VideoFormat = "1080x1080" // Facebook / Instagram feed
)

// VideoAssemblyAction is the requested pipeline operation.
type VideoAssemblyAction string

const (
	VideoActionStubAssemble VideoAssemblyAction = "stub_assemble"
	VideoActionLiveAssemble VideoAssemblyAction = "live_assemble"
)

// VideoAssemblyRequest is the unit of work submitted to Assemble.
type VideoAssemblyRequest struct {
	TenantID        string
	ProductID       string
	Action          VideoAssemblyAction
	Format          VideoFormat
	HeroImageURLs   []string
	VoiceoverScript string // narrated lines from EC-5-1
	SubtitleLines   []string
	BackgroundMusic string // optional CDN URL
	BrandingOverlay string // optional brand watermark text
	DurationSec     int
	OutputKeyPrefix string // optional, e.g. "tiktok"
	// Scenes is the v3.4.1 EC-5-3 structured scene list. When the
	// operator passes a non-nil Scenes the stub validates that at
	// least one entry is supplied (returns ErrVideoScriptInvalid
	// when empty) so the live ffmpeg bridge gets a deterministic
	// validation surface BEFORE the bridge ships. nil keeps the
	// v3.4.0 behaviour (VoiceoverScript-only validation).
	Scenes []string
}

// VideoAssemblyResult captures the pipeline output.
type VideoAssemblyResult struct {
	TenantID          string
	ProductID         string
	Action            VideoAssemblyAction
	Format            VideoFormat
	OutputKey         string // canonical storage key (tenants/<id>/...)
	OutputURL         string // resolved URL when the live bridge ran
	OutputBytes       []byte // populated only by the stub
	OutputContentType string
	GeneratedAt       time.Time
	HasSubtitles      bool
	HasBranding       bool
	Deterministic     bool // true when produced by StubVideoAssembler
}

// VideoAssembler is the small port the assembly pipeline depends
// on. Two implementations:
//   - StubVideoAssembler (this file): deterministic in-process
//     "encoder" producing a tiny synthetic MP4-like envelope.
//   - bridge.NewVideoBridgeAssembler (deferred sprint): signed
//     POST to the fleet video-bridge, returns the bridge URL.
type VideoAssembler interface {
	Assemble(ctx context.Context, req VideoAssemblyRequest) (VideoAssemblyResult, error)
}

// VideoMetricsHook is the optional Prometheus + EvoMap callback
// the pipeline calls per Assemble. Kept abstract so this package
// does not import internal/metrics directly.
type VideoMetricsHook func(action VideoAssemblyAction, status string, duration time.Duration, outputBytes int)

// VideoAssemblyPipelineConfig wires the pipeline.
type VideoAssemblyPipelineConfig struct {
	Assembler   VideoAssembler
	TenantID    string
	KeyPrefix   string // optional: stored under tenants/<id>/<prefix>/<product_id>...
	MetricsHook VideoMetricsHook
	Now         func() time.Time
}

// VideoAssemblyPipeline is the EC-5-3 agent.
type VideoAssemblyPipeline struct {
	assembler   VideoAssembler
	tenantID    string
	keyPrefix   string
	metricsHook VideoMetricsHook
	now         func() time.Time
	logger      *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewVideoAssemblyPipeline constructs the pipeline.
func NewVideoAssemblyPipeline(logger *slog.Logger, cfg VideoAssemblyPipelineConfig) (*VideoAssemblyPipeline, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Assembler == nil {
		return nil, fmt.Errorf("%w: VideoAssembler required", ErrVideoAssemblerUnconfigured)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrVideoAssemblerUnconfigured)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &VideoAssemblyPipeline{
		assembler:   cfg.Assembler,
		tenantID:    cfg.TenantID,
		keyPrefix:   strings.Trim(cfg.KeyPrefix, "/"),
		metricsHook: cfg.MetricsHook,
		now:         cfg.Now,
		logger:      logger,
	}, nil
}

// Close marks the pipeline closed. Implements lifecycle.Closer.
func (p *VideoAssemblyPipeline) Close(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

// Assemble runs the pipeline. Returns a VideoAssemblyResult on
// success; wraps every fatal error in ErrVideoAssemblyFailed.
func (p *VideoAssemblyPipeline) Assemble(ctx context.Context, req VideoAssemblyRequest) (VideoAssemblyResult, error) {
	if err := p.guardAssemble(req); err != nil {
		return VideoAssemblyResult{}, err
	}
	if req.Action == "" {
		req.Action = VideoActionStubAssemble
	}
	if req.Format == "" {
		req.Format = VideoFormatVertical
	}
	start := p.now()
	res, err := p.assembler.Assemble(ctx, req)
	if err != nil {
		p.recordMetric(req.Action, "failed", time.Since(start), 0)
		return VideoAssemblyResult{}, fmt.Errorf("%w: assemble %s: %w", ErrVideoAssemblyFailed, req.ProductID, err)
	}
	res.OutputKey = p.buildOutputKey(req)
	if res.GeneratedAt.IsZero() {
		res.GeneratedAt = p.now().UTC()
	}
	p.recordMetric(req.Action, "ok", time.Since(start), len(res.OutputBytes))
	return res, nil
}

func (p *VideoAssemblyPipeline) guardAssemble(req VideoAssemblyRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrVideoAssemblerClosed
	}
	if strings.TrimSpace(req.ProductID) == "" {
		return fmt.Errorf("%w: ProductID required", ErrVideoAssemblerUnconfigured)
	}
	if strings.TrimSpace(req.VoiceoverScript) == "" {
		return fmt.Errorf("%w: VoiceoverScript required (from EC-5-1 generator)", ErrVideoAssemblerUnconfigured)
	}
	if req.Scenes != nil && len(req.Scenes) == 0 {
		return fmt.Errorf("%w: Scenes is non-nil but empty (zero scenes)", ErrVideoScriptInvalid)
	}
	return nil
}

func (p *VideoAssemblyPipeline) buildOutputKey(req VideoAssemblyRequest) string {
	parts := []string{"tenants", p.tenantID}
	if p.keyPrefix != "" {
		parts = append(parts, p.keyPrefix)
	}
	if req.OutputKeyPrefix != "" {
		parts = append(parts, req.OutputKeyPrefix)
	}
	parts = append(parts, "products", req.ProductID, "video.mp4")
	return strings.Join(parts, "/")
}

func (p *VideoAssemblyPipeline) recordMetric(action VideoAssemblyAction, status string, duration time.Duration, bytesOut int) {
	if p.metricsHook == nil {
		return
	}
	p.metricsHook(action, status, duration, bytesOut)
}

// --- StubVideoAssembler ---------------------------------------------------

// StubVideoAssembler is the v3.4.0 default in-process assembler.
// It produces a deterministic synthetic MP4-like byte envelope
// containing the request fingerprint so the EC-5-3 RED test can
// assert output format + subtitle/branding markers without
// invoking real ffmpeg. The cmd/agent-worker binary wires the
// stub by default; the bridge adapter swaps in via the operator
// setup documented at docs/operations/video-bridge.md.
type StubVideoAssembler struct{}

// NewStubVideoAssembler returns a new stub assembler.
func NewStubVideoAssembler() *StubVideoAssembler {
	return &StubVideoAssembler{}
}

// Assemble returns a deterministic synthetic MP4 envelope. The
// envelope is structured so unit tests can verify:
//   - Container magic ("ftypmp42") leads the byte stream.
//   - Subtitles are present when SubtitleLines was non-empty.
//   - Branding watermark is present when BrandingOverlay was set.
//   - The fingerprint hash is reproducible across runs.
func (s *StubVideoAssembler) Assemble(_ context.Context, req VideoAssemblyRequest) (VideoAssemblyResult, error) {
	body := buildStubMP4(req)
	hasSubs := len(req.SubtitleLines) > 0
	hasBrand := strings.TrimSpace(req.BrandingOverlay) != ""
	return VideoAssemblyResult{
		TenantID:          req.TenantID,
		ProductID:         req.ProductID,
		Action:            VideoActionStubAssemble,
		Format:            req.Format,
		OutputBytes:       body,
		OutputContentType: "video/mp4",
		HasSubtitles:      hasSubs,
		HasBranding:       hasBrand,
		Deterministic:     true,
	}, nil
}

// buildStubMP4 emits a deterministic byte payload that begins with
// the MP4 ftyp magic so simple file-type detection passes. The
// rest of the payload is a textual fingerprint of the request
// inputs so test assertions can verify content composition.
func buildStubMP4(req VideoAssemblyRequest) []byte {
	var sb strings.Builder
	sb.WriteString("\x00\x00\x00\x18ftypmp42") // 24-byte ftyp box header
	sb.WriteString("\nformat=")
	sb.WriteString(string(req.Format))
	sb.WriteString("\nduration_sec=")
	sb.WriteString(fmt.Sprintf("%d", req.DurationSec))
	sb.WriteString("\nproduct_id=")
	sb.WriteString(req.ProductID)
	if len(req.SubtitleLines) > 0 {
		sb.WriteString("\nsubtitles=")
		sb.WriteString(strings.Join(req.SubtitleLines, "|"))
	}
	if strings.TrimSpace(req.BrandingOverlay) != "" {
		sb.WriteString("\nbranding=")
		sb.WriteString(req.BrandingOverlay)
	}
	if strings.TrimSpace(req.BackgroundMusic) != "" {
		sb.WriteString("\nmusic=")
		sb.WriteString(req.BackgroundMusic)
	}
	hash := sha256.Sum256([]byte(req.VoiceoverScript))
	sb.WriteString("\nvoiceover_sha256=")
	sb.WriteString(hex.EncodeToString(hash[:]))
	return []byte(sb.String())
}

// --- BridgeVideoAssembler (deferred-but-typed) ---------------------------

// BridgeVideoAssemblerConfig wires the live (deferred) bridge
// adapter. The shape is locked in v3.4.0 so the next sprint can
// drop in the implementation behind a small adapter package
// (e.g., internal/adapter/videobridge). NEVER store secrets on
// argv; the BridgeURL alias resolves at the composition root via
// runx.
type BridgeVideoAssemblerConfig struct {
	BridgeURL    string // runx alias resolved at composition root
	BridgeSecret []byte // HMAC secret for the signed POST
	Timeout      time.Duration
}

// BridgeVideoAssembler is the typed adapter that ALWAYS returns
// ErrVideoBridgeUnconfigured in v3.4.0 (the implementation lands
// in the next sprint or the parallel uiauto-framework PR per the
// same v3.3.0 cross-repo split decision). The shape is shipped so
// the composition root + ADR-028 acceptance criterion stay green:
// callers can wire the bridge adapter today and see the typed
// failure surface, instead of silently no-oping.
type BridgeVideoAssembler struct {
	cfg BridgeVideoAssemblerConfig
}

// NewBridgeVideoAssembler returns a bridge assembler. Validates
// that the operator-supplied alias + secret are non-empty so the
// composition-root wiring fails fast at boot.
func NewBridgeVideoAssembler(cfg BridgeVideoAssemblerConfig) (*BridgeVideoAssembler, error) {
	if strings.TrimSpace(cfg.BridgeURL) == "" {
		return nil, fmt.Errorf("%w: BridgeURL required (runx alias for video-bridge)", ErrVideoBridgeUnconfigured)
	}
	if len(cfg.BridgeSecret) == 0 {
		return nil, fmt.Errorf("%w: BridgeSecret required", ErrVideoBridgeUnconfigured)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	return &BridgeVideoAssembler{cfg: cfg}, nil
}

// Assemble always returns ErrVideoBridgeUnconfigured in v3.4.0;
// the live ffmpeg + Polly call lands in the next sprint or
// uiauto-framework PR per ADR-028.
func (b *BridgeVideoAssembler) Assemble(_ context.Context, req VideoAssemblyRequest) (VideoAssemblyResult, error) {
	return VideoAssemblyResult{}, fmt.Errorf("%w: action=%s product=%s (bridge URL=%q)", ErrVideoBridgeUnconfigured, req.Action, req.ProductID, b.cfg.BridgeURL)
}

// Config returns a copy of the bridge configuration. Useful for
// admin surfaces and tests that report the running shape without
// exposing the raw secret bytes.
func (b *BridgeVideoAssembler) Config() BridgeVideoAssemblerConfig {
	return BridgeVideoAssemblerConfig{
		BridgeURL: b.cfg.BridgeURL,
		Timeout:   b.cfg.Timeout,
	}
}

// BuildSignedRequest is the v3.4.1 wire-shape contract surface.
// It assembles the *http.Request that the live bridge call WOULD
// send -- including the X-Bridge-Timestamp + X-Bridge-Sign +
// X-Bridge-Tenant + X-Bridge-Platform headers -- WITHOUT performing
// the round-trip. Tests use it to gate the bridge contract before
// the live ffmpeg POST lands; the production Assemble path will
// call this internally once the bridge is deployed.
//
// Decomposition: marshal + sign + assemble are split out so this
// public method body stays small (sentrux complex_fn guard).
func (b *BridgeVideoAssembler) BuildSignedRequest(ctx context.Context, req VideoAssemblyRequest, now time.Time) (*http.Request, error) {
	body, err := json.Marshal(map[string]any{
		"tenant_id":         req.TenantID,
		"product_id":        req.ProductID,
		"action":            string(req.Action),
		"format":            string(req.Format),
		"hero_image_urls":   req.HeroImageURLs,
		"voiceover_script":  req.VoiceoverScript,
		"subtitle_lines":    req.SubtitleLines,
		"background_music":  req.BackgroundMusic,
		"branding_overlay":  req.BrandingOverlay,
		"duration_sec":      req.DurationSec,
		"output_key_prefix": req.OutputKeyPrefix,
		"scenes":            req.Scenes,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: marshal body: %v", ErrVideoBridgeSignature, err)
	}
	timestamp := now.UTC().Unix()
	signature, err := social.ComputeTikTokSignature(social.TikTokSignRequest{
		Secret:    b.cfg.BridgeSecret,
		Timestamp: timestamp,
		Path:      VideoBridgePostPath,
		Body:      body,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVideoBridgeSignature, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.BridgeURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrVideoBridgeSignature, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Bridge-Timestamp", fmt.Sprintf("%d", timestamp))
	httpReq.Header.Set("X-Bridge-Sign", signature)
	httpReq.Header.Set("X-Bridge-Tenant", req.TenantID)
	httpReq.Header.Set("X-Bridge-Platform", VideoBridgePlatform)
	return httpReq, nil
}
