package evomap

import (
	"strings"
	"testing"
	"time"
)

func TestAggregateSelfImprovementKPIs(t *testing.T) {
	t.Parallel()
	caps := []Capsule{
		{KPIs: KPIs{
			SelfImprovementEvidenceTotal: 2,
			SelfImprovementPromotedTotal: 1,
			SelfImprovementRejectedTotal: 1,
			SelfImprovementRewardMean:    0.25,
			AgentraceEvidenceTotal:       12,
		}},
		{KPIs: KPIs{
			SelfImprovementEvidenceTotal: 1,
			SelfImprovementReworkTotal:   1,
			SelfImprovementRewardMean:    0.5,
			AgentraceEvidenceTotal:       4,
		}},
	}
	got := Aggregate(caps)
	if got.TotalSelfImprovementEvidence != 3 {
		t.Fatalf("TotalSelfImprovementEvidence=%d want 3", got.TotalSelfImprovementEvidence)
	}
	if got.TotalSelfImprovementPromoted != 1 {
		t.Fatalf("TotalSelfImprovementPromoted=%d want 1", got.TotalSelfImprovementPromoted)
	}
	if got.TotalSelfImprovementRejected != 1 {
		t.Fatalf("TotalSelfImprovementRejected=%d want 1", got.TotalSelfImprovementRejected)
	}
	if got.TotalSelfImprovementRework != 1 {
		t.Fatalf("TotalSelfImprovementRework=%d want 1", got.TotalSelfImprovementRework)
	}
	if got.MeanSelfImprovementReward != 0.375 {
		t.Fatalf("MeanSelfImprovementReward=%f want 0.375", got.MeanSelfImprovementReward)
	}
	if got.TotalAgentraceEvidence != 16 {
		t.Fatalf("TotalAgentraceEvidence=%d want 16", got.TotalAgentraceEvidence)
	}
}

func TestRenderCapsuleMarkdownIncludesSelfImprovementFields(t *testing.T) {
	t.Parallel()
	md := RenderCapsuleMarkdown(time.Date(2026, 5, 13, 2, 45, 0, 0, time.UTC), AggregateResult{
		SampleCount:                  2,
		TotalSelfImprovementEvidence: 3,
		TotalSelfImprovementPromoted: 1,
		TotalSelfImprovementRejected: 1,
		TotalSelfImprovementRework:   1,
		MeanSelfImprovementReward:    0.375,
		TotalAgentraceEvidence:       16,
		BinaryDistribution:           map[string]int{"mc-api": 2},
		WindowStart:                  time.Date(2026, 5, 13, 2, 0, 0, 0, time.UTC),
		WindowEnd:                    time.Date(2026, 5, 13, 2, 30, 0, 0, time.UTC),
	})
	for _, want := range []string{
		"total self-improvement evidence: 3",
		"self-improvement promoted/rejected/rework: 1/1/1",
		"mean self-improvement reward: 0.375",
		"total Agenttrace evidence inputs: 16",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}
