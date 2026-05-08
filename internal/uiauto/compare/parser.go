package compare

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// playwrightReport is the shape Playwright emits with --reporter=json.
// Only the fields the comparison consumes are decoded; everything else is
// dropped. Suites can nest, so ParsePlaywrightReport flattens recursively.
type playwrightReport struct {
	Suites []playwrightSuite `json:"suites"`
}

type playwrightSuite struct {
	Title  string            `json:"title"`
	File   string            `json:"file"`
	Suites []playwrightSuite `json:"suites"`
	Specs  []playwrightSpec  `json:"specs"`
}

type playwrightSpec struct {
	Title string           `json:"title"`
	File  string           `json:"file"`
	Tests []playwrightTest `json:"tests"`
}

type playwrightTest struct {
	Results []playwrightResult `json:"results"`
}

type playwrightResult struct {
	Status     string                  `json:"status"`
	DurationMs int64                   `json:"duration"`
	Error      *playwrightError        `json:"error,omitempty"`
	Steps      []playwrightStep        `json:"steps,omitempty"`
	Attachment []playwrightAttachOmit  `json:"attachments,omitempty"`
	Annot      []playwrightAnnotations `json:"annotations,omitempty"`
}

type playwrightError struct {
	Message string `json:"message,omitempty"`
}

type playwrightStep struct {
	Title    string           `json:"title"`
	Selector string           `json:"selector,omitempty"`
	Steps    []playwrightStep `json:"steps,omitempty"`
	Error    *playwrightError `json:"error,omitempty"`
}

type playwrightAttachOmit struct{}
type playwrightAnnotations struct{}

// ParsePlaywrightReport flattens a Playwright JSON report into one
// PlaywrightSpec per spec file. The first test's first result wins; flake
// retries are intentionally not modeled at this layer (the v2.6.0 sprint
// will expand this).
func ParsePlaywrightReport(raw []byte) ([]PlaywrightSpec, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var rep playwrightReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, fmt.Errorf("decode playwright report: %w", err)
	}
	out := make([]PlaywrightSpec, 0, len(rep.Suites))
	for _, suite := range rep.Suites {
		out = append(out, flattenSuite(suite, "")...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec < out[j].Spec })
	return out, nil
}

func flattenSuite(suite playwrightSuite, parentFile string) []PlaywrightSpec {
	file := suite.File
	if file == "" {
		file = parentFile
	}
	var out []PlaywrightSpec
	for _, spec := range suite.Specs {
		s := flattenSpec(spec, file)
		if s.Spec != "" {
			out = append(out, s)
		}
	}
	for _, child := range suite.Suites {
		out = append(out, flattenSuite(child, file)...)
	}
	return out
}

func flattenSpec(spec playwrightSpec, defaultFile string) PlaywrightSpec {
	file := spec.File
	if file == "" {
		file = defaultFile
	}
	out := PlaywrightSpec{Spec: file, Title: spec.Title, Result: ResultUnknown}
	if len(spec.Tests) == 0 || len(spec.Tests[0].Results) == 0 {
		return out
	}
	first := spec.Tests[0].Results[0]
	out.Result = playwrightStatusToResult(first.Status)
	out.DurationMs = first.DurationMs
	out.Selectors = collectSelectors(first.Steps)
	out.Anchor = firstSelector(out.Selectors)
	if first.Error != nil {
		out.Error = strings.TrimSpace(first.Error.Message)
	}
	return out
}

func playwrightStatusToResult(status string) Result {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "passed", "expected":
		return ResultPass
	case "failed", "unexpected":
		return ResultFail
	case "timedout", "interrupted":
		return ResultError
	case "skipped":
		return ResultSkipped
	default:
		return ResultUnknown
	}
}

func collectSelectors(steps []playwrightStep) []string {
	if len(steps) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(steps))
	var out []string
	var walk func([]playwrightStep)
	walk = func(items []playwrightStep) {
		for _, s := range items {
			sel := strings.TrimSpace(s.Selector)
			if sel != "" {
				if _, ok := seen[sel]; !ok {
					seen[sel] = struct{}{}
					out = append(out, sel)
				}
			}
			if len(s.Steps) > 0 {
				walk(s.Steps)
			}
		}
	}
	walk(steps)
	return out
}

func firstSelector(sels []string) string {
	if len(sels) == 0 {
		return ""
	}
	return sels[0]
}

