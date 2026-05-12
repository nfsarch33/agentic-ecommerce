package selfimprove

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func replayEvidence(decision Decision, artifacts []string) Evidence {
	return Evidence{
		ID:           "ev-replay-1",
		TenantID:     "tenant-a",
		ProducerID:   "producer-agent",
		ReviewerID:   "reviewer-agent",
		Decision:     decision,
		Claim:        "replayed Agenttrace evidence improved reward promotion quality",
		ArtifactRefs: artifacts,
		RewardValue:  0.6,
		ObservedAt:   time.Date(2026, 5, 13, 3, 0, 0, 0, time.UTC),
		Agentrace:    AgentraceSummary{ToolCalls: 5, Bottlenecks: 1, ParallelRatio: 0.75},
	}
}

func TestDecodeEvidenceReplayKeepsValidRowsAndRecordsRejects(t *testing.T) {
	t.Parallel()
	valid := replayEvidence(DecisionPromote, []string{"docs/operations/v8-p09-self-improvement.md"})
	invalid := replayEvidence(DecisionPromote, nil)
	validLine, err := json.Marshal(valid)
	if err != nil {
		t.Fatalf("Marshal valid: %v", err)
	}
	invalidLine, err := json.Marshal(invalid)
	if err != nil {
		t.Fatalf("Marshal invalid: %v", err)
	}
	input := strings.Join([]string{
		string(validLine),
		"{bad-json",
		string(invalidLine),
	}, "\n")

	got, err := DecodeEvidenceReplay(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeEvidenceReplay: %v", err)
	}
	if len(got.Accepted) != 1 {
		t.Fatalf("accepted=%d want 1", len(got.Accepted))
	}
	if got.Accepted[0].ID != valid.ID {
		t.Fatalf("accepted ID=%q want %q", got.Accepted[0].ID, valid.ID)
	}
	if len(got.Rejected) != 2 {
		t.Fatalf("rejected=%d want 2", len(got.Rejected))
	}
	if got.Rejected[0].Line != 2 || got.Rejected[1].Line != 3 {
		t.Fatalf("rejected lines=%+v want 2 and 3", got.Rejected)
	}
}

func TestEncodeRewardArtifactsNDJSONRoundTrip(t *testing.T) {
	t.Parallel()
	replay := ReplayResult{Accepted: []Evidence{
		replayEvidence(DecisionPromote, []string{"docs/operations/v8-p09-self-improvement.md"}),
		replayEvidence(DecisionReject, []string{"reports/research/rejected.md"}),
	}}
	artifacts, err := RewardArtifacts(replay.Accepted)
	if err != nil {
		t.Fatalf("RewardArtifacts: %v", err)
	}
	var buf bytes.Buffer
	if err := EncodeRewardArtifactsNDJSON(&buf, artifacts); err != nil {
		t.Fatalf("EncodeRewardArtifactsNDJSON: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("lines=%d want 1\n%s", len(lines), buf.String())
	}
	var decoded RewardArtifact
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("decode reward artifact: %v", err)
	}
	if decoded.EvidenceID != "ev-replay-1" {
		t.Fatalf("EvidenceID=%q want ev-replay-1", decoded.EvidenceID)
	}
	if decoded.Signal.PolicyName != "autoresearch-producer-reviewer" {
		t.Fatalf("PolicyName=%q", decoded.Signal.PolicyName)
	}
}

func TestDecodeEvidenceReplayPromotionRequiresArtifacts(t *testing.T) {
	t.Parallel()
	ev := replayEvidence(DecisionPromote, nil)
	line, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := DecodeEvidenceReplay(strings.NewReader(string(line)))
	if err != nil {
		t.Fatalf("DecodeEvidenceReplay: %v", err)
	}
	if len(got.Accepted) != 0 {
		t.Fatalf("accepted=%d want 0", len(got.Accepted))
	}
	if len(got.Rejected) != 1 || got.Rejected[0].Line != 1 {
		t.Fatalf("rejected=%+v want one line-1 rejection", got.Rejected)
	}
}
