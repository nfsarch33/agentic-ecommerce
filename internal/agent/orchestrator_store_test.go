package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInMemoryStoreListsRunsByAgentNewestFirstAndReturnsClones(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	older := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	newer := older.Add(time.Minute)
	runs := []Run{
		{ID: "pricing-old", AgentID: "pricing", State: RunSucceeded, Result: map[string]any{"sku": "OLD"}, CreatedAt: older},
		{ID: "sourcing-run", AgentID: "sourcing", State: RunSucceeded, Result: map[string]any{"sku": "SRC"}, CreatedAt: newer},
		{ID: "pricing-new", AgentID: "pricing", State: RunSucceeded, Result: map[string]any{"sku": "NEW"}, CreatedAt: newer},
	}
	for _, run := range runs {
		if err := store.CreateRun(context.Background(), run); err != nil {
			t.Fatalf("CreateRun(%s): %v", run.ID, err)
		}
	}

	listed, err := store.ListRunsByAgent(context.Background(), "pricing")
	if err != nil {
		t.Fatalf("ListRunsByAgent: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed = %+v, want two pricing runs", listed)
	}
	if listed[0].ID != "pricing-new" || listed[1].ID != "pricing-old" {
		t.Fatalf("listed order = [%s %s], want newest first", listed[0].ID, listed[1].ID)
	}

	listed[0].Result["sku"] = "MUTATED"
	stored, err := store.GetRun(context.Background(), "pricing-new")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.Result["sku"] != "NEW" {
		t.Fatalf("stored run was mutated through listed clone: %+v", stored.Result)
	}
}

func TestInMemoryStoreRejectsUpdatesForMissingRuns(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	err := store.UpdateRun(context.Background(), Run{ID: "missing", AgentID: "pricing"})
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("UpdateRun error = %v, want ErrRunNotFound", err)
	}
	if _, err := store.GetRun(context.Background(), "missing"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("GetRun error = %v, want ErrRunNotFound", err)
	}
}
