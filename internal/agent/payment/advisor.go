package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// v4.3.0 typed sentinel errors.
var (
	ErrHighRiskBlocked    = errors.New("advisor: high-risk payment blocked")
	ErrAdvisorUnavailable = errors.New("advisor: service unavailable")
)

// RiskLevel classifies the risk assessment outcome.
type RiskLevel string

const (
	RiskLevelLow    RiskLevel = "low"
	RiskLevelMedium RiskLevel = "medium"
	RiskLevelHigh   RiskLevel = "high"
)

// Recommendation is the advisor's action guidance.
type Recommendation string

const (
	RecommendApprove Recommendation = "approve"
	RecommendReview  Recommendation = "review"
	RecommendBlock   Recommendation = "block"
)

// AssessmentInput is the input for risk assessment.
type AssessmentInput struct {
	TenantID      string `json:"tenant_id"`
	OrderID       string `json:"order_id"`
	AmountCents   int64  `json:"amount_cents"`
	Currency      string `json:"currency"`
	PaymentMethod string `json:"payment_method"`
	CustomerID    string `json:"customer_id"`
	IsNewCustomer bool   `json:"is_new_customer"`
	RecentRefunds int    `json:"recent_refunds"`
	ShipCountry   string `json:"ship_country"`
}

// AssessmentResult is the advisor's output.
type AssessmentResult struct {
	RiskScore      int            `json:"risk_score"`
	RiskLevel      RiskLevel      `json:"risk_level"`
	Recommendation Recommendation `json:"recommendation"`
	Reason         string         `json:"reason"`
	Source         string         `json:"source"`
	AssessedAt     time.Time      `json:"assessed_at"`
}

const highValueThresholdCents = 50000

// LLMClient abstracts the LLM call for risk assessment.
type LLMClient interface {
	AssessRisk(ctx context.Context, input AssessmentInput) (AssessmentResult, error)
}

// OperatorApprovalGate checks whether an operator has approved a
// high-risk payment. Returns true if approved.
type OperatorApprovalGate interface {
	RequestApproval(ctx context.Context, tenantID, orderID string, result AssessmentResult) (bool, error)
}

// AdvisorConfig wires the advisor.
type AdvisorConfig struct {
	LLM          LLMClient
	ApprovalGate OperatorApprovalGate
	Now          func() time.Time
	Metrics      AdvisorMetrics
}

// AdvisorMetrics is the Prometheus instrumentation port.
type AdvisorMetrics interface {
	IncAssessment(tenantID string, riskLevel RiskLevel)
	ObserveDuration(seconds float64)
}

// Advisor is the v4.3.0 AI payment risk assessor. Uses IronClaw
// LLM-first with rule-based fallback per the v3.2.0 failover
// pattern.
type Advisor struct {
	llm          LLMClient
	approvalGate OperatorApprovalGate
	now          func() time.Time
	metrics      AdvisorMetrics
}

// NewAdvisor constructs the advisor.
func NewAdvisor(cfg AdvisorConfig) *Advisor {
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Advisor{
		llm:          cfg.LLM,
		approvalGate: cfg.ApprovalGate,
		now:          cfg.Now,
		metrics:      cfg.Metrics,
	}
}

// Assess runs a risk assessment. Only triggered for high-value
// charges (>A$500 = 50000 cents).
func (a *Advisor) Assess(ctx context.Context, input AssessmentInput) (AssessmentResult, error) {
	start := a.now()
	signals := gatherSignals(input)
	result, err := a.runLLM(ctx, input)
	if err != nil {
		result = runRuleFallback(signals, a.now())
	}
	a.recordMetrics(input.TenantID, result, start)
	return routeByRisk(ctx, result, input, a.approvalGate)
}

type riskSignals struct {
	isHighValue   bool
	isNewCustomer bool
	highRefunds   bool
	riskyCountry  bool
}

func gatherSignals(input AssessmentInput) riskSignals {
	return riskSignals{
		isHighValue:   input.AmountCents > highValueThresholdCents,
		isNewCustomer: input.IsNewCustomer,
		highRefunds:   input.RecentRefunds > 3,
		riskyCountry:  isHighRiskCountry(input.ShipCountry),
	}
}

func (a *Advisor) runLLM(ctx context.Context, input AssessmentInput) (AssessmentResult, error) {
	if a.llm == nil {
		return AssessmentResult{}, ErrAdvisorUnavailable
	}
	result, err := a.llm.AssessRisk(ctx, input)
	if err != nil {
		return AssessmentResult{}, fmt.Errorf("%w: %v", ErrAdvisorUnavailable, err)
	}
	result.Source = "llm"
	result.AssessedAt = a.now()
	return result, nil
}

func runRuleFallback(signals riskSignals, now time.Time) AssessmentResult {
	score := 20
	var reasons []string

	if signals.highRefunds {
		score += 50
		reasons = append(reasons, ">3 recent refunds")
	}
	if signals.isNewCustomer && signals.isHighValue {
		score += 30
		reasons = append(reasons, "new customer + high value")
	}
	if signals.riskyCountry {
		score += 15
		reasons = append(reasons, "high-risk shipping country")
	}
	if score > 100 {
		score = 100
	}
	level, rec := classifyScore(score)
	reason := "rule-based assessment"
	if len(reasons) > 0 {
		reason = strings.Join(reasons, "; ")
	}
	return AssessmentResult{
		RiskScore:      score,
		RiskLevel:      level,
		Recommendation: rec,
		Reason:         reason,
		Source:         "rules",
		AssessedAt:     now,
	}
}

func classifyScore(score int) (RiskLevel, Recommendation) {
	switch {
	case score >= 70:
		return RiskLevelHigh, RecommendBlock
	case score >= 40:
		return RiskLevelMedium, RecommendReview
	default:
		return RiskLevelLow, RecommendApprove
	}
}

func routeByRisk(ctx context.Context, result AssessmentResult, input AssessmentInput, gate OperatorApprovalGate) (AssessmentResult, error) {
	if result.RiskLevel != RiskLevelHigh {
		return result, nil
	}
	if gate == nil {
		return result, ErrHighRiskBlocked
	}
	approved, err := gate.RequestApproval(ctx, input.TenantID, input.OrderID, result)
	if err != nil {
		return result, fmt.Errorf("%w: approval gate: %v", ErrHighRiskBlocked, err)
	}
	if !approved {
		return result, ErrHighRiskBlocked
	}
	result.Recommendation = RecommendReview
	return result, nil
}

func (a *Advisor) recordMetrics(tenantID string, result AssessmentResult, start time.Time) {
	if a.metrics == nil {
		return
	}
	a.metrics.IncAssessment(tenantID, result.RiskLevel)
	a.metrics.ObserveDuration(a.now().Sub(start).Seconds())
}

var highRiskCountries = map[string]bool{
	"NG": true, "GH": true, "PK": true,
}

func isHighRiskCountry(code string) bool {
	return highRiskCountries[strings.ToUpper(code)]
}
