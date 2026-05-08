// Package gcpsecrets is the v2.7.0 TenantSecretStore adapter that
// resolves secrets via Google Cloud Secret Manager paths of the
// form "projects/<project>/secrets/agentic-ecommerce_<tenantID>_<key>/versions/latest".
//
// Like awssecrets, this adapter is contract-only: it documents the
// path layout the Terraform module provisions and builds the
// canonical resource name. Runtime SDK wiring lives in the deploy
// package and is wired in via cloud.google.com/go/secretmanager at
// build time.
package gcpsecrets

import (
	"context"
	"fmt"
	"regexp"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// Resolver returns the GCP Secret Manager value at name.
type Resolver interface {
	Resolve(ctx context.Context, name string) (string, error)
}

// Store implements port.TenantSecretStore by routing requests
// through Resolver.
type Store struct {
	project  string
	prefix   string
	resolver Resolver
}

// NewStore returns a Store. project is the GCP project ID; prefix is
// prepended to each secret name. Pass "" prefix to use the canonical
// "agentic-ecommerce" prefix.
func NewStore(project, prefix string, resolver Resolver) *Store {
	if prefix == "" {
		prefix = "agentic-ecommerce"
	}
	return &Store{project: project, prefix: prefix, resolver: resolver}
}

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// GetTenantSecret implements port.TenantSecretStore.
func (s *Store) GetTenantSecret(ctx context.Context, tenantID, key string) (string, error) {
	if s.resolver == nil {
		return "", fmt.Errorf("gcpsecrets: resolver not configured")
	}
	if s.project == "" {
		return "", fmt.Errorf("gcpsecrets: project not configured")
	}
	if tenantID == "" || !keyPattern.MatchString(tenantID) {
		return "", fmt.Errorf("%w: tenantID=%q", port.ErrSecretInvalidArgument, tenantID)
	}
	if key == "" || !keyPattern.MatchString(key) {
		return "", fmt.Errorf("%w: key=%q", port.ErrSecretInvalidArgument, key)
	}
	return s.resolver.Resolve(ctx, s.name(tenantID, key))
}

// Name returns the canonical GCP Secret Manager resource name for
// the (tenantID, key) tuple.
func (s *Store) Name(tenantID, key string) string {
	return s.name(tenantID, key)
}

func (s *Store) name(tenantID, key string) string {
	// GCP secret IDs allow [a-zA-Z0-9_-]; we collapse to lowercase
	// and join with underscore so the (prefix, tenantID, key)
	// tuple round-trips through Terraform string interpolation.
	return fmt.Sprintf("projects/%s/secrets/%s_%s_%s/versions/latest", s.project, s.prefix, tenantID, key)
}
