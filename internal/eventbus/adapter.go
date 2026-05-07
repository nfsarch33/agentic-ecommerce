package eventbus

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/agent"
)

// EventBusAdapter bridges the agent orchestrator's EventSink to the event bus Publisher.
type EventBusAdapter struct {
	publisher Publisher
	source    string
}

func NewEventBusAdapter(publisher Publisher, source string) *EventBusAdapter {
	return &EventBusAdapter{publisher: publisher, source: source}
}

func (a *EventBusAdapter) Emit(ctx context.Context, agentEvent agent.AgentEvent) error {
	event := Event{
		ID:        uuid.NewString(),
		Type:      AgentRunCompleted,
		TenantID:  "default",
		Timestamp: time.Now().UTC(),
		Source:    a.source,
		Payload: map[string]any{
			"run_id":     agentEvent.RunID,
			"task_id":    agentEvent.TaskID,
			"agent_id":   agentEvent.AgentID,
			"state":      string(agentEvent.State),
			"event_type": string(agentEvent.Type),
		},
	}
	if agentEvent.Payload != nil {
		for k, v := range agentEvent.Payload {
			event.Payload[k] = v
		}
	}
	return a.publisher.Publish(ctx, event)
}
