package metrics

// RegisterComparisonMetrics extends the Registry with the v4.14.0
// uiauto-vs-Playwright comparison harness metric surfaces. Called
// once during NewRegistry construction.
//
// Cardinality budget per series (v4.14.0):
//
//	ec_uiauto_comparison_accuracy{tool}
//	  ~ tools(2: uiauto|playwright) = 2 series.
//	ec_uiauto_comparison_speed_ms{tool}
//	  ~ tools(2: uiauto|playwright) = 2 series.
//	ec_uiauto_comparison_agreement_rate (no labels)
//	  ~ 1 series.
//	ec_uiauto_comparison_scenario_duration_ms{tool, scenario}
//	  ~ tools(2) * scenarios(5) = 10 series.
//	ec_uiauto_comparison_scenario_pass_rate{tool, scenario}
//	  ~ tools(2) * scenarios(5) = 10 series.
//
// Total ~ 25 additive series; well under the per-binary 10_000 cap.
func RegisterComparisonMetrics(r *Registry) {
	r.ComparisonAccuracy = newGauge(
		r,
		"ec_uiauto_comparison_accuracy",
		"v4.14.0 uiauto comparison accuracy by tool (uiauto|playwright).",
	)
	r.ComparisonSpeedMs = newGauge(
		r,
		"ec_uiauto_comparison_speed_ms",
		"v4.14.0 uiauto comparison avg speed (ms) by tool.",
	)
	r.ComparisonAgreementRate = newGauge(
		r,
		"ec_uiauto_comparison_agreement_rate",
		"v4.14.0 uiauto comparison agreement rate (0..1).",
	)
	r.ComparisonScenarioDurationMs = newGauge(
		r,
		"ec_uiauto_comparison_scenario_duration_ms",
		"v4.14.0 per-scenario comparison duration (ms) by tool + scenario.",
	)
	r.ComparisonScenarioPassRate = newGauge(
		r,
		"ec_uiauto_comparison_scenario_pass_rate",
		"v4.14.0 per-scenario comparison pass rate (0..1) by tool + scenario.",
	)
}
