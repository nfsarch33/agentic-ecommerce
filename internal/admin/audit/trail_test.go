package audit_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/admin/audit"
)

func TestLog_StoresEntries(t *testing.T) {
	t.Parallel()
	trail := audit.NewAuditLogger()
	trail.Log("alice", "update", "order/123", map[string]any{"field": "status"})
	trail.Log("bob", "delete", "product/456", nil)

	entries := trail.Query(audit.AuditFilter{})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestQuery_ByActor(t *testing.T) {
	t.Parallel()
	trail := audit.NewAuditLogger()
	trail.Log("alice", "update", "order/1", nil)
	trail.Log("bob", "create", "order/2", nil)
	trail.Log("alice", "delete", "order/3", nil)

	entries := trail.Query(audit.AuditFilter{Actor: "alice"})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for alice, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Actor != "alice" {
			t.Fatalf("unexpected actor: %s", e.Actor)
		}
	}
}

func TestQuery_ByTimeRange(t *testing.T) {
	t.Parallel()
	trail := audit.NewAuditLogger()

	before := time.Now().Add(-2 * time.Hour)
	after := time.Now().Add(-1 * time.Hour)

	trail.LogAt("alice", "old_action", "r/1", nil, before)
	trail.LogAt("alice", "new_action", "r/2", nil, time.Now())

	entries := trail.Query(audit.AuditFilter{Since: after})
	if len(entries) != 1 {
		t.Fatalf("expected 1 recent entry, got %d", len(entries))
	}
	if entries[0].Action != "new_action" {
		t.Fatalf("wrong action: %s", entries[0].Action)
	}
}

func TestImmutability_NoDeleteOrUpdate(t *testing.T) {
	t.Parallel()
	trail := audit.NewAuditLogger()
	trail.Log("alice", "create", "order/1", nil)
	before := trail.Query(audit.AuditFilter{})
	// Attempting to get a mutable reference shouldn't allow modification
	before[0].Action = "tampered"
	after := trail.Query(audit.AuditFilter{})
	if after[0].Action == "tampered" {
		t.Fatal("audit trail entries should be immutable copies")
	}
}

func TestQuery_ByResource(t *testing.T) {
	t.Parallel()
	trail := audit.NewAuditLogger()
	trail.Log("alice", "read", "order/1", nil)
	trail.Log("bob", "read", "product/5", nil)
	trail.Log("alice", "write", "order/1", nil)

	entries := trail.Query(audit.AuditFilter{Resource: "order/1"})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for order/1, got %d", len(entries))
	}
}

func TestLog_EmptyTrail(t *testing.T) {
	t.Parallel()
	trail := audit.NewAuditLogger()
	entries := trail.Query(audit.AuditFilter{})
	if len(entries) != 0 {
		t.Fatalf("expected empty trail, got %d", len(entries))
	}
}
