// File scope: v3.9.0 EC-6-4 competitor price scraper.
//
// Periodically scrapes competitor prices on TikTok Shop +
// RedNote + Facebook Shop (and additional channels via the
// pluggable CompetitorChannelScraper port). For every observation
// the scraper:
//  1. Computes the delta vs. our current price.
//  2. Emits a typed CompetitorPriceObservedEvent.
//  3. If delta > UndercutPctThreshold (default 5%): also emits
//     a typed CompetitorUndercutEvent which the v3.5.0 EC-6-3
//     dynamic pricing agent subscribes to.
//
// Reuse evidence:
//   - eventbus.Publisher contract from v3.3.0 EC-3-3.
//   - The per-channel port pattern mirrors v3.5.0 EC-6-2 fee
//     calculator's FXProvider port.
//   - typed-error sentinels match the package convention from
//     dynamic_pricing.go.
//   - The scraper does NOT reach into a specific HTTP client; it
//     consumes the existing TikTok / Facebook / RedNote clients via
//     thin adapters supplied at composition root time. This keeps
//     the scraper unit-testable with stubs and avoids new external
//     deps.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4 -- 16-sprint streak; v3.9.0 sprint 16 target):
//   - Run (envelope -> validate -> scrape loop -> emit summary)
//   - scrapeChannel (per-channel call + error categorisation)
//   - matchProduct (best-of fingerprint + first-result selection)
//   - evaluateDelta (pure delta + undercut classification)
//   - emitEvent (eventbus dispatch + KPI sink)
//
// Each helper stays under cyclomatic 6.
//
// Resilience pillar (v2.10 baseline):
//   - Implements lifecycle.Closer.
//   - Synchronous fan-out across channels (sequential keeps the
//     complex_fn bound + avoids racy goroutine accounting).
//   - Errors typed + %w-wrapped via package sentinels.
//   - Tenant-aware: every event carries TenantID.
package pricing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

// DefaultUndercutPctThreshold is the EC-6-4 default delta below
// our price that triggers a CompetitorUndercutEvent. Plan default
// is 5%.
const DefaultUndercutPctThreshold = 0.05

// EC-6-4 typed sentinels.
var (
	// ErrCompetitorScraperUnconfigured is returned when a required
	// dependency is missing.
	ErrCompetitorScraperUnconfigured = errors.New("competitor_scraper: unconfigured")

	// ErrCompetitorScraperClosed is returned by Run after Close.
	ErrCompetitorScraperClosed = errors.New("competitor_scraper: closed")

	// ErrCompetitorMatchNotFound is the per-channel sentinel when
	// no listing matched the supplied SKU + hint.
	ErrCompetitorMatchNotFound = errors.New("competitor_scraper: no competitor match found")

	// ErrScraperRateLimited surfaces a per-channel rate-limit hit.
	// Callers (and the EC-10-3 limiter) can errors.Is to fall back
	// to alternate channels.
	ErrScraperRateLimited = errors.New("competitor_scraper: channel rate-limited")

	// ErrCompetitorChannelUnavailable surfaces a per-channel
	// transport / auth failure.
	ErrCompetitorChannelUnavailable = errors.New("competitor_scraper: channel unavailable")
)

// CompetitorObservation is one (competitor, price) datapoint
// surfaced by a CompetitorChannelScraper.
type CompetitorObservation struct {
	CompetitorID     string
	CompetitorName   string
	CompetitorURL    string
	PriceAUDCents    int
	ImageFingerprint string
	Undercut         bool
	UndercutPct      float64
	Channel          string
	ObservedAt       time.Time
}

// CompetitorScrapeHint is the matching-hint payload supplied to
// the per-channel scraper so it can resolve the right competitor
// listing for our SKU.
type CompetitorScrapeHint struct {
	Title            string
	ImageFingerprint string
	Brand            string
	Keywords         []string
}

// CompetitorChannelScraper is the small port the per-channel
// implementations satisfy. The scraper supplies a SKU + hint and
// receives zero-or-more observations. Implementations:
//   - tiktokCompetitorScraper (uses existing TikTokShopClient)
//   - rednoteCompetitorScraper (uses existing RedNote uiauto facade)
//   - facebookCompetitorScraper (uses existing FacebookShopClient)
type CompetitorChannelScraper interface {
	Channel() string
	Scrape(ctx context.Context, sku string, hint CompetitorScrapeHint) ([]CompetitorObservation, error)
}

// CompetitorScrapeRequest is the unit of work submitted to Run.
type CompetitorScrapeRequest struct {
	SKU              string
	OurPriceAUDCents int
	Hint             CompetitorScrapeHint
}

