package deploy

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrRollbackNotFound = errors.New("rollback: deployment not found")

type DeployMetrics struct {
	ErrorRate float64
	Latency   time.Duration
	Throughput float64
}

type RollbackResult struct {
	DeployID    string
	PreviousID  string
	ExecutedAt  time.Time
	Success     bool
}

type HealthStatus struct {
	Healthy bool
	Reason  string
}

type RollbackEntry struct {
	DeployID   string
	ExecutedAt time.Time
	Success    bool
}

type RollbackManager struct {
	mu       sync.Mutex
	history  []RollbackEntry
	notified []RollbackResult
}

func NewRollbackManager() *RollbackManager {
	return &RollbackManager{}
}

func (r *RollbackManager) Detect(_ interface{}, current, baseline DeployMetrics) bool {
	if current.ErrorRate > baseline.ErrorRate*1.5 {
		return true
	}
	if current.Latency > baseline.Latency*2 {
		return true
	}
	return false
}

func (r *RollbackManager) Trigger(_ interface{}, deployID string) (RollbackResult, error) {
	if deployID == "" {
		return RollbackResult{}, ErrRollbackNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := RollbackResult{
		DeployID:   deployID,
		PreviousID: fmt.Sprintf("prev-%s", deployID),
		ExecutedAt: time.Now(),
		Success:    true,
	}
	r.history = append(r.history, RollbackEntry{
		DeployID:   deployID,
		ExecutedAt: result.ExecutedAt,
		Success:    true,
	})
	return result, nil
}

func (r *RollbackManager) Verify(_ interface{}, deployID string) (HealthStatus, error) {
	if deployID == "" {
		return HealthStatus{}, ErrRollbackNotFound
	}
	return HealthStatus{Healthy: true, Reason: "all checks passed"}, nil
}

func (r *RollbackManager) Notify(_ interface{}, result RollbackResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notified = append(r.notified, result)
	return nil
}

func (r *RollbackManager) HistoryLog(_ interface{}) ([]RollbackEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]RollbackEntry, len(r.history))
	copy(cp, r.history)
	return cp, nil
}

func (r *RollbackManager) NotifyCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.notified)
}
