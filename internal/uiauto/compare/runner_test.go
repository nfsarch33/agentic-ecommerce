package compare

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeSpecKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"e2e/home.spec.ts", "home"},
		{"home.spec.ts", "home"},
		{"home", "home"},
		{"reports/playwright/home.json", "home"},
	}
	for _, tc := range tests {
		got := normalizeSpecKey(tc.in)
		if got != tc.want {
			t.Errorf("normalizeSpecKey(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}
}

func TestLoadFixtures(t *testing.T) {
	t.Parallel()
	dir := writeFixtures(t)
	src, err := LoadFixtures(dir)
	if err != nil {
		t.Fatalf("LoadFixtures: %v", err)
	}
	if got := len(src.Mappings); got != 2 {
		t.Fatalf("mappings len got=%d want=2", got)
	}
	if _, ok := src.PlaywrightResults["home"]; !ok {
		t.Errorf("missing playwright key home; have %v", keys(src.PlaywrightResults))
	}
	if ui, ok := src.UIAutoResults["home"]; !ok || ui.TierUsed != TierLight {
		t.Errorf("uiauto home missing or wrong tier: %+v", src.UIAutoResults)
	}
}

func TestLoadFromDirs_InfersMappingWhenMissing(t *testing.T) {
	t.Parallel()
	dir := writeFixtures(t)
	src, err := LoadFromDirs(filepath.Join(dir, "playwright"), filepath.Join(dir, "uiauto"), "")
	if err != nil {
		t.Fatalf("LoadFromDirs: %v", err)
	}
	if len(src.Mappings) != 2 {
		t.Fatalf("inferred mappings len=%d want=2: %+v", len(src.Mappings), src.Mappings)
	}
}

func TestCompose(t *testing.T) {
	t.Parallel()
	src := Sources{
		Mappings: []ScenarioMapping{
			{Spec: "home", Scenario: "home"},
			{Spec: "checkout", Scenario: "checkout"},
			{Spec: "missing", Scenario: "missing"},
		},
		PlaywrightResults: map[string]PlaywrightSpec{
			"home":     {Spec: "e2e/home.spec.ts", Result: ResultPass, DurationMs: 100},
			"checkout": {Spec: "e2e/checkout.spec.ts", Result: ResultPass, DurationMs: 1000},
		},
		UIAutoResults: map[string]UIAutoScenario{
			"home":     {Scenario: "home", Result: ResultPass, DurationMs: 200, TierUsed: TierLight},
			"checkout": {Scenario: "checkout", Result: ResultFail, DurationMs: 500, Error: "selector"},
		},
	}
	got := Compose(src)
	if len(got) != 3 {
		t.Fatalf("compose len got=%d want=3", len(got))
	}
	if !got[0].Agreement {
		t.Errorf("home should agree")
	}
	if got[1].Agreement {
		t.Errorf("checkout should disagree")
	}
	if got[2].UIAuto.Result != ResultUnknown || got[2].Playwright.Result != ResultUnknown {
		t.Errorf("missing should be unknown both sides: %+v", got[2])
	}
}

func writeFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pwDir := filepath.Join(dir, "playwright")
	uiDir := filepath.Join(dir, "uiauto")
	if err := os.MkdirAll(pwDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	homePW := `{"suites":[{"file":"e2e/home.spec.ts","specs":[{"title":"home","file":"e2e/home.spec.ts","tests":[{"results":[{"status":"passed","duration":120}]}]}]}]}`
	checkoutPW := `{"suites":[{"file":"e2e/checkout.spec.ts","specs":[{"title":"checkout","file":"e2e/checkout.spec.ts","tests":[{"results":[{"status":"passed","duration":700}]}]}]}]}`
	homeUI := `{"scenario_id":"home","total_steps":1,"passed_steps":1,"total_latency_ms":350,"tier_breakdown":{"light":1},"steps":[{"step_index":0,"status":"passed","selector":"h1","tier":"light"}]}`
	checkoutUI := `{"scenario_id":"checkout","total_steps":1,"passed_steps":1,"total_latency_ms":600,"tier_breakdown":{"smart":1},"steps":[{"step_index":0,"status":"passed","selector":"button","tier":"smart","heal_path":"fingerprint->structural"}]}`
	mapping := `[{"spec":"home","scenario":"home"},{"spec":"checkout","scenario":"checkout"}]`
	mustWrite(t, filepath.Join(pwDir, "home.json"), homePW)
	mustWrite(t, filepath.Join(pwDir, "checkout.json"), checkoutPW)
	mustWrite(t, filepath.Join(uiDir, "home.json"), homeUI)
	mustWrite(t, filepath.Join(uiDir, "checkout.json"), checkoutUI)
	mustWrite(t, filepath.Join(dir, "mapping.json"), mapping)
	return dir
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func keys(m map[string]PlaywrightSpec) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestInferMappings_Sorted(t *testing.T) {
	t.Parallel()
	pw := map[string]PlaywrightSpec{"b": {}, "a": {}}
	ui := map[string]UIAutoScenario{"c": {}, "a": {}}
	got := inferMappings(pw, ui)
	wantSpecs := []string{"a", "b", "c"}
	gotSpecs := make([]string, len(got))
	for i, m := range got {
		gotSpecs[i] = m.Spec
	}
	if !reflect.DeepEqual(gotSpecs, wantSpecs) {
		t.Errorf("infer order got=%v want=%v", gotSpecs, wantSpecs)
	}
}
