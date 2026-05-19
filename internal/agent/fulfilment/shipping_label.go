// File scope: v3.8.0 EC-7-3 shipping label generator.
//
// Generates a carrier shipping label for an internal order. The
// carrier is selected by cheapest-quote-within-SLA across the
// configured client set (AusPost + DHL by default). Falls back to the
// next-cheapest carrier when the chosen one fails the CreateLabel
// call. Returns the cached label on repeated calls keyed by the
// (tenant_id, order_id) tuple so the EC-7-4 status propagator can
// observe a stable tracking_number.
//
// Reuse evidence:
//   - DropshipAgent (v3.5.0 EC-7-2) sentinel + lifecycle.Closer
//     pattern. Same cyclomatic discipline.
//   - eventbus.Publisher contract from v3.3.0 EC-3-3.
//   - In-memory idempotency store mirrors v3.3.0 EC-3-4 sync store.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 13-sprint streak; v3.8.0 sprint 14 target):
//   - Generate (envelope -> validate -> cache -> selectCarrier ->
//     requestLabel -> persist + publish -> return)
//   - selectCarrier (cheapest-within-SLA over the quote set)
//   - requestLabel (CreateLabel + fallback dispatch)
//   - cacheLabel (in-memory dedup; thread-safe)
//
// Each helper stays under cyclomatic 6.

package fulfilment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/carrier"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

// EC-7-3 typed sentinels.
var (
	// ErrShippingLabelGeneratorUnconfigured is returned when a
	// required dependency is missing.
	ErrShippingLabelGeneratorUnconfigured = errors.New("fulfilment: shipping label generator unconfigured")

	// ErrShippingLabelGeneratorClosed is returned after Close.
	ErrShippingLabelGeneratorClosed = errors.New("fulfilment: shipping label generator closed")

	// ErrSLANotMet wraps carrier.ErrSLANotMet so callers can match
	// either via errors.Is.
	ErrSLANotMet = carrier.ErrSLANotMet

	// ErrAllCarriersFailed signals every configured carrier failed
	// the CreateLabel call after the fallback path exhausted.
	ErrAllCarriersFailed = errors.New("fulfilment: every carrier failed; saga rolled back")
)

// DefaultDomesticAUSLADays is the EC-7-3 acceptance criterion: 5-day
// delivery for domestic AU. Used as the default when ShipmentRequest
// does not specify an explicit SLA.
const DefaultDomesticAUSLADays = 5

// CarrierClient is the small port both AusPost + DHL adapters
// implement. The port lives here so the generator stays decoupled
// from the carrier package wire shape.
type CarrierClient interface {
	Name() string
	Quote(ctx context.Context, req carrier.QuoteRequest) (carrier.Quote, error)
	CreateLabel(ctx context.Context, req carrier.LabelRequest) (carrier.Label, error)
}

// ShipmentRequest is the per-order shape submitted to Generate.
type ShipmentRequest struct {
	TenantID      string
	OrderID       string
	BuyerEmail    string
	OriginCountry string
	OriginPost    string
	DestCountry   string
	DestPost      string
	WeightGrams   int
	SLADays       int
}

// LabelResult is Generate's return shape.
type LabelResult struct {
	TenantID       string
	OrderID        string
	Carrier        string
	TrackingNumber string
	LabelPDFURL    string
	CostAUDCents   int
	ETADays        int
	SLADays        int
	GeneratedAt    time.Time
	Cached         bool
}

// ShippingLabelMetrics is the small port the generator emits the
// ec_shipping_labels_generated_total counter + cost histogram
// through.
type ShippingLabelMetrics interface {
	RecordShippingLabel(tenantID, carrierName, status string)
	ObserveShippingLabelCost(tenantID, carrierName string, costAUDCents int)
}

// ShippingLabelKPISample is the EvoMap KPI sample emitted per call.
type ShippingLabelKPISample struct {
	TenantID     string
	Carrier      string
	Status       string // generated | cached | sla_breach | all_failed
	CostAUDCents int
	ETADays      int
}

// ShippingLabelKPIHook is the optional EvoMap emission hook.
type ShippingLabelKPIHook func(ShippingLabelKPISample)

// ShippingLabelConfig wires a ShippingLabelGenerator.
type ShippingLabelConfig struct {
	Carriers   []CarrierClient
	Publisher  eventbus.Publisher
	DefaultSLA int
	Metrics    ShippingLabelMetrics
	KPIHook    ShippingLabelKPIHook
	Now        func() time.Time
}

// ShippingLabelGenerator is the v3.8.0 EC-7-3 generator.
type ShippingLabelGenerator struct {
	carriers   []CarrierClient
	publisher  eventbus.Publisher
	defaultSLA int
	metrics    ShippingLabelMetrics
	kpiHook    ShippingLabelKPIHook
	now        func() time.Time
	logger     *slog.Logger

	mu     sync.Mutex
	cache  map[string]LabelResult
	closed bool
}

