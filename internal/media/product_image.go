// File scope: v3.2.0 EC-2-2 AI hero image generation + background
// removal pipeline.
//
// The pipeline downloads supplier hero images, removes the
// background, and stores the resulting transparent PNG in the
// configured port.MediaStore (local for dev, OCI Object Storage in
// prod). It also exposes a hook for lifestyle-replacement
// generation that defers to a future image-bridge story (Bedrock
// Titan / Stability via the fleet bridge -- never directly from
// the MacBook per the OOM lessons in resource-guard.mdc).
//
// Resilience pillar (v2.10 baseline):
//
//   - Implements lifecycle.Closer.
//   - Single-shot synchronous Process call -- no raw goroutines.
//     The fan-out workerpool lives at the composition root so
//     batch-mode (50 product enrichment) can submit Process tasks
//     to a Pool the cmd/agent-worker owns.
//   - All errors typed + %w-wrapped via package sentinels.
//   - Tenant awareness: every stored object's key is namespaced
//     under tenants/<tenant_id>/ so MediaStore-level RLS
//     (filesystem prefix or OCI bucket prefix) keeps tenants
//     isolated.
//   - Memwatch hint: the background-removal path holds a decoded
//     image in memory equal to width*height*4 bytes. The pipeline
//     defers to the image-bridge for any task larger than
//     MaxLocalDecodeBytes (1 MiB) so the MacBook never decodes a
//     large supplier asset directly.
//
// Bridge wiring discipline (per ADR-028 + resource-guard):
//
//   - For background removal at v3.2.0 the package ships a small
//     deterministic StubBackgroundRemover so unit + integration
//     tests + storefront dev runs do not require Bedrock or rembg.
//     The composition root in cmd/agent-worker selects the real
//     remover (Bedrock Vision / image-bridge) at startup.
//   - For lifestyle generation (Bedrock Titan) the v3.2.0 sprint
//     declines to ship the heavy bridge here -- ActionLifestyle-
//     Generation returns ErrImageBridgeUnconfigured by design and
//     the operator setup path is documented in
//     docs/operations/image-bridge.md (this file lands as part of
//     the same PR).
//
// Cite skill: go-clean-architecture (port + adapter -- the
// pipeline depends on Downloader, BackgroundRemover, port.MediaStore;
// the cmd/* binary wires the production adapters at startup).
package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// EC-2-2 typed sentinels.
var (
	ErrImagePipelineUnconfigured = errors.New("media: product image pipeline unconfigured")
	ErrImagePipelineClosed       = errors.New("media: product image pipeline closed")
	ErrImageProcessingFailed     = errors.New("media: image processing failed")
	ErrImageBridgeUnconfigured   = errors.New("media: image bridge unconfigured (lifestyle generation deferred)")
	ErrImageTooLarge             = errors.New("media: image exceeds local decode ceiling")
)

// Action is the requested image-pipeline operation.
type Action string

const (
	ActionBackgroundRemoval   Action = "background_removal"
	ActionLifestyleGeneration Action = "lifestyle_generation"
)

// MaxLocalDecodeBytes caps the size of an image the pipeline will
// decode locally. Above this, the call MUST be routed through the
// fleet image-bridge (per resource-guard memwatch ceilings).
const MaxLocalDecodeBytes = 1 << 20 // 1 MiB

// ImageDownloader fetches a supplier image. Returns the bytes +
// a content-type hint. Implementations live in adapter packages
// (HTTP downloader, fixture downloader, etc.).
type ImageDownloader interface {
	Download(ctx context.Context, url string) ([]byte, string, error)
}

// BackgroundRemover transforms a source image into a transparent-
// background PNG. Implementations: StubBackgroundRemover (this file,
// for tests + dev), BedrockBackgroundRemover (cmd/* wiring; not
// shipped in v3.2.0 -- see image-bridge story).
type BackgroundRemover interface {
	Remove(ctx context.Context, src []byte, contentType string) ([]byte, error)
}

// ProductImageRequest is the unit of work submitted to Process.
type ProductImageRequest struct {
	ProductID string
	ImageURL  string
	Action    Action
	Variant   string // optional; e.g. "hero", "thumb"
}

// ProcessedImage captures the pipeline output. The bytes are
// returned alongside the StoredObject so tests can re-decode for
// transparency assertions without round-tripping the store.
type ProcessedImage struct {
	ProductID         string
	Action            Action
	StoredObject      port.StoredMediaObject
	OutputBytes       []byte
	OutputContentType string
	GeneratedAt       time.Time
}

