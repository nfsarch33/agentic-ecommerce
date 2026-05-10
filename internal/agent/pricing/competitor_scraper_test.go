// File scope: v3.9.0 EC-6-4 competitor scraper RED tests.
// TDD-first per the v3.9.0 plan. The scraper feeds price signals
// into the v3.5.0 EC-6-3 dynamic pricing agent via the existing
// eventbus contract.
package pricing

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

type stubScraperPublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (p *stubScraperPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *stubScraperPublisher) Close() error { return nil }

func (p *stubScraperPublisher) snapshot() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.events))
	copy(out, p.events)
	return out
}

// stubChannelScraper drives the per-channel scrape result for tests.
type stubChannelScraper struct {
	channel string
	results map[string][]CompetitorObservation
	err     error
	rateErr error
	mu      sync.Mutex
	calls   int
}

func (s *stubChannelScraper) Channel() string { return s.channel }

func (s *stubChannelScraper) Scrape(_ context.Context, sku string, hint CompetitorScrapeHint) ([]CompetitorObservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.rateErr != nil {
		return nil, s.rateErr
	}
	if s.err != nil {
		return nil, s.err
	}
	if rows, ok := s.results[sku]; ok {
		return rows, nil
	}
	return nil, ErrCompetitorMatchNotFound
}

func (s *stubChannelScraper) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newCompetitorScraperHarness(t *testing.T, scrapers []CompetitorChannelScraper) (*CompetitorScraper, *stubScraperPublisher) {
	t.Helper()
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	pub := &stubScraperPublisher{}
	scraper, err := NewCompetitorScraper(nil, CompetitorScraperConfig{
		TenantID:             "tenant-1",
		Channels:             scrapers,
		Publisher:            pub,
		UndercutPctThreshold: 0.05,
		Now:                  func() time.Time { return clk },
	})
	if err != nil {
		t.Fatalf("NewCompetitorScraper: %v", err)
	}
	t.Cleanup(func() { _ = scraper.Close(context.Background()) })
	return scraper, pub
}

func TestCompetitorScraper_DetectsTikTokUndercut(t *testing.T) {
	t.Parallel()
	tiktok := &stubChannelScraper{
		channel: "tiktok",
		results: map[string][]CompetitorObservation{
			"sku-a": {{
				CompetitorID:  "comp-x",
				CompetitorURL: "https://tiktok.example/comp-x",
				PriceAUDCents: 7000,
			}},
		},
	}
	scraper, pub := newCompetitorScraperHarness(t, []CompetitorChannelScraper{tiktok})

	res, err := scraper.Run(context.Background(), CompetitorScrapeRequest{
		SKU:              "sku-a",
		OurPriceAUDCents: 8000,
		Hint:             CompetitorScrapeHint{Title: "Wireless Earbuds"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Observations) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(res.Observations))
	}
	if !res.Observations[0].Undercut {
		t.Fatalf("expected undercut detected (our 8000 vs theirs 7000)")
	}

	// Both observation + undercut events should be emitted.
	events := pub.snapshot()
	var observed, undercut int
	for _, e := range events {
		switch e.Type {
		case eventbus.CompetitorPriceObserved:
			observed++
		case eventbus.CompetitorUndercut:
			undercut++
		}
	}
	if observed != 1 || undercut != 1 {
		t.Fatalf("expected observed=1 undercut=1, got observed=%d undercut=%d (events=%v)", observed, undercut, events)
	}
}

