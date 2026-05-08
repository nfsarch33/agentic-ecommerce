package compare

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteReport_Roundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rep := Report{
		GeneratedAt: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		Mode:        "fixtures",
		Items: []Comparison{
			{
				Spec:       "home",
				Playwright: PlaywrightSpec{Spec: "e2e/home.spec.ts", Result: ResultPass, DurationMs: 100, Selectors: []string{"h1"}},
				UIAuto:     UIAutoScenario{Scenario: "home", Result: ResultPass, DurationMs: 200, TierUsed: TierLight, Selectors: []string{"h1"}},
				Agreement:  true,
			},
			{
				Spec:       "checkout",
				Playwright: PlaywrightSpec{Spec: "e2e/checkout.spec.ts", Result: ResultPass, DurationMs: 1500, Selectors: []string{"button[name=add]"}},
				UIAuto: UIAutoScenario{
					Scenario:       "checkout",
					Result:         ResultPass,
					DurationMs:     1800,
					TierUsed:       TierSmart,
					Selectors:      []string{"button[name=add]"},
					SelfHealEvents: []SelfHealEvent{{Step: 1, Reason: "drift", HealedFrom: "fingerprint", HealedTo: "structural", Tier: TierSmart}},
				},
				Agreement: true,
				Notes:     "both passed; uiauto self-healed 1 step(s) (tier=smart)",
			},
		},
	}
	rep.Summary = Summarize(rep.Items)
	jsonPath, mdPath, err := WriteReport(rep, dir)
	if err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	if filepath.Dir(jsonPath) != dir {
		t.Fatalf("json path %q outside %q", jsonPath, dir)
	}
	jsonRaw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read json: %v", err)
	}
	var roundTrip Report
	if err := json.Unmarshal(jsonRaw, &roundTrip); err != nil {
		t.Fatalf("decode round trip: %v", err)
	}
	if roundTrip.Mode != "fixtures" {
		t.Errorf("mode lost: got %q", roundTrip.Mode)
	}
	if roundTrip.Summary.SelfHealEvents != 1 {
		t.Errorf("selfheal lost: got %d", roundTrip.Summary.SelfHealEvents)
	}
	mdRaw, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read md: %v", err)
	}
	md := string(mdRaw)
	wantSubstrings := []string{
		"# uiauto vs Playwright comparison",
		"`fixtures`",
		"| Total scenarios | 2 |",
		"| `checkout` | pass (1500ms) | pass (1800ms) | smart | 1 | yes |",
		"### `checkout`",
		"step 1 (tier=smart): `fingerprint` -> `structural`",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(md, s) {
			t.Errorf("markdown missing %q", s)
		}
	}
}
