//go:build v481_smoke

package v481

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/agent/coaching"
)

type stubCoachingRepo struct {
	sessions []coaching.CoachingSession
	accepted map[string]bool
}

func newStubCoachingRepo() *stubCoachingRepo {
	return &stubCoachingRepo{accepted: make(map[string]bool)}
}

func (r *stubCoachingRepo) SaveSession(_ context.Context, s coaching.CoachingSession) error {
	r.sessions = append(r.sessions, s)
	return nil
}

func (r *stubCoachingRepo) AcceptTip(_ context.Context, _, sessionID string) error {
	r.accepted[sessionID] = true
	return nil
}

func (r *stubCoachingRepo) ListSessions(_ context.Context, _, _ string, limit int) ([]coaching.CoachingSession, error) {
	if limit > len(r.sessions) {
		limit = len(r.sessions)
	}
	return r.sessions[:limit], nil
}

type stubLLM struct {
	tip string
	err error
}

func (s *stubLLM) GenerateCoachingTip(_ context.Context, _ coaching.CoachingContext, _ map[string]float64) (string, float64, error) {
	if s.err != nil {
		return "", 0, s.err
	}
	return s.tip, 0.9, nil
}

// Scenario: Coaching tip request → LLM/rule response → history → acceptance
func TestCoachingFlow_E2E(t *testing.T) {
	t.Parallel()
	repo := newStubCoachingRepo()
	seq := 0
	engine, err := coaching.NewCoachingEngine(nil, coaching.EngineConfig{
		Repository: repo,
		LLM:        &stubLLM{tip: "Try adjusting your TikTok product titles for better conversion."},
		IDFunc:     func() string { seq++; return "cs-e2e-" + string(rune('0'+seq)) },
		Now:        func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewCoachingEngine: %v", err)
	}

	resp, err := engine.GenerateTip(context.Background(), coaching.TipRequest{
		TenantID:       "t1",
		OperatorID:     "op1",
		Context:        coaching.ContextChannelOptimize,
		CurrentMetrics: map[string]float64{"channel_error_rate": 0.05},
	})
	if err != nil {
		t.Fatalf("GenerateTip: %v", err)
	}
	if resp.Source != coaching.SourceLLM {
		t.Fatalf("source = %s, want llm", resp.Source)
	}
	if len(repo.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(repo.sessions))
	}

	sessionID := repo.sessions[0].SessionID
	if err := engine.AcceptTip(context.Background(), "t1", sessionID); err != nil {
		t.Fatalf("AcceptTip: %v", err)
	}
	if !repo.accepted[sessionID] {
		t.Fatal("expected session accepted")
	}
}

// Scenario: Context routing - pricing context → pricing tips
func TestCoachingFlow_ContextRouting(t *testing.T) {
	t.Parallel()
	repo := newStubCoachingRepo()
	seq := 0
	engine, _ := coaching.NewCoachingEngine(nil, coaching.EngineConfig{
		Repository: repo,
		LLM:        &stubLLM{err: errors.New("llm unavailable")},
		IDFunc:     func() string { seq++; return "cs-route-" + string(rune('0'+seq)) },
		Now:        func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	})

	resp, err := engine.GenerateTip(context.Background(), coaching.TipRequest{
		TenantID:       "t1",
		OperatorID:     "op1",
		Context:        coaching.ContextPricingStrategy,
		CurrentMetrics: map[string]float64{"margin_pct": 0.10},
	})
	if err != nil {
		t.Fatalf("GenerateTip: %v", err)
	}
	if resp.Context != coaching.ContextPricingStrategy {
		t.Fatalf("context = %s, want pricing_strategy", resp.Context)
	}
	if resp.Source != coaching.SourceRule {
		t.Fatalf("source = %s, want rule (fallback)", resp.Source)
	}
}
