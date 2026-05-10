package coaching

import (
	"context"
	"errors"
	"testing"
	"time"
)

type inMemoryCoachingRepo struct {
	sessions []CoachingSession
	accepted map[string]bool
}

func newInMemoryCoachingRepo() *inMemoryCoachingRepo {
	return &inMemoryCoachingRepo{accepted: make(map[string]bool)}
}

func (r *inMemoryCoachingRepo) SaveSession(_ context.Context, s CoachingSession) error {
	r.sessions = append(r.sessions, s)
	return nil
}

func (r *inMemoryCoachingRepo) AcceptTip(_ context.Context, _, sessionID string) error {
	r.accepted[sessionID] = true
	return nil
}

func (r *inMemoryCoachingRepo) ListSessions(_ context.Context, _, _ string, limit int) ([]CoachingSession, error) {
	if limit > len(r.sessions) {
		limit = len(r.sessions)
	}
	return r.sessions[:limit], nil
}

type fakeLLMProvider struct {
	tip        string
	confidence float64
	err        error
}

func (f *fakeLLMProvider) GenerateCoachingTip(_ context.Context, _ CoachingContext, _ map[string]float64) (string, float64, error) {
	if f.err != nil {
		return "", 0, f.err
	}
	return f.tip, f.confidence, nil
}

type recordingPublisher struct {
	events []publishedEvent
}

type publishedEvent struct {
	TenantID   string
	OperatorID string
	Context    CoachingContext
	Source     TipSource
}

func (p *recordingPublisher) PublishTipDelivered(_ context.Context, tenantID, operatorID string, ctx CoachingContext, source TipSource) error {
	p.events = append(p.events, publishedEvent{
		TenantID:   tenantID,
		OperatorID: operatorID,
		Context:    ctx,
		Source:     source,
	})
	return nil
}

type recordingCoachingMetrics struct {
	tips []coachingMetricRecord
}

type coachingMetricRecord struct {
	TenantID string
	Context  CoachingContext
	Source   TipSource
}

func (m *recordingCoachingMetrics) RecordCoachingTip(tenantID string, ctx CoachingContext, source TipSource) {
	m.tips = append(m.tips, coachingMetricRecord{
		TenantID: tenantID,
		Context:  ctx,
		Source:   source,
	})
}

func newCoachingHarness(t *testing.T, llm LLMProvider) (*CoachingEngine, *inMemoryCoachingRepo, *recordingPublisher, *recordingCoachingMetrics) {
	t.Helper()
	repo := newInMemoryCoachingRepo()
	pub := &recordingPublisher{}
	met := &recordingCoachingMetrics{}
	seq := 0
	engine, err := NewCoachingEngine(nil, EngineConfig{
		Repository: repo,
		LLM:        llm,
		Publisher:  pub,
		Metrics:    met,
		IDFunc:     func() string { seq++; return "cs-" + string(rune('0'+seq)) },
		Now:        func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewCoachingEngine: %v", err)
	}
	return engine, repo, pub, met
}

func TestCoachingEngine_LLMTipGeneration(t *testing.T) {
	t.Parallel()
	llm := &fakeLLMProvider{tip: "Consider A/B testing your pricing for the top 3 SKUs.", confidence: 0.92}
	engine, repo, pub, met := newCoachingHarness(t, llm)

	resp, err := engine.GenerateTip(context.Background(), TipRequest{
		TenantID:       "t1",
		OperatorID:     "op1",
		Context:        ContextPricingStrategy,
		CurrentMetrics: map[string]float64{"margin_pct": 0.25},
	})
	if err != nil {
		t.Fatalf("GenerateTip: %v", err)
	}
	if resp.Source != SourceLLM {
		t.Fatalf("source = %s, want llm", resp.Source)
	}
	if resp.Confidence != 0.92 {
		t.Fatalf("confidence = %f, want 0.92", resp.Confidence)
	}
	if len(repo.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(repo.sessions))
	}
	if len(pub.events) != 1 {
		t.Fatalf("published events = %d, want 1", len(pub.events))
	}
	if len(met.tips) != 1 || met.tips[0].Source != SourceLLM {
		t.Fatalf("metrics source = %v, want llm", met.tips)
	}
}

