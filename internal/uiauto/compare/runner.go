package compare

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Sources collects everything needed to assemble a Report. Populate
// PlaywrightResults and UIAutoResults via LoadFixtures (mode=fixtures) or
// LoadFromDirs (mode=runtime). Mappings are used to align rows and to set
// the spec key in the Comparison output.
type Sources struct {
	Mappings          []ScenarioMapping
	PlaywrightResults map[string]PlaywrightSpec
	UIAutoResults     map[string]UIAutoScenario
}

// LoadFixtures reads the bundled deterministic fixtures shipped under
// test/uiauto/fixtures. The directory layout is:
//
//	<root>/playwright/<spec-name>.json   -- one playwright report per spec
//	<root>/uiauto/<scenario-name>.json   -- one demo-metrics.json per scenario
//	<root>/mapping.json                  -- []ScenarioMapping
//
// The fixtures mode is the default for `make uiauto-compare` so the gate
// stays hermetic in CI: no docker, no Chrome, no LLM upstream.
func LoadFixtures(fixturesDir string) (Sources, error) {
	mappings, err := readMappings(filepath.Join(fixturesDir, "mapping.json"))
	if err != nil {
		return Sources{}, fmt.Errorf("read mapping: %w", err)
	}
	pw, err := readPlaywrightDir(filepath.Join(fixturesDir, "playwright"))
	if err != nil {
		return Sources{}, fmt.Errorf("read playwright fixtures: %w", err)
	}
	ui, err := readUIAutoDir(filepath.Join(fixturesDir, "uiauto"))
	if err != nil {
		return Sources{}, fmt.Errorf("read uiauto fixtures: %w", err)
	}
	return Sources{Mappings: mappings, PlaywrightResults: pw, UIAutoResults: ui}, nil
}

// LoadFromDirs reads runtime artifacts, with the same per-spec/per-scenario
// shape as the fixtures layout. mappingPath is optional; when empty we infer
// the mapping from filename overlap (spec basename without `.spec.ts` and
// scenario id).
func LoadFromDirs(playwrightDir, uiautoDir, mappingPath string) (Sources, error) {
	pw, err := readPlaywrightDir(playwrightDir)
	if err != nil {
		return Sources{}, fmt.Errorf("read playwright dir: %w", err)
	}
	ui, err := readUIAutoDir(uiautoDir)
	if err != nil {
		return Sources{}, fmt.Errorf("read uiauto dir: %w", err)
	}
	var mappings []ScenarioMapping
	if mappingPath != "" {
		mappings, err = readMappings(mappingPath)
		if err != nil {
			return Sources{}, fmt.Errorf("read mapping: %w", err)
		}
	} else {
		mappings = inferMappings(pw, ui)
	}
	return Sources{Mappings: mappings, PlaywrightResults: pw, UIAutoResults: ui}, nil
}

func readMappings(path string) ([]ScenarioMapping, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []ScenarioMapping
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func readPlaywrightDir(dir string) (map[string]PlaywrightSpec, error) {
	out := make(map[string]PlaywrightSpec)
	entries, err := readJSONFiles(dir)
	if err != nil {
		return nil, err
	}
	// Each per-spec report file has its scenario key in the filename
	// (e.g. admin-login.json maps to scenario "admin-login"), independent
	// of the internal spec path which may follow Playwright's own naming
	// (e.g. e2e/auth-admin.spec.ts). Indexing by file basename keeps the
	// lookup deterministic and lets ScenarioMapping align rows by intent.
	for name, raw := range entries {
		specs, err := ParsePlaywrightReport(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if len(specs) == 0 {
			continue
		}
		base := strings.TrimSuffix(name, ".json")
		// When the report holds multiple specs, prefer the one whose
		// spec path contains the filename; otherwise fall back to the
		// first entry.
		picked := specs[0]
		for _, s := range specs {
			if strings.Contains(s.Spec, base) {
				picked = s
				break
			}
		}
		out[base] = picked
	}
	return out, nil
}

func readUIAutoDir(dir string) (map[string]UIAutoScenario, error) {
	out := make(map[string]UIAutoScenario)
	entries, err := readJSONFiles(dir)
	if err != nil {
		return nil, err
	}
	// Index by file basename so the runner stays consistent with
	// readPlaywrightDir. The scenario_id field inside the metrics file
	// is preserved on the parsed value so the report can show it
	// alongside the spec name.
	for name, raw := range entries {
		ui, err := ParseUIAutoMetrics(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[strings.TrimSuffix(name, ".json")] = ui
	}
	return out, nil
}

func readJSONFiles(dir string) (map[string][]byte, error) {
	if dir == "" {
		return nil, nil
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(entries))
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, ent.Name()))
		if rerr != nil {
			return nil, fmt.Errorf("%s: %w", ent.Name(), rerr)
		}
		out[ent.Name()] = raw
	}
	return out, nil
}

// normalizeSpecKey strips directories so a spec recorded as
// `e2e/checkout.spec.ts` matches a fixture filename of `checkout.spec.ts`
// or `checkout`. Used as the lookup key inside Compose().
func normalizeSpecKey(spec string) string {
	if spec == "" {
		return ""
	}
	base := filepath.Base(spec)
	base = strings.TrimSuffix(base, ".json")
	base = strings.TrimSuffix(base, ".spec.ts")
	return base
}

func inferMappings(pw map[string]PlaywrightSpec, ui map[string]UIAutoScenario) []ScenarioMapping {
	keys := make(map[string]struct{}, len(pw)+len(ui))
	for k := range pw {
		keys[k] = struct{}{}
	}
	for k := range ui {
		keys[k] = struct{}{}
	}
	out := make([]ScenarioMapping, 0, len(keys))
	for k := range keys {
		out = append(out, ScenarioMapping{Spec: k, Scenario: k})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec < out[j].Spec })
	return out
}

// Compose joins Sources into ordered Comparisons via the mapping.
// The order of comparisons follows the mapping order.
func Compose(src Sources) []Comparison {
	out := make([]Comparison, 0, len(src.Mappings))
	for _, m := range src.Mappings {
		pw := lookupPlaywright(src.PlaywrightResults, m.Spec)
		ui := lookupUIAuto(src.UIAutoResults, m.Scenario)
		out = append(out, Diff(m.Spec, pw, ui))
	}
	return out
}

func lookupPlaywright(in map[string]PlaywrightSpec, spec string) PlaywrightSpec {
	if v, ok := in[spec]; ok {
		return v
	}
	if v, ok := in[normalizeSpecKey(spec)]; ok {
		return v
	}
	return PlaywrightSpec{Spec: spec, Result: ResultUnknown}
}

func lookupUIAuto(in map[string]UIAutoScenario, scenario string) UIAutoScenario {
	if v, ok := in[scenario]; ok {
		return v
	}
	return UIAutoScenario{Scenario: scenario, Result: ResultUnknown, TierUsed: TierUnknown}
}
