package main

import (
	"net/http"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

type recentEventsResponse struct {
	Events []eventResponse `json:"events"`
}

type eventResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Severity   string         `json:"severity"`
	Message    string         `json:"message"`
	OccurredAt string         `json:"occurred_at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func (s *server) recentEventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}

	limit := queryInt(r, "limit", 20)
	if limit < 1 || limit > 100 {
		limit = 20
	}

	events := []eventbus.Event(nil)
	if s.eventBus != nil {
		events = s.eventBus.Delivered()
	}

	out := make([]eventResponse, 0, min(limit, len(events)))
	for i := len(events) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, toEventResponse(events[i]))
	}
	writeJSON(w, http.StatusOK, recentEventsResponse{Events: out})
}

func toEventResponse(event eventbus.Event) eventResponse {
	occurredAt := event.Timestamp.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return eventResponse{
		ID:         event.ID,
		Type:       string(event.Type),
		Severity:   eventSeverity(event),
		Message:    eventMessage(event),
		OccurredAt: occurredAt.Format(time.RFC3339),
		Metadata: map[string]any{
			"tenant_id": event.TenantID,
			"source":    event.Source,
			"payload":   event.Payload,
		},
	}
}

func eventSeverity(event eventbus.Event) string {
	if _, ok := event.Payload["error"]; ok {
		return "error"
	}
	if passed, ok := event.Payload["passed"].(bool); ok && !passed {
		return "warning"
	}
	return "info"
}

func eventMessage(event eventbus.Event) string {
	switch event.Type {
	case eventbus.ProductCreated:
		return "Product was created"
	case eventbus.ProductUpdated:
		return "Product was updated"
	case eventbus.OrderPlaced:
		return "Order was placed"
	case eventbus.SyncCompleted:
		return "Sync completed"
	case eventbus.AgentRunCompleted:
		return "Agent run completed"
	case eventbus.ComplianceChecked:
		if passed, ok := event.Payload["passed"].(bool); ok && !passed {
			return "Compliance check needs review"
		}
		return "Compliance check completed"
	default:
		return "Event recorded"
	}
}
