package payment_test

import (
	"context"
	"errors"
	"testing"
	"time"

	paymentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/payment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLLM struct {
	result paymentagent.AssessmentResult
	err    error
}

func (m *mockLLM) AssessRisk(_ context.Context, _ paymentagent.AssessmentInput) (paymentagent.AssessmentResult, error) {
	return m.result, m.err
}

type mockApprovalGate struct {
	approved bool
	err      error
}

func (m *mockApprovalGate) RequestApproval(_ context.Context, _, _ string, _ paymentagent.AssessmentResult) (bool, error) {
	return m.approved, m.err
}

type spyAdvisorMetrics struct {
	assessments []string
	durations   []float64
}

func (s *spyAdvisorMetrics) IncAssessment(tenantID string, level paymentagent.RiskLevel) {
	s.assessments = append(s.assessments, tenantID+":"+string(level))
}

func (s *spyAdvisorMetrics) ObserveDuration(sec float64) {
	s.durations = append(s.durations, sec)
}

func fixedNow() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) }

func TestAdvisor_LowRiskApprove(t *testing.T) {
	t.Parallel()
	advisor := paymentagent.NewAdvisor(paymentagent.AdvisorConfig{
		LLM: &mockLLM{result: paymentagent.AssessmentResult{
			RiskScore: 15, RiskLevel: paymentagent.RiskLevelLow,
			Recommendation: paymentagent.RecommendApprove, Reason: "trusted customer",
		}},
		Now: fixedNow,
	})
	result, err := advisor.Assess(context.Background(), paymentagent.AssessmentInput{
		TenantID: "t1", OrderID: "o1", AmountCents: 60000, Currency: "AUD",
		CustomerID: "c1", IsNewCustomer: false, RecentRefunds: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, paymentagent.RiskLevelLow, result.RiskLevel)
	assert.Equal(t, paymentagent.RecommendApprove, result.Recommendation)
}

func TestAdvisor_MediumRiskPass(t *testing.T) {
	t.Parallel()
	advisor := paymentagent.NewAdvisor(paymentagent.AdvisorConfig{
		LLM: &mockLLM{result: paymentagent.AssessmentResult{
			RiskScore: 50, RiskLevel: paymentagent.RiskLevelMedium,
			Recommendation: paymentagent.RecommendReview, Reason: "elevated signals",
		}},
		Now: fixedNow,
	})
	result, err := advisor.Assess(context.Background(), paymentagent.AssessmentInput{
		TenantID: "t1", OrderID: "o2", AmountCents: 60000, Currency: "AUD",
		CustomerID: "c2", IsNewCustomer: true, RecentRefunds: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, paymentagent.RiskLevelMedium, result.RiskLevel)
	assert.Equal(t, paymentagent.RecommendReview, result.Recommendation)
}

func TestAdvisor_HighRiskBlocked(t *testing.T) {
	t.Parallel()
	advisor := paymentagent.NewAdvisor(paymentagent.AdvisorConfig{
		LLM: &mockLLM{result: paymentagent.AssessmentResult{
			RiskScore: 85, RiskLevel: paymentagent.RiskLevelHigh,
			Recommendation: paymentagent.RecommendBlock, Reason: "suspicious pattern",
		}},
		ApprovalGate: &mockApprovalGate{approved: false},
		Now:          fixedNow,
	})
	_, err := advisor.Assess(context.Background(), paymentagent.AssessmentInput{
		TenantID: "t1", OrderID: "o3", AmountCents: 100000, Currency: "AUD",
		CustomerID: "c3", IsNewCustomer: true, RecentRefunds: 5,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, paymentagent.ErrHighRiskBlocked)
}

func TestAdvisor_LLMFailoverToRules(t *testing.T) {
	t.Parallel()
	metrics := &spyAdvisorMetrics{}
	advisor := paymentagent.NewAdvisor(paymentagent.AdvisorConfig{
		LLM:     &mockLLM{err: errors.New("llm unavailable")},
		Now:     fixedNow,
		Metrics: metrics,
	})
	result, err := advisor.Assess(context.Background(), paymentagent.AssessmentInput{
		TenantID: "t1", OrderID: "o4", AmountCents: 60000, Currency: "AUD",
		CustomerID: "c4", IsNewCustomer: true, RecentRefunds: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, "rules", result.Source)
	assert.Equal(t, paymentagent.RiskLevelMedium, result.RiskLevel)
	assert.Contains(t, result.Reason, "new customer + high value")
}

func TestAdvisor_OperatorApprovalGate(t *testing.T) {
	t.Parallel()
	advisor := paymentagent.NewAdvisor(paymentagent.AdvisorConfig{
		LLM: &mockLLM{result: paymentagent.AssessmentResult{
			RiskScore: 90, RiskLevel: paymentagent.RiskLevelHigh,
			Recommendation: paymentagent.RecommendBlock, Reason: "fraud signals",
		}},
		ApprovalGate: &mockApprovalGate{approved: true},
		Now:          fixedNow,
	})
	result, err := advisor.Assess(context.Background(), paymentagent.AssessmentInput{
		TenantID: "t1", OrderID: "o5", AmountCents: 200000, Currency: "AUD",
		CustomerID: "c5", IsNewCustomer: false, RecentRefunds: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, paymentagent.RecommendReview, result.Recommendation)
}

func TestAdvisor_RuleFallback_HighRefundsBlock(t *testing.T) {
	t.Parallel()
	advisor := paymentagent.NewAdvisor(paymentagent.AdvisorConfig{
		LLM: nil,
		Now: fixedNow,
	})
	result, err := advisor.Assess(context.Background(), paymentagent.AssessmentInput{
		TenantID: "t1", OrderID: "o6", AmountCents: 60000, Currency: "AUD",
		CustomerID: "c6", IsNewCustomer: true, RecentRefunds: 5, ShipCountry: "NG",
	})
	require.ErrorIs(t, err, paymentagent.ErrHighRiskBlocked)
	assert.Equal(t, paymentagent.RiskLevelHigh, result.RiskLevel)
}
