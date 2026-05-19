//go:build v321_smoke

package v321

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/agent/enrichment"
	"github.com/nfsarch33/helixon-ec/internal/port"
	"github.com/nfsarch33/helixon-ec/internal/rag"
	"github.com/nfsarch33/helixon-ec/internal/seo"
)

// fixtureProduct is the slim shape used to drive the v3.2.1 50-product
// smoke through the enrichment pipeline. The intent is to cover five
// realistic supplier categories x ten products each so the histogram
// + assertions exercise the full quality-score surface.
type fixtureProduct struct {
	ID                 string
	Category           string
	Topic              string
	ChineseTitle       string
	ChineseDescription string
	ChineseSpecs       []string
	PriceCNYCents      int
	PriceCents         int
	Stock              int
	ImageURL           string
	BG                 color.RGBA
	FG                 color.RGBA
}

func (f fixtureProduct) toEnrichmentProduct() enrichment.EnrichmentProduct {
	return enrichment.EnrichmentProduct{
		ID:                 f.ID,
		ChineseTitle:       f.ChineseTitle,
		ChineseDescription: f.ChineseDescription,
		ChineseSpecs:       f.ChineseSpecs,
		Category:           f.Category,
		PriceCNYCents:      f.PriceCNYCents,
	}
}

// buildFixtureProducts returns exactly count products spread evenly
// across the five supplier-realistic categories. Deterministic IDs +
// per-product RGB seeds so every run is bit-for-bit reproducible.
func buildFixtureProducts(count int) []fixtureProduct {
	cats := []struct {
		category, topic string
		zhTitle, zhDesc string
		zhSpecs         []string
		priceCN         int
		bg              color.RGBA
		fg              color.RGBA
	}{
		{
			category: "electronics", topic: "earbuds",
			zhTitle: "高品质无线蓝牙耳机", zhDesc: "无线蓝牙耳机, 续航36小时, 主动降噪",
			zhSpecs: []string{"36-hour battery", "active noise cancelling", "type-c"},
			priceCN: 4500, bg: color.RGBA{R: 240, G: 240, B: 245, A: 255}, fg: color.RGBA{R: 60, G: 90, B: 200, A: 255},
		},
		{
			category: "home", topic: "decor",
			zhTitle: "现代简约家居装饰", zhDesc: "高品质家居装饰, 易清洁, 北欧风格",
			zhSpecs: []string{"nordic style", "easy clean", "matte finish"},
			priceCN: 2200, bg: color.RGBA{R: 250, G: 248, B: 240, A: 255}, fg: color.RGBA{R: 130, G: 80, B: 60, A: 255},
		},
		{
			category: "fitness", topic: "fitness gear",
			zhTitle: "多功能健身器材", zhDesc: "便携健身器材, 适合家用和户外",
			zhSpecs: []string{"portable", "non-slip grip", "5-30 kg load"},
			priceCN: 3800, bg: color.RGBA{R: 250, G: 250, B: 250, A: 255}, fg: color.RGBA{R: 180, G: 40, B: 30, A: 255},
		},
		{
			category: "beauty", topic: "skincare",
			zhTitle: "温和护肤精华", zhDesc: "温和无刺激, 适合敏感肌肤",
			zhSpecs: []string{"sensitive skin", "fragrance free", "vegan"},
			priceCN: 1500, bg: color.RGBA{R: 248, G: 235, B: 240, A: 255}, fg: color.RGBA{R: 200, G: 90, B: 130, A: 255},
		},
		{
			category: "kitchen", topic: "kitchen tools",
			zhTitle: "智能厨房工具", zhDesc: "省时高效, 食品级安全材料",
			zhSpecs: []string{"food-grade", "dishwasher safe", "ergonomic grip"},
			priceCN: 1900, bg: color.RGBA{R: 235, G: 245, B: 250, A: 255}, fg: color.RGBA{R: 30, G: 130, B: 90, A: 255},
		},
	}

	out := make([]fixtureProduct, 0, count)
	per := count / len(cats)
	if per == 0 {
		per = 1
	}
	for i, c := range cats {
		for j := 0; j < per; j++ {
			id := fmt.Sprintf("%s-%03d", c.category, j+1)
			out = append(out, fixtureProduct{
				ID:                 id,
				Category:           c.category,
				Topic:              c.topic,
				ChineseTitle:       fmt.Sprintf("%s #%d", c.zhTitle, j+1),
				ChineseDescription: c.zhDesc,
				ChineseSpecs:       c.zhSpecs,
				PriceCNYCents:      c.priceCN,
				PriceCents:         (c.priceCN * 215) / 100, // ~CNY->AUD for fixture realism
				Stock:              25 + (i*7+j*3)%50,
				ImageURL:           fmt.Sprintf("https://supplier.fixture.test/%s.png", id),
				BG:                 c.bg,
				FG:                 c.fg,
			})
		}
	}
	// Pad/trim so the caller always gets exactly `count` products
	// even when count is not a multiple of len(cats).
	for len(out) < count {
		base := out[len(out)%len(cats)]
		base.ID = fmt.Sprintf("%s-extra-%03d", base.Category, len(out)-len(cats)*per+1)
		base.ImageURL = fmt.Sprintf("https://supplier.fixture.test/%s.png", base.ID)
		out = append(out, base)
	}
	if len(out) > count {
		out = out[:count]
	}
	return out
}

