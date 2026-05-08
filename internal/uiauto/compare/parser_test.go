package compare

import (
	"reflect"
	"testing"
)

func TestParsePlaywrightReport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want []PlaywrightSpec
	}{
		{
			name: "empty",
			raw:  "",
			want: nil,
		},
		{
			name: "single passing spec, single suite",
			raw:  `{"suites":[{"file":"e2e/home.spec.ts","specs":[{"title":"home page renders the hero","file":"e2e/home.spec.ts","tests":[{"results":[{"status":"passed","duration":421,"steps":[{"title":"goto","selector":"/"},{"title":"toBeVisible","selector":"role=heading[name=/agentic ecommerce/i]"}]}]}]}]}]}`,
			want: []PlaywrightSpec{
				{
					Spec:       "e2e/home.spec.ts",
					Title:      "home page renders the hero",
					Result:     ResultPass,
					DurationMs: 421,
					Anchor:     "/",
					Selectors:  []string{"/", "role=heading[name=/agentic ecommerce/i]"},
				},
			},
		},
		{
			name: "failing spec captures error and dedup selectors",
			raw:  `{"suites":[{"file":"e2e/checkout.spec.ts","specs":[{"title":"shopper checks out","file":"e2e/checkout.spec.ts","tests":[{"results":[{"status":"failed","duration":1200,"error":{"message":"locator missing"},"steps":[{"title":"click","selector":"button[name=add]","steps":[{"title":"click","selector":"button[name=add]"}]}]}]}]}]}]}`,
			want: []PlaywrightSpec{
				{
					Spec:       "e2e/checkout.spec.ts",
					Title:      "shopper checks out",
					Result:     ResultFail,
					DurationMs: 1200,
					Anchor:     "button[name=add]",
					Selectors:  []string{"button[name=add]"},
					Error:      "locator missing",
				},
			},
		},
		{
			name: "nested suite is flattened and order is alphabetical",
			raw:  `{"suites":[{"specs":[],"suites":[{"file":"e2e/products.spec.ts","specs":[{"title":"products","file":"e2e/products.spec.ts","tests":[{"results":[{"status":"passed","duration":111}]}]}]},{"file":"e2e/admin-agents.spec.ts","specs":[{"title":"admin agents","file":"e2e/admin-agents.spec.ts","tests":[{"results":[{"status":"timedOut","duration":5000}]}]}]}]}]}`,
			want: []PlaywrightSpec{
				{Spec: "e2e/admin-agents.spec.ts", Title: "admin agents", Result: ResultError, DurationMs: 5000},
				{Spec: "e2e/products.spec.ts", Title: "products", Result: ResultPass, DurationMs: 111},
			},
		},
		{
			name: "skipped status",
			raw:  `{"suites":[{"file":"e2e/v200.spec.ts","specs":[{"title":"release","file":"e2e/v200.spec.ts","tests":[{"results":[{"status":"skipped","duration":0}]}]}]}]}`,
			want: []PlaywrightSpec{{Spec: "e2e/v200.spec.ts", Title: "release", Result: ResultSkipped}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParsePlaywrightReport([]byte(tc.raw))
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mismatch\n got: %+v\nwant: %+v", got, tc.want)
			}
		})
	}
}

