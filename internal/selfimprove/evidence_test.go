package selfimprove

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validEvidence() Evidence {
	return Evidence{
		ID:           "ev-001",
		TenantID:     "tenant-a",
		ProducerID:   "producer-agent",
		ReviewerID:   "reviewer-agent",
		Decision:     DecisionPromote,
		Claim:        "resource guard alert reporting improved sprint closeout observability",
		ArtifactRefs: []string{"docs/operations/v8-p08-oom-observability-qa.md"},
		RewardValue:  0.75,
		ObservedAt:   time.Date(2026, 5, 13, 2, 0, 0, 0, time.UTC),
		Agentrace: AgentraceSummary{
			ToolCalls:     12,
			Bottlenecks:   1,
			ErrorCount:    0,
			ParallelRatio: 0.8,
		},
	}
}

func TestValidateEvidenceRequiresProducerReviewerAndArtifacts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*Evidence)
	}{
		{name: "missing producer", edit: func(e *Evidence) { e.ProducerID = "" }},
		{name: "missing reviewer", edit: func(e *Evidence) { e.ReviewerID = "" }},
		{name: "same producer reviewer", edit: func(e *Evidence) { e.ReviewerID = e.ProducerID }},
		{name: "missing artifacts", edit: func(e *Evidence) { e.ArtifactRefs = nil }},
		{name: "reward below range", edit: func(e *Evidence) { e.RewardValue = -1.1 }},
		{name: "reward above range", edit: func(e *Evidence) { e.RewardValue = 1.1 }},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ev := validEvidence()
			tt.edit(&ev)
			if err := ValidateEvidence(ev); !errors.Is(err, ErrInvalidEvidence) {
				t.Fatalf("ValidateEvidence err=%v want ErrInvalidEvidence", err)
			}
		})
	}

	if err := ValidateEvidence(validEvidence()); err != nil {
		t.Fatalf("ValidateEvidence valid evidence: %v", err)
	}
}

func TestBuildReportSummarisesProducerReviewerEvidence(t *testing.T) {
	t.Parallel()
	promoted := validEvidence()
	rejected := validEvidence()
	rejected.ID = "ev-002"
	rejected.Decision = DecisionReject
	rejected.RewardValue = -0.25
	rejected.Claim = "unverified optimisation should not be promoted"
	rejected.ArtifactRefs = []string{"reports/research/rejected-claim.md"}

	report, err := BuildReport(ReportInput{
		GeneratedAt: time.Date(2026, 5, 13, 2, 30, 0, 0, time.UTC),
		Sprint:      "ec-v8-p09-self-improvement",
		Evidence:    []Evidence{promoted, rejected},
	})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}

	for _, want := range []string{
		"kind: self_improvement_report",
		"sprint: ec-v8-p09-self-improvement",
		"- reviewed evidence: 2",
		"- promoted: 1",
		"- rejected: 1",
		"- reward mean: 0.250",
		"- agenttrace tool calls: 12",
		"producer-agent -> reviewer-agent",
		"docs/operations/v8-p08-oom-observability-qa.md",
		"unverified optimisation should not be promoted",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

func TestRewardArtifactsOnlyPromoteEvidenceBackedDecisions(t *testing.T) {
	t.Parallel()
	promoted := validEvidence()
	rework := validEvidence()
	rework.ID = "ev-003"
	rework.Decision = DecisionRework
	rework.RewardValue = 0.1

	artifacts, err := RewardArtifacts([]Evidence{promoted, rework})
	if err != nil {
		t.Fatalf("RewardArtifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("RewardArtifacts len=%d want 1", len(artifacts))
	}
	got := artifacts[0]
	if got.EvidenceID != promoted.ID {
		t.Fatalf("EvidenceID=%q want %q", got.EvidenceID, promoted.ID)
	}
	if len(got.ArtifactRefs) != 1 || got.ArtifactRefs[0] != promoted.ArtifactRefs[0] {
		t.Fatalf("ArtifactRefs=%v want %v", got.ArtifactRefs, promoted.ArtifactRefs)
	}
	if got.Signal.ActionID != promoted.ID {
		t.Fatalf("ActionID=%q want %q", got.Signal.ActionID, promoted.ID)
	}
	if got.Signal.TenantID != promoted.TenantID {
		t.Fatalf("TenantID=%q want %q", got.Signal.TenantID, promoted.TenantID)
	}
	if got.Signal.RewardValue != promoted.RewardValue {
		t.Fatalf("RewardValue=%f want %f", got.Signal.RewardValue, promoted.RewardValue)
	}
	if got.Signal.PolicyName != "autoresearch-producer-reviewer" {
		t.Fatalf("PolicyName=%q", got.Signal.PolicyName)
	}
}

func TestEvoMapKPIsSummariseEvidence(t *testing.T) {
	t.Parallel()
	promoted := validEvidence()
	rejected := validEvidence()
	rejected.ID = "ev-004"
	rejected.Decision = DecisionReject
	rejected.RewardValue = -0.25
	rejected.Agentrace.ToolCalls = 3
	rework := validEvidence()
	rework.ID = "ev-005"
	rework.Decision = DecisionRework
	rework.RewardValue = 0
	rework.Agentrace.ToolCalls = 2

	got, err := EvoMapKPIs([]Evidence{promoted, rejected, rework})
	if err != nil {
		t.Fatalf("EvoMapKPIs: %v", err)
	}
	if got.SelfImprovementEvidenceTotal != 3 {
		t.Fatalf("SelfImprovementEvidenceTotal=%d want 3", got.SelfImprovementEvidenceTotal)
	}
	if got.SelfImprovementPromotedTotal != 1 || got.SelfImprovementRejectedTotal != 1 || got.SelfImprovementReworkTotal != 1 {
		t.Fatalf("decision counters = %d/%d/%d want 1/1/1", got.SelfImprovementPromotedTotal, got.SelfImprovementRejectedTotal, got.SelfImprovementReworkTotal)
	}
	if got.SelfImprovementRewardMean != (0.75-0.25+0)/3 {
		t.Fatalf("SelfImprovementRewardMean=%f", got.SelfImprovementRewardMean)
	}
	if got.AgentraceEvidenceTotal != 17 {
		t.Fatalf("AgentraceEvidenceTotal=%d want 17", got.AgentraceEvidenceTotal)
	}
}
