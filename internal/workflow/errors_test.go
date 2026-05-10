package workflow_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/workflow"
)

func TestSentinelErrorsAreDetectable(t *testing.T) {
	tests := []struct {
		name     string
		sentinel error
	}{
		{"ErrLauncherUnconfigured", workflow.ErrLauncherUnconfigured},
		{"ErrWizardIdentityNil", workflow.ErrWizardIdentityNil},
		{"ErrOrderTenantRequired", workflow.ErrOrderTenantRequired},
		{"ErrOrderChannelRequired", workflow.ErrOrderChannelRequired},
		{"ErrOrderExternalIDMissing", workflow.ErrOrderExternalIDMissing},
		{"ErrOrderNoLineItems", workflow.ErrOrderNoLineItems},
		{"ErrOrderOccurredAtMissing", workflow.ErrOrderOccurredAtMissing},
		{"ErrReturnNotEligible", workflow.ErrReturnNotEligible},
		{"ErrLargeRefundApprovalRequired", workflow.ErrLargeRefundApprovalRequired},
		{"ErrReturnSagaRolledBack", workflow.ErrReturnSagaRolledBack},
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
