// File scope: v3.5.0 EC-6-3 dynamic pricing agent RED tests.
// TDD-first per the v3.5.0 plan (story 3; depends on EC-6-1 cost
// events + EC-6-2 fee calculator + the v3.2.0 LLM failover
// pattern).
//
// Acceptance per ADR-028 EC-6-3:
//   - Rule-first: margin floor guardrail blocks sub-floor suggestions.
//   - LLM-second: suggested price stays within guardrails.
//   - Operator approval gate fires for changes >15%.
//   - Failover to rule-only when LLM unavailable.
//   - Decisions emitted as PriceChange* events the channel router
//     and WC adapter will consume in subsequent sprints.
package pricing

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/billing"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// dynamicRecordingPublisher captures every Price* event the agent
// emits. Suffixed `dynamic` to avoid colliding with package-level
// helpers in agent.go.
type dynamicRecordingPublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (p *dynamicRecordingPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *dynamicRecordingPublisher) Close() error { return nil }

func (p *dynamicRecordingPublisher) snapshot() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.events))
	copy(out, p.events)
	return out
}

// dynamicFakeLLM is a minimal port.AITextGenerator double used to
// drive the LLM-second decision path.
type dynamicFakeLLM struct {
	response port.AICompletionResponse
	err      error
	mu       sync.Mutex
	calls    int
}

func (f *dynamicFakeLLM) Complete(_ context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return port.AICompletionResponse{}, f.err
	}
	return f.response, nil
}

// dynamicStaticFXProvider mirrors the billing test fixture.
type dynamicStaticFXProvider struct{ rate billing.FXRate }

func (s dynamicStaticFXProvider) LatestRate(_ context.Context) (billing.FXRate, error) {
	return s.rate, nil
}

func newDynamicPricingHarness(t *testing.T, llm *dynamicFakeLLM) (*PricingAgent, *dynamicRecordingPublisher) {
	t.Helper()
	now := time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC)
	calc, err := billing.NewPlatformFeeCalculator(billing.PlatformFeeCalculatorConfig{
		FXProvider: dynamicStaticFXProvider{rate: billing.FXRate{AUDPerCNY: 0.21, FetchedAt: now, Source: "test"}},
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewPlatformFeeCalculator: %v", err)
	}
	pub := &dynamicRecordingPublisher{}
	agent, err := NewPricingAgent(nil, PricingAgentConfig{
		TenantID:                "tenant-1",
		FeeCalculator:           calc,
		Publisher:               pub,
		LLM:                     llm,
		MarginFloorPct:          0.35,
		LargeChangeThresholdPct: 0.15,
		Now:                     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewPricingAgent: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close(context.Background()) })
	return agent, pub
}

