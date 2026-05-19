package deploy_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/deploy"
)

func TestRollback_DetectIdentifiesDegradation(t *testing.T) {
	t.Parallel()
	m := deploy.NewRollbackManager()
	baseline := deploy.DeployMetrics{ErrorRate: 0.01, Latency: 100 * time.Millisecond}
	current := deploy.DeployMetrics{ErrorRate: 0.05, Latency: 100 * time.Millisecond}
	if !m.Detect(nil, current, baseline) {
		t.Fatal("expected degradation detected for high error rate")
	}
}

func TestRollback_NoDegradationReturnsFalse(t *testing.T) {
	t.Parallel()
	m := deploy.NewRollbackManager()
	baseline := deploy.DeployMetrics{ErrorRate: 0.05, Latency: 100 * time.Millisecond}
	current := deploy.DeployMetrics{ErrorRate: 0.04, Latency: 90 * time.Millisecond}
	if m.Detect(nil, current, baseline) {
		t.Fatal("expected no degradation detected")
	}
}

func TestRollback_TriggerExecutesRollback(t *testing.T) {
	t.Parallel()
	m := deploy.NewRollbackManager()
	result, err := m.Trigger(nil, "deploy-123")
	if err != nil {
		t.Fatalf("trigger failed: %v", err)
	}
	if !result.Success {
		t.Fatal("expected rollback success")
	}
	if result.DeployID != "deploy-123" {
		t.Fatalf("expected deploy-123, got %s", result.DeployID)
	}
}

func TestRollback_VerifyConfirmsHealth(t *testing.T) {
	t.Parallel()
	m := deploy.NewRollbackManager()
	health, err := m.Verify(nil, "deploy-123")
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if !health.Healthy {
		t.Fatal("expected healthy after rollback")
	}
}

func TestRollback_NotifySendsAlert(t *testing.T) {
	t.Parallel()
	m := deploy.NewRollbackManager()
	result := deploy.RollbackResult{DeployID: "d1", Success: true}
	m.Notify(nil, result)
	if m.NotifyCount() != 1 {
		t.Fatalf("expected 1 notification, got %d", m.NotifyCount())
	}
}

func TestRollback_HistoryLogsEntry(t *testing.T) {
	t.Parallel()
	m := deploy.NewRollbackManager()
	m.Trigger(nil, "d1")
	m.Trigger(nil, "d2")
	history, err := m.HistoryLog(nil)
	if err != nil {
		t.Fatalf("history log failed: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
}
