package deploy_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/deploy"
)

func TestPipeline_BuildProducesArtifact(t *testing.T) {
	t.Parallel()
	p := deploy.NewPipeline()
	a, err := p.Build(nil, deploy.BuildConfig{Name: "api", Version: "v1.0"})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if a.ID == "" {
		t.Fatal("expected non-empty artifact ID")
	}
}

func TestPipeline_TestValidatesArtifact(t *testing.T) {
	t.Parallel()
	p := deploy.NewPipeline()
	a, _ := p.Build(nil, deploy.BuildConfig{Name: "api", Version: "v1"})
	report, err := p.Test(nil, a)
	if err != nil {
		t.Fatalf("test failed: %v", err)
	}
	if !report.Passed_ {
		t.Fatal("expected tests to pass")
	}
}

func TestPipeline_StageDeploysToEnv(t *testing.T) {
	t.Parallel()
	p := deploy.NewPipeline()
	a, _ := p.Build(nil, deploy.BuildConfig{Name: "api", Version: "v1"})
	d, err := p.Stage(nil, a, "staging")
	if err != nil {
		t.Fatalf("stage failed: %v", err)
	}
	if d.Env != "staging" {
		t.Fatalf("expected staging env, got %s", d.Env)
	}
}

func TestPipeline_PromoteMoves(t *testing.T) {
	t.Parallel()
	p := deploy.NewPipeline()
	a, _ := p.Build(nil, deploy.BuildConfig{Name: "api", Version: "v1"})
	d, _ := p.Stage(nil, a, "staging")
	if err := p.Promote(nil, d.ID, "production"); err != nil {
		t.Fatalf("promote failed: %v", err)
	}
}

func TestPipeline_RollbackReverts(t *testing.T) {
	t.Parallel()
	p := deploy.NewPipeline()
	a, _ := p.Build(nil, deploy.BuildConfig{Name: "api", Version: "v1"})
	d1, _ := p.Stage(nil, a, "prod")
	a2, _ := p.Build(nil, deploy.BuildConfig{Name: "api", Version: "v2"})
	d2, _ := p.Stage(nil, a2, "prod")
	if err := p.Rollback(nil, d2.ID); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	_ = d1
}

func TestPipeline_FailedBuildReturnsError(t *testing.T) {
	t.Parallel()
	p := deploy.NewPipeline()
	_, err := p.Build(nil, deploy.BuildConfig{})
	if err != deploy.ErrBuildFailed {
		t.Fatalf("expected ErrBuildFailed, got %v", err)
	}
}

func TestPipeline_FailedTestBlocksStage(t *testing.T) {
	t.Parallel()
	p := deploy.NewPipeline()
	// Empty artifact ID will fail test
	_, err := p.Stage(nil, deploy.Artifact{}, "staging")
	if err != deploy.ErrTestsFailed {
		t.Fatalf("expected ErrTestsFailed, got %v", err)
	}
}
