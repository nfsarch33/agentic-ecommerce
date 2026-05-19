// Package funnelanal provides funnel analytics: step tracking, drop-off detection, and cohort analysis.
package funnelanal

import (
	"sync"
	"time"
)

// Step represents a single step in a funnel.
type Step struct {
	Name  string
	Order int
}

// FunnelEvent represents a user event occurring at a funnel step.
type FunnelEvent struct {
	SessionID  string
	UserID     string
	Step       string
	OccurredAt time.Time
}

// Funnel represents a named sequence of steps.
type Funnel struct {
	ID    string
	Name  string
	Steps []Step
}

// FunnelReport contains the results of a funnel analysis.
type FunnelReport struct {
	StepCounts     map[string]int
	DropOffRates   map[string]float64
	ConversionRate float64
}

// EventStore is a thread-safe store for funnel events.
type EventStore struct {
	mu     sync.RWMutex
	events map[string][]FunnelEvent // keyed by sessionID
}

// NewEventStore creates a new EventStore.
func NewEventStore() *EventStore {
	return &EventStore{
		events: make(map[string][]FunnelEvent),
	}
}

// Record stores a funnel event.
func (s *EventStore) Record(event FunnelEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[event.SessionID] = append(s.events[event.SessionID], event)
}

// SessionEvents returns all events for the given session ID.
func (s *EventStore) SessionEvents(sessionID string) []FunnelEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	evts := s.events[sessionID]
	if len(evts) == 0 {
		return nil
	}
	result := make([]FunnelEvent, len(evts))
	copy(result, evts)
	return result
}

// FunnelAnalyzer analyzes funnel events against a funnel definition.
type FunnelAnalyzer struct{}

// Analyze processes the provided events against the given funnel and returns a report.
// ConversionRate = count(last step) / count(first step); 0 if no steps or first step has 0 count.
func (a *FunnelAnalyzer) Analyze(funnel Funnel, events []FunnelEvent) FunnelReport {
	report := FunnelReport{
		StepCounts:   make(map[string]int),
		DropOffRates: make(map[string]float64),
	}

	if len(funnel.Steps) == 0 {
		return report
	}

	// Build step name -> order map.
	stepOrder := make(map[string]int, len(funnel.Steps))
	for _, s := range funnel.Steps {
		stepOrder[s.Name] = s.Order
		report.StepCounts[s.Name] = 0
	}

	// Count events per step (only steps belonging to the funnel).
	for _, e := range events {
		if _, ok := stepOrder[e.Step]; ok {
			report.StepCounts[e.Step]++
		}
	}

	// Sort funnel steps by Order to calculate drop-off.
	steps := make([]Step, len(funnel.Steps))
	copy(steps, funnel.Steps)
	sortStepsByOrder(steps)

	// Calculate drop-off rates between consecutive steps.
	for i := 1; i < len(steps); i++ {
		prev := steps[i-1]
		curr := steps[i]
		prevCount := report.StepCounts[prev.Name]
		currCount := report.StepCounts[curr.Name]
		var dropOff float64
		if prevCount > 0 {
			dropOff = float64(prevCount-currCount) / float64(prevCount)
		}
		report.DropOffRates[curr.Name] = dropOff
	}

	// Conversion rate = last step count / first step count.
	firstCount := report.StepCounts[steps[0].Name]
	lastCount := report.StepCounts[steps[len(steps)-1].Name]
	if firstCount > 0 {
		report.ConversionRate = float64(lastCount) / float64(firstCount)
	}

	return report
}

// sortStepsByOrder performs an insertion sort of steps by Order field.
func sortStepsByOrder(steps []Step) {
	for i := 1; i < len(steps); i++ {
		for j := i; j > 0 && steps[j].Order < steps[j-1].Order; j-- {
			steps[j], steps[j-1] = steps[j-1], steps[j]
		}
	}
}
