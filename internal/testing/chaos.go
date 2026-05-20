package testing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrExperimentNotFound = errors.New("chaos: experiment not found")
	ErrInvalidTarget      = errors.New("chaos: invalid target")
)

type ExperimentID string

type Fault struct {
	Type     string
	Target   string
	Duration time.Duration
	Delay    time.Duration
}

type experiment struct {
	id      ExperimentID
	fault   Fault
	active  bool
	startAt time.Time
}

type ChaosEngine struct {
	mu          sync.Mutex
	experiments map[ExperimentID]*experiment
	seq         int
}

func NewChaosEngine() *ChaosEngine {
	return &ChaosEngine{experiments: make(map[ExperimentID]*experiment)}
}

func (c *ChaosEngine) Inject(_ context.Context, fault Fault) (ExperimentID, error) {
	if fault.Target == "" {
		return "", ErrInvalidTarget
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	id := ExperimentID(fmt.Sprintf("exp-%d", c.seq))
	c.experiments[id] = &experiment{
		id:      id,
		fault:   fault,
		active:  true,
		startAt: time.Now(),
	}
	return id, nil
}

func NetworkPartition(_ context.Context, target string, duration time.Duration) Fault {
	return Fault{Type: "network_partition", Target: target, Duration: duration}
}

func LatencySpike(_ context.Context, target string, delay time.Duration) Fault {
	return Fault{Type: "latency_spike", Target: target, Delay: delay}
}

func (c *ChaosEngine) Recovery(_ context.Context, id ExperimentID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.experiments[id]
	if !ok {
		return ErrExperimentNotFound
	}
	exp.active = false
	return nil
}

func (c *ChaosEngine) IsActive(id ExperimentID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.experiments[id]
	return ok && exp.active
}

func (c *ChaosEngine) GetFault(id ExperimentID) (Fault, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.experiments[id]
	if !ok {
		return Fault{}, false
	}
	return exp.fault, true
}
