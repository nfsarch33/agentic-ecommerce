package hello_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/pkg/marketplace/sdk"
	"github.com/nfsarch33/helixon-ec/pkg/marketplace/sdk/example/hello"
)

func TestPluginManifestIsValid(t *testing.T) {
	t.Parallel()
	p := hello.New()
	m := p.Manifest()
	if !sdk.IsValidSlug(m.Slug) {
		t.Fatalf("manifest slug %q must be kebab-case", m.Slug)
	}
	if !sdk.IsValidSemver(m.Version) {
		t.Fatalf("manifest version %q must be MAJOR.MINOR.PATCH", m.Version)
	}
	if len(m.EventSubscriptions) == 0 {
		t.Fatalf("hello plugin should subscribe to at least one event for the example")
	}
}

func TestSmokeCheckPasses(t *testing.T) {
	t.Parallel()
	p := hello.New()
	sb := sdk.NewTestSandbox(t, p.Manifest(), sdk.WithTenant("tenant-demo"))
	sb.SmokeCheck(context.Background(), p)

	history := p.History()
	if len(history) != 4 {
		t.Fatalf("expected 4 history entries, got %d: %v", len(history), history)
	}
	if !strings.HasPrefix(history[0], "install:tenant-demo") {
		t.Fatalf("expected install:tenant-demo as first event, got %q", history[0])
	}
	if !strings.HasPrefix(history[1], "activate:tenant-demo") {
		t.Fatalf("expected activate:tenant-demo as second event, got %q", history[1])
	}
	if !strings.HasPrefix(history[2], "deactivate:tenant-demo") {
		t.Fatalf("expected deactivate:tenant-demo as third event, got %q", history[2])
	}
	if !strings.HasPrefix(history[3], "uninstall:tenant-demo") {
		t.Fatalf("expected uninstall:tenant-demo as fourth event, got %q", history[3])
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	settings := hello.FromSettings(map[string]any{"greeting": "g'day"})
	if settings.Greeting != "g'day" {
		t.Fatalf("expected greeting=g'day, got %q", settings.Greeting)
	}

	missing := hello.FromSettings(map[string]any{"unrelated": 42})
	if missing.Greeting != "" {
		t.Fatalf("expected zero greeting when missing, got %q", missing.Greeting)
	}
}