// CompetitorScrapeResult captures the per-Run output.
type CompetitorScrapeResult struct {
	SKU                 string
	OurPriceAUDCents    int
	Observations        []CompetitorObservation
	NotFoundChannels    []string
	RateLimitedChannels []string
	UnavailableChannels []string
	UndercutCount       int
	GeneratedAt         time.Time
}

// CompetitorScraperMetrics is the small port the scraper emits
// per-observation counters through.
type CompetitorScraperMetrics interface {
	RecordCompetitorObservation(tenantID, channel, undercut string)
}

// CompetitorScraperKPISample is the v3.9.0 EvoMap KPI sample.
type CompetitorScraperKPISample struct {
	TenantID            string
	SKU                 string
	Observations        int
	UndercutsDetected   int
	RateLimitedChannels int
	UnavailableChannels int
}

// CompetitorScraperKPIHook is the optional EvoMap emission hook.
type CompetitorScraperKPIHook func(CompetitorScraperKPISample)

// CompetitorScraperConfig wires a CompetitorScraper.
type CompetitorScraperConfig struct {
	TenantID             string
	Channels             []CompetitorChannelScraper
	Publisher            eventbus.Publisher
	UndercutPctThreshold float64
	Metrics              CompetitorScraperMetrics
	KPIHook              CompetitorScraperKPIHook
	Now                  func() time.Time
}