// MetricsHook is the optional Prometheus + EvoMap callback the
// pipeline calls per Process. Kept abstract so this package does
// not import internal/metrics directly (coupling).
type MetricsHook func(action Action, status string, duration time.Duration, bytesIn, bytesOut int)

// ProductImagePipelineConfig wires the pipeline.
type ProductImagePipelineConfig struct {
	Downloader  ImageDownloader
	Remover     BackgroundRemover
	Store       port.MediaStore
	TenantID    string
	KeyPrefix   string // optional: stored under tenants/<id>/<prefix>/<product_id>...
	MetricsHook MetricsHook
	Now         func() time.Time
}

// ProductImagePipeline is the EC-2-2 agent.
type ProductImagePipeline struct {
	downloader  ImageDownloader
	remover     BackgroundRemover
	store       port.MediaStore
	tenantID    string
	keyPrefix   string
	metricsHook MetricsHook
	now         func() time.Time
	logger      *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewProductImagePipeline constructs the pipeline.
func NewProductImagePipeline(logger *slog.Logger, cfg ProductImagePipelineConfig) (*ProductImagePipeline, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Downloader == nil {
		return nil, fmt.Errorf("%w: ImageDownloader required", ErrImagePipelineUnconfigured)
	}
	if cfg.Remover == nil {
		return nil, fmt.Errorf("%w: BackgroundRemover required", ErrImagePipelineUnconfigured)
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: port.MediaStore required", ErrImagePipelineUnconfigured)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrImagePipelineUnconfigured)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &ProductImagePipeline{
		downloader:  cfg.Downloader,
		remover:     cfg.Remover,
		store:       cfg.Store,
		tenantID:    cfg.TenantID,
		keyPrefix:   strings.Trim(cfg.KeyPrefix, "/"),
		metricsHook: cfg.MetricsHook,
		now:         cfg.Now,
		logger:      logger,
	}, nil
}

// Close marks the pipeline closed. Implements lifecycle.Closer.
func (p *ProductImagePipeline) Close(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

// Process runs the pipeline. Returns a typed ProcessedImage on
// success; wraps every fatal error in ErrImageProcessingFailed.
func (p *ProductImagePipeline) Process(ctx context.Context, req ProductImageRequest) (ProcessedImage, error) {
	if err := p.guardProcess(req); err != nil {
		return ProcessedImage{}, err
	}
	if req.Action == "" {
		req.Action = ActionBackgroundRemoval
	}
	start := p.now()

	if req.Action == ActionLifestyleGeneration {
		// Lifestyle replacement requires the heavy Bedrock Titan
		// pathway; the v3.2.0 sprint defers the bridge wiring to a
		// follow-up sprint per the plan. Operator setup lives at
		// docs/operations/image-bridge.md.
		p.recordMetric(req.Action, "deferred", time.Since(start), 0, 0)
		return ProcessedImage{}, fmt.Errorf("%w: action %s requires the image-bridge (see docs/operations/image-bridge.md)", ErrImageBridgeUnconfigured, req.Action)
	}

	body, contentType, err := p.downloader.Download(ctx, req.ImageURL)
	if err != nil {
		p.recordMetric(req.Action, "download_failed", time.Since(start), 0, 0)
		return ProcessedImage{}, fmt.Errorf("%w: download %s: %w", ErrImageProcessingFailed, req.ImageURL, err)
	}
	if len(body) > MaxLocalDecodeBytes {
		p.recordMetric(req.Action, "too_large", time.Since(start), len(body), 0)
		return ProcessedImage{}, fmt.Errorf("%w: %d bytes > %d MaxLocalDecodeBytes (route through image-bridge)", ErrImageTooLarge, len(body), MaxLocalDecodeBytes)
	}
	processed, err := p.remover.Remove(ctx, body, contentType)
	if err != nil {
		p.recordMetric(req.Action, "remove_failed", time.Since(start), len(body), 0)
		return ProcessedImage{}, fmt.Errorf("%w: bg remove %s: %w", ErrImageProcessingFailed, req.ProductID, err)
	}
	stored, err := p.storeObject(ctx, req, processed)
	if err != nil {
		p.recordMetric(req.Action, "store_failed", time.Since(start), len(body), len(processed))
		return ProcessedImage{}, fmt.Errorf("%w: store %s: %w", ErrImageProcessingFailed, req.ProductID, err)
	}
	p.recordMetric(req.Action, "ok", time.Since(start), len(body), len(processed))
	return ProcessedImage{
		ProductID:         req.ProductID,
		Action:            req.Action,
		StoredObject:      stored,
		OutputBytes:       processed,
		OutputContentType: "image/png",
		GeneratedAt:       p.now().UTC(),
	}, nil
}

func (p *ProductImagePipeline) guardProcess(req ProductImageRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrImagePipelineClosed
	}
	if strings.TrimSpace(req.ProductID) == "" || strings.TrimSpace(req.ImageURL) == "" {
		return fmt.Errorf("%w: ProductID + ImageURL required", ErrImagePipelineUnconfigured)
	}
	return nil
}

func (p *ProductImagePipeline) storeObject(ctx context.Context, req ProductImageRequest, body []byte) (port.StoredMediaObject, error) {
	parts := []string{"tenants", p.tenantID}
	if p.keyPrefix != "" {
		parts = append(parts, p.keyPrefix)
	}
	variant := req.Variant
	if variant == "" {
		variant = "hero"
	}
	parts = append(parts, "products", req.ProductID, variant+".png")
	key := strings.Join(parts, "/")
	return p.store.Put(ctx, port.MediaObject{
		Key:         key,
		ContentType: "image/png",
		Body:        bytes.NewReader(body),
	})
}

func (p *ProductImagePipeline) recordMetric(action Action, status string, duration time.Duration, bytesIn, bytesOut int) {
	if p.metricsHook == nil {
		return
	}
	p.metricsHook(action, status, duration, bytesIn, bytesOut)
}

// --- StubBackgroundRemover -------------------------------------------------

// StubBackgroundRemover is a deterministic in-process bg remover
// useful for tests, dev compose, and the storefront preview path.
// It samples the four corner pixels of the input image, picks the
// dominant corner colour as the background, and replaces every
// pixel within colourMatchTolerance of that colour with full
// transparency.
//
// In production the cmd/* binary wires either:
//   - BedrockBackgroundRemover (calls Bedrock Vision via the
//     fleet image-bridge -- defer to image-bridge story per plan), or
//   - RembgBackgroundRemover (rembg Python sidecar via HTTP --
//     also routed through image-bridge per resource-guard).
//
// The stub is a real implementation so the v3.2.0 EC-2-2 RED test
// + operator dev preview both pass without any heavy dependency.
type StubBackgroundRemover struct {
	tolerance uint32
}

// NewStubBackgroundRemover returns a remover with the default
// colour-match tolerance (24 / 255 per channel). Used by the
// EC-2-2 RED test + the local-dev compose profile.
func NewStubBackgroundRemover() *StubBackgroundRemover {
	return &StubBackgroundRemover{tolerance: 24}
}

// Remove decodes src as PNG/JPEG/WEBP, transparency-paints the
// dominant background, and returns a PNG with alpha channel.
func (s *StubBackgroundRemover) Remove(_ context.Context, src []byte, _ string) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return nil, fmt.Errorf("empty image bounds")
	}
	bg := pickDominantCornerColour(img)
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a == 0 {
				out.Set(x, y, color.NRGBA{0, 0, 0, 0})
				continue
			}
			if colourClose(r, g, b, bg, s.tolerance) {
				out.Set(x, y, color.NRGBA{0, 0, 0, 0})
				continue
			}
			out.Set(x, y, color.NRGBA{
				R: uint8(r >> 8),
				G: uint8(g >> 8),
				B: uint8(b >> 8),
				A: uint8(a >> 8),
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, fmt.Errorf("encode result png: %w", err)
	}
	return buf.Bytes(), nil
}

