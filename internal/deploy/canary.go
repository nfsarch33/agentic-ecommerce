package deploy

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrCanaryNotFound  = errors.New("canary: deployment not found")
	ErrThresholdExceeded = errors.New("canary: error rate threshold exceeded")
)

type CanaryMetrics struct {
	DeployID    string
	ErrorRate   float64
	Latency     time.Duration
	TrafficPct  int
}

type MetricThreshold struct {
	MaxErrorRate float64
}

type CanaryDeployment struct {
	ID         string
	TrafficPct int
	Stable     bool
	metrics    CanaryMetrics
}

type CanaryManager struct {
	mu         sync.Mutex
	deployments map[string]*CanaryDeployment
	threshold  MetricThreshold
	seq        int
}

func NewCanaryManager(threshold MetricThreshold) *CanaryManager {
	return &CanaryManager{
		deployments: make(map[string]*CanaryDeployment),
		threshold:   threshold,
	}
}

func (c *CanaryManager) CreateDeployment() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	id := fmt.Sprintf("canary-%d", c.seq)
	c.deployments[id] = &CanaryDeployment{ID: id, TrafficPct: 0, Stable: true}
	return id
}

func (c *CanaryManager) Split(_ interface{}, deployID string, percentage int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.deployments[deployID]
	if !ok {
		return ErrCanaryNotFound
	}
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}
	d.TrafficPct = percentage
	return nil
}

func (c *CanaryManager) Monitor(_ interface{}, deployID string, _ time.Duration) CanaryMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.deployments[deployID]
	if !ok {
		return CanaryMetrics{DeployID: deployID}
	}
	m := d.metrics
	m.DeployID = deployID
	m.TrafficPct = d.TrafficPct
	return m
}

func (c *CanaryManager) SetMetrics(deployID string, m CanaryMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d, ok := c.deployments[deployID]; ok {
		d.metrics = m
	}
}

func (c *CanaryManager) Promote(_ interface{}, deployID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.deployments[deployID]
	if !ok {
		return ErrCanaryNotFound
	}
	if c.threshold.MaxErrorRate > 0 && d.metrics.ErrorRate > c.threshold.MaxErrorRate {
		return ErrThresholdExceeded
	}
	d.TrafficPct = 100
	return nil
}

func (c *CanaryManager) Abort(_ interface{}, deployID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.deployments[deployID]
	if !ok {
		return ErrCanaryNotFound
	}
	d.TrafficPct = 0
	d.Stable = false
	return nil
}

func (c *CanaryManager) TrafficPct(deployID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d, ok := c.deployments[deployID]; ok {
		return d.TrafficPct
	}
	return -1
}
