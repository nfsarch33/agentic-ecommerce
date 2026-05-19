// Package abtest provides an A/B test framework: variant assignment, statistical significance.
package abtest

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sort"
	"sync"
)

// Variant represents one variant in an A/B experiment.
// Weight is the fraction of traffic assigned to this variant; all weights must sum to 1.0.
type Variant struct {
	ID     string
	Name   string
	Weight float64
}

// Experiment represents a named A/B experiment with one or more variants.
type Experiment struct {
	ID       string
	Name     string
	Variants []Variant
	Active   bool
}

// ErrNotFound is returned when an experiment is not in the store.
var ErrNotFound = errors.New("experiment not found")

// ErrInvalidWeights is returned when variant weights do not sum to 1.0.
var ErrInvalidWeights = errors.New("variant weights must sum to 1.0")

// validateWeights checks that variant weights sum to approximately 1.0.
func validateWeights(variants []Variant) error {
	if len(variants) == 0 {
		return ErrInvalidWeights
	}
	sum := 0.0
	for _, v := range variants {
		sum += v.Weight
	}
	if math.Abs(sum-1.0) > 1e-9 {
		return ErrInvalidWeights
	}
	return nil
}

// ExperimentStore is a thread-safe store for experiments.
type ExperimentStore struct {
	mu          sync.RWMutex
	experiments map[string]Experiment
}

// NewExperimentStore creates a new ExperimentStore.
func NewExperimentStore() *ExperimentStore {
	return &ExperimentStore{
		experiments: make(map[string]Experiment),
	}
}

// Add stores an experiment, returning an error if weights are invalid.
func (s *ExperimentStore) Add(exp Experiment) error {
	if err := validateWeights(exp.Variants); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.experiments[exp.ID] = exp
	return nil
}

// Get retrieves an experiment by ID. Returns ErrNotFound if not present.
func (s *ExperimentStore) Get(id string) (*Experiment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.experiments[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := exp
	return &cp, nil
}

// Deactivate marks an experiment as inactive.
func (s *ExperimentStore) Deactivate(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if exp, ok := s.experiments[id]; ok {
		exp.Active = false
		s.experiments[id] = exp
	}
}

// Assigner assigns users to experiment variants deterministically.
type Assigner struct {
	store *ExperimentStore
}

// NewAssigner creates an Assigner backed by the given store.
func NewAssigner(store *ExperimentStore) *Assigner {
	return &Assigner{store: store}
}

// Assign returns the variant ID for a given user in a given experiment.
// Assignment is deterministic: hash(experimentID+userID) mod 10000 is used to
// select a variant by cumulative weight buckets.
func (a *Assigner) Assign(experimentID, userID string) (string, error) {
	exp, err := a.store.Get(experimentID)
	if err != nil {
		return "", err
	}

	// Hash experiment+user to a stable uint64.
	h := sha256.Sum256([]byte(experimentID + "\x00" + userID))
	n := binary.BigEndian.Uint64(h[:8])

	// Map to [0, 10000).
	bucket := float64(n%10000) / 10000.0

	// Walk variants by cumulative weight.
	cumulative := 0.0
	for _, v := range exp.Variants {
		cumulative += v.Weight
		if bucket < cumulative {
			return v.ID, nil
		}
	}

	// Fallback to last variant (handles floating-point edge).
	return exp.Variants[len(exp.Variants)-1].ID, nil
}

// ConversionResult holds aggregate conversion data for one variant.
type ConversionResult struct {
	VariantID   string
	Conversions int
	Impressions int
}

// ZScore computes the z-score for each variant's conversion rate relative to the baseline
// (first element of results). Returns map of variantID -> z-score.
// Baseline variant maps to 0.0. Variants with 0 impressions map to 0.0.
func ZScore(results []ConversionResult) map[string]float64 {
	out := make(map[string]float64, len(results))
	if len(results) == 0 {
		return out
	}

	baseline := results[0]
	var baseRate float64
	if baseline.Impressions > 0 {
		baseRate = float64(baseline.Conversions) / float64(baseline.Impressions)
	}
	out[baseline.VariantID] = 0.0

	for _, r := range results[1:] {
		if r.Impressions == 0 {
			out[r.VariantID] = 0.0
			continue
		}
		variantRate := float64(r.Conversions) / float64(r.Impressions)

		// Pooled proportion for standard error.
		totalConv := baseline.Conversions + r.Conversions
		totalImp := baseline.Impressions + r.Impressions
		pooled := float64(totalConv) / float64(totalImp)

		se := math.Sqrt(pooled * (1.0 - pooled) * (1.0/float64(baseline.Impressions) + 1.0/float64(r.Impressions)))
		if se == 0 {
			out[r.VariantID] = 0.0
			continue
		}
		out[r.VariantID] = (variantRate - baseRate) / se
	}

	return out
}

// IsSignificant returns true if the absolute z-score exceeds the threshold for the given p-value.
// For pValue=0.05 (two-tailed), the threshold is ~1.96.
// This implementation uses a simple threshold table for common p-values.
func IsSignificant(zScore float64, pValue float64) bool {
	threshold := zThreshold(pValue)
	return math.Abs(zScore) >= threshold
}

// zThreshold returns the z-score threshold for the given two-tailed p-value.
// Uses a small lookup table; defaults to 1.96 for p=0.05.
func zThreshold(pValue float64) float64 {
	// Common thresholds (two-tailed).
	thresholds := []struct {
		p float64
		z float64
	}{
		{0.10, 1.645},
		{0.05, 1.960},
		{0.01, 2.576},
		{0.001, 3.291},
	}

	// Find the nearest match.
	sort.Slice(thresholds, func(i, j int) bool {
		return math.Abs(thresholds[i].p-pValue) < math.Abs(thresholds[j].p-pValue)
	})

	return thresholds[0].z
}