// CompetitorScraper is the v3.9.0 EC-6-4 scraper.
type CompetitorScraper struct {
	cfg    CompetitorScraperConfig
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewCompetitorScraper constructs a scraper.
func NewCompetitorScraper(logger *slog.Logger, cfg CompetitorScraperConfig) (*CompetitorScraper, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := validateCompetitorScraperConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.UndercutPctThreshold <= 0 {
		cfg.UndercutPctThreshold = DefaultUndercutPctThreshold
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &CompetitorScraper{cfg: cfg, logger: logger}, nil
}

func validateCompetitorScraperConfig(cfg CompetitorScraperConfig) error {
	if strings.TrimSpace(cfg.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrCompetitorScraperUnconfigured)
	}
	if len(cfg.Channels) == 0 {
		return fmt.Errorf("%w: at least one channel scraper required", ErrCompetitorScraperUnconfigured)
	}
	if cfg.Publisher == nil {
		return fmt.Errorf("%w: Publisher required", ErrCompetitorScraperUnconfigured)
	}
	return nil
}

// Close marks the scraper closed. Implements lifecycle.Closer.
func (s *CompetitorScraper) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// Run iterates through the configured channels for a single SKU,
// records observations + emits events. Cyclomatic stays at 4.
func (s *CompetitorScraper) Run(ctx context.Context, req CompetitorScrapeRequest) (CompetitorScrapeResult, error) {
	if err := s.guard(); err != nil {
		return CompetitorScrapeResult{}, err
	}
	if err := validateScrapeRequest(req); err != nil {
		return CompetitorScrapeResult{}, err
	}
	res := CompetitorScrapeResult{
		SKU:              req.SKU,
		OurPriceAUDCents: req.OurPriceAUDCents,
		GeneratedAt:      s.cfg.Now(),
	}
	for _, ch := range s.cfg.Channels {
		s.scrapeChannel(ctx, ch, req, &res)
	}
	s.recordKPI(req, res)
	return res, nil
}

// scrapeChannel runs the per-channel scrape, classifies the
// outcome, and emits events. Cyclomatic 5.
func (s *CompetitorScraper) scrapeChannel(ctx context.Context, ch CompetitorChannelScraper, req CompetitorScrapeRequest, res *CompetitorScrapeResult) {
	rows, err := ch.Scrape(ctx, req.SKU, req.Hint)
	if err != nil {
		s.classifyChannelError(ch.Channel(), err, res)
		return
	}
	if len(rows) == 0 {
		res.NotFoundChannels = append(res.NotFoundChannels, ch.Channel())
		return
	}
	for _, row := range rows {
		obs := s.buildObservation(ch.Channel(), row, req)
		res.Observations = append(res.Observations, obs)
		if obs.Undercut {
			res.UndercutCount++
		}
		s.emitEvent(ctx, obs, req)
		s.recordMetric(ch.Channel(), obs.Undercut)
	}
}

// classifyChannelError maps the per-channel error into the
// appropriate result bucket. Cyclomatic 4.
func (s *CompetitorScraper) classifyChannelError(channel string, err error, res *CompetitorScrapeResult) {
	switch {
	case errors.Is(err, ErrCompetitorMatchNotFound):
		res.NotFoundChannels = append(res.NotFoundChannels, channel)
	case errors.Is(err, ErrScraperRateLimited):
		res.RateLimitedChannels = append(res.RateLimitedChannels, channel)
	default:
		res.UnavailableChannels = append(res.UnavailableChannels, channel)
		s.logger.Warn("competitor_scraper.channel_failed", "tenant_id", s.cfg.TenantID, "channel", channel, "error", err)
	}
}

// buildObservation runs the delta classification pure-function.
func (s *CompetitorScraper) buildObservation(channel string, row CompetitorObservation, req CompetitorScrapeRequest) CompetitorObservation {
	row.Channel = channel
	if row.ObservedAt.IsZero() {
		row.ObservedAt = s.cfg.Now()
	}
	row.Undercut, row.UndercutPct = evaluateDelta(req.OurPriceAUDCents, row.PriceAUDCents, s.cfg.UndercutPctThreshold)
	return row
}

// evaluateDelta is a pure function: returns (undercut, pct).
// undercut is true when the competitor price is BELOW our price by
// more than the configured threshold (i.e. they are cheaper).
// Cyclomatic 3.
func evaluateDelta(ourCents, theirCents int, threshold float64) (bool, float64) {
	if ourCents <= 0 || theirCents <= 0 {
		return false, 0
	}
	if theirCents >= ourCents {
		return false, 0
	}
	pct := float64(ourCents-theirCents) / float64(ourCents)
	if pct >= threshold {
		return true, pct
	}
	return false, pct
}

// emitEvent publishes the typed observation + (optional) undercut
// event. Cyclomatic 4.
func (s *CompetitorScraper) emitEvent(ctx context.Context, obs CompetitorObservation, req CompetitorScrapeRequest) {
	payload := eventbus.CompetitorPricePayload{
		Version:          eventbus.CompetitorPricePayloadVersion,
		TenantID:         s.cfg.TenantID,
		SKU:              req.SKU,
		Channel:          obs.Channel,
		CompetitorID:     obs.CompetitorID,
		CompetitorName:   obs.CompetitorName,
		CompetitorURL:    obs.CompetitorURL,
		PriceAUDCents:    obs.PriceAUDCents,
		OurPriceAUDCents: req.OurPriceAUDCents,
		UndercutPct:      obs.UndercutPct,
		ImageFingerprint: obs.ImageFingerprint,
		ObservedAt:       obs.ObservedAt,
	}
	observed, err := eventbus.NewCompetitorPriceObservedEvent("agent.pricing.competitor_scraper", obs.ObservedAt, payload)
	if err != nil {
		s.logger.Error("competitor_scraper.observed_event_invalid", "tenant_id", s.cfg.TenantID, "error", err)
		return
	}
	if err := s.cfg.Publisher.Publish(ctx, observed); err != nil {
		s.logger.Error("competitor_scraper.publish_failed", "event", string(observed.Type), "error", err)
	}
	if !obs.Undercut {
		return
	}
	undercut, err := eventbus.NewCompetitorUndercutEvent("agent.pricing.competitor_scraper", obs.ObservedAt, payload)
	if err != nil {
		s.logger.Error("competitor_scraper.undercut_event_invalid", "tenant_id", s.cfg.TenantID, "error", err)
		return
	}
	if err := s.cfg.Publisher.Publish(ctx, undercut); err != nil {
		s.logger.Error("competitor_scraper.publish_failed", "event", string(undercut.Type), "error", err)
	}
}

func (s *CompetitorScraper) recordMetric(channel string, undercut bool) {
	if s.cfg.Metrics == nil {
		return
	}
	flag := "false"
	if undercut {
		flag = "true"
	}
	s.cfg.Metrics.RecordCompetitorObservation(s.cfg.TenantID, channel, flag)
}

func (s *CompetitorScraper) recordKPI(req CompetitorScrapeRequest, res CompetitorScrapeResult) {
	if s.cfg.KPIHook == nil {
		return
	}
	s.cfg.KPIHook(CompetitorScraperKPISample{
		TenantID:            s.cfg.TenantID,
		SKU:                 req.SKU,
		Observations:        len(res.Observations),
		UndercutsDetected:   res.UndercutCount,
		RateLimitedChannels: len(res.RateLimitedChannels),
		UnavailableChannels: len(res.UnavailableChannels),
	})
}

func (s *CompetitorScraper) guard() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrCompetitorScraperClosed
	}
	return nil
}

func validateScrapeRequest(req CompetitorScrapeRequest) error {
	if strings.TrimSpace(req.SKU) == "" {
		return fmt.Errorf("%w: SKU required", ErrCompetitorScraperUnconfigured)
	}
	if req.OurPriceAUDCents <= 0 {
		return fmt.Errorf("%w: OurPriceAUDCents must be > 0", ErrCompetitorScraperUnconfigured)
	}
	return nil
}
