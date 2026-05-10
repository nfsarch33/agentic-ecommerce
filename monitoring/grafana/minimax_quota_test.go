package grafana_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMinimaxQuotaDashboardValidJSON(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dashPath := filepath.Join(filepath.Dir(thisFile), "minimax-quota.json")

	data, err := os.ReadFile(dashPath)
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}

	var dash map[string]any
	if err := json.Unmarshal(data, &dash); err != nil {
		t.Fatalf("parse dashboard JSON: %v", err)
	}

	panels, ok := dash["panels"].([]any)
	if !ok {
		t.Fatal("missing panels array")
	}
	if len(panels) != 5 {
		t.Fatalf("panel count = %d, want 5", len(panels))
	}

	expectedTitles := map[string]bool{
		"MiniMax Request Rate per Key":              false,
		"MiniMax Failover Timeline":                 false,
		"MiniMax Quota Status (Cooldown Remaining)": false,
		"MiniMax Request Latency (p50/p95/p99)":     false,
		"MiniMax Active Key Indicator":              false,
	}
	for _, p := range panels {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		title, _ := pm["title"].(string)
		if _, exists := expectedTitles[title]; exists {
			expectedTitles[title] = true
		}
	}
	for title, found := range expectedTitles {
		if !found {
			t.Errorf("missing expected panel: %s", title)
		}
	}
}