func TestCoachingEngine_RuleFallbackOnLLMFailure(t *testing.T) {
	t.Parallel()
	llm := &fakeLLMProvider{err: errors.New("llm unavailable")}
	engine, repo, _, _ := newCoachingHarness(t, llm)

	resp, err := engine.GenerateTip(context.Background(), TipRequest{
		TenantID:       "t1",
		OperatorID:     "op1",
		Context:        ContextOnboarding,
		CurrentMetrics: map[string]float64{"conversion_rate": 0.01},
	})
	if err != nil {
		t.Fatalf("GenerateTip: %v", err)
	}
	if resp.Source != SourceRule {
		t.Fatalf("source = %s, want rule", resp.Source)
	}
	if resp.Confidence != 0.6 {
		t.Fatalf("confidence = %f, want 0.6", resp.Confidence)
	}
	if resp.Tip == "" {
		t.Fatal("expected non-empty rule-based tip")
	}
	if len(repo.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1 (history persisted on fallback too)", len(repo.sessions))
	}
}

func TestCoachingEngine_HistoryPersisted(t *testing.T) {
	t.Parallel()
	llm := &fakeLLMProvider{tip: "Test tip.", confidence: 0.80}
	engine, repo, _, _ := newCoachingHarness(t, llm)

	_, _ = engine.GenerateTip(context.Background(), TipRequest{
		TenantID:   "t1",
		OperatorID: "op1",
		Context:    ContextChannelOptimize,
	})
	_, _ = engine.GenerateTip(context.Background(), TipRequest{
		TenantID:   "t1",
		OperatorID: "op1",
		Context:    ContextInventoryMgmt,
	})

	if len(repo.sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(repo.sessions))
	}
	if repo.sessions[0].Context != ContextChannelOptimize {
		t.Fatalf("session[0].context = %s, want channel_optimization", repo.sessions[0].Context)
	}
	if repo.sessions[1].Context != ContextInventoryMgmt {
		t.Fatalf("session[1].context = %s, want inventory_management", repo.sessions[1].Context)
	}
}

func TestCoachingEngine_ContextSpecificRouting(t *testing.T) {
	t.Parallel()
	llm := &fakeLLMProvider{err: errors.New("llm down")}
	engine, _, _, _ := newCoachingHarness(t, llm)

	resp, err := engine.GenerateTip(context.Background(), TipRequest{
		TenantID:       "t1",
		OperatorID:     "op1",
		Context:        ContextPricingStrategy,
		CurrentMetrics: map[string]float64{"margin_pct": 0.10},
	})
	if err != nil {
		t.Fatalf("GenerateTip: %v", err)
	}
	if resp.Context != ContextPricingStrategy {
		t.Fatalf("context = %s, want pricing_strategy", resp.Context)
	}
	if resp.Tip == "" {
		t.Fatal("expected pricing-specific tip for low margin")
	}
}

func TestCoachingEngine_OperatorAcceptanceTracking(t *testing.T) {
	t.Parallel()
	llm := &fakeLLMProvider{tip: "Great tip.", confidence: 0.85}
	engine, repo, _, _ := newCoachingHarness(t, llm)

	_, _ = engine.GenerateTip(context.Background(), TipRequest{
		TenantID:   "t1",
		OperatorID: "op1",
		Context:    ContextOnboarding,
	})

	sessionID := repo.sessions[0].SessionID
	if err := engine.AcceptTip(context.Background(), "t1", sessionID); err != nil {
		t.Fatalf("AcceptTip: %v", err)
	}
	if !repo.accepted[sessionID] {
		t.Fatal("expected session marked as accepted")
	}
}
