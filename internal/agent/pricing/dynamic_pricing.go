// File scope: v3.5.0 EC-6-3 dynamic pricing agent.
//
// The package already hosts the v2.10 deterministic pricing
// strategy recommender (Agent / Recommend in agent.go); this file
// adds the v3.5.0 EC-6-3 PricingAgent that subscribes to
// SupplierCostChangedEvent (EC-6-1) and competitor signals,
// applies operator guardrails + LLM-second decisions, and emits
// PriceChange* events to the eventbus.
//
// Reuse evidence:
//   - port.AITextGenerator from v3.2.0 EC-2-1 + v3.4.0 EC-5-1 (LLM
//     failover pattern: rule-first + LLM second + template fallback).
//   - billing.PlatformFeeCalculator from v3.5.0 EC-6-2 (resolves the
//     CNY->AUD cost component).
//   - eventbus.Publisher contract from v3.3.0 EC-3-3.
//   - The rule + LLM + fallback pattern mirrors v3.2.0 EC-2-1
//     enrichment.DescriptionGenerator -- here the "template" is the
//     deterministic competitor-shadow rule.
//   - The package-internal `charmPrice` helper from agent.go is
//     deliberately NOT reused; the v2.10 agent operates in cents,
//     the v3.5.0 agent operates on raw integer cents and lets the
//     external pricing-display layer charm-round if needed.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4 -- 7-sprint streak; v3.5.0 sprint 8 target):
//   - Decide (envelope -> validate -> floor -> suggest -> guardrail
//     -> approval -> emit)
//   - applyGuardrails (pure; clamps + flags GuardrailBlocked)
//   - requestLLMSuggestion (LLM call + JSON parse + clamp)
//   - routeApproval (pending-vs-applied gate)
//   - HandleSupplierCostChanged (eventbus dispatch)
//
// Each helper stays under cyclomatic 6.
//
// Resilience pillar (v2.10 baseline):
//   - Implements lifecycle.Closer.
//   - LLM call is synchronous; no raw goroutines.
//   - Errors typed + %w-wrapped via package sentinels.
//   - Tenant-aware: every event carries TenantID.
package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/billing"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

// DefaultMarginFloorPct is the v3.5.0 EC-6-3 default margin floor
// (35% per the plan example).
const DefaultMarginFloorPct = 0.35

// DefaultLargeChangeThresholdPct is the operator-approval gate
// threshold (15% per the EC-6-3 spec).
const DefaultLargeChangeThresholdPct = 0.15

// PriceDecisionSource identifies whether the proposed price came
// from the LLM or the deterministic rule fallback.
type PriceDecisionSource string

// PriceDecisionSource enum values.
const (
	PriceDecisionSourceLLM  PriceDecisionSource = "llm"
	PriceDecisionSourceRule PriceDecisionSource = "rule"
)

// EC-6-3 typed sentinels.
var (
	// ErrPricingAgentUnconfigured is returned when a required
	// dependency (TenantID / Publisher) is missing.
	ErrPricingAgentUnconfigured = errors.New("pricing: agent unconfigured")

	// ErrPricingAgentClosed is returned by Decide / HandleEvent
	// after Close.
	ErrPricingAgentClosed = errors.New("pricing: agent closed")

	// ErrInvalidPriceDecisionInput is returned by Decide when the
	// input contract is violated.
	ErrInvalidPriceDecisionInput = errors.New("pricing: invalid decision input")

	// ErrPriceBelowFloor is the sentinel surfaced when the LLM
	// suggestion (or rule fallback) cannot clear the margin floor.
	ErrPriceBelowFloor = errors.New("pricing: proposed price below margin floor")

	// ErrApprovalRequired is the sentinel for changes above the
	// configurable threshold.
	ErrApprovalRequired = errors.New("pricing: operator approval required")

	// ErrPricingLLMUnavailable is the sentinel for the rule-only
	// failover path. Surfaced via the result struct's Source =
	// PriceDecisionSourceRule rather than as a fatal error.
	ErrPricingLLMUnavailable = errors.New("pricing: llm unavailable; failed over to rule")
)

