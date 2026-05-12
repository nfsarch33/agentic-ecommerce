package evomap

import (
	"strings"
	"testing"
	"time"
)

func TestRenderCapsuleMarkdownIncludesResourceGuardFields(t *testing.T) {
	t.Parallel()

	md := RenderCapsuleMarkdown(time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC), AggregateResult{
		WindowStart:                   time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC),
		WindowEnd:                     time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC),
		SampleCount:                   2,
		TotalResourceGuardAlerts:      3,
		MaxSentruxDesktopProcessCount: 1,
		TotalWorkerpoolResizes:        4,
		BinaryDistribution:            map[string]int{"mc-api": 2},
	})

	for _, want := range []string{
		"total resource guard alerts: 3",
		"max Sentrux desktop processes: 1",
		"total workerpool resizes: 4",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}