func TestPricingAgent_ReducesPriceWhenCompetitorUndercuts(t *testing.T) {
	t.Parallel()
	llm := &dynamicFakeLLM{
		response: port.AICompletionResponse{
			Content:    `{"recommended_price_aud_cents": 5500, "rationale": "competitor at A$50; reduce by 8% for margin protection"}`,
			TokensUsed: 80,
		},
	}
	agent, pub := newDynamicPricingHarness(t, llm)
	res, err := agent.Decide(context.Background(), PriceDecisionInput{
		ProductID:               "prod-1",
		Channel:                 billing.ChannelWooCommerce,
		CurrentPriceAUDCents:    6000,
		CostCNYCents:            8000,
		ShippingEstAUDCents:     500,
		CompetitorPriceAUDCents: 5000,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if res.ProposedPriceAUDCents == 0 {
		t.Fatalf("ProposedPriceAUDCents = 0, want non-zero LLM suggestion")
	}
	if res.ProposedPriceAUDCents > 6000 {
		t.Fatalf("ProposedPriceAUDCents = %d > current 6000 -- want competitor reduction", res.ProposedPriceAUDCents)
	}
	if !res.Applied {
		t.Fatalf("Applied=false; expected sub-15%% change applied without approval")
	}
	if res.Source != PriceDecisionSourceLLM {
		t.Fatalf("Source = %s, want llm", res.Source)
	}
	if events := pub.snapshot(); len(events) != 1 || events[0].Type != eventbus.PriceChangeApplied {
		t.Fatalf("events = %+v, want one PriceChangeApplied", events)
	}
}

func TestPricingAgent_GuardrailBlocksSubFloorSuggestion(t *testing.T) {
	t.Parallel()
	// LLM tries to set price below the margin floor: cost ¥10000 *
	// 0.21 = A$21.00; floor 35% means price > 32.30 needed; LLM
	// suggests 30.00 (16% margin -- below floor).
	llm := &dynamicFakeLLM{
		response: port.AICompletionResponse{
			Content: `{"recommended_price_aud_cents": 3000, "rationale": "aggressive undercut"}`,
		},
	}
	agent, pub := newDynamicPricingHarness(t, llm)
	res, err := agent.Decide(context.Background(), PriceDecisionInput{
		ProductID:            "prod-2",
		Channel:              billing.ChannelWooCommerce,
		CurrentPriceAUDCents: 5000,
		CostCNYCents:         10000,
		ShippingEstAUDCents:  200,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !res.GuardrailBlocked {
		t.Fatalf("GuardrailBlocked=false, want true (sub-floor suggestion)")
	}
	if res.Applied {
		t.Fatalf("Applied=true, want false when guardrail blocks")
	}
	if res.RuleFloorPriceAUDCents == 0 {
		t.Fatalf("RuleFloorPriceAUDCents = 0; want computed floor")
	}
	if events := pub.snapshot(); len(events) != 0 {
		t.Fatalf("events = %d, want 0 (sub-floor blocked, no event)", len(events))
	}
}

func TestPricingAgent_LLMSuggestionWithinGuardrails(t *testing.T) {
	t.Parallel()
	llm := &dynamicFakeLLM{
		response: port.AICompletionResponse{
			Content: `{"recommended_price_aud_cents": 6500, "rationale": "competitor parity"}`,
		},
	}
	agent, pub := newDynamicPricingHarness(t, llm)
	res, err := agent.Decide(context.Background(), PriceDecisionInput{
		ProductID:            "prod-3",
		Channel:              billing.ChannelTikTok,
		CurrentPriceAUDCents: 6000,
		CostCNYCents:         8000,
		ShippingEstAUDCents:  300,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if res.ProposedPriceAUDCents != 6500 {
		t.Fatalf("ProposedPriceAUDCents = %d, want 6500", res.ProposedPriceAUDCents)
	}
	if !res.Applied {
		t.Fatalf("Applied=false, want true (within guardrails + sub-15%% delta)")
	}
	if events := pub.snapshot(); len(events) != 1 || events[0].Type != eventbus.PriceChangeApplied {
		t.Fatalf("events = %+v, want one PriceChangeApplied", events)
	}
}

func TestPricingAgent_ApprovalGateFiresForLargeChanges(t *testing.T) {
	t.Parallel()
	// LLM suggests 7500 from current 6000 -- a 25% increase (>15%).
	llm := &dynamicFakeLLM{
		response: port.AICompletionResponse{
			Content: `{"recommended_price_aud_cents": 7500, "rationale": "premium positioning"}`,
		},
	}
	agent, pub := newDynamicPricingHarness(t, llm)
	res, err := agent.Decide(context.Background(), PriceDecisionInput{
		ProductID:            "prod-4",
		Channel:              billing.ChannelFacebook,
		CurrentPriceAUDCents: 6000,
		CostCNYCents:         8000,
		ShippingEstAUDCents:  300,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !res.PendingApproval {
		t.Fatalf("PendingApproval=false, want true for >15%% change")
	}
	if res.Applied {
		t.Fatalf("Applied=true, want false (>15%% gate fires)")
	}
	events := pub.snapshot()
	if len(events) != 1 || events[0].Type != eventbus.PriceChangePendingApproval {
		t.Fatalf("events = %+v, want one PriceChangePendingApproval", events)
	}
}

func TestPricingAgent_FailoverToRuleOnlyWhenLLMUnavailable(t *testing.T) {
	t.Parallel()
	llm := &dynamicFakeLLM{err: errors.New("llm timeout")}
	agent, pub := newDynamicPricingHarness(t, llm)
	res, err := agent.Decide(context.Background(), PriceDecisionInput{
		ProductID:               "prod-5",
		Channel:                 billing.ChannelTikTok,
		CurrentPriceAUDCents:    6000,
		CostCNYCents:            8000,
		ShippingEstAUDCents:     300,
		CompetitorPriceAUDCents: 5800,
	})
	if err != nil {
		t.Fatalf("Decide failover: %v", err)
	}
	if res.Source != PriceDecisionSourceRule {
		t.Fatalf("Source = %s, want rule (LLM failover)", res.Source)
	}
	if res.ProposedPriceAUDCents == 0 {
		t.Fatalf("ProposedPriceAUDCents = 0; rule path must produce a price")
	}
	if !res.Applied {
		t.Fatalf("Applied=false; rule-only with sub-15%% delta should auto-apply")
	}
	if events := pub.snapshot(); len(events) != 1 {
		t.Fatalf("events = %d, want 1 (rule path still emits)", len(events))
	}
}

func TestPricingAgent_RuleFollowsCompetitorWhenAboveFloor(t *testing.T) {
	t.Parallel()
	llm := &dynamicFakeLLM{err: errors.New("llm down")}
	agent, _ := newDynamicPricingHarness(t, llm)
	res, err := agent.Decide(context.Background(), PriceDecisionInput{
		ProductID:               "prod-rule",
		Channel:                 billing.ChannelTikTok,
		CurrentPriceAUDCents:    6000,
		CostCNYCents:            8000,
		ShippingEstAUDCents:     200,
		CompetitorPriceAUDCents: 5500,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if res.ProposedPriceAUDCents > 6000 {
		t.Fatalf("ProposedPriceAUDCents = %d, want competitor-shadow reduction", res.ProposedPriceAUDCents)
	}
	if res.ProposedPriceAUDCents < res.RuleFloorPriceAUDCents {
		t.Fatalf("ProposedPriceAUDCents = %d below floor %d", res.ProposedPriceAUDCents, res.RuleFloorPriceAUDCents)
	}
}

func TestPricingAgent_RejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	agent, _ := newDynamicPricingHarness(t, &dynamicFakeLLM{})
	cases := []struct {
		name  string
		input PriceDecisionInput
	}{
		{name: "empty_product_id", input: PriceDecisionInput{Channel: billing.ChannelWooCommerce, CurrentPriceAUDCents: 1000, CostCNYCents: 100}},
		{name: "zero_current_price", input: PriceDecisionInput{ProductID: "p", Channel: billing.ChannelWooCommerce, CostCNYCents: 100}},
		{name: "negative_cost", input: PriceDecisionInput{ProductID: "p", Channel: billing.ChannelWooCommerce, CurrentPriceAUDCents: 1000, CostCNYCents: -1}},
		{name: "missing_channel", input: PriceDecisionInput{ProductID: "p", CurrentPriceAUDCents: 1000}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := agent.Decide(context.Background(), tc.input)
			if !errors.Is(err, ErrInvalidPriceDecisionInput) {
				t.Fatalf("%s: err=%v, want ErrInvalidPriceDecisionInput", tc.name, err)
			}
		})
	}
}

func TestPricingAgent_DecideAfterCloseRejects(t *testing.T) {
	t.Parallel()
	agent, _ := newDynamicPricingHarness(t, &dynamicFakeLLM{})
	if err := agent.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := agent.Decide(context.Background(), PriceDecisionInput{
		ProductID:            "p",
		Channel:              billing.ChannelWooCommerce,
		CurrentPriceAUDCents: 1000,
		CostCNYCents:         100,
	})
	if !errors.Is(err, ErrPricingAgentClosed) {
		t.Fatalf("Decide after Close: err=%v, want ErrPricingAgentClosed", err)
	}
}

func TestPricingAgent_RejectsMissingDeps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  PricingAgentConfig
	}{
		{name: "no_tenant", cfg: PricingAgentConfig{Publisher: &dynamicRecordingPublisher{}}},
		{name: "no_publisher", cfg: PricingAgentConfig{TenantID: "t"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewPricingAgent(nil, tc.cfg)
			if !errors.Is(err, ErrPricingAgentUnconfigured) {
				t.Fatalf("%s: err=%v, want ErrPricingAgentUnconfigured", tc.name, err)
			}
		})
	}
}

func TestPricingAgent_HandleSupplierCostChangedEvent(t *testing.T) {
	t.Parallel()
	llm := &dynamicFakeLLM{
		response: port.AICompletionResponse{
			Content: `{"recommended_price_aud_cents": 6300}`,
		},
	}
	agent, pub := newDynamicPricingHarness(t, llm)
	payload := eventbus.SupplierCostChangedPayload{
		Version:          eventbus.SupplierCostChangedPayloadVersion,
		TenantID:         "tenant-1",
		Source:           "1688",
		SupplierSKU:      "prod-evt",
		BaselineCNYCents: 1000,
		ObservedCNYCents: 1100,
		DeltaPct:         0.10,
		Direction:        "up",
		ThresholdPct:     0.05,
		ObservedAt:       time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC),
	}
	evt, err := eventbus.NewSupplierCostChangedEvent("test", time.Date(2026, 5, 10, 5, 0, 0, 0, time.UTC), payload)
	if err != nil {
		t.Fatalf("NewSupplierCostChangedEvent: %v", err)
	}
	if err := agent.RegisterProduct(ProductPricingState{
		ProductID:               "prod-evt",
		Channel:                 billing.ChannelWooCommerce,
		CurrentPriceAUDCents:    6000,
		CostCNYCents:            1000,
		ShippingEstAUDCents:     200,
		CompetitorPriceAUDCents: 0,
	}); err != nil {
		t.Fatalf("RegisterProduct: %v", err)
	}
	if err := agent.HandleSupplierCostChanged(context.Background(), evt); err != nil {
		t.Fatalf("HandleSupplierCostChanged: %v", err)
	}
	events := pub.snapshot()
	if len(events) == 0 {
		t.Fatalf("no events after HandleSupplierCostChanged")
	}
}
