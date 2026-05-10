package coord_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/coord"
)

func TestCoordSentinelErrorsAreDetectable(t *testing.T) {
	tests := []struct {
		name     string
		sentinel error
	}{
		{"ErrAgentNameRequired", coord.ErrAgentNameRequired},
		{"ErrTenantIDRequired", coord.ErrTenantIDRequired},
		{"ErrSKURequired", coord.ErrSKURequired},
		{"ErrActionRequired", coord.ErrActionRequired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := fmt.Errorf("context: %w", tc.sentinel)
			if !errors.Is(wrapped, tc.sentinel) {
				t.Fatalf("errors.Is failed for %s through wrapping", tc.name)
			}
		})
	}
}