// PriceDecisionInput is the unit of work submitted to Decide.
type PriceDecisionInput struct {
	ProductID               string
	Channel                 billing.Channel
	CurrentPriceAUDCents    int
	CostCNYCents            int
	ShippingEstAUDCents     int
	CompetitorPriceAUDCents int // optional; 0 means "no competitor signal"
	TikTokCommissionPct     float64
}

// PriceDecisionResult captures the agent run output. Inspectable
// without subscribing to the eventbus.
type PriceDecisionResult struct {
	ProductID              string
	Channel                billing.Channel
	CurrentPriceAUDCents   int
	ProposedPriceAUDCents  int
	DeltaPct               float64
	GrossMarginPct         float64
	RuleFloorPriceAUDCents int
	Source                 PriceDecisionSource
	Reason                 string
	Applied                bool
	PendingApproval        bool
	GuardrailBlocked       bool
	GeneratedAt            time.Time
}

// ProductPricingState is the v3.5.0 in-memory state the agent
// consults when handling SupplierCostChangedEvent. Operators
// register the products they want the agent to manage; future
// v3.5.x can swap for a Postgres-backed adapter.
type ProductPricingState struct {
	ProductID               string
	Channel                 billing.Channel
	CurrentPriceAUDCents    int
	CostCNYCents            int
	ShippingEstAUDCents     int
	CompetitorPriceAUDCents int
	TikTokCommissionPct     float64
}

// PricingAgentMetrics is the small port the agent emits decision
// counters + the change-pct histogram through.
type PricingAgentMetrics interface {
	RecordPricingDecision(tenantID, outcome string)
	ObservePriceChangePct(deltaPct float64)
}

// PricingAgentKPISample is the EvoMap KPI sample emitted per
// Decide call.
type PricingAgentKPISample struct {
	TenantID string
	Outcome  string
	DeltaPct float64
}

// PricingAgentKPIHook is the optional EvoMap emission hook.
type PricingAgentKPIHook func(PricingAgentKPISample)

// PricingAgentConfig wires a PricingAgent.
type PricingAgentConfig struct {
	TenantID                string
	FeeCalculator           *billing.PlatformFeeCalculator
	Publisher               eventbus.Publisher
	LLM                     port.AITextGenerator
	MarginFloorPct          float64
	LargeChangeThresholdPct float64
	Metrics                 PricingAgentMetrics
	KPIHook                 PricingAgentKPIHook
	Now                     func() time.Time
}

// PricingAgent is the v3.5.0 EC-6-3 dynamic pricing agent.
type PricingAgent struct {
	cfg    PricingAgentConfig
	logger *slog.Logger

	mu       sync.Mutex
	closed   bool
	products map[string]ProductPricingState
}

// NewPricingAgent constructs an agent.
func NewPricingAgent(logger *slog.Logger, cfg PricingAgentConfig) (*PricingAgent, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := validatePricingAgentConfig(cfg); err != nil {
		return nil, err
	}
	applyPricingAgentDefaults(&cfg)
	return &PricingAgent{
		cfg:      cfg,
		logger:   logger,
		products: map[string]ProductPricingState{},
	}, nil
}

func validatePricingAgentConfig(cfg PricingAgentConfig) error {
	if strings.TrimSpace(cfg.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrPricingAgentUnconfigured)
	}
	if cfg.Publisher == nil {
		return fmt.Errorf("%w: Publisher required", ErrPricingAgentUnconfigured)
	}
	return nil
}

