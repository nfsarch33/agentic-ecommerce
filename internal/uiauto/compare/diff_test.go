package compare

import (
	"strings"
	"testing"
)

func TestDiff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		spec      string
		pw        PlaywrightSpec
		ui        UIAutoScenario
		wantAgree bool
		wantNote  string
	}{
		{
			name:      "both pass clean",
			spec:      "home",
			pw:        PlaywrightSpec{Result: ResultPass, DurationMs: 100},
			ui:        UIAutoScenario{Result: ResultPass, DurationMs: 200, TierUsed: TierLight},
			wantAgree: true,
			wantNote:  "",
		},
		{
			name:      "both pass but uiauto self-healed",
			spec:      "home",
			pw:        PlaywrightSpec{Result: ResultPass, DurationMs: 100},
			ui:        UIAutoScenario{Result: ResultPass, DurationMs: 200, TierUsed: TierSmart, SelfHealEvents: []SelfHealEvent{{Step: 1, Reason: "drift"}}},
			wantAgree: true,
			wantNote:  "self-healed 1 step(s)",
		},
		{
			name:      "playwright pass, uiauto fail",
			spec:      "checkout",
			pw:        PlaywrightSpec{Result: ResultPass, DurationMs: 800},
			ui:        UIAutoScenario{Result: ResultFail, DurationMs: 400, Error: "selector not found"},
			wantAgree: false,
			wantNote:  "uiauto failed",
		},
		{
			name:      "uiauto pass, playwright fail",
			spec:      "admin-agents",
			pw:        PlaywrightSpec{Result: ResultFail, DurationMs: 5000, Error: "heading not found"},
			ui:        UIAutoScenario{Result: ResultPass, DurationMs: 600, TierUsed: TierSmart},
			wantAgree: false,
			wantNote:  "uiauto passed via tier=smart",
		},
		{
			name:      "uiauto unknown",
			spec:      "x",
			pw:        PlaywrightSpec{Result: ResultPass},
			ui:        UIAutoScenario{Result: ResultUnknown},
			wantAgree: false,
			wantNote:  "uiauto result missing",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Diff(tc.spec, tc.pw, tc.ui)
			if got.Agreement != tc.wantAgree {
				t.Errorf("agreement got=%v want=%v", got.Agreement, tc.wantAgree)
			}
			if tc.wantNote != "" && !strings.Contains(got.Notes, tc.wantNote) {
				t.Errorf("notes %q does not contain %q", got.Notes, tc.wantNote)
			}
			if tc.wantNote == "" && got.Notes != "" {
				t.Errorf("expected empty notes, got %q", got.Notes)
			}
		})
	}
}

func TestSummarize(t *testing.T) {
	t.Parallel()
	items := []Comparison{
		{Playwright: PlaywrightSpec{Result: ResultPass}, UIAuto: UIAutoScenario{Result: ResultPass}, Agreement: true},
		{Playwright: PlaywrightSpec{Result: ResultFail}, UIAuto: UIAutoScenario{Result: ResultFail}, Agreement: true},
		{Playwright: PlaywrightSpec{Result: ResultPass}, UIAuto: UIAutoScenario{Result: ResultFail, SelfHealEvents: []SelfHealEvent{{Step: 1}, {Step: 2}}}, Agreement: false},
		{Playwright: PlaywrightSpec{Result: ResultFail}, UIAuto: UIAutoScenario{Result: ResultPass, SelfHealEvents: []SelfHealEvent{{Step: 1}}}, Agreement: false},
	}
	got := Summarize(items)
	want := Summary{Total: 4, Agreed: 2, Disagreed: 2, BothPass: 1, BothFail: 1, PlaywrightOnlyPass: 1, UIAutoOnlyPass: 1, SelfHealEvents: 3}
	if got != want {
		t.Errorf("summary mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	if got := truncate("hello world", 5); got != "he..." {
		t.Errorf("truncate(>3) got %q want %q", got, "he...")
	}
	if got := truncate("hello world", 100); got != "hello world" {
		t.Errorf("truncate(no change) got %q", got)
	}
	if got := truncate("hi", 1); got != "h" {
		t.Errorf("truncate(<4) got %q", got)
	}
}
