package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/agent"
)

func TestEventBusAdapter_Emit(t *testing.T) {
	bus := NewInMemoryBus()
	adapter := NewEventBusAdapter(bus, "test-source")

	agentEvt := agent.AgentEvent{
		Type:      agent.EventRunSucceeded,
		RunID:     "run-123",
		TaskID:    "task-456",
		AgentID:   "pricing",
		State:     agent.RunSucceeded,
		Payload:   map[string]any{"result_keys": []string{"price"}},
		CreatedAt: time.Now().UTC(),
	}

	if err := adapter.Emit(context.Background(), agentEvt); err != nil {
		t.Fatalf("emit: %v", err)
	}

	delivered := bus.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("delivered = %d, want 1", len(delivered))
	}
	evt := delivered[0]
	if evt.Type != AgentRunCompleted {
		t.Errorf("type = %q, want %q", evt.Type, AgentRunCompleted)
	}
	if evt.Source != "test-source" {
		t.Errorf("source = %q, want %q", evt.Source, "test-source")
	}
	if evt.Payload["run_id"] != "run-123" {
		t.Errorf("payload.run_id = %v, want run-123", evt.Payload["run_id"])
	}
	if evt.Payload["agent_id"] != "pricing" {
		t.Errorf("payload.agent_id = %v, want pricing", evt.Payload["agent_id"])
	}
	if evt.Payload["state"] != "succeeded" {
		t.Errorf("payload.state = %v, want succeeded", evt.Payload["state"])
	}
	if evt.Payload["agent_event_type"] != "agent_run_succeeded" {
		t.Errorf("payload.agent_event_type = %v, want agent_run_succeeded", evt.Payload["agent_event_type"])
	}
	if evt.Payload["event_created_at"] == "" {
		t.Errorf("payload.event_created_at should be populated")
	}
}

func TestEventBusAdapter_ImplementsEventSink(t *testing.T) {
	bus := NewInMemoryBus()
	adapter := NewEventBusAdapter(bus, "test")
	var sink agent.EventSink = adapter
	_ = sink
}

func TestEventBusAdapter_PropagatesPublishError(t *testing.T) {
	bus := NewInMemoryBus()
	_ = bus.Close()
	adapter := NewEventBusAdapter(bus, "test")

	err := adapter.Emit(context.Background(), agent.AgentEvent{
		Type:    agent.EventRunStarted,
		RunID:   "r1",
		AgentID: "a1",
	})
	if err != ErrBusClosed {
		t.Errorf("emit on closed bus = %v, want ErrBusClosed", err)
	}
}