// NewShippingLabelGenerator constructs the generator.
func NewShippingLabelGenerator(logger *slog.Logger, cfg ShippingLabelConfig) (*ShippingLabelGenerator, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if len(cfg.Carriers) == 0 {
		return nil, fmt.Errorf("%w: at least one CarrierClient required", ErrShippingLabelGeneratorUnconfigured)
	}
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("%w: Publisher required", ErrShippingLabelGeneratorUnconfigured)
	}
	if cfg.DefaultSLA <= 0 {
		cfg.DefaultSLA = DefaultDomesticAUSLADays
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &ShippingLabelGenerator{
		carriers:   cfg.Carriers,
		publisher:  cfg.Publisher,
		defaultSLA: cfg.DefaultSLA,
		metrics:    cfg.Metrics,
		kpiHook:    cfg.KPIHook,
		now:        cfg.Now,
		logger:     logger,
		cache:      make(map[string]LabelResult),
	}, nil
}

// Close marks the generator closed. lifecycle.Closer contract.
func (g *ShippingLabelGenerator) Close(_ context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	return nil
}

// Generate runs the EC-7-3 pipeline:
// validate -> cache lookup -> selectCarrier -> requestLabel -> cache + publish.
//
// Cyclomatic stays at 4: validate / cache / select+request / persist.
func (g *ShippingLabelGenerator) Generate(ctx context.Context, req ShipmentRequest) (LabelResult, error) {
	if err := g.guard(); err != nil {
		return LabelResult{}, err
	}
	if err := validateShipmentRequest(req); err != nil {
		return LabelResult{}, err
	}
	if cached, ok := g.lookupCache(req.TenantID, req.OrderID); ok {
		g.recordOutcome(ShippingLabelKPISample{TenantID: req.TenantID, Carrier: cached.Carrier, Status: "cached", CostAUDCents: cached.CostAUDCents, ETADays: cached.ETADays})
		return cached, nil
	}
	sla := req.SLADays
	if sla <= 0 {
		sla = g.defaultSLA
	}
	ranked, err := g.selectCarrier(ctx, req, sla)
	if err != nil {
		g.recordOutcome(ShippingLabelKPISample{TenantID: req.TenantID, Status: "sla_breach"})
		return LabelResult{}, err
	}
	label, supplier, err := g.requestLabel(ctx, req, ranked)
	if err != nil {
		g.recordOutcome(ShippingLabelKPISample{TenantID: req.TenantID, Status: "all_failed"})
		return LabelResult{}, err
	}
	res := LabelResult{
		TenantID:       req.TenantID,
		OrderID:        req.OrderID,
		Carrier:        supplier,
		TrackingNumber: label.TrackingNumber,
		LabelPDFURL:    label.LabelPDFURL,
		CostAUDCents:   label.CostAUDCents,
		ETADays:        label.ETADays,
		SLADays:        sla,
		GeneratedAt:    g.now(),
	}
	g.cacheLabel(req.TenantID, req.OrderID, res)
	g.publishGenerated(ctx, res)
	g.recordOutcome(ShippingLabelKPISample{TenantID: req.TenantID, Carrier: supplier, Status: "generated", CostAUDCents: res.CostAUDCents, ETADays: res.ETADays})
	if g.metrics != nil {
		g.metrics.ObserveShippingLabelCost(req.TenantID, supplier, res.CostAUDCents)
	}
	return res, nil
}

// selectCarrier asks every configured carrier for a Quote, drops any
// quote that fails the SLA, and returns the cheapest-first ordered
// list. AusPost wins on ties (domestic AU default per spec).
func (g *ShippingLabelGenerator) selectCarrier(ctx context.Context, req ShipmentRequest, slaDays int) ([]carrier.Quote, error) {
	quoteReq := carrier.QuoteRequest{
		TenantID:      req.TenantID,
		OriginCountry: req.OriginCountry,
		OriginPost:    req.OriginPost,
		DestCountry:   req.DestCountry,
		DestPost:      req.DestPost,
		WeightGrams:   req.WeightGrams,
	}
	var fits []carrier.Quote
	for _, c := range g.carriers {
		q, err := c.Quote(ctx, quoteReq)
		if err != nil {
			g.logger.Warn("fulfilment.shipping_label.quote_failed", "carrier", c.Name(), "error", err)
			continue
		}
		if q.ETADays > slaDays {
			continue
		}
		fits = append(fits, q)
	}
	if len(fits) == 0 {
		return nil, fmt.Errorf("%w: sla=%d days; no carrier within window", ErrSLANotMet, slaDays)
	}
	rankCarrierQuotes(fits)
	return fits, nil
}

