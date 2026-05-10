// Package coaching provides contextual coaching for operators.
//
// v4.8.0 Story 2 -- coaching context integration.
//
// The CoachingEngine analyses an operator's current action + metrics
// and produces 1-2 sentence coaching tips. Two strategies:
//
//  1. LLM-driven: reuses the v3.2.0 IronClaw failover pattern
//  2. Rule-based fallback: 20 pre-written tips indexed by
//     action type + metric threshold
//
// Decomposition discipline (HARD GATE: complex_fn=4):
//
//   - GenerateTip     -> selectContext + runLLM + runRuleFallback + persistHistory
//   - selectContext    -> map action to coaching context (cyclomatic 3)
//   - runLLM          -> call LLM provider (cyclomatic 2)
//   - runRuleFallback  -> match rules (cyclomatic 3)
//   - persistHistory   -> store to repository (cyclomatic 2)
package coaching

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var (
	ErrCoachingUnconfigured = errors.New("coaching: engine unconfigured")
	ErrContextUnknown       = errors.New("coaching: unknown context")
)

type CoachingContext string

const (
	ContextOnboarding      CoachingContext = "onboarding"
	ContextPricingStrategy CoachingContext = "pricing_strategy"
	ContextChannelOptimize CoachingContext = "channel_optimization"
	ContextInventoryMgmt   CoachingContext = "inventory_management"
)

type TipSource string

const (
	SourceLLM  TipSource = "llm"
	SourceRule TipSource = "rule"
)

type TipRequest struct {
	TenantID       string             `json:"tenant_id"`
	OperatorID     string             `json:"operator_id"`
	Context        CoachingContext    `json:"context"`
	CurrentMetrics map[string]float64 `json:"current_metrics"`
}

type TipResponse struct {
	Tip        string          `json:"tip"`
	Confidence float64         `json:"confidence"`
	Source     TipSource       `json:"source"`
	Context    CoachingContext `json:"context"`
}

type CoachingSession struct {
	SessionID  string          `json:"session_id"`
	TenantID   string          `json:"tenant_id"`
	OperatorID string          `json:"operator_id"`
	Context    CoachingContext `json:"context"`
	Tip        string          `json:"tip"`
	Source     TipSource       `json:"source"`
	Accepted   bool            `json:"accepted"`
	CreatedAt  time.Time       `json:"created_at"`
}

type CoachingRepository interface {
	SaveSession(ctx context.Context, session CoachingSession) error
	AcceptTip(ctx context.Context, tenantID, sessionID string) error
	ListSessions(ctx context.Context, tenantID, operatorID string, limit int) ([]CoachingSession, error)
}

type LLMProvider interface {
	GenerateCoachingTip(ctx context.Context, coachingCtx CoachingContext, metrics map[string]float64) (string, float64, error)
}

type CoachingEventPublisher interface {
	PublishTipDelivered(ctx context.Context, tenantID, operatorID string, coachingCtx CoachingContext, source TipSource) error
}

type CoachingMetrics interface {
	RecordCoachingTip(tenantID string, coachingCtx CoachingContext, source TipSource)
}

type EngineConfig struct {
	Repository CoachingRepository
	LLM        LLMProvider
	Publisher  CoachingEventPublisher
	Metrics    CoachingMetrics
	IDFunc     func() string
	Now        func() time.Time
}

type CoachingEngine struct {
	repo      CoachingRepository
	llm       LLMProvider
	publisher CoachingEventPublisher
	metrics   CoachingMetrics
	idFunc    func() string
	now       func() time.Time
	logger    *slog.Logger
}

func NewCoachingEngine(logger *slog.Logger, cfg EngineConfig) (*CoachingEngine, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("%w: CoachingRepository required", ErrCoachingUnconfigured)
	}
	if cfg.IDFunc == nil {
		cfg.IDFunc = func() string { return fmt.Sprintf("cs-%d", time.Now().UnixNano()) }
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &CoachingEngine{
		repo:      cfg.Repository,
		llm:       cfg.LLM,
		publisher: cfg.Publisher,
		metrics:   cfg.Metrics,
		idFunc:    cfg.IDFunc,
		now:       cfg.Now,
		logger:    logger,
	}, nil
}

func (e *CoachingEngine) GenerateTip(ctx context.Context, req TipRequest) (TipResponse, error) {
	if err := e.validateContext(req.Context); err != nil {
		return TipResponse{}, err
	}
	resp, err := e.runLLM(ctx, req)
	if err != nil {
		e.logger.Warn("coaching.llm_failed, falling back to rules", "error", err)
		resp = e.runRuleFallback(req)
	}
	if err := e.persistHistory(ctx, req, resp); err != nil {
		e.logger.Error("coaching.persist_failed", "error", err)
	}
	e.emitMetrics(req, resp)
	e.publishEvent(ctx, req, resp)
	return resp, nil
}

func (e *CoachingEngine) AcceptTip(ctx context.Context, tenantID, sessionID string) error {
	return e.repo.AcceptTip(ctx, tenantID, sessionID)
}

func (e *CoachingEngine) validateContext(c CoachingContext) error {
	switch c {
	case ContextOnboarding, ContextPricingStrategy, ContextChannelOptimize, ContextInventoryMgmt:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrContextUnknown, c)
	}
}

