// Package hello demonstrates the minimum viable plugin built against
// the public Plugin SDK. Copy this directory as a starting point for
// a real plugin: replace the manifest, fill in the lifecycle hooks
// with your business logic, and ship a hello_test.go that calls
// sdk.NewTestSandbox(t, manifest).SmokeCheck(...) to gate every PR.
//
// The example intentionally does not import any internal/* packages
// so plugin authors can verify the SDK is sufficient on its own.
package hello

import (
	"context"
	"fmt"
	"sync"

	"github.com/nfsarch33/helixon-ec/pkg/marketplace/sdk"
)

// Plugin is the example "hello" plugin. It records every lifecycle
// transition in a thread-safe in-memory log so callers can assert
// that hooks fired in the expected order. Real plugins would replace
// the log with side effects (HTTP calls, DB writes, etc.).
type Plugin struct {
	mu      sync.Mutex
	history []string
	greeter func(tenantID string) string
}

// New returns a Plugin with the default greeter. Pass a custom
// greeter to demonstrate dependency injection.
func New() *Plugin {
	return &Plugin{
		greeter: func(tenantID string) string {
			return fmt.Sprintf("hello, tenant %s", tenantID)
		},
	}
}

// Manifest returns the plugin's immutable identity. The manifest is
// what the host registry persists when the plugin is installed.
func (p *Plugin) Manifest() sdk.Manifest {
	return sdk.Manifest{
		Slug:        "hello",
		Name:        "Hello Plugin",
		Version:     "1.0.0",
		Vendor:      "Agentic Labs",
		Description: "Minimal SDK example demonstrating lifecycle hooks and one event subscription.",
		Category:    "developer-tools",
		EventSubscriptions: []sdk.EventName{
			"order.placed",
		},
		Permissions: []sdk.Permission{
			sdk.PermissionEmitEvents,
		},
	}
}

// Install runs once per (tenant, plugin) pair. Use this hook to
// provision per-tenant resources (DB rows, S3 prefixes, etc.).
func (p *Plugin) Install(_ context.Context, tenantID string) error {
	p.record("install:" + tenantID)
	return nil
}

// Activate transitions the plugin to "ready to receive events".
// Idempotent: calling twice is a no-op.
func (p *Plugin) Activate(_ context.Context, tenantID string) error {
	p.record("activate:" + tenantID + ":" + p.greeter(tenantID))
	return nil
}

// Deactivate pauses event delivery without deleting state. Use when
// the tenant temporarily wants the plugin off (e.g. paused billing).
func (p *Plugin) Deactivate(_ context.Context, tenantID string) error {
	p.record("deactivate:" + tenantID)
	return nil
}

// Uninstall is the terminal hook. Tear down per-tenant state created
// by Install. The host removes the installation row after this
// returns nil.
func (p *Plugin) Uninstall(_ context.Context, tenantID string) error {
	p.record("uninstall:" + tenantID)
	return nil
}

// History returns a defensive copy of the recorded lifecycle events.
// Tests assert on the contents; production code does not need this.
func (p *Plugin) History() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.history))
	copy(out, p.history)
	return out
}

// Settings is the typed settings struct the plugin understands. The
// host stores this as a generic map[string]any; the plugin author is
// responsible for marshalling.
type Settings struct {
	Greeting string `json:"greeting"`
}

// FromSettings converts a host settings blob into the typed shape.
// Returns the zero value when the blob is missing or malformed -- the
// plugin treats settings as a soft, optional input.
func FromSettings(values map[string]any) Settings {
	greeting, _ := values["greeting"].(string)
	return Settings{Greeting: greeting}
}

func (p *Plugin) record(event string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.history = append(p.history, event)
}
