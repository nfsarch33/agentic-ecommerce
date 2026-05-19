package deploy_test

import (
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/deploy"
)

func TestCanary_SplitRoutesPercentage(t *testing.T) {
	t.Parallel()
	m := deploy.NewCanaryManager(deploy.MetricThreshold{MaxErrorRate: 0.05})
	id := m.CreateDeployment()
	if err := m.Split(nil, id, 20); err != nil {
		t.Fatalf("split failed: %v", err)
	}
	if m.TrafficPct(id) != 20 {
		t.Fatalf("expected 20%% traffic, got %d", m.TrafficPct(id))
	}
}

func TestCanary_MonitorCollectsMetrics(t *testing.T) {
	t.Parallel()
	m := deploy.NewCanaryManager(deploy.MetricThreshold{})
	id := m.CreateDeployment()
	m.Split(nil, id, 10)
	metrics := m.Monitor(nil, id, 100*time.Millisecond)
	if metrics.DeployID != id {
		t.Fatalf("expected deploy id %s, got %s", id, metrics.DeployID)
	}
}

func TestCanary_PromoteShiftsAllTraffic(t *testing.T) {
	t.Parallel()
	m := deploy.NewCanaryManager(deploy.MetricThreshold{MaxErrorRate: 0.1})
	id := m.CreateDeployment()
	m.Split(nil, id, 50)
	if err := m.Promote(nil, id); err != nil {
		t.Fatalf("promote failed: %v", err)
	}
	if m.TrafficPct(id) != 100 {
		t.Fatalf("expected 100%% after promote, got %d%%", m.TrafficPct(id))
	}
}

func TestCanary_AbortRevertsTraffic(t *testing.T) {
	t.Parallel()
	m := deploy.NewCanaryManager(deploy.MetricThreshold{})
	id := m.CreateDeployment()
	m.Split(nil, id, 50)
	m.Abort(nil, id)
	if m.TrafficPct(id) != 0 {
		t.Fatalf("expected 0%% after abort, got %d%%", m.TrafficPct(id))
	}
}

func TestCanary_ThresholdTriggersAutoAbort(t *testing.T) {
	t.Parallel()
	m := deploy.NewCanaryManager(deploy.MetricThreshold{MaxErrorRate: 0.05})
	id := m.CreateDeployment()
	m.Split(nil, id, 10)
	m.SetMetrics(id, deploy.CanaryMetrics{ErrorRate: 0.10}) // exceeds threshold
	err := m.Promote(nil, id)
	if err != deploy.ErrThresholdExceeded {
		t.Fatalf("expected ErrThresholdExceeded, got %v", err)
	}
}

func TestCanary_ZeroPercentageAllowed(t *testing.T) {
	t.Parallel()
	m := deploy.NewCanaryManager(deploy.MetricThreshold{})
	id := m.CreateDeployment()
	if err := m.Split(nil, id, 0); err != nil {
		t.Fatalf("zero percentage not allowed: %v", err)
	}
}

func TestCanary_ConcurrentSplitSafe(t *testing.T) {
	t.Parallel()
	m := deploy.NewCanaryManager(deploy.MetricThreshold{})
	id := m.CreateDeployment()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(pct int) {
			defer wg.Done()
			m.Split(nil, id, pct%100)
		}(i)
	}
	wg.Wait()
}