func (e *CoachingEngine) runLLM(ctx context.Context, req TipRequest) (TipResponse, error) {
	if e.llm == nil {
		return TipResponse{}, errors.New("no LLM provider configured")
	}
	tip, confidence, err := e.llm.GenerateCoachingTip(ctx, req.Context, req.CurrentMetrics)
	if err != nil {
		return TipResponse{}, err
	}
	return TipResponse{
		Tip:        tip,
		Confidence: confidence,
		Source:     SourceLLM,
		Context:    req.Context,
	}, nil
}

func (e *CoachingEngine) runRuleFallback(req TipRequest) TipResponse {
	tip := selectRuleTip(req.Context, req.CurrentMetrics)
	return TipResponse{
		Tip:        tip,
		Confidence: 0.6,
		Source:     SourceRule,
		Context:    req.Context,
	}
}

func (e *CoachingEngine) persistHistory(ctx context.Context, req TipRequest, resp TipResponse) error {
	session := CoachingSession{
		SessionID:  e.idFunc(),
		TenantID:   req.TenantID,
		OperatorID: req.OperatorID,
		Context:    resp.Context,
		Tip:        resp.Tip,
		Source:     resp.Source,
		CreatedAt:  e.now(),
	}
	return e.repo.SaveSession(ctx, session)
}

func (e *CoachingEngine) emitMetrics(req TipRequest, resp TipResponse) {
	if e.metrics == nil {
		return
	}
	e.metrics.RecordCoachingTip(req.TenantID, resp.Context, resp.Source)
}

func (e *CoachingEngine) publishEvent(ctx context.Context, req TipRequest, resp TipResponse) {
	if e.publisher == nil {
		return
	}
	if err := e.publisher.PublishTipDelivered(ctx, req.TenantID, req.OperatorID, resp.Context, resp.Source); err != nil {
		e.logger.Warn("coaching.publish_failed", "error", err)
	}
}

var ruleBank = map[CoachingContext][]ruleEntry{
	ContextOnboarding: {
		{threshold: "conversion_rate", below: 0.02, tip: "Focus on adding high-quality product images to improve your conversion rate during the first week."},
		{threshold: "products_listed", below: 5, tip: "List at least 10 products across 2-3 categories to establish your store presence."},
		{threshold: "channels_active", below: 2, tip: "Connect at least two sales channels (e.g., TikTok + Instagram) to maximize reach."},
		{threshold: "", below: 0, tip: "Complete your store profile with a compelling brand story and return policy."},
		{threshold: "", below: 0, tip: "Set up automated order confirmation emails to build customer trust early."},
	},
	ContextPricingStrategy: {
		{threshold: "margin_pct", below: 0.15, tip: "Your margins are below 15%. Review supplier costs or consider bundling products for better value."},
		{threshold: "competitor_gap_pct", below: -0.10, tip: "Your prices are significantly above competitors. Consider a targeted discount campaign."},
		{threshold: "price_change_frequency", below: 0, tip: "Review your pricing strategy weekly based on competitor movements and demand signals."},
		{threshold: "", below: 0, tip: "Consider implementing tiered pricing for bulk orders to increase average order value."},
		{threshold: "", below: 0, tip: "Set price alerts for your top 5 products to respond quickly to market changes."},
	},
	ContextChannelOptimize: {
		{threshold: "channel_error_rate", below: 0, tip: "Monitor channel sync errors daily during the first month of operation."},
		{threshold: "listing_sync_lag_min", below: 0, tip: "Reduce listing sync lag by scheduling updates during off-peak hours."},
		{threshold: "", below: 0, tip: "Optimize product titles per channel: TikTok favours short punchy titles, Instagram benefits from hashtag-rich descriptions."},
		{threshold: "", below: 0, tip: "Enable automatic inventory sync across all channels to prevent overselling."},
		{threshold: "", below: 0, tip: "Review channel-specific analytics weekly to identify your best-performing platform."},
	},
	ContextInventoryMgmt: {
		{threshold: "stockout_rate", below: 0, tip: "Set reorder points for your top 20% products to avoid stockouts during peak demand."},
		{threshold: "overstock_ratio", below: 0, tip: "Review slow-moving inventory monthly and consider markdown strategies."},
		{threshold: "", below: 0, tip: "Enable low-stock alerts at 20% of average weekly sales volume per SKU."},
		{threshold: "", below: 0, tip: "Consider dropshipping for long-tail products to reduce warehousing costs."},
		{threshold: "", below: 0, tip: "Track lead times per supplier and build buffer stock for unreliable sources."},
	},
}

type ruleEntry struct {
	threshold string
	below     float64
	tip       string
}

func selectRuleTip(coachingCtx CoachingContext, metrics map[string]float64) string {
	rules, ok := ruleBank[coachingCtx]
	if !ok || len(rules) == 0 {
		return "Review your dashboard metrics regularly for improvement opportunities."
	}
	for _, rule := range rules {
		if rule.threshold == "" {
			continue
		}
		if val, found := metrics[rule.threshold]; found && val < rule.below {
			return rule.tip
		}
	}
	return rules[len(rules)-1].tip
}
