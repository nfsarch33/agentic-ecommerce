package sdk_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/pkg/marketplace/sdk"
)

type stubPlugin struct {
	manifest    sdk.Manifest
	installErr  error
	activateErr error
}

func (p *stubPlugin) Manifest() sdk.Manifest                       { return p.manifest }
func (p *stubPlugin) Install(_ context.Context, _ string) error    { return p.installErr }
func (p *stubPlugin) Activate(_ context.Context, _ string) error   { return p.activateErr }
func (p *stubPlugin) Deactivate(_ context.Context, _ string) error { return nil }
func (p *stubPlugin) Uninstall(_ context.Context, _ string) error  { return nil }

func validManifest() sdk.Manifest {
	return sdk.Manifest{
		Slug:    "hello",
		Name:    "Hello Plugin",
		Version: "1.0.0",
		Vendor:  "Example Vendor",
	}
}

func TestSmokeCheckExercisesFullLifecycle(t *testing.T) {
	t.Parallel()
	plugin := &stubPlugin{manifest: validManifest()}
	sb := sdk.NewTestSandbox(t, plugin.manifest)

	sb.SmokeCheck(context.Background(), plugin)

	if got := sb.HooksRecorded(); got < 2 {
		t.Fatalf("expected at least 2 hook records (activate + deactivate), got %d", got)
	}
}

func TestInstallNilPluginReturnsError(t *testing.T) {
	t.Parallel()
	sb := sdk.NewTestSandbox(t, validManifest())
	_, err := sb.Install(context.Background(), nil)
	if err == nil {
		t.Fatal("expected nil-plugin Install to fail")
	}
}

func TestActivateThenDeactivateChangesState(t *testing.T) {
	t.Parallel()
	plugin := &stubPlugin{manifest: validManifest()}
	sb := sdk.NewTestSandbox(t, plugin.manifest)
	ctx := context.Background()
	if _, err := sb.Install(ctx, plugin); err != nil {
		t.Fatalf("install: %v", err)
	}
	row, err := sb.Activate(ctx, plugin)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if row.State != sdk.StateActive {
		t.Fatalf("expected active, got %s", row.State)
	}
	row, err = sb.Deactivate(ctx, plugin)
	if err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if row.State != sdk.StateDeactivated {
		t.Fatalf("expected deactivated, got %s", row.State)
	}
}

func TestPluginInstallHookErrorPropagates(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("install boom")
	plugin := &stubPlugin{manifest: validManifest(), installErr: wantErr}
	sb := sdk.NewTestSandbox(t, plugin.manifest)
	_, err := sb.Install(context.Background(), plugin)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected install error to wrap %v, got %v", wantErr, err)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	sb := sdk.NewTestSandbox(t, validManifest())
	sb.SetSettings(map[string]any{"feature_flag": true, "tier": "gold"})
	got := sb.Settings()
	if got["feature_flag"] != true {
		t.Fatalf("expected feature_flag=true, got %v", got["feature_flag"])
	}
	if got["tier"] != "gold" {
		t.Fatalf("expected tier=gold, got %v", got["tier"])
	}
}

func TestPermissionAndStateConstantsExposed(t *testing.T) {
	t.Parallel()
	if sdk.PermissionEmitEvents == "" {
		t.Fatalf("PermissionEmitEvents must be exported")
	}
	if sdk.StateActive == "" {
		t.Fatalf("StateActive must be exported")
	}
	if !sdk.IsValidSlug("hello-plugin") {
		t.Fatalf("IsValidSlug should accept kebab-case")
	}
	if !sdk.IsValidSemver("1.2.3") {
		t.Fatalf("IsValidSemver should accept MAJOR.MINOR.PATCH")
	}
}

func TestEventNamesDefensiveCopy(t *testing.T) {
	t.Parallel()
	m := sdk.Manifest{EventSubscriptions: []sdk.EventName{"order.placed"}}
	got := sdk.EventNames(m)
	got[0] = "mutated"
	if m.EventSubscriptions[0] == "mutated" {
		t.Fatalf("EventNames must defensive-copy")
	}
}

func TestTenantIDFromOption(t *testing.T) {
	t.Parallel()
	sb := sdk.NewTestSandbox(t, validManifest(), sdk.WithTenant("tenant-x"))
	if got := sb.TenantID(); got != "tenant-x" {
		t.Fatalf("expected tenant-x, got %s", got)
	}
}

func TestHookTimeoutIsPositive(t *testing.T) {
	t.Parallel()
	sb := sdk.NewTestSandbox(t, validManifest())
	if d := sb.HookTimeout(); d <= 0 {
		t.Fatalf("expected positive hook timeout, got %v", d)
	}
}