// requestLabel calls CreateLabel on the cheapest carrier; on failure
// falls back to the next ranked carrier; surfaces ErrAllCarriersFailed
// if every option fails.
func (g *ShippingLabelGenerator) requestLabel(ctx context.Context, req ShipmentRequest, ranked []carrier.Quote) (carrier.Label, string, error) {
	labelReq := carrier.LabelRequest{
		TenantID:    req.TenantID,
		OrderID:     req.OrderID,
		BuyerEmail:  req.BuyerEmail,
		OriginPost:  req.OriginPost,
		DestPost:    req.DestPost,
		DestCountry: req.DestCountry,
		WeightGrams: req.WeightGrams,
	}
	clients := g.clientsByName()
	var lastErr error
	for _, q := range ranked {
		client, ok := clients[q.Carrier]
		if !ok {
			continue
		}
		label, err := client.CreateLabel(ctx, labelReq)
		if err == nil {
			return label, client.Name(), nil
		}
		lastErr = err
		g.logger.Warn("fulfilment.shipping_label.label_failed", "carrier", client.Name(), "error", err)
	}
	if lastErr == nil {
		lastErr = errors.New("no carrier produced a label")
	}
	return carrier.Label{}, "", fmt.Errorf("%w: %v", ErrAllCarriersFailed, lastErr)
}

// cacheLabel writes the label to the in-memory dedup cache.
func (g *ShippingLabelGenerator) cacheLabel(tenantID, orderID string, res LabelResult) {
	key := buildLabelCacheKey(tenantID, orderID)
	g.mu.Lock()
	g.cache[key] = res
	g.mu.Unlock()
}

func (g *ShippingLabelGenerator) lookupCache(tenantID, orderID string) (LabelResult, bool) {
	key := buildLabelCacheKey(tenantID, orderID)
	g.mu.Lock()
	defer g.mu.Unlock()
	res, ok := g.cache[key]
	if !ok {
		return LabelResult{}, false
	}
	res.Cached = true
	return res, true
}

func (g *ShippingLabelGenerator) clientsByName() map[string]CarrierClient {
	out := make(map[string]CarrierClient, len(g.carriers))
	for _, c := range g.carriers {
		out[c.Name()] = c
	}
	return out
}

func (g *ShippingLabelGenerator) guard() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return ErrShippingLabelGeneratorClosed
	}
	return nil
}

func (g *ShippingLabelGenerator) recordOutcome(s ShippingLabelKPISample) {
	if g.metrics != nil {
		g.metrics.RecordShippingLabel(s.TenantID, s.Carrier, s.Status)
	}
	if g.kpiHook != nil {
		g.kpiHook(s)
	}
}

func (g *ShippingLabelGenerator) publishGenerated(ctx context.Context, res LabelResult) {
	payload := eventbus.ShipmentLabelGeneratedPayload{
		Version:        eventbus.ShipmentLabelGeneratedPayloadVersion,
		TenantID:       res.TenantID,
		OrderID:        res.OrderID,
		Carrier:        res.Carrier,
		TrackingNumber: res.TrackingNumber,
		LabelPDFURL:    res.LabelPDFURL,
		CostAUDCents:   res.CostAUDCents,
		ETADays:        res.ETADays,
		SLADays:        res.SLADays,
		OccurredAt:     res.GeneratedAt,
	}
	evt, err := eventbus.NewShipmentLabelGeneratedEvent("agent.fulfilment.shipping_label", res.GeneratedAt, payload)
	if err != nil {
		g.logger.Error("fulfilment.shipping_label.event_invalid", "error", err)
		return
	}
	if err := g.publisher.Publish(ctx, evt); err != nil {
		g.logger.Error("fulfilment.shipping_label.publish_failed", "error", err)
	}
}

// rankCarrierQuotes sorts cheapest-first; on tie, AusPost wins
// (domestic AU default per EC-7-3 spec).
func rankCarrierQuotes(quotes []carrier.Quote) {
	sort.SliceStable(quotes, func(i, j int) bool {
		if quotes[i].CostAUDCents != quotes[j].CostAUDCents {
			return quotes[i].CostAUDCents < quotes[j].CostAUDCents
		}
		return tieBreakRank(quotes[i].Carrier) < tieBreakRank(quotes[j].Carrier)
	})
}

func tieBreakRank(name string) int {
	if name == carrier.CarrierAusPost {
		return 0
	}
	return 1
}

func buildLabelCacheKey(tenantID, orderID string) string {
	return tenantID + "\x00" + orderID
}

func validateShipmentRequest(req ShipmentRequest) error {
	if strings.TrimSpace(req.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id required", ErrShippingLabelGeneratorUnconfigured)
	}
	if strings.TrimSpace(req.OrderID) == "" {
		return fmt.Errorf("%w: order_id required", ErrShippingLabelGeneratorUnconfigured)
	}
	if strings.TrimSpace(req.DestPost) == "" {
		return fmt.Errorf("%w: dest_post required", ErrShippingLabelGeneratorUnconfigured)
	}
	if req.WeightGrams <= 0 {
		return fmt.Errorf("%w: weight must be positive", ErrShippingLabelGeneratorUnconfigured)
	}
	return nil
}
