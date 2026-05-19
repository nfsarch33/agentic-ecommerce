package deploy

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrBuildFailed      = errors.New("pipeline: build failed")
	ErrTestsFailed      = errors.New("pipeline: tests failed")
	ErrDeployNotFound   = errors.New("pipeline: deployment not found")
	ErrNoHistory        = errors.New("pipeline: no previous deployment to rollback to")
)

type BuildConfig struct {
	Name    string
	Version string
	Env     map[string]string
}

type Artifact struct {
	ID      string
	Name    string
	Version string
	BuiltAt time.Time
}

type TestReport struct {
	Passed int
	Failed int
	Passed_ bool
}

type Deployment struct {
	ID         string
	ArtifactID string
	Env        string
	Status     string
	DeployedAt time.Time
}

type Pipeline struct {
	mu          sync.Mutex
	deployments map[string]Deployment
	history     map[string][]Deployment // env -> stack
	seq         int
}

func NewPipeline() *Pipeline {
	return &Pipeline{
		deployments: make(map[string]Deployment),
		history:     make(map[string][]Deployment),
	}
}

func (p *Pipeline) Build(_ interface{}, cfg BuildConfig) (Artifact, error) {
	if cfg.Name == "" {
		return Artifact{}, ErrBuildFailed
	}
	p.mu.Lock()
	p.seq++
	id := fmt.Sprintf("artifact-%d", p.seq)
	p.mu.Unlock()
	return Artifact{ID: id, Name: cfg.Name, Version: cfg.Version, BuiltAt: time.Now()}, nil
}

func (p *Pipeline) Test(_ interface{}, artifact Artifact) (TestReport, error) {
	if artifact.ID == "" {
		return TestReport{}, ErrTestsFailed
	}
	return TestReport{Passed: 10, Failed: 0, Passed_: true}, nil
}

func (p *Pipeline) Stage(_ interface{}, artifact Artifact, env string) (Deployment, error) {
	report, err := p.Test(nil, artifact)
	if err != nil || !report.Passed_ {
		return Deployment{}, ErrTestsFailed
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	id := fmt.Sprintf("deploy-%d", p.seq)
	d := Deployment{
		ID:         id,
		ArtifactID: artifact.ID,
		Env:        env,
		Status:     "staged",
		DeployedAt: time.Now(),
	}
	p.deployments[id] = d
	p.history[env] = append(p.history[env], d)
	return d, nil
}

func (p *Pipeline) Promote(_ interface{}, deployID string, targetEnv string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.deployments[deployID]
	if !ok {
		return ErrDeployNotFound
	}
	d.Env = targetEnv
	d.Status = "promoted"
	p.deployments[deployID] = d
	p.history[targetEnv] = append(p.history[targetEnv], d)
	return nil
}

func (p *Pipeline) Rollback(_ interface{}, deployID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.deployments[deployID]
	if !ok {
		return ErrDeployNotFound
	}
	stack := p.history[d.Env]
	if len(stack) < 2 {
		return ErrNoHistory
	}
	prev := stack[len(stack)-2]
	prev.Status = "rolled_back"
	p.deployments[deployID] = prev
	return nil
}