// buildFixtureTrendSources returns one TrendSource per platform (3
// platforms: tiktok, google_trends, rednote). Each source carries
// long-tail keywords for the fixture topics so the EC-2-3 SEO
// injector finds keywords for every product.
func buildFixtureTrendSources() []rag.TrendSource {
	return []rag.TrendSource{
		&fixtureTrendSource{
			name: "tiktok",
			records: []rag.TrendRecord{
				{Keyword: "wireless earbuds 2026", Score: 0.95, Region: "AU", Volume: 12000},
				{Keyword: "noise cancelling earbuds", Score: 0.88, Region: "AU", Volume: 9500},
				{Keyword: "long battery earbuds", Score: 0.80, Region: "AU", Volume: 8200},
				{Keyword: "decor 2026", Score: 0.85, Region: "AU", Volume: 7000},
				{Keyword: "nordic decor", Score: 0.78, Region: "AU", Volume: 6100},
				{Keyword: "fitness gear home", Score: 0.86, Region: "AU", Volume: 8000},
				{Keyword: "portable fitness gear", Score: 0.74, Region: "AU", Volume: 5400},
				{Keyword: "skincare routine", Score: 0.92, Region: "AU", Volume: 11000},
				{Keyword: "sensitive skincare", Score: 0.81, Region: "AU", Volume: 7600},
				{Keyword: "kitchen tools 2026", Score: 0.83, Region: "AU", Volume: 7300},
				{Keyword: "smart kitchen tools", Score: 0.76, Region: "AU", Volume: 5800},
			},
		},
		&fixtureTrendSource{
			name: "google_trends",
			records: []rag.TrendRecord{
				{Keyword: "best wireless earbuds", Score: 0.83, Region: "AU", Volume: 9800},
				{Keyword: "modern home decor", Score: 0.75, Region: "AU", Volume: 5200},
				{Keyword: "fitness gear australia", Score: 0.72, Region: "AU", Volume: 4900},
				{Keyword: "vegan skincare", Score: 0.85, Region: "AU", Volume: 7100},
				{Keyword: "ergonomic kitchen tools", Score: 0.71, Region: "AU", Volume: 4400},
			},
		},
		&fixtureTrendSource{
			name: "rednote",
			records: []rag.TrendRecord{
				{Keyword: "好物推荐 earbuds", Score: 0.88, Region: "CN", Volume: 7000},
				{Keyword: "好物推荐 decor", Score: 0.74, Region: "CN", Volume: 5300},
				{Keyword: "好物推荐 fitness", Score: 0.79, Region: "CN", Volume: 6200},
				{Keyword: "好物推荐 skincare", Score: 0.87, Region: "CN", Volume: 7700},
				{Keyword: "好物推荐 kitchen", Score: 0.76, Region: "CN", Volume: 5900},
			},
		},
	}
}

// fixtureTrendSource is a deterministic in-memory rag.TrendSource.
type fixtureTrendSource struct {
	name    string
	records []rag.TrendRecord
}