// uiautoMetrics matches the on-disk shape written by ui-agent demo's
// DemoMetricsSummary (see demo.go). We deliberately decode only the fields
// the comparison report needs.
type uiautoMetrics struct {
	ScenarioID      string              `json:"scenario_id"`
	ScenarioName    string              `json:"scenario_name"`
	TotalSteps      int                 `json:"total_steps"`
	PassedSteps     int                 `json:"passed_steps"`
	FailedSteps     int                 `json:"failed_steps"`
	TotalLatencyMs  int64               `json:"total_latency_ms"`
	TierBreakdown   map[string]int      `json:"tier_breakdown"`
	HealPathSummary map[string]int      `json:"heal_path_summary"`
	Steps           []uiautoMetricsStep `json:"steps"`
}

type uiautoMetricsStep struct {
	StepIndex   int    `json:"step_index"`
	Status      string `json:"status"`
	Selector    string `json:"selector,omitempty"`
	Tier        string `json:"tier"`
	HealPath    string `json:"heal_path,omitempty"`
	Error       string `json:"error,omitempty"`
	Instruction string `json:"instruction,omitempty"`
}

// ParseUIAutoMetrics converts a single demo-metrics.json blob into the
// comparison-ready UIAutoScenario shape.
func ParseUIAutoMetrics(raw []byte) (UIAutoScenario, error) {
	if len(raw) == 0 {
		return UIAutoScenario{Result: ResultUnknown}, nil
	}
	var m uiautoMetrics
	if err := json.Unmarshal(raw, &m); err != nil {
		return UIAutoScenario{}, fmt.Errorf("decode uiauto metrics: %w", err)
	}
	out := UIAutoScenario{
		Scenario:      m.ScenarioID,
		Name:          m.ScenarioName,
		DurationMs:    m.TotalLatencyMs,
		TierBreakdown: tierBreakdown(m.TierBreakdown),
		TierUsed:      pickPrimaryTier(m.TierBreakdown),
		Result:        verdictFromCounts(m.TotalSteps, m.PassedSteps, m.FailedSteps),
	}
	out.Selectors, out.SelfHealEvents, out.Error = walkSteps(m.Steps)
	return out, nil
}

func verdictFromCounts(total, passed, failed int) Result {
	switch {
	case total == 0:
		return ResultUnknown
	case failed > 0:
		return ResultFail
	case passed == total:
		return ResultPass
	default:
		return ResultError
	}
}

func tierBreakdown(in map[string]int) map[Tier]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[Tier]int, len(in))
	for k, v := range in {
		out[normalizeTier(k)] = v
	}
	return out
}

func pickPrimaryTier(in map[string]int) Tier {
	if len(in) == 0 {
		return TierUnknown
	}
	var best Tier = TierUnknown
	var bestCount int
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := in[k]
		if v > bestCount {
			bestCount = v
			best = normalizeTier(k)
		}
	}
	return best
}

func normalizeTier(s string) Tier {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "light":
		return TierLight
	case "smart":
		return TierSmart
	case "vlm":
		return TierVLM
	default:
		return TierUnknown
	}
}

func walkSteps(steps []uiautoMetricsStep) ([]string, []SelfHealEvent, string) {
	if len(steps) == 0 {
		return nil, nil, ""
	}
	seenSel := make(map[string]struct{}, len(steps))
	var sels []string
	var heals []SelfHealEvent
	var firstErr string
	for _, s := range steps {
		sel := strings.TrimSpace(s.Selector)
		if sel != "" {
			if _, ok := seenSel[sel]; !ok {
				seenSel[sel] = struct{}{}
				sels = append(sels, sel)
			}
		}
		if heal := strings.TrimSpace(s.HealPath); heal != "" {
			from, to := splitHealPath(heal)
			heals = append(heals, SelfHealEvent{
				Step:       s.StepIndex,
				Reason:     s.Instruction,
				HealedFrom: from,
				HealedTo:   to,
				Tier:       normalizeTier(s.Tier),
			})
		}
		if firstErr == "" && strings.EqualFold(strings.TrimSpace(s.Status), "failed") {
			firstErr = strings.TrimSpace(s.Error)
		}
	}
	return sels, heals, firstErr
}

// splitHealPath parses ui-agent's heal_path of the shape "from->to" or
// "fingerprint->structural->llm" and returns the first and last segments.
func splitHealPath(in string) (string, string) {
	parts := strings.Split(in, "->")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return parts[0], parts[0]
	default:
		return parts[0], parts[len(parts)-1]
	}
}