func TestCompetitorScraper_DetectsRedNoteUndercut(t *testing.T) {
	t.Parallel()
	rednote := &stubChannelScraper{
		channel: "rednote",
		results: map[string][]CompetitorObservation{
			"sku-a": {{
				CompetitorID:  "rn-comp-1",
				PriceAUDCents: 6500,
			}},
		},
	}
	scraper, pub := newCompetitorScraperHarness(t, []CompetitorChannelScraper{rednote})
	_, err := scraper.Run(context.Background(), CompetitorScrapeRequest{
		SKU:              "sku-a",
		OurPriceAUDCents: 8000,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasEventOfType(pub.snapshot(), eventbus.CompetitorUndercut) {
		t.Fatalf("expected CompetitorUndercut event")
	}
}

func TestCompetitorScraper_DetectsFacebookUndercut(t *testing.T) {
	t.Parallel()
	fb := &stubChannelScraper{
		channel: "facebook",
		results: map[string][]CompetitorObservation{
			"sku-a": {{
				CompetitorID:  "fb-comp-9",
				PriceAUDCents: 7100,
			}},
		},
	}
	scraper, pub := newCompetitorScraperHarness(t, []CompetitorChannelScraper{fb})
	_, err := scraper.Run(context.Background(), CompetitorScrapeRequest{
		SKU:              "sku-a",
		OurPriceAUDCents: 8000,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasEventOfType(pub.snapshot(), eventbus.CompetitorUndercut) {
		t.Fatalf("expected CompetitorUndercut event for facebook")
	}
}

func TestCompetitorScraper_NoMatchReturnsNotFound(t *testing.T) {
	t.Parallel()
	tiktok := &stubChannelScraper{
		channel: "tiktok",
		results: map[string][]CompetitorObservation{},
	}
	scraper, pub := newCompetitorScraperHarness(t, []CompetitorChannelScraper{tiktok})
	res, err := scraper.Run(context.Background(), CompetitorScrapeRequest{
		SKU:              "sku-missing",
		OurPriceAUDCents: 5000,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Observations) != 0 {
		t.Fatalf("expected 0 observations, got %d", len(res.Observations))
	}
	if len(res.NotFoundChannels) != 1 || res.NotFoundChannels[0] != "tiktok" {
		t.Fatalf("expected tiktok in NotFoundChannels, got %v", res.NotFoundChannels)
	}
	if hasEventOfType(pub.snapshot(), eventbus.CompetitorPriceObserved) {
		t.Fatalf("did not expect observation events when no matches")
	}
}

func TestCompetitorScraper_RateLimitFallbackToAlt(t *testing.T) {
	t.Parallel()
	primary := &stubChannelScraper{
		channel: "tiktok",
		rateErr: ErrScraperRateLimited,
	}
	fallback := &stubChannelScraper{
		channel: "rednote",
		results: map[string][]CompetitorObservation{
			"sku-a": {{CompetitorID: "rn-x", PriceAUDCents: 7000}},
		},
	}
	scraper, pub := newCompetitorScraperHarness(t, []CompetitorChannelScraper{primary, fallback})
	res, err := scraper.Run(context.Background(), CompetitorScrapeRequest{
		SKU:              "sku-a",
		OurPriceAUDCents: 8000,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// One channel rate-limited but the other succeeded -> overall ok.
	if len(res.Observations) != 1 {
		t.Fatalf("expected 1 observation from fallback, got %d", len(res.Observations))
	}
	if len(res.RateLimitedChannels) != 1 || res.RateLimitedChannels[0] != "tiktok" {
		t.Fatalf("expected tiktok rate-limited, got %v", res.RateLimitedChannels)
	}
	if !hasEventOfType(pub.snapshot(), eventbus.CompetitorUndercut) {
		t.Fatalf("expected CompetitorUndercut event from fallback")
	}
	if primary.callCount() != 1 || fallback.callCount() != 1 {
		t.Fatalf("expected 1 call each, got primary=%d fallback=%d", primary.callCount(), fallback.callCount())
	}
}

func TestCompetitorScraper_PriceDeltaThresholdConfigurable(t *testing.T) {
	t.Parallel()
	tiktok := &stubChannelScraper{
		channel: "tiktok",
		results: map[string][]CompetitorObservation{
			"sku-a": {{CompetitorID: "comp-x", PriceAUDCents: 7800}}, // 2.5% below
		},
	}
	pub := &stubScraperPublisher{}
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	// Threshold 5%: 2.5% delta should NOT trigger undercut.
	scraper, err := NewCompetitorScraper(nil, CompetitorScraperConfig{
		TenantID:             "tenant-1",
		Channels:             []CompetitorChannelScraper{tiktok},
		Publisher:            pub,
		UndercutPctThreshold: 0.05,
		Now:                  func() time.Time { return clk },
	})
	if err != nil {
		t.Fatalf("NewCompetitorScraper: %v", err)
	}
	t.Cleanup(func() { _ = scraper.Close(context.Background()) })

	_, err = scraper.Run(context.Background(), CompetitorScrapeRequest{
		SKU:              "sku-a",
		OurPriceAUDCents: 8000,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hasEventOfType(pub.snapshot(), eventbus.CompetitorUndercut) {
		t.Fatalf("did not expect undercut at threshold 5%% with 2.5%% delta")
	}
	if !hasEventOfType(pub.snapshot(), eventbus.CompetitorPriceObserved) {
		t.Fatalf("expected observation event regardless of threshold")
	}

	// Now lower threshold -> should fire.
	pub2 := &stubScraperPublisher{}
	scraper2, err := NewCompetitorScraper(nil, CompetitorScraperConfig{
		TenantID:             "tenant-1",
		Channels:             []CompetitorChannelScraper{tiktok},
		Publisher:            pub2,
		UndercutPctThreshold: 0.01,
		Now:                  func() time.Time { return clk },
	})
	if err != nil {
		t.Fatalf("NewCompetitorScraper #2: %v", err)
	}
	t.Cleanup(func() { _ = scraper2.Close(context.Background()) })
	_, err = scraper2.Run(context.Background(), CompetitorScrapeRequest{
		SKU:              "sku-a",
		OurPriceAUDCents: 8000,
	})
	if err != nil {
		t.Fatalf("Run #2: %v", err)
	}
	if !hasEventOfType(pub2.snapshot(), eventbus.CompetitorUndercut) {
		t.Fatalf("expected undercut at threshold 1%% with 2.5%% delta")
	}
}

func TestCompetitorScraper_ChannelUnavailableSurfaced(t *testing.T) {
	t.Parallel()
	bad := &stubChannelScraper{
		channel: "tiktok",
		err:     ErrCompetitorChannelUnavailable,
	}
	scraper, pub := newCompetitorScraperHarness(t, []CompetitorChannelScraper{bad})
	res, err := scraper.Run(context.Background(), CompetitorScrapeRequest{
		SKU:              "sku-a",
		OurPriceAUDCents: 8000,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.UnavailableChannels) != 1 {
		t.Fatalf("expected tiktok unavailable, got %v", res.UnavailableChannels)
	}
	if hasEventOfType(pub.snapshot(), eventbus.CompetitorPriceObserved) {
		t.Fatalf("did not expect observation events when channel unavailable")
	}
}

func TestCompetitorScraper_ClosedReturnsError(t *testing.T) {
	t.Parallel()
	scraper, _ := newCompetitorScraperHarness(t, []CompetitorChannelScraper{
		&stubChannelScraper{channel: "tiktok"},
	})
	if err := scraper.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := scraper.Run(context.Background(), CompetitorScrapeRequest{SKU: "x", OurPriceAUDCents: 100})
	if !errors.Is(err, ErrCompetitorScraperClosed) {
		t.Fatalf("expected ErrCompetitorScraperClosed, got %v", err)
	}
}

func TestCompetitorScraper_RecordsKPI(t *testing.T) {
	t.Parallel()
	tiktok := &stubChannelScraper{
		channel: "tiktok",
		results: map[string][]CompetitorObservation{
			"sku-a": {{CompetitorID: "comp-x", PriceAUDCents: 6000}},
		},
	}
	pub := &stubScraperPublisher{}
	var kpis []CompetitorScraperKPISample
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	scraper, err := NewCompetitorScraper(nil, CompetitorScraperConfig{
		TenantID:             "tenant-1",
		Channels:             []CompetitorChannelScraper{tiktok},
		Publisher:            pub,
		UndercutPctThreshold: 0.05,
		KPIHook:              func(s CompetitorScraperKPISample) { kpis = append(kpis, s) },
		Now:                  func() time.Time { return clk },
	})
	if err != nil {
		t.Fatalf("NewCompetitorScraper: %v", err)
	}
	t.Cleanup(func() { _ = scraper.Close(context.Background()) })
	if _, err := scraper.Run(context.Background(), CompetitorScrapeRequest{
		SKU:              "sku-a",
		OurPriceAUDCents: 8000,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(kpis) == 0 {
		t.Fatalf("expected at least one KPI sample")
	}
	if kpis[0].UndercutsDetected != 1 {
		t.Fatalf("expected UndercutsDetected=1, got %d", kpis[0].UndercutsDetected)
	}
}

func hasEventOfType(events []eventbus.Event, t eventbus.EventType) bool {
	for _, e := range events {
		if e.Type == t {
			return true
		}
	}
	return false
}
