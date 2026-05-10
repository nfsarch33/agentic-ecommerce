// File scope: v3.9.1 -- test double for TemporalWorkflowExecutor.
//
// Implements just the ExecuteWorkflow surface the launcher uses;
// production wiring resolves to the full Temporal client.Client.
package workflow

import (
	"context"

	"go.temporal.io/sdk/client"
)

type stubTemporalClient struct {
	invocations      int
	lastWorkflowName string
	lastInput        TenantOnboardingInput
	err              error
}

func (s *stubTemporalClient) ExecuteWorkflow(_ context.Context, _ client.StartWorkflowOptions, workflow any, args ...any) (client.WorkflowRun, error) {
	s.invocations++
	if name, ok := workflow.(string); ok {
		s.lastWorkflowName = name
	}
	if len(args) > 0 {
		if input, ok := args[0].(TenantOnboardingInput); ok {
			s.lastInput = input
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return nil, nil
}
