package compare

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// UIAutoExecutor runs a TestScenario through the OmniParser bridge
// (or mock). Callers supply a concrete implementation; tests inject
// a mock that returns deterministic results.
type UIAutoExecutor interface {
	Execute(ctx context.Context, sc TestScenario) (ToolResult, error)
}

// PlaywrightExecutor runs a TestScenario through Playwright (subprocess
// or mock). Same injection pattern as UIAutoExecutor.
type PlaywrightExecutor interface {
	Execute(ctx context.Context, sc TestScenario) (ToolResult, error)
}

// ComparisonRunner orchestrates side-by-side execution of a
// TestScenario through both uiauto and Playwright executors.
type ComparisonRunner struct {
	uiauto     UIAutoExecutor
	playwright PlaywrightExecutor
	logger     *slog.Logger
	now        func() time.Time
}

// NewComparisonRunner constructs a runner with injected executors.
func NewComparisonRunner(
	ua UIAutoExecutor,
	pw PlaywrightExecutor,
	logger *slog.Logger,
) *ComparisonRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &ComparisonRunner{
		uiauto:     ua,
		playwright: pw,
		logger:     logger,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// Run executes the scenario through both tools and returns the
// combined ComparisonResult. Decomposed into executeUIAuto +
// executePlaywright + compareResults per the plan.
func (cr *ComparisonRunner) Run(ctx context.Context, sc TestScenario) (ComparisonResult, error) {
	uaResult, uaErr := cr.executeUIAuto(ctx, sc)
	if uaErr != nil {
		uaResult.Error = uaErr.Error()
	}
	pwResult, pwErr := cr.executePlaywright(ctx, sc)
	if pwErr != nil {
		pwResult.Error = pwErr.Error()
	}
	return cr.compareResults(sc, uaResult, pwResult), nil
}

// RunAll executes multiple scenarios and returns all results.
func (cr *ComparisonRunner) RunAll(
	ctx context.Context,
	scenarios []TestScenario,
) ([]ComparisonResult, error) {
	results := make([]ComparisonResult, 0, len(scenarios))
	for _, sc := range scenarios {
		res, err := cr.Run(ctx, sc)
		if err != nil {
			return nil, fmt.Errorf("scenario %q: %w", sc.Name, err)
		}
		results = append(results, res)
	}
	return results, nil
}

func (cr *ComparisonRunner) executeUIAuto(
	ctx context.Context,
	sc TestScenario,
) (ToolResult, error) {
	cr.logger.Info("executing uiauto", "scenario", sc.Name)
	result, err := cr.uiauto.Execute(ctx, sc)
	result.Tool = "uiauto"
	return result, err
}

func (cr *ComparisonRunner) executePlaywright(
	ctx context.Context,
	sc TestScenario,
) (ToolResult, error) {
	cr.logger.Info("executing playwright", "scenario", sc.Name)
	result, err := cr.playwright.Execute(ctx, sc)
	result.Tool = "playwright"
	return result, err
}

func (cr *ComparisonRunner) compareResults(
	sc TestScenario,
	ua ToolResult,
	pw ToolResult,
) ComparisonResult {
	return ComparisonResult{
		Scenario:         sc,
		UIAutoResult:     ua,
		PlaywrightResult: pw,
		TimeDelta:        ua.DurationMs - pw.DurationMs,
		AccuracyDelta:    ua.PassRate() - pw.PassRate(),
	}
}
