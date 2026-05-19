package inventory_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/inventory"
)

func item(sku string, qty int, version int) inventory.Item {
	return inventory.Item{SKU: sku, Quantity: qty, Version: version}
}

func TestReconcile_AllAdditions(t *testing.T) {
	t.Parallel()
	engine := inventory.NewSyncEngine()
	plan := engine.Reconcile(nil, []inventory.Item{item("A", 10, 1), item("B", 5, 1)})
	if len(plan.Additions) != 2 {
		t.Fatalf("expected 2 additions, got %d", len(plan.Additions))
	}
	if len(plan.Removals) != 0 || len(plan.Updates) != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("unexpected plan state: %+v", plan)
	}
}

func TestReconcile_AllRemovals(t *testing.T) {
	t.Parallel()
	engine := inventory.NewSyncEngine()
	plan := engine.Reconcile([]inventory.Item{item("A", 10, 1)}, nil)
	if len(plan.Removals) != 1 {
		t.Fatalf("expected 1 removal, got %d", len(plan.Removals))
	}
}

func TestReconcile_Updates(t *testing.T) {
	t.Parallel()
	engine := inventory.NewSyncEngine()
	local := []inventory.Item{item("A", 10, 1)}
	remote := []inventory.Item{item("A", 20, 2)}
	plan := engine.Reconcile(local, remote)
	if len(plan.Updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(plan.Updates))
	}
	if plan.Updates[0].Remote.Quantity != 20 {
		t.Fatalf("wrong updated qty: %d", plan.Updates[0].Remote.Quantity)
	}
}

func TestReconcile_Conflicts(t *testing.T) {
	t.Parallel()
	engine := inventory.NewSyncEngine()
	// same SKU, same version but different quantities = conflict
	local := []inventory.Item{item("A", 10, 1)}
	remote := []inventory.Item{item("A", 99, 1)}
	plan := engine.Reconcile(local, remote)
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(plan.Conflicts))
	}
}

func TestReconcile_EmptyBothSides(t *testing.T) {
	t.Parallel()
	engine := inventory.NewSyncEngine()
	plan := engine.Reconcile(nil, nil)
	if len(plan.Additions)+len(plan.Removals)+len(plan.Updates)+len(plan.Conflicts) != 0 {
		t.Fatalf("expected empty plan, got %+v", plan)
	}
}

func TestReconcile_NoChange(t *testing.T) {
	t.Parallel()
	engine := inventory.NewSyncEngine()
	items := []inventory.Item{item("A", 10, 1), item("B", 5, 2)}
	plan := engine.Reconcile(items, items)
	if len(plan.Additions)+len(plan.Removals)+len(plan.Updates)+len(plan.Conflicts) != 0 {
		t.Fatalf("expected no-op plan, got %+v", plan)
	}
}