func applyPricingAgentDefaults(cfg *PricingAgentConfig) {
	if cfg.MarginFloorPct <= 0 {
		cfg.MarginFloorPct = DefaultMarginFloorPct
	}
	if cfg.LargeChangeThresholdPct <= 0 {
		cfg.LargeChangeThresholdPct = DefaultLargeChangeThresholdPct
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
}

// Close marks the agent closed.
func (a *PricingAgent) Close(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

// MarginFloorPct returns the configured margin floor.
func (a *PricingAgent) MarginFloorPct() float64 { return a.cfg.MarginFloorPct }

// LargeChangeThresholdPct returns the configured approval gate.
func (a *PricingAgent) LargeChangeThresholdPct() float64 { return a.cfg.LargeChangeThresholdPct }

// RegisterProduct stores the operator-supplied state for a product
// so HandleSupplierCostChanged knows which products to re-decide.
func (a *PricingAgent) RegisterProduct(p ProductPricingState) error {
	if strings.TrimSpace(p.ProductID) == "" {
		return fmt.Errorf("%w: ProductID required", ErrInvalidPriceDecisionInput)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrPricingAgentClosed
	}
	a.products[p.ProductID] = p
	return nil
}

// Decide runs the EC-6-3 pipeline:
// validate -> rule floor -> LLM -> approval gate -> emit.
//
// Decomposition keeps cyclomatic per-function under 6.
func (a *PricingAgent) Decide(ctx context.Context, in PriceDecisionInput) (PriceDecisionResult, error) {
	if err := a.guard(); err != nil {
		return PriceDecisionResult{}, err
	}
	if err := validatePriceDecisionInput(in); err != nil {
		return PriceDecisionResult{}, err
	}
	floor, marginAtFloor := a.computeFloor(in)
	candidate, source := a.suggestPrice(ctx, in, floor)
	res := PriceDecisionResult{
		ProductID:              in.ProductID,
		Channel:                in.Channel,
		CurrentPriceAUDCents:   in.CurrentPriceAUDCents,
		ProposedPriceAUDCents:  candidate,
		RuleFloorPriceAUDCents: floor,
		Source:                 source,
		GeneratedAt:            a.cfg.Now(),
		GrossMarginPct:         marginAtFloor,
	}
	res = applyGuardrails(res, floor, in, a.cfg.MarginFloorPct)
	res = routeApproval(res, a.cfg.LargeChangeThresholdPct)
	a.emit(ctx, res)
	a.recordOutcome(res)
	return res, nil
}

// computeFloor returns the smallest selling price that still hits
// the configured margin floor at the given cost + shipping.
func (a *PricingAgent) computeFloor(in PriceDecisionInput) (int, float64) {
	costAUD := 0
	if a.cfg.FeeCalculator != nil {
		costAUD = a.estimateCostAUD(in)
	}
	floor := minPriceForMarginFloor(costAUD, in.ShippingEstAUDCents, in.Channel, in.TikTokCommissionPct, a.cfg.MarginFloorPct)
	return floor, a.cfg.MarginFloorPct
}

// estimateCostAUD reaches into the fee calculator to convert the
// CNY cost using the latest FX. Returns 0 on any provider error
// so the caller can still compute a defensive floor.
func (a *PricingAgent) estimateCostAUD(in PriceDecisionInput) int {
	res, err := a.cfg.FeeCalculator.CalculateMargin(context.Background(), billing.PriceComponents{
		Channel:              in.Channel,
		SellingPriceAUDCents: maxPricingInt(in.CurrentPriceAUDCents, 1000),
		CostCNYCents:         in.CostCNYCents,
		ShippingEstAUDCents:  in.ShippingEstAUDCents,
		TikTokCommissionPct:  in.TikTokCommissionPct,
	})
	if err != nil {
		a.logger.Warn("pricing.fee_calc_failed", "tenant_id", a.cfg.TenantID, "product_id", in.ProductID, "error", err)
		return 0
	}
	return res.CostAUDCents
}

// suggestPrice consults the LLM first and falls back to the rule
// when the LLM is unavailable or returns garbage.
func (a *PricingAgent) suggestPrice(ctx context.Context, in PriceDecisionInput, floor int) (int, PriceDecisionSource) {
	candidate, ok := a.requestLLMSuggestion(ctx, in)
	if ok {
		return candidate, PriceDecisionSourceLLM
	}
	return ruleSuggest(in, floor), PriceDecisionSourceRule
}

// requestLLMSuggestion calls the LLM, parses the JSON response, and
// returns ok=false on any failure (the caller falls back to rule).
func (a *PricingAgent) requestLLMSuggestion(ctx context.Context, in PriceDecisionInput) (int, bool) {
	if a.cfg.LLM == nil {
		return 0, false
	}
	system, user := buildPricingPrompt(in, a.cfg.MarginFloorPct, a.cfg.LargeChangeThresholdPct)
	resp, err := a.cfg.LLM.Complete(ctx, port.AICompletionRequest{
		Messages: []port.AIMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: pricingFloatPtr(0.2),
		MaxTokens:   pricingIntPtr(200),
	})
	if err != nil {
		a.logger.Warn("pricing.llm_unavailable", "tenant_id", a.cfg.TenantID, "product_id", in.ProductID, "error", err)
		return 0, false
	}
	candidate, ok := parseLLMSuggestion(resp.Content)
	if !ok || candidate <= 0 {
		return 0, false
	}
	return candidate, true
}

// emit publishes the typed PriceChange event corresponding to the
// final decision.
func (a *PricingAgent) emit(ctx context.Context, res PriceDecisionResult) {
	if res.GuardrailBlocked {
		return
	}
	payload := eventbus.PriceChangeApprovalPayload{
		Version:               eventbus.PriceChangePayloadVersion,
		TenantID:              a.cfg.TenantID,
		ProductID:             res.ProductID,
		Channel:               string(res.Channel),
		OldPriceAUDCents:      res.CurrentPriceAUDCents,
		ProposedPriceAUDCents: res.ProposedPriceAUDCents,
		DeltaPct:              res.DeltaPct,
		Reason:                res.Reason,
		DecisionSource:        string(res.Source),
		GuardrailFloorPct:     a.cfg.MarginFloorPct,
		OccurredAt:            res.GeneratedAt,
	}
	var (
		evt eventbus.Event
		err error
	)
	if res.PendingApproval {
		evt, err = eventbus.NewPriceChangePendingApprovalEvent("agent.pricing", res.GeneratedAt, payload)
	} else {
		evt, err = eventbus.NewPriceChangeAppliedEvent("agent.pricing", res.GeneratedAt, payload)
	}
	if err != nil {
		a.logger.Error("pricing.event_invalid", "tenant_id", a.cfg.TenantID, "error", err)
		return
	}
	if err := a.cfg.Publisher.Publish(ctx, evt); err != nil {
		a.logger.Error("pricing.publish_failed", "tenant_id", a.cfg.TenantID, "error", err)
	}
}

// HandleSupplierCostChanged dispatches a SupplierCostChangedEvent
// to Decide for any registered product matching the SKU.
func (a *PricingAgent) HandleSupplierCostChanged(ctx context.Context, evt eventbus.Event) error {
	if err := a.guard(); err != nil {
		return err
	}
	if evt.Type != eventbus.SupplierCostChanged {
		return fmt.Errorf("pricing: unexpected event type %s", evt.Type)
	}
	if evt.TenantID != a.cfg.TenantID {
		return fmt.Errorf("pricing: tenant mismatch event=%s agent=%s", evt.TenantID, a.cfg.TenantID)
	}
	sku := pricingStringFromMap(evt.Payload, "supplier_sku")
	cost := pricingIntFromMap(evt.Payload, "observed_cny_cents")
	state, ok := a.lookupProduct(sku)
	if !ok {
		a.logger.Info("pricing.no_product_for_sku", "sku", sku, "tenant_id", a.cfg.TenantID)
		return nil
	}
	state.CostCNYCents = cost
	_, err := a.Decide(ctx, PriceDecisionInput{
		ProductID:               state.ProductID,
		Channel:                 state.Channel,
		CurrentPriceAUDCents:    state.CurrentPriceAUDCents,
		CostCNYCents:            state.CostCNYCents,
		ShippingEstAUDCents:     state.ShippingEstAUDCents,
		CompetitorPriceAUDCents: state.CompetitorPriceAUDCents,
		TikTokCommissionPct:     state.TikTokCommissionPct,
	})
	return err
}

func (a *PricingAgent) lookupProduct(sku string) (ProductPricingState, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if state, ok := a.products[sku]; ok {
		return state, true
	}
	return ProductPricingState{}, false
}

func (a *PricingAgent) guard() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrPricingAgentClosed
	}
	return nil
}

