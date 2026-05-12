package metrics

import (
	"strings"
	"testing"
)

func TestV8SelfImprovementMetricsRegisteredAndExposed(t *testing.T) {
	t.Parallel()

	r := NewRegistry("mc-api")
	r.SelfImprovementEvidenceTotal.Inc(Labels{"decision": "promote"})
	r.SelfImprovementReward.Set(0.75, Labels{})
	r.AgentraceEvidenceTotal.Inc(Labels{"source": "replay"})

	body := scrape(t, r)
	for _, want := range []string{
		"ec_self_improvement_evidence_total",
		"ec_self_improvement_reward",
		"ec_agentrace_evidence_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q\n%s", want, body)
		}
	}
}
