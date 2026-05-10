package compare

// ActionType enumerates the browser actions a comparison scenario can
// express. Both the uiauto (OmniParser bridge) and Playwright
// (subprocess) execution paths interpret these identically.
type ActionType string

const (
	ActionNavigate      ActionType = "navigate"
	ActionClick         ActionType = "click"
	ActionType_         ActionType = "type"
	ActionScroll        ActionType = "scroll"
	ActionWait          ActionType = "wait"
	ActionAssertText    ActionType = "assert_text"
	ActionAssertElement ActionType = "assert_element"
)

// Action is a single step within a comparison scenario.
type Action struct {
	Type     ActionType `json:"type"     yaml:"type"`
	Selector string     `json:"selector" yaml:"selector,omitempty"`
	Value    string     `json:"value"    yaml:"value,omitempty"`
	URL      string     `json:"url"      yaml:"url,omitempty"`
	WaitMs   int64      `json:"wait_ms"  yaml:"wait_ms,omitempty"`
}

// Assertion defines a pass/fail check at the end of a scenario.
type Assertion struct {
	Type     ActionType `json:"type"     yaml:"type"`
	Selector string     `json:"selector" yaml:"selector,omitempty"`
	Expected string     `json:"expected" yaml:"expected,omitempty"`
}

// TestScenario is the input to ComparisonRunner.Run.
type TestScenario struct {
	Name       string      `json:"name"       yaml:"name"`
	URL        string      `json:"url"        yaml:"url"`
	Actions    []Action    `json:"actions"    yaml:"actions"`
	Assertions []Assertion `json:"assertions" yaml:"assertions"`
}

// ToolResult captures the outcome of running a scenario through one tool.
type ToolResult struct {
	Tool             string           `json:"tool"`
	DurationMs       int64            `json:"duration_ms"`
	AssertionResults []AssertionCheck `json:"assertion_results"`
	Error            string           `json:"error,omitempty"`
}

// AssertionCheck records whether a single assertion passed or failed.
type AssertionCheck struct {
	Assertion Assertion `json:"assertion"`
	Passed    bool      `json:"passed"`
	Error     string    `json:"error,omitempty"`
}

// PassRate returns the fraction of assertions that passed (0.0–1.0).
func (tr ToolResult) PassRate() float64 {
	if len(tr.AssertionResults) == 0 {
		return 0
	}
	var passed int
	for _, a := range tr.AssertionResults {
		if a.Passed {
			passed++
		}
	}
	return float64(passed) / float64(len(tr.AssertionResults))
}

// ComparisonResult is the output of ComparisonRunner.Run.
type ComparisonResult struct {
	Scenario         TestScenario `json:"scenario"`
	UIAutoResult     ToolResult   `json:"uiauto_result"`
	PlaywrightResult ToolResult   `json:"playwright_result"`
	TimeDelta        int64        `json:"time_delta_ms"`
	AccuracyDelta    float64      `json:"accuracy_delta"`
}