func (a *PricingAgent) recordOutcome(res PriceDecisionResult) {
	outcome := outcomeForResult(res)
	if a.cfg.Metrics != nil {
		a.cfg.Metrics.RecordPricingDecision(a.cfg.TenantID, outcome)
		if !res.GuardrailBlocked {
			a.cfg.Metrics.ObservePriceChangePct(math.Abs(res.DeltaPct))
		}
	}
	if a.cfg.KPIHook != nil {
		a.cfg.KPIHook(PricingAgentKPISample{TenantID: a.cfg.TenantID, Outcome: outcome, DeltaPct: res.DeltaPct})
	}
}

// outcomeForResult maps the result struct flags to a single
// outcome label for the metrics counter. Pure; cyclomatic 4.
func outcomeForResult(res PriceDecisionResult) string {
	switch {
	case res.GuardrailBlocked:
		return "guardrail_blocked"
	case res.PendingApproval:
		return "approval_pending"
	case res.Source == PriceDecisionSourceRule:
		return "llm_failover"
	default:
		return "approved"
	}
}

func validatePriceDecisionInput(in PriceDecisionInput) error {
	if strings.TrimSpace(in.ProductID) == "" {
		return fmt.Errorf("%w: ProductID required", ErrInvalidPriceDecisionInput)
	}
	if strings.TrimSpace(string(in.Channel)) == "" {
		return fmt.Errorf("%w: channel required", ErrInvalidPriceDecisionInput)
	}
	if in.CurrentPriceAUDCents <= 0 {
		return fmt.Errorf("%w: current_price_aud_cents must be > 0", ErrInvalidPriceDecisionInput)
	}
	if in.CostCNYCents < 0 {
		return fmt.Errorf("%w: cost_cny_cents cannot be negative", ErrInvalidPriceDecisionInput)
	}
	if in.ShippingEstAUDCents < 0 {
		return fmt.Errorf("%w: shipping_est_aud_cents cannot be negative", ErrInvalidPriceDecisionInput)
	}
	return nil
}