func pickDominantCornerColour(img image.Image) [3]uint32 {
	bounds := img.Bounds()
	corners := [4][3]uint32{}
	xs := []int{bounds.Min.X, bounds.Max.X - 1, bounds.Min.X, bounds.Max.X - 1}
	ys := []int{bounds.Min.Y, bounds.Min.Y, bounds.Max.Y - 1, bounds.Max.Y - 1}
	for i := 0; i < 4; i++ {
		r, g, b, _ := img.At(xs[i], ys[i]).RGBA()
		corners[i] = [3]uint32{r, g, b}
	}
	// Sum + average for robustness against single-pixel noise.
	var avg [3]uint32
	for i := 0; i < 4; i++ {
		for j := 0; j < 3; j++ {
			avg[j] += corners[i][j]
		}
	}
	for j := 0; j < 3; j++ {
		avg[j] /= 4
	}
	return avg
}

func colourClose(r, g, b uint32, bg [3]uint32, tolerance uint32) bool {
	tol := tolerance << 8 // upscale to 16-bit channel space
	return absUint32(r, bg[0]) <= tol && absUint32(g, bg[1]) <= tol && absUint32(b, bg[2]) <= tol
}

func absUint32(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// CopyAll is a tiny io helper exposed for tests + adapters. Avoids
// pulling io/ioutil throughout the package.
func CopyAll(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