func (f *fixtureTrendSource) Platform() string { return f.name }

func (f *fixtureTrendSource) Fetch(_ context.Context) ([]rag.TrendRecord, error) {
	out := make([]rag.TrendRecord, len(f.records))
	copy(out, f.records)
	return out, nil
}

// fixtureLLM is a deterministic port.AITextGenerator that emits a
// high-quality 150-180 char English JSON description per category.
// The LLM stub is intentionally template-based so the smoke test is
// hermetic + reproducible without any network IO.
type fixtureLLM struct {
	calls atomic.Int32
}

func (f *fixtureLLM) Complete(_ context.Context, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	f.calls.Add(1)
	category := extractCategory(req)
	payload := struct {
		EnglishTitle       string `json:"english_title"`
		EnglishDescription string `json:"english_description"`
	}{
		EnglishTitle:       fixtureTitle(category),
		EnglishDescription: fixtureDescription(category),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return port.AICompletionResponse{}, fmt.Errorf("fixture llm marshal: %w", err)
	}
	return port.AICompletionResponse{Content: string(body), TokensUsed: 220}, nil
}

func extractCategory(req port.AICompletionRequest) string {
	for _, msg := range req.Messages {
		if msg.Role != "user" {
			continue
		}
		for _, line := range strings.Split(msg.Content, "\n") {
			if strings.HasPrefix(line, "Category: ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Category: "))
			}
		}
	}
	return ""
}

// fixtureTitle returns a deterministic English title per category.
func fixtureTitle(category string) string {
	switch category {
	case "electronics":
		return "Premium Wireless Earbuds"
	case "home":
		return "Modern Nordic Home Decor"
	case "fitness":
		return "Versatile Fitness Gear Set"
	case "beauty":
		return "Gentle Skincare Essentials"
	case "kitchen":
		return "Smart Kitchen Helper Tool"
	default:
		return "Quality Imported Product"
	}
}

// fixtureDescription returns a deterministic ~150-180 char English
// description per category. Length is tuned to the v3.2.0 quality
// scorer sweet spot (160 chars) so every fixture clears the 0.75
// quality floor without depending on keyword presence (the
// description-gen stage runs BEFORE the SEO trend keyword lookup
// in the production pipeline, so DescriptionRequest.Keywords is
// intentionally nil for the smoke test).
func fixtureDescription(category string) string {
	switch category {
	case "electronics":
		return "Premium wireless earbuds with crisp clear sound and long battery life. Comfortable fit for daily commute, workouts, and remote work. Perfect everyday pick."
	case "home":
		return "Modern Nordic home decor that brightens any room. Easy to clean and built to last. Perfect for bedroom, living room, or kitchen counter as a thoughtful gift."
	case "fitness":
		return "Versatile fitness gear that supports daily workouts and busy training. Lightweight and durable for gym, home, or outdoor use. Backed by a smart guarantee."
	case "beauty":
		return "Gentle skincare essentials for daily routines and travel. Skin friendly and easy to apply for a fresh natural finish. Perfect for morning and evening prep."
	case "kitchen":
		return "Smart kitchen helpers that make cooking easier and faster. Dishwasher safe and built to last. Perfect for daily meal prep, weekend baking, and entertaining."
	default:
		return "Quality product imported from a verified supplier. Built for daily use and backed by reliable craftsmanship. Perfect for everyday tasks and casual gifts."
	}
}

// fixtureDownloader is the in-test ImageDownloader: the v321 smoke
// builds a tiny PNG per product in-process so the pipeline runs
// completely offline.
type fixtureDownloader struct {
	products map[string]fixtureProduct
}

func newFixtureDownloader(products []fixtureProduct) *fixtureDownloader {
	m := make(map[string]fixtureProduct, len(products))
	for _, p := range products {
		m[p.ImageURL] = p
	}
	return &fixtureDownloader{products: m}
}

func (d *fixtureDownloader) Download(_ context.Context, url string) ([]byte, string, error) {
	p, ok := d.products[url]
	if !ok {
		return nil, "", fmt.Errorf("fixture downloader: unknown url %q", url)
	}
	body, err := encodeFixturePNG(p.BG, p.FG)
	if err != nil {
		return nil, "", err
	}
	return body, "image/png", nil
}

