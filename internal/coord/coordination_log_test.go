package coord

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCoordinationLog_AppendAndRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test_coordination.ndjson")

	cl, err := NewCoordinationLog(path)
	if err != nil {
		t.Fatalf("NewCoordinationLog: %v", err)
	}
	defer cl.Close()

	entry := CoordinationLogEntry{
		Timestamp:    time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
		TenantID:     "t1",
		SKU:          "sku-1",
		Agents:       []string{"pricing", "fulfilment"},
		ConflictType: "price_vs_inventory",
		Resolution:   "pricing_wins",
		PolicyName:   "weighted_priority",
		ChosenAgent:  "pricing",
		RewardValue:  0.8,
	}

	if err := cl.Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := cl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if !strings.Contains(lines[0], `"chosen_agent":"pricing"`) {
		t.Fatalf("line does not contain chosen_agent: %s", lines[0])
	}
}

func TestCoordinationLog_MultipleAppends(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.ndjson")

	cl, err := NewCoordinationLog(path)
	if err != nil {
		t.Fatalf("NewCoordinationLog: %v", err)
	}
	defer cl.Close()

	for i := 0; i < 10; i++ {
		entry := CoordinationLogEntry{
			Timestamp:   time.Now(),
			TenantID:    "t1",
			SKU:         "sku-1",
			Agents:      []string{"pricing"},
			ChosenAgent: "pricing",
		}
		if err := cl.Append(entry); err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
	}
	if err := cl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 10 {
		t.Fatalf("lines = %d, want 10", len(lines))
	}
}

func TestInMemoryCoordinationLog_CapturesEntries(t *testing.T) {
	t.Parallel()
	log := &InMemoryCoordinationLog{}

	entry := CoordinationLogEntry{
		TenantID:    "t1",
		SKU:         "sku-1",
		ChosenAgent: "pricing",
	}
	if err := log.Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].ChosenAgent != "pricing" {
		t.Fatalf("chosen = %s, want pricing", entries[0].ChosenAgent)
	}
}
