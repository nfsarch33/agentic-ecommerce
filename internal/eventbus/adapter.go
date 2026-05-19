package eventbus

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/agent"
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
	timestamp := agentEvent.CreatedAt.UTC()
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	event := Event{
		ID:        uuid.NewString(),
		Type:      AgentRunCompleted,
		TenantID:  "default",
		Timestamp: timestamp,
		Source:    a.source,
		Payload: map[string]any{
			"run_id":           agentEvent.RunID,
			"task_id":          agentEvent.TaskID,
			"agent_id":         agentEvent.AgentID,
			"state":            string(agentEvent.State),
			"agent_event_type": string(agentEvent.Type),
			"event_created_at": timestamp.Format(time.RFC3339),
		},
	}
	if agentEvent.Payload != nil {
		for k, v := range agentEvent.Payload {
			event.Payload[k] = v
		}
	}
	return a.publisher.Publish(ctx, event)
}