// encodeFixturePNG produces a 6x6 NRGBA PNG with a 1-pixel border of
// `bg` and a 4x4 centre of `fg`. The shape matches the
// product_image_transparency test fixture so the same StubBackground-
// Remover assertions apply transitively.
func encodeFixturePNG(bg, fg color.RGBA) ([]byte, error) {
	const w, h = 6, 6
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := bg
			if x >= 1 && x < w-1 && y >= 1 && y < h-1 {
				c = fg
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode fixture png: %w", err)
	}
	return buf.Bytes(), nil
}

// fixtureMediaStore is the in-test port.MediaStore double.
type fixtureMediaStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFixtureMediaStore() *fixtureMediaStore {
	return &fixtureMediaStore{objects: map[string][]byte{}}
}

func (s *fixtureMediaStore) Put(_ context.Context, object port.MediaObject) (port.StoredMediaObject, error) {
	body, err := io.ReadAll(object.Body)
	if err != nil {
		return port.StoredMediaObject{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[object.Key] = body
	return port.StoredMediaObject{
		Key:         object.Key,
		URL:         "memory://" + object.Key,
		ContentType: object.ContentType,
		SizeBytes:   int64(len(body)),
		StoredAt:    time.Now().UTC(),
	}, nil
}

func (s *fixtureMediaStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("fixture media store: missing %q", key)
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (s *fixtureMediaStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

// fixtureCatalogueImporter is the in-test seo.CatalogueImporter
// satisfying the WC sync stub contract. Tracks unique SKUs +
// per-call evidence so the test can assert idempotent behaviour
// across the 50-product sweep.
type fixtureCatalogueImporter struct {
	mu       sync.Mutex
	rows     map[string]string
	calls    int
	newSKUs  int
	rowOrder []string
}

func newFixtureCatalogueImporter() *fixtureCatalogueImporter {
	return &fixtureCatalogueImporter{rows: map[string]string{}}
}

// Upsert satisfies seo.CatalogueImporter. The stub records a
// per-SKU value (currently the title) so re-runs can be detected
// as no-op overwrites versus duplicate creates.
func (s *fixtureCatalogueImporter) Upsert(_ context.Context, req seo.CatalogueUpsertRequest) (seo.CatalogueUpsertResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	created := false
	if _, ok := s.rows[req.SKU]; !ok {
		s.newSKUs++
		s.rowOrder = append(s.rowOrder, req.SKU)
		created = true
	}
	s.rows[req.SKU] = req.Title
	return seo.CatalogueUpsertResult{SKU: req.SKU, Created: created}, nil
}

func (s *fixtureCatalogueImporter) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *fixtureCatalogueImporter) NewSKUs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newSKUs
}

func (s *fixtureCatalogueImporter) StoredCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

// ragTrendsAdapter satisfies seo.TrendKeywordSource by querying the
// rag.Service for chunks whose stored keyword (Document.Title)
// matches the supplied topic. The production cmd/agent-worker
// composition root wires the same adapter shape on top of
// rag.TrendIngestor.TrendScore -- here we only need the slim
// search path because the smoke test pre-seeds the rag store.
type ragTrendsAdapter struct {
	service *rag.Service
}

func (a *ragTrendsAdapter) TrendingKeywords(ctx context.Context, tenantID, topic string) ([]string, error) {
	results, err := a.service.Search(ctx, rag.SearchQuery{
		TenantID: tenantID,
		Text:     topic,
		TopK:     5,
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(results))
	seen := make(map[string]struct{})
	for _, r := range results {
		kw := strings.TrimSpace(r.Title)
		if kw == "" {
			continue
		}
		if _, ok := seen[kw]; ok {
			continue
		}
		seen[kw] = struct{}{}
		out = append(out, kw)
	}
	return out, nil
}

// pngHasTransparentPixel reports whether the encoded PNG has at
// least one alpha=0 pixel. Used by the smoke test to confirm the
// image stage continues to produce transparency for every product.
func pngHasTransparentPixel(t *testing.T, data []byte) bool {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode pipeline png: %v", err)
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a == 0 {
				return true
			}
		}
	}
	return false
}
