package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"reflect"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
)

// File scope: v2.6.1 cmd/* DI refactor coverage. Validates the new
// buildWorkerDeps + registerWorkflowsAndActivities + mainImpl pattern
// without standing up a real Temporal frontend. Tests use a recording
// fakeRegistry for the worker contract and a stub temporalDialer that
// returns typed errors for the dial-failure branch.

// fakeRegistry records every Register* call so the test can assert
// the wiring contract without standing up a real temporal worker.
type fakeRegistry struct {
	workflows  []any
	activities []registeredActivity
}

type registeredActivity struct {
	name string
	fn   any
}

func (f *fakeRegistry) RegisterWorkflow(w any) { f.workflows = append(f.workflows, w) }
func (f *fakeRegistry) RegisterActivityWithOptions(a any, opts activity.RegisterOptions) {
	f.activities = append(f.activities, registeredActivity{name: opts.Name, fn: a})
}

// TestRegisterWorkflowsAndActivities pins the wiring contract: 5
// workflows + 24 activities covering compliance, content, media,
// sourcing, and tenant-onboarding surfaces. Regression alarm if a
// future refactor drops any activity from the registry.
func TestRegisterWorkflowsAndActivities(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_URL", "")
	t.Setenv("ECOMMERCE_AI_BRIDGE_URL", "")
	t.Setenv("MINIMAX_BRIDGE_URL", "")

	deps, err := buildWorkerDeps(context.Background(), discardLogger(), agentScheduleConfig{TaskQueue: "ec-workflows"})
	if err != nil {
		t.Fatalf("buildWorkerDeps: %v", err)
	}
	t.Cleanup(deps.RepoCleanup)

	reg := &fakeRegistry{}
	registerWorkflowsAndActivities(reg, deps)

	if got, want := len(reg.workflows), 5; got != want {
		t.Fatalf("workflows registered = %d, want %d", got, want)
	}
	if got, want := len(reg.activities), 24; got != want {
		t.Fatalf("activities registered = %d, want %d", got, want)
	}

	wantNames := map[string]bool{
		"product_publish.check_compliance":       false,
		"product_publish.validate_media":         false,
		"product_publish.publish_to_woocommerce": false,
		"product_publish.record_workflow_event":  false,
		"content_generation.fact_check":          false,
		"media_processing.source":                false,
		"sourcing.search_suppliers":              false,
		"tenant.validate_registration":           false,
		"tenant.provision_record":                false,
		"tenant.seed_default_plan":               false,
		"tenant.issue_welcome_notification":      false,
		"tenant.register_default_plugins":        false,
		"tenant.rollback_record":                 false,
	}
	for _, a := range reg.activities {
		if _, ok := wantNames[a.name]; ok {
			wantNames[a.name] = true
		}
	}
	for name, found := range wantNames {
		if !found {
			t.Errorf("activity %q missing from registry; got %v", name, registryNames(reg))
		}
	}
}

func registryNames(reg *fakeRegistry) []string {
	names := make([]string, 0, len(reg.activities))
	for _, a := range reg.activities {
		names = append(names, a.name)
	}
	return names
}

// TestBuildWorkerDeps_FillsTaskQueueFromEnv ensures the task queue
// override flows from getenv into deps.TaskQueue rather than being
// hard-coded.
func TestBuildWorkerDeps_FillsTaskQueueFromEnv(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_URL", "")
	t.Setenv("ECOMMERCE_TEMPORAL_TASK_QUEUE", "custom-queue")
	t.Setenv("ECOMMERCE_AI_BRIDGE_URL", "")
	t.Setenv("MINIMAX_BRIDGE_URL", "")

	deps, err := buildWorkerDeps(context.Background(), discardLogger(), agentScheduleConfig{})
	if err != nil {
		t.Fatalf("buildWorkerDeps: %v", err)
	}
	t.Cleanup(deps.RepoCleanup)
	if deps.TaskQueue != "custom-queue" {
		t.Fatalf("TaskQueue = %q, want custom-queue", deps.TaskQueue)
	}
	if deps.Logger == nil || deps.Repo == nil || deps.PublishActivities == nil ||
		deps.ContentActivities == nil || deps.MediaActivities == nil ||
		deps.SourcingActivities == nil || deps.OnboardingActivities == nil ||
		deps.RepoCleanup == nil {
		t.Errorf("deps fields must be wired: %+v", deps)
	}
}

// TestBuildWorkerDeps_PropagatesRepoError surfaces the postgres pool
// failure when ECOMMERCE_DB_URL points at an invalid DSN.
func TestBuildWorkerDeps_PropagatesRepoError(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_URL", "://not-a-valid-dsn")

	_, err := buildWorkerDeps(context.Background(), discardLogger(), agentScheduleConfig{})
	if err == nil {
		t.Fatal("expected error from invalid DSN")
	}
	if !errors.Is(err, err) || err.Error() == "" {
		t.Errorf("err must be non-empty: %v", err)
	}
}

// TestMainImpl_DialFailureReturnsOne drives mainImpl with a stub
// dialer that returns a typed error, validating the dial-failure
// branch.
func TestMainImpl_DialFailureReturnsOne(t *testing.T) {
	t.Setenv("ECOMMERCE_AGENT_SCHEDULES_ENABLED", "false")
	dialErr := errors.New("connection refused")
	dial := func(opts client.Options) (client.Client, error) {
		if opts.HostPort == "" {
			t.Errorf("HostPort empty")
		}
		return nil, dialErr
	}
	var buf bytes.Buffer
	got := mainImpl(context.Background(), &buf, os.Getenv, dial)
	if got != 1 {
		t.Fatalf("expected exit 1, got %d", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("temporal_worker.client")) {
		t.Errorf("expected temporal_worker.client log, got %s", buf.String())
	}
}

// TestMainImpl_ScheduleConfigErrorReturnsOne verifies the agent
// schedule config validation branch exits with 1 when the env vars
// are malformed.
func TestMainImpl_ScheduleConfigErrorReturnsOne(t *testing.T) {
	t.Setenv("ECOMMERCE_AGENT_SCHEDULES_DEFAULT_INTERVAL", "0s")
	dial := func(client.Options) (client.Client, error) {
		t.Fatal("dial should not be called when config is invalid")
		return nil, nil
	}
	var buf bytes.Buffer
	got := mainImpl(context.Background(), &buf, os.Getenv, dial)
	if got != 1 {
		t.Fatalf("expected exit 1, got %d", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("temporal_worker.agent_schedules_config")) {
		t.Errorf("expected schedule config log, got %s", buf.String())
	}
}

// TestNewContentGenerationActivitiesFromEnvWithBridge exercises the
// configured-bridge branch (previously 0% covered) by pointing the
// bridge URL at a closed loopback so the minimax client returns an
// error and the function logs + falls back without panicking.
func TestNewContentGenerationActivitiesFromEnvWithBridge(t *testing.T) {
	t.Setenv("ECOMMERCE_AI_BRIDGE_URL", "http://127.0.0.1:1")
	t.Setenv("ECOMMERCE_AI_MODEL", "test-model")
	t.Setenv("ECOMMERCE_AI_BRIDGE_TEST_MODE", "true")

	activities := newContentGenerationActivitiesFromEnv(discardLogger())
	if reflect.ValueOf(activities).IsNil() {
		t.Fatal("activities should not be nil")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
