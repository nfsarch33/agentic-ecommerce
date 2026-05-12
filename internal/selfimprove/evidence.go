// Package selfimprove builds replayable evidence and reward artifacts for
// autoresearch producer-reviewer loops.
package selfimprove

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/coord"
	"github.com/nfsarch33/agentic-ecommerce/internal/evomap"
)

var ErrInvalidEvidence = errors.New("selfimprove: invalid evidence")

type Decision string

const (
	DecisionPromote Decision = "promote"
	DecisionReject  Decision = "reject"
	DecisionRework  Decision = "rework"
)

type AgentraceSummary struct {
	ToolCalls     int
	Bottlenecks   int
	ErrorCount    int
	ParallelRatio float64
}

type Evidence struct {
	ID           string
	TenantID     string
	ProducerID   string
	ReviewerID   string
	Decision     Decision
	Claim        string
	ArtifactRefs []string
	RewardValue  float64
	ObservedAt   time.Time
	Agentrace    AgentraceSummary
}

type ReportInput struct {
	GeneratedAt time.Time
	Sprint      string
	Evidence    []Evidence
}

type RewardArtifact struct {
	EvidenceID   string             `json:"evidence_id"`
	ArtifactRefs []string           `json:"artifact_refs"`
	Claim        string             `json:"claim"`
	Signal       coord.RewardSignal `json:"signal"`
}

func ValidateEvidence(e Evidence) error {
	switch {
	case strings.TrimSpace(e.ProducerID) == "":
		return fmt.Errorf("%w: producer required", ErrInvalidEvidence)
	case strings.TrimSpace(e.ReviewerID) == "":
		return fmt.Errorf("%w: reviewer required", ErrInvalidEvidence)
	case e.ProducerID == e.ReviewerID:
		return fmt.Errorf("%w: producer and reviewer must differ", ErrInvalidEvidence)
	case len(e.ArtifactRefs) == 0:
		return fmt.Errorf("%w: artifact refs required", ErrInvalidEvidence)
	case e.RewardValue < -1 || e.RewardValue > 1:
		return fmt.Errorf("%w: reward value out of range", ErrInvalidEvidence)
	}
	if !validDecision(e.Decision) {
		return fmt.Errorf("%w: unknown decision", ErrInvalidEvidence)
	}
	return nil
}

func BuildReport(input ReportInput) (string, error) {
	if input.GeneratedAt.IsZero() {
		input.GeneratedAt = time.Now().UTC()
	}
	var stats reportStats
	for _, ev := range input.Evidence {
		if err := ValidateEvidence(ev); err != nil {
			return "", err
		}
		stats.add(ev)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "---\n")
	fmt.Fprintf(&sb, "kind: self_improvement_report\n")
	fmt.Fprintf(&sb, "sprint: %s\n", input.Sprint)
	fmt.Fprintf(&sb, "generated_at: %s\n", input.GeneratedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&sb, "evidence_count: %d\n", len(input.Evidence))
	fmt.Fprintf(&sb, "---\n\n")
	fmt.Fprintf(&sb, "# Self-Improvement Report -- %s\n\n", input.Sprint)
	fmt.Fprintf(&sb, "## Summary\n\n")
	fmt.Fprintf(&sb, "- reviewed evidence: %d\n", len(input.Evidence))
	fmt.Fprintf(&sb, "- promoted: %d\n", stats.promoted)
	fmt.Fprintf(&sb, "- rejected: %d\n", stats.rejected)
	fmt.Fprintf(&sb, "- rework: %d\n", stats.rework)
	fmt.Fprintf(&sb, "- reward mean: %.3f\n\n", stats.meanReward())
	fmt.Fprintf(&sb, "## Evidence\n\n")
	for _, ev := range input.Evidence {
		fmt.Fprintf(&sb, "### %s\n\n", ev.ID)
		fmt.Fprintf(&sb, "- producer-reviewer: %s -> %s\n", ev.ProducerID, ev.ReviewerID)
		fmt.Fprintf(&sb, "- decision: %s\n", ev.Decision)
		fmt.Fprintf(&sb, "- reward: %.3f\n", ev.RewardValue)
		fmt.Fprintf(&sb, "- claim: %s\n", ev.Claim)
		fmt.Fprintf(&sb, "- agenttrace tool calls: %d\n", ev.Agentrace.ToolCalls)
		fmt.Fprintf(&sb, "- agenttrace bottlenecks: %d\n", ev.Agentrace.Bottlenecks)
		fmt.Fprintf(&sb, "- agenttrace errors: %d\n", ev.Agentrace.ErrorCount)
		fmt.Fprintf(&sb, "- agenttrace parallelism: %.3f\n", ev.Agentrace.ParallelRatio)
		fmt.Fprintf(&sb, "- artifacts:\n")
		for _, ref := range ev.ArtifactRefs {
			fmt.Fprintf(&sb, "  - %s\n", ref)
		}
		fmt.Fprintf(&sb, "\n")
	}
	return sb.String(), nil
}

func RewardArtifacts(evidence []Evidence) ([]RewardArtifact, error) {
	var out []RewardArtifact
	for _, ev := range evidence {
		if err := ValidateEvidence(ev); err != nil {
			return nil, err
		}
		if ev.Decision != DecisionPromote {
			continue
		}
		out = append(out, RewardArtifact{
			EvidenceID:   ev.ID,
			ArtifactRefs: append([]string(nil), ev.ArtifactRefs...),
			Claim:        ev.Claim,
			Signal: coord.RewardSignal{
				AgentID:     ev.ReviewerID,
				ActionID:    ev.ID,
				TenantID:    ev.TenantID,
				Outcome:     coord.RewardOutcomeSuccess,
				RewardValue: ev.RewardValue,
				PolicyName:  "autoresearch-producer-reviewer",
				Timestamp:   ev.ObservedAt,
			},
		})
	}
	return out, nil
}

func EvoMapKPIs(evidence []Evidence) (evomap.KPIs, error) {
	var stats reportStats
	var agentraceInputs int
	for _, ev := range evidence {
		if err := ValidateEvidence(ev); err != nil {
			return evomap.KPIs{}, err
		}
		stats.add(ev)
		agentraceInputs += ev.Agentrace.ToolCalls
	}
	return evomap.KPIs{
		SelfImprovementEvidenceTotal: len(evidence),
		SelfImprovementPromotedTotal: stats.promoted,
		SelfImprovementRejectedTotal: stats.rejected,
		SelfImprovementReworkTotal:   stats.rework,
		SelfImprovementRewardMean:    stats.meanReward(),
		AgentraceEvidenceTotal:       agentraceInputs,
	}, nil
}

type reportStats struct {
	promoted int
	rejected int
	rework   int
	reward   float64
	samples  int
}

func (s *reportStats) add(ev Evidence) {
	switch ev.Decision {
	case DecisionPromote:
		s.promoted++
	case DecisionReject:
		s.rejected++
	case DecisionRework:
		s.rework++
	}
	s.reward += ev.RewardValue
	s.samples++
}

func (s reportStats) meanReward() float64 {
	if s.samples == 0 {
		return 0
	}
	return s.reward / float64(s.samples)
}

func validDecision(decision Decision) bool {
	switch decision {
	case DecisionPromote, DecisionReject, DecisionRework:
		return true
	default:
		return false
	}
}
