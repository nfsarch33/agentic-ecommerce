// Package secrets is the v2.8.0 consolidation of the per-tenant
// TenantSecretStore adapters. Prior to v2.8.0 the package layer
// carried three peer packages (stubsecrets, awssecrets, gcpsecrets)
// which inflated cross-module coupling without reducing duplication.
// This package collapses them into a single backend-selectable
// Manager mirroring internal/adapter/notification (one package,
// multiple typed senders).
//
// Manager always implements port.TenantSecretStore. Backend selects
// the concrete strategy. Tests cover each backend including the
// failure paths (missing resolver, missing project, missing seed
// values, invalid argv).
package secrets

import (
	"context"
	"fmt"
	"regexp"
	"sync"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// Backend selects the underlying secret store strategy.
type Backend int

const (
	// BackendStub is the in-memory store used by local boot, dev
	// compose, and unit tests. Seeded values are keyed
	// "<tenantID>/<key>" for readability.
	BackendStub Backend = iota + 1

	// BackendAWS is the AWS Secrets Manager strategy. Paths follow
	// "<prefix>/<tenantID>/<key>" with default prefix
	// "agentic-ecommerce".
	BackendAWS

	// BackendGCP is the Google Cloud Secret Manager strategy. Resource
	// names follow
	// "projects/<project>/secrets/<prefix>_<tenantID>_<key>/versions/latest"
	// with default prefix "agentic-ecommerce".
	BackendGCP
)

// String returns the human-readable backend name.
func (b Backend) String() string {
	switch b {
	case BackendStub:
		return "stub"
	case BackendAWS:
		return "aws"
	case BackendGCP:
		return "gcp"
	default:
		return "unknown"
	}
}

// CloudResolver returns the cloud-store value for the canonical path
// (AWS Secrets Manager) or resource name (GCP Secret Manager).
type CloudResolver interface {
	Resolve(ctx context.Context, path string) (string, error)
}

// Option configures a Manager. Concrete options live below
// (WithSeed / WithCloudResolver / WithPrefix / WithGCPProject).
// Options are typed and validated in New so callers cannot pass
// the wrong option for the chosen backend.
type Option func(*config)

// WithSeed seeds the BackendStub store with the supplied
// "<tenantID>/<key>" -> value map.
func WithSeed(seed map[string]string) Option {
	return func(c *config) { c.seed = seed }
}

// WithCloudResolver supplies the resolver used by BackendAWS and
// BackendGCP. Required for cloud backends.
func WithCloudResolver(r CloudResolver) Option {
	return func(c *config) { c.resolver = r }
}

// WithPrefix overrides the default "agentic-ecommerce" path prefix.
// Empty string falls back to the default.
func WithPrefix(prefix string) Option {
	return func(c *config) { c.prefix = prefix }
}

// WithGCPProject sets the GCP project ID. Required for BackendGCP.
func WithGCPProject(project string) Option {
	return func(c *config) { c.project = project }
}

// Manager is the unified TenantSecretStore. Construct via New.
type Manager struct {
	backend  Backend
	prefix   string
	project  string
	resolver CloudResolver

	stubMu   sync.RWMutex
	stubData map[string]string
}

// New returns a Manager bound to backend with the supplied options.
// Returns an error rather than panicking when an option is missing
// or the backend is unknown so callers can surface the error at
// startup before serving traffic.
func New(backend Backend, opts ...Option) (*Manager, error) {
	cfg := config{prefix: defaultPrefix}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.prefix == "" {
		cfg.prefix = defaultPrefix
	}

	m := &Manager{
		backend:  backend,
		prefix:   cfg.prefix,
		project:  cfg.project,
		resolver: cfg.resolver,
	}

	switch backend {
	case BackendStub:
		m.stubData = make(map[string]string, len(cfg.seed))
		for k, v := range cfg.seed {
			m.stubData[k] = v
		}
	case BackendAWS:
		if cfg.resolver == nil {
			return nil, fmt.Errorf("secrets: backend=aws requires WithCloudResolver")
		}
	case BackendGCP:
		if cfg.resolver == nil {
			return nil, fmt.Errorf("secrets: backend=gcp requires WithCloudResolver")
		}
		if cfg.project == "" {
			return nil, fmt.Errorf("secrets: backend=gcp requires WithGCPProject")
		}
	default:
		return nil, fmt.Errorf("secrets: unknown backend=%d", backend)
	}
	return m, nil
}

// Backend returns the strategy this Manager was constructed with.
func (m *Manager) Backend() Backend { return m.backend }

// GetTenantSecret implements port.TenantSecretStore. Returned values
// are opaque: callers MUST NOT log them per the no-shell-leak rule.
func (m *Manager) GetTenantSecret(ctx context.Context, tenantID, key string) (string, error) {
	if err := validate(tenantID, key); err != nil {
		return "", err
	}
	switch m.backend {
	case BackendStub:
		return m.lookupStub(tenantID, key)
	case BackendAWS:
		return m.resolver.Resolve(ctx, m.awsPath(tenantID, key))
	case BackendGCP:
		return m.resolver.Resolve(ctx, m.gcpName(tenantID, key))
	default:
		return "", fmt.Errorf("secrets: unknown backend=%d", m.backend)
	}
}

// Set inserts or overwrites a stub value. Only valid for BackendStub.
func (m *Manager) Set(tenantID, key, value string) error {
	if m.backend != BackendStub {
		return fmt.Errorf("secrets: Set only valid for backend=stub (have %s)", m.backend)
	}
	if err := validate(tenantID, key); err != nil {
		return err
	}
	m.stubMu.Lock()
	m.stubData[stubPath(tenantID, key)] = value
	m.stubMu.Unlock()
	return nil
}

// Path returns the canonical AWS Secrets Manager path for the
// (tenantID, key) tuple. Only valid for BackendAWS. Used by deploy
// automation to pre-create secret rows before the application boots.
func (m *Manager) Path(tenantID, key string) (string, error) {
	if m.backend != BackendAWS {
		return "", fmt.Errorf("secrets: Path only valid for backend=aws (have %s)", m.backend)
	}
	if err := validate(tenantID, key); err != nil {
		return "", err
	}
	return m.awsPath(tenantID, key), nil
}

// Name returns the canonical GCP Secret Manager resource name for
// the (tenantID, key) tuple. Only valid for BackendGCP.
func (m *Manager) Name(tenantID, key string) (string, error) {
	if m.backend != BackendGCP {
		return "", fmt.Errorf("secrets: Name only valid for backend=gcp (have %s)", m.backend)
	}
	if err := validate(tenantID, key); err != nil {
		return "", err
	}
	return m.gcpName(tenantID, key), nil
}

func (m *Manager) lookupStub(tenantID, key string) (string, error) {
	m.stubMu.RLock()
	v, ok := m.stubData[stubPath(tenantID, key)]
	m.stubMu.RUnlock()
	if !ok {
		return "", fmt.Errorf("%w: tenant=%s key=%s", port.ErrSecretNotFound, tenantID, key)
	}
	return v, nil
}

func (m *Manager) awsPath(tenantID, key string) string {
	return fmt.Sprintf("%s/%s/%s", m.prefix, tenantID, key)
}

func (m *Manager) gcpName(tenantID, key string) string {
	// GCP secret IDs allow [a-zA-Z0-9_-]; the prefix/tenant/key
	// tuple is joined with underscores to round-trip through
	// Terraform string interpolation.
	return fmt.Sprintf("projects/%s/secrets/%s_%s_%s/versions/latest", m.project, m.prefix, tenantID, key)
}

const defaultPrefix = "agentic-ecommerce"

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

type config struct {
	seed     map[string]string
	resolver CloudResolver
	prefix   string
	project  string
}

func stubPath(tenantID, key string) string { return tenantID + "/" + key }

func validate(tenantID, key string) error {
	if tenantID == "" || !keyPattern.MatchString(tenantID) {
		return fmt.Errorf("%w: tenantID=%q", port.ErrSecretInvalidArgument, tenantID)
	}
	if key == "" || !keyPattern.MatchString(key) {
		return fmt.Errorf("%w: key=%q", port.ErrSecretInvalidArgument, key)
	}
	return nil
}
