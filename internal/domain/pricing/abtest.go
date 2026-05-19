package pricing

import (
	"errors"
	"hash/fnv"
	"sync"
)

var (
	ErrExperimentNotFound = errors.New("experiment not found")
	ErrNoConversions      = errors.New("no conversions recorded")
)

type PriceVariant struct {
	Name  string
	Price int
}

type conversionRecord struct {
	count   int
	revenue int
}

type Experiment struct {
	ID       string
	Name     string
	Variants []PriceVariant
}

type ABTestEngine struct {
	mu          sync.RWMutex
	experiments map[string]*Experiment
	conversions map[string]map[string]*conversionRecord // expID -> variantName -> record
}

func NewABTestEngine() *ABTestEngine {
	return &ABTestEngine{
		experiments: make(map[string]*Experiment),
		conversions: make(map[string]map[string]*conversionRecord),
	}
}

func (e *ABTestEngine) CreateExperiment(id, name string, variants []PriceVariant) (Experiment, error) {
	exp := &Experiment{ID: id, Name: name, Variants: variants}
	e.mu.Lock()
	e.experiments[id] = exp
	e.conversions[id] = make(map[string]*conversionRecord)
	for _, v := range variants {
		e.conversions[id][v.Name] = &conversionRecord{}
	}
	e.mu.Unlock()
	return *exp, nil
}

func (e *ABTestEngine) AssignVariant(experimentID, userID string) (PriceVariant, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	exp, ok := e.experiments[experimentID]
	if !ok {
		return PriceVariant{}, ErrExperimentNotFound
	}
	// Deterministic hash: same user always gets same variant
	h := fnv.New32a()
	h.Write([]byte(experimentID + ":" + userID))
	idx := int(h.Sum32()) % len(exp.Variants)
	return exp.Variants[idx], nil
}

func (e *ABTestEngine) TrackConversion(experimentID, userID string, revenue int) error {
	variant, err := e.AssignVariant(experimentID, userID)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.conversions[experimentID][variant.Name]
	rec.count++
	rec.revenue += revenue
	return nil
}

func (e *ABTestEngine) DeclareWinner(experimentID string) (PriceVariant, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	exp, ok := e.experiments[experimentID]
	if !ok {
		return PriceVariant{}, ErrExperimentNotFound
	}
	total := 0
	for _, rec := range e.conversions[experimentID] {
		total += rec.count
	}
	if total == 0 {
		return PriceVariant{}, ErrNoConversions
	}
	var winner PriceVariant
	bestRate := -1.0
	for _, v := range exp.Variants {
		rec := e.conversions[experimentID][v.Name]
		if rec.count == 0 {
			continue
		}
		rate := float64(rec.revenue) / float64(rec.count)
		if rate > bestRate {
			bestRate = rate
			winner = v
		}
	}
	return winner, nil
}
