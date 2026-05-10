package grafana

import (
	"encoding/json"
	"os"
	"testing"
)

func TestUIAutoComparisonDashboard_ValidJSON(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("uiauto-comparison.json")
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	var dash map[string]any
	if err := json.Unmarshal(raw, &dash); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	wantUID := "uiauto-comparison-v4140"
	if uid, ok := dash["uid"].(string); !ok || uid != wantUID {
		t.Errorf("uid got %q want %q", dash["uid"], wantUID)
	}
	if schema, ok := dash["schemaVersion"].(float64); !ok || schema < 39 {
		t.Errorf("schemaVersion got %v want >= 39", dash["schemaVersion"])
	}
	panels, ok := dash["panels"].([]any)
	if !ok || len(panels) < 4 {
		t.Fatalf("expected >= 4 panels, got %d", len(panels))
	}
	expectedTitles := map[string]bool{
		"Accuracy Agreement Rate":                false,
		"Speed Comparison (avg ms per scenario)": false,
		"Accuracy per Tool per Scenario":         false,
		"Historical Agreement Rate Trend":        false,
	}
	for _, p := range panels {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		title, _ := pm["title"].(string)
		if _, found := expectedTitles[title]; found {
			expectedTitles[title] = true
		}
	}
	for title, found := range expectedTitles {
		if !found {
			t.Errorf("missing panel %q", title)
		}
	}
	tags, ok := dash["tags"].([]any)
	if !ok {
		t.Fatal("missing tags")
	}
	hasV4140 := false
	for _, tag := range tags {
		if s, ok := tag.(string); ok && s == "v4.14.0" {
			hasV4140 = true
		}
	}
	if !hasV4140 {
		t.Error("missing v4.14.0 tag")
	}
}