// applyGuardrails clamps the candidate price to the rule floor and
// flags GuardrailBlocked when the candidate can't clear the floor.
func applyGuardrails(res PriceDecisionResult, floor int, in PriceDecisionInput, marginFloorPct float64) PriceDecisionResult {
	if floor <= 0 {
		// Defensive: no fee calculator available. Treat any
		// positive candidate as acceptable; operator must wire the
		// calculator in production.
		res.DeltaPct = computeDeltaPct(in.CurrentPriceAUDCents, res.ProposedPriceAUDCents)
		return res
	}
	if res.ProposedPriceAUDCents < floor {
		res.GuardrailBlocked = true
		res.Reason = fmt.Sprintf("proposed %d < floor %d (margin floor %.0f%%)", res.ProposedPriceAUDCents, floor, marginFloorPct*100)
		return res
	}
	res.DeltaPct = computeDeltaPct(in.CurrentPriceAUDCents, res.ProposedPriceAUDCents)
	return res
}

// routeApproval flips PendingApproval / Applied based on the
// configured threshold. Pure; cyclomatic 3.
func routeApproval(res PriceDecisionResult, threshold float64) PriceDecisionResult {
	if res.GuardrailBlocked {
		return res
	}
	abs := math.Abs(res.DeltaPct)
	if abs > threshold {
		res.PendingApproval = true
		if res.Reason == "" {
			res.Reason = fmt.Sprintf("delta %.2f%% > threshold %.2f%% -- operator approval required", abs*100, threshold*100)
		}
		return res
	}
	res.Applied = true
	if res.Reason == "" {
		res.Reason = fmt.Sprintf("delta %.2f%% within threshold %.2f%% -- auto-applied", abs*100, threshold*100)
	}
	return res
}

// minPriceForMarginFloor solves for the minimum selling price that
// hits the margin floor:
//
//	margin_pct >= floor_pct
//	(s - cost_aud - fee(s) - shipping) / s >= floor
//	s * (1 - floor) >= cost_aud + fee_fixed + shipping + s * fee_pct
//	s * (1 - floor - fee_pct) >= cost_aud + fee_fixed + shipping
//	s >= (cost_aud + fee_fixed + shipping) / (1 - floor - fee_pct)
//
// Pure arithmetic; cyclomatic 2.
func minPriceForMarginFloor(costAUDCents, shippingAUDCents int, channel billing.Channel, tikTokCommissionPct, floorPct float64) int {
	feePct, feeFixed := channelFeeShape(channel, tikTokCommissionPct)
	denom := 1 - floorPct - feePct
	if denom <= 0 {
		return math.MaxInt32
	}
	num := float64(costAUDCents+feeFixed+shippingAUDCents) / denom
	return int(math.Ceil(num))
}

