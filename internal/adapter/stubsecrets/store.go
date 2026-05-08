// Package stubsecrets provides an in-memory TenantSecretStore for
// local boot, dev compose, and unit tests. Production wiring uses
// the awssecrets or gcpsecrets adapter behind the same port.
package stubsecrets

import (
	"context"
	"fmt"
	"regexp"
	"sync"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// keyPattern enforces the same shape adapters use to build the
// remote secret path. Mirrors marketplace.IsValidSlug so stub data
// behaves like production data.
var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Store is an in-memory TenantSecretStore. Zero value is ready to
// use; concurrent readers are safe.
type Store struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewStore returns a Store seeded from the optional values map. The
// values map is keyed as "<tenantID>/<key>" for readability.
func NewStore(values map[string]string) *Store {
	s := &Store{data: make(map[string]string, len(values))}
	for k, v := range values {
		s.data[k] = v
	}
	return s
}

// Set stores value under the (tenantID, key) tuple.
func (s *Store) Set(tenantID, key, value string) error {
	if err := validate(tenantID, key); err != nil {
		return err
	}
	s.mu.Lock()
	s.data[path(tenantID, key)] = value
	s.mu.Unlock()
	return nil
}

// GetTenantSecret implements port.TenantSecretStore.
func (s *Store) GetTenantSecret(_ context.Context, tenantID, key string) (string, error) {
	if err := validate(tenantID, key); err != nil {
		return "", err
	}
	s.mu.RLock()
	v, ok := s.data[path(tenantID, key)]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("%w: tenant=%s key=%s", port.ErrSecretNotFound, tenantID, key)
	}
	return v, nil
}

func path(tenantID, key string) string { return tenantID + "/" + key }

func validate(tenantID, key string) error {
	if tenantID == "" || !keyPattern.MatchString(tenantID) {
		return fmt.Errorf("%w: tenantID=%q", port.ErrSecretInvalidArgument, tenantID)
	}
	if key == "" || !keyPattern.MatchString(key) {
		return fmt.Errorf("%w: key=%q", port.ErrSecretInvalidArgument, key)
	}
	return nil
}