func TestParsePlaywrightReport_BadJSON(t *testing.T) {
	t.Parallel()
	_, err := ParsePlaywrightReport([]byte("{not json"))
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestParseUIAutoMetrics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want UIAutoScenario
	}{
		{
			name: "empty",
			raw:  "",
			want: UIAutoScenario{Result: ResultUnknown},
		},
		{
			name: "all-pass single tier light",
			raw:  `{"scenario_id":"home","scenario_name":"Home flow","total_steps":2,"passed_steps":2,"failed_steps":0,"total_latency_ms":520,"tier_breakdown":{"light":2},"steps":[{"step_index":0,"status":"passed","selector":"h1","tier":"light"},{"step_index":1,"status":"passed","selector":"a.cta","tier":"light"}]}`,
			want: UIAutoScenario{
				Scenario:      "home",
				Name:          "Home flow",
				Result:        ResultPass,
				DurationMs:    520,
				TierUsed:      TierLight,
				TierBreakdown: map[Tier]int{TierLight: 2},
				Selectors:     []string{"h1", "a.cta"},
			},
		},
		{
			name: "self-heal escalation captured",
			raw:  `{"scenario_id":"checkout","total_steps":3,"passed_steps":3,"failed_steps":0,"total_latency_ms":1500,"tier_breakdown":{"light":2,"smart":1},"steps":[{"step_index":0,"status":"passed","selector":"a[name=cart]","tier":"light"},{"step_index":1,"status":"passed","selector":"button[name=checkout]","tier":"smart","heal_path":"fingerprint->structural->llm","instruction":"click checkout"},{"step_index":2,"status":"passed","selector":"input[name=email]","tier":"light"}]}`,
			want: UIAutoScenario{
				Scenario:      "checkout",
				Result:        ResultPass,
				DurationMs:    1500,
				TierUsed:      TierLight,
				TierBreakdown: map[Tier]int{TierLight: 2, TierSmart: 1},
				Selectors:     []string{"a[name=cart]", "button[name=checkout]", "input[name=email]"},
				SelfHealEvents: []SelfHealEvent{
					{Step: 1, Reason: "click checkout", HealedFrom: "fingerprint", HealedTo: "llm", Tier: TierSmart},
				},
			},
		},
		{
			name: "failed step records first error",
			raw:  `{"scenario_id":"admin","total_steps":2,"passed_steps":1,"failed_steps":1,"total_latency_ms":900,"tier_breakdown":{"light":1,"vlm":1},"steps":[{"step_index":0,"status":"passed","selector":"input#email","tier":"light"},{"step_index":1,"status":"failed","selector":"button[type=submit]","tier":"vlm","error":"chromedp click timed out"}]}`,
			want: UIAutoScenario{
				Scenario:      "admin",
				Result:        ResultFail,
				DurationMs:    900,
				TierUsed:      TierLight,
				TierBreakdown: map[Tier]int{TierLight: 1, TierVLM: 1},
				Selectors:     []string{"input#email", "button[type=submit]"},
				Error:         "chromedp click timed out",
			},
		},
		{
			name: "tie in tier_breakdown picks alphabetical first",
			raw:  `{"scenario_id":"x","total_steps":2,"passed_steps":2,"tier_breakdown":{"light":1,"smart":1}}`,
			want: UIAutoScenario{Scenario: "x", Result: ResultPass, TierUsed: TierLight, TierBreakdown: map[Tier]int{TierLight: 1, TierSmart: 1}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseUIAutoMetrics([]byte(tc.raw))
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mismatch\n got: %+v\nwant: %+v", got, tc.want)
			}
		})
	}
}

func TestNormalizeTier(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want Tier
	}{
		{"light", TierLight},
		{"LIGHT", TierLight},
		{" smart ", TierSmart},
		{"VLM", TierVLM},
		{"powerful", TierUnknown},
		{"", TierUnknown},
	}
	for _, tc := range tests {
		got := normalizeTier(tc.in)
		if got != tc.want {
			t.Errorf("normalizeTier(%q)=%v want=%v", tc.in, got, tc.want)
		}
	}
}

func TestSplitHealPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in       string
		from, to string
	}{
		{"", "", ""},
		{"fingerprint", "fingerprint", "fingerprint"},
		{"fingerprint->structural", "fingerprint", "structural"},
		{"fingerprint -> structural -> llm", "fingerprint", "llm"},
	}
	for _, tc := range tests {
		gotFrom, gotTo := splitHealPath(tc.in)
		if gotFrom != tc.from || gotTo != tc.to {
			t.Errorf("splitHealPath(%q)=(%q,%q) want=(%q,%q)", tc.in, gotFrom, gotTo, tc.from, tc.to)
		}
	}
}
