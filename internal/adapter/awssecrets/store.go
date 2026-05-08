// Package awssecrets is the v2.7.0 TenantSecretStore adapter that
// resolves secrets via AWS Secrets Manager paths of the form
// "agentic-ecommerce/<tenantID>/<key>". The adapter is contract-only:
// it documents the path layout the Terraform module provisions and
// builds the canonical secret name. Runtime SDK wiring lives in the
// deploy package and is wired in via aws-sdk-go-v2 at build time.
//
// Keeping the adapter contract-only here mirrors the v2.5.0 stripe
// adapter and v2.6.0 webhook outbound adapter pattern: the Go code
// commits to the contract and a runtime build flag pulls in the
// SDK. This avoids dragging the AWS SDK into the default backend
// build and keeps unit tests hermetic.
package awssecrets

import (
	"context"
	"fmt"
	"regexp"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// Resolver returns the AWS Secrets Manager value at path.
type Resolver interface {
	Resolve(ctx context.Context, path string) (string, error)
}

// Store implements port.TenantSecretStore by routing requests
// through Resolver. Pass a real AWS Secrets Manager client at
// deploy time; tests inject a fake.
type Store struct {
	prefix   string
	resolver Resolver
}

// NewStore returns a Store. prefix is prepended to every path; pass
// "" to use the canonical "agentic-ecommerce" prefix.
func NewStore(prefix string, resolver Resolver) *Store {
	if prefix == "" {
		prefix = "agentic-ecommerce"
	}
	return &Store{prefix: prefix, resolver: resolver}
}

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// GetTenantSecret implements port.TenantSecretStore. The constructed
// path is "<prefix>/<tenantID>/<key>".
func (s *Store) GetTenantSecret(ctx context.Context, tenantID, key string) (string, error) {
	if s.resolver == nil {
		return "", fmt.Errorf("awssecrets: resolver not configured")
	}
	if tenantID == "" || !keyPattern.MatchString(tenantID) {
		return "", fmt.Errorf("%w: tenantID=%q", port.ErrSecretInvalidArgument, tenantID)
	}
	if key == "" || !keyPattern.MatchString(key) {
		return "", fmt.Errorf("%w: key=%q", port.ErrSecretInvalidArgument, key)
	}
	return s.resolver.Resolve(ctx, s.path(tenantID, key))
}

// Path returns the canonical AWS Secrets Manager path for the
// (tenantID, key) tuple. Exposed so deploy automation can pre-create
// the secret rows before the application boots.
func (s *Store) Path(tenantID, key string) string {
	return s.path(tenantID, key)
}

func (s *Store) path(tenantID, key string) string {
	return fmt.Sprintf("%s/%s/%s", s.prefix, tenantID, key)
}