// channelFeeShape returns (variable_pct, fixed_aud_cents) for the
// fee model; mirrors billing.feeFor* without re-running the
// calculator on every floor query.
func channelFeeShape(channel billing.Channel, tikTokCommissionPct float64) (float64, int) {
	switch channel {
	case billing.ChannelTikTok:
		return clampTikTokCommission(tikTokCommissionPct), 0
	case billing.ChannelFacebook:
		return billing.FacebookCommissionPct, 0
	case billing.ChannelWooCommerce:
		return billing.StripePctRate, billing.StripeFixedAUDCents
	default:
		return 0, 0
	}
}

// clampTikTokCommission mirrors billing.clampTikTokCommission so
// the agent does not depend on package-private symbols.
func clampTikTokCommission(pct float64) float64 {
	if pct <= 0 {
		return billing.DefaultTikTokCommissionPct
	}
	if pct < billing.MinTikTokCommissionPct {
		return billing.MinTikTokCommissionPct
	}
	if pct > billing.MaxTikTokCommissionPct {
		return billing.MaxTikTokCommissionPct
	}
	return pct
}

// ruleSuggest is the rule-only fallback. Strategy:
//   - If competitor is known AND >= floor, shadow it (price match).
//   - Else hold current price (no signal).
func ruleSuggest(in PriceDecisionInput, floor int) int {
	if in.CompetitorPriceAUDCents > 0 {
		if in.CompetitorPriceAUDCents > floor {
			return in.CompetitorPriceAUDCents
		}
		return floor
	}
	if in.CurrentPriceAUDCents >= floor {
		return in.CurrentPriceAUDCents
	}
	return floor
}

// pricingPromptResponse is the JSON shape we ask the LLM to emit.
type pricingPromptResponse struct {
	RecommendedPriceAUDCents int    `json:"recommended_price_aud_cents"`
	Rationale                string `json:"rationale,omitempty"`
}

// parseLLMSuggestion extracts the recommended price + rationale.
// Mirrors the v3.2.0 EC-2-1 enrichment.parseGeneratedDescription
// pattern (strips fenced JSON, falls back gracefully).
func parseLLMSuggestion(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	stripped := strings.TrimPrefix(raw, "```json")
	stripped = strings.TrimPrefix(stripped, "```")
	stripped = strings.TrimSuffix(stripped, "```")
	stripped = strings.TrimSpace(stripped)
	var payload pricingPromptResponse
	if err := json.Unmarshal([]byte(stripped), &payload); err != nil {
		return 0, false
	}
	return payload.RecommendedPriceAUDCents, payload.RecommendedPriceAUDCents > 0
}

func buildPricingPrompt(in PriceDecisionInput, marginFloor, threshold float64) (string, string) {
	system := fmt.Sprintf(
		"You are a pricing analyst. Recommend a new selling price in AUD cents to maximise margin while staying within the operator guardrails. Margin floor=%.2f%%; large-change approval threshold=%.2f%%. Output JSON only: {\"recommended_price_aud_cents\":<int>,\"rationale\":\"<str>\"}.",
		marginFloor*100, threshold*100,
	)
	user := fmt.Sprintf(
		"Product: %s\nChannel: %s\nCurrent price (AUD cents): %d\nCost (CNY cents): %d\nShipping est (AUD cents): %d\nCompetitor price (AUD cents): %d",
		in.ProductID, in.Channel, in.CurrentPriceAUDCents, in.CostCNYCents, in.ShippingEstAUDCents, in.CompetitorPriceAUDCents,
	)
	return system, user
}

func computeDeltaPct(current, proposed int) float64 {
	if current <= 0 {
		return 0
	}
	return float64(proposed-current) / float64(current)
}

// pricingStringFromMap is the typed map accessor for event handler
// dispatch (suffixed to avoid collision with package agent.go).
func pricingStringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// pricingIntFromMap is the typed map accessor for event handler
// dispatch.
func pricingIntFromMap(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func pricingFloatPtr(v float64) *float64 { return &v }
func pricingIntPtr(v int) *int           { return &v }

func maxPricingInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
