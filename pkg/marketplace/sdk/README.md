# Plugin SDK -- Agentic Ecommerce Marketplace

The `pkg/marketplace/sdk` package is the public, stable surface for
building third-party plugins for the Agentic Ecommerce marketplace.
Everything you need to ship a plugin lives here. You should never
need to import any `internal/*` package.

## 10-minute path

```bash
mkdir myplugin && cd myplugin
go mod init github.com/your-org/myplugin
go get github.com/nfsarch33/agentic-ecommerce/pkg/marketplace/sdk
```

Create `myplugin.go`:

```go
package myplugin

import (
    "context"

    "github.com/nfsarch33/agentic-ecommerce/pkg/marketplace/sdk"
)

type Plugin struct{}

func (Plugin) Manifest() sdk.Manifest {
    return sdk.Manifest{
        Slug:    "my-plugin",
        Name:    "My Plugin",
        Version: "0.1.0",
        Vendor:  "Your Org",
    }
}

func (Plugin) Install(ctx context.Context, tenantID string) error    { return nil }
func (Plugin) Activate(ctx context.Context, tenantID string) error   { return nil }
func (Plugin) Deactivate(ctx context.Context, tenantID string) error { return nil }
func (Plugin) Uninstall(ctx context.Context, tenantID string) error  { return nil }
```

Create `myplugin_test.go`:

```go
package myplugin_test

import (
    "context"
    "testing"

    "github.com/nfsarch33/agentic-ecommerce/pkg/marketplace/sdk"

    "github.com/your-org/myplugin"
)

func TestPluginSmoke(t *testing.T) {
    p := myplugin.Plugin{}
    sb := sdk.NewTestSandbox(t, p.Manifest())
    sb.SmokeCheck(context.Background(), p)
}
```

Run:

```bash
go test ./...
```

That's it. The `TestSandbox` walks your plugin through the full
Install -> Activate -> Deactivate -> Uninstall lifecycle against the
real registry state machine.

## Concepts

### Manifest

The `sdk.Manifest` struct identifies the plugin and declares what
permissions and events it needs. The host validates the manifest on
`Install` and rejects malformed slugs, non-semver versions, or
self-dependencies.

| Field                | Required | Notes                                                |
|----------------------|----------|------------------------------------------------------|
| `Slug`               | yes      | Kebab-case, must start with a letter                 |
| `Name`               | yes      | Human-readable display name                          |
| `Version`            | yes      | Strict `MAJOR.MINOR.PATCH`                           |
| `Vendor`             | yes      | Your org name                                        |
| `Description`        | no       | Shown in the marketplace storefront                  |
| `Category`           | no       | One of `payments`, `notifications`, `marketing`, ... |
| `EventSubscriptions` | no       | Event names you want to receive                      |
| `Permissions`        | no       | Capabilities you request from the host               |
| `Dependencies`       | no       | Other plugins you need installed first               |

### Lifecycle hooks

```
Install -> Activate -> Deactivate -> Activate -> Uninstall
```

- **Install**: runs once per `(tenant, plugin)` pair. Provision
  per-tenant resources here.
- **Activate**: transition from installed/deactivated to active. Must
  be idempotent (calling twice is a no-op).
- **Deactivate**: pause without deleting state. The installation row
  survives so settings persist.
- **Uninstall**: terminal hook. Tear down per-tenant state here. The
  host removes the installation row after this returns nil.

All hooks receive a `context.Context` and MUST honour cancellation.

### Optional interfaces

Plugins can opt into richer behaviour by implementing additional
interfaces:

- `sdk.EventSubscriber` -- declare a subset of the manifest's
  `EventSubscriptions` to actually receive at runtime
- `sdk.RouteExtender` -- expose additional HTTP routes mounted by
  the host at `/api/v1/marketplace/tenants/<tenant>/plugins/<slug>/<path>`

### Sandboxing

The host enforces a per-`(tenant, slug)` token bucket on lifecycle
hooks (default 60/min). If your plugin loops, it trips
`sdk.ErrSandboxBudgetExceeded` rather than crashing the worker.

### Settings

Per-tenant settings live as `map[string]any` in the host registry.
Use a typed `From*Settings` adapter in your plugin to convert it to
a struct your business logic understands. Settings are optional --
your plugin should treat them as soft input.

## Example

See `pkg/marketplace/sdk/example/hello/` for a heavily commented
minimum-viable plugin. Run its tests:

```bash
go test ./pkg/marketplace/sdk/example/hello/...
```

## Stability

The SDK follows the v1 API stability policy documented at
`docs/api-versioning.md`. Concretely:

- No breaking changes to exported symbols through host v3.x
- Host v2.9.0 introduces the SDK as v1
- New host versions may add fields and methods, never remove

Errors are typed sentinels you can match with `errors.Is`:

```go
if errors.Is(err, sdk.ErrPluginAlreadyInstalled) { ... }
```

## Versioning your plugin

Use [semver](https://semver.org/) for your plugin's `Version` field.
The host's dependency resolver supports `^X.Y.Z` (caret) and
`=X.Y.Z` (exact) constraints; an empty constraint is treated as
caret of the resolved version.

## Reporting issues

File issues at <https://github.com/nfsarch33/agentic-ecommerce/issues>.
Tag with `area/sdk` so plugin-related work stays organized.
