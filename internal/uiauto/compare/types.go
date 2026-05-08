// Package compare implements the v2.1.0 uiauto vs Playwright comparison
// generator. It parses Playwright JSON reporter output and uiauto-framework
// demo-metrics.json (cited from
// ~/Code/personal/uiauto-framework/cmd/ui-agent/demo.go DemoMetricsSummary)
// and produces a structured diff per scenario.
//
// The package is split so subprocess driving and parsing stay isolated:
// runner.go owns process I/O, parser.go owns deterministic JSON shaping,
// diff.go owns set comparison, and report.go owns output formatting.
// Every public function is exercised by table-driven tests under the
// _test.go siblings; no reflection or interface{} is used outside JSON
// decode targets.
package compare

import "time"

// Result enumerates the per-spec verdict shared by both runners.
type Result string

const (
	ResultPass    Result = "pass"
	ResultFail    Result = "fail"
	ResultError   Result = "error"
	ResultSkipped Result = "skipped"
	ResultUnknown Result = "unknown"
)

// Tier mirrors uiauto-framework's MemberAgent tier vocabulary
// (light | smart | vlm). Cited from
// ~/Code/personal/uiauto-framework/pkg/uiauto/agent.go ModelTier.
type Tier string

const (
	TierLight   Tier = "light"
	TierSmart   Tier = "smart"
	TierVLM     Tier = "vlm"
	TierUnknown Tier = "unknown"
)

// PlaywrightSpec is the parsed Playwright result for one spec file. Pulled
// from `bunx playwright test --reporter=json` output, which emits a tree of
// suites with file-scoped specs and per-test attempts.
type PlaywrightSpec struct {
	Spec       string   `json:"spec"`
	Title      string   `json:"title,omitempty"`
	Result     Result   `json:"result"`
	DurationMs int64    `json:"duration_ms"`
	Anchor     string   `json:"anchor,omitempty"`
	Selectors  []string `json:"selectors,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// SelfHealEvent captures one tier promotion event from the uiauto run.
// Mirrors the shape published by ui-agent demo via DemoStepResult.HealPath
// plus the originating tier transition.
type SelfHealEvent struct {
	Step       int    `json:"step"`
	Reason     string `json:"reason"`
	HealedFrom string `json:"healed_from,omitempty"`
	HealedTo   string `json:"healed_to,omitempty"`
	Tier       Tier   `json:"tier"`
}

// UIAutoScenario is the parsed uiauto-framework result for one scenario
// JSON. Aggregated from ui-agent demo's demo-metrics.json output.
type UIAutoScenario struct {
	Scenario       string          `json:"scenario"`
	Name           string          `json:"name,omitempty"`
	Result         Result          `json:"result"`
	DurationMs     int64           `json:"duration_ms"`
	TierUsed       Tier            `json:"tier_used"`
	TierBreakdown  map[Tier]int    `json:"tier_breakdown,omitempty"`
	Selectors      []string        `json:"selectors,omitempty"`
	SelfHealEvents []SelfHealEvent `json:"selfheal_events,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// Comparison is the per-scenario diff produced by Diff().
type Comparison struct {
	Spec       string         `json:"spec"`
	Playwright PlaywrightSpec `json:"playwright"`
	UIAuto     UIAutoScenario `json:"uiauto"`
	Agreement  bool           `json:"agreement"`
	Notes      string         `json:"notes,omitempty"`
}

// Summary aggregates a list of Comparisons for the report header.
type Summary struct {
	Total              int `json:"total"`
	Agreed             int `json:"agreed"`
	Disagreed          int `json:"disagreed"`
	BothPass           int `json:"both_pass"`
	BothFail           int `json:"both_fail"`
	PlaywrightOnlyPass int `json:"playwright_only_pass"`
	UIAutoOnlyPass     int `json:"uiauto_only_pass"`
	SelfHealEvents     int `json:"selfheal_events_total"`
}

// Report is the top-level artifact written by WriteReport.
type Report struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Mode        string       `json:"mode"`
	Items       []Comparison `json:"items"`
	Summary     Summary      `json:"summary"`
}

// ScenarioMapping pairs one Playwright spec path with one uiauto scenario
// name. Lookup is alphabetical so the report ordering is deterministic.
type ScenarioMapping struct {
	Spec     string `json:"spec"`
	Scenario string `json:"scenario"`
}
