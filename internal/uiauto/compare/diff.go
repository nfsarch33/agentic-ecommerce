package compare

import "fmt"

// Diff produces a Comparison row given a Playwright spec and a uiauto
// scenario. Both inputs are pure data; no I/O happens here. The function
// is idempotent and table-driven testable.
func Diff(spec string, pw PlaywrightSpec, ui UIAutoScenario) Comparison {
	out := Comparison{Spec: spec, Playwright: pw, UIAuto: ui}
	out.Agreement = pw.Result == ui.Result
	out.Notes = describeDifference(pw, ui)
	return out
}

func describeDifference(pw PlaywrightSpec, ui UIAutoScenario) string {
	switch {
	case pw.Result == ResultPass && ui.Result == ResultFail:
		return fmt.Sprintf("playwright passed in %dms; uiauto failed: %s", pw.DurationMs, truncate(ui.Error, 160))
	case pw.Result == ResultFail && ui.Result == ResultPass:
		return fmt.Sprintf("uiauto passed via tier=%s in %dms; playwright failed: %s", ui.TierUsed, ui.DurationMs, truncate(pw.Error, 160))
	case pw.Result == ResultPass && ui.Result == ResultPass:
		if len(ui.SelfHealEvents) > 0 {
			return fmt.Sprintf("both passed; uiauto self-healed %d step(s) (tier=%s)", len(ui.SelfHealEvents), ui.TierUsed)
		}
		return ""
	case pw.Result == ResultFail && ui.Result == ResultFail:
		return "both failed; review selectors-tried log"
	case ui.Result == ResultUnknown:
		return "uiauto result missing or could not be parsed"
	case pw.Result == ResultUnknown:
		return "playwright result missing or could not be parsed"
	default:
		return ""
	}
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

// Summarize aggregates a list of Comparisons. Pass-only/fail-only categories
// surface flake-discovery candidates: where Playwright passed and uiauto
// failed (or vice versa) we likely have a real selector or selfheal gap.
func Summarize(items []Comparison) Summary {
	var s Summary
	s.Total = len(items)
	for _, it := range items {
		if it.Agreement {
			s.Agreed++
		} else {
			s.Disagreed++
		}
		switch {
		case it.Playwright.Result == ResultPass && it.UIAuto.Result == ResultPass:
			s.BothPass++
		case it.Playwright.Result == ResultFail && it.UIAuto.Result == ResultFail:
			s.BothFail++
		case it.Playwright.Result == ResultPass && it.UIAuto.Result != ResultPass:
			s.PlaywrightOnlyPass++
		case it.UIAuto.Result == ResultPass && it.Playwright.Result != ResultPass:
			s.UIAutoOnlyPass++
		}
		s.SelfHealEvents += len(it.UIAuto.SelfHealEvents)
	}
	return s
}
