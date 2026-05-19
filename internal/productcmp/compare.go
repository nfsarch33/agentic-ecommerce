// Package productcmp provides product comparison, attribute matrix management,
// and Jaccard-similarity-based recommendation.
package productcmp

import (
	"errors"
	"sync"
)

const (
	TypeString  = "string"
	TypeNumber  = "number"
	TypeBoolean = "boolean"

	defaultMaxCompare = 4
)

var ErrMaxExceeded = errors.New("maximum products in comparison exceeded")

// Attribute is a typed key-value descriptor for a product.
type Attribute struct {
	Name  string
	Value string
	Type  string
}

// Product represents a product entity with typed attributes.
type Product struct {
	ID         string
	Name       string
	Price      float64
	Attributes []Attribute
}

// ComparisonResult holds the side-by-side comparison of multiple products.
type ComparisonResult struct {
	Products            []Product
	SharedAttributes    []string
	DifferingAttributes []string
}

// Matrix is a thread-safe collection of products prepared for comparison.
type Matrix struct {
	mu         sync.RWMutex
	products   map[string]Product
	order      []string
	MaxCompare int
}

// NewMatrix creates a Matrix with the default max compare limit.
func NewMatrix() *Matrix {
	return &Matrix{
		products:   make(map[string]Product),
		MaxCompare: defaultMaxCompare,
	}
}

// Add inserts a product. Returns ErrMaxExceeded if the limit is reached.
func (m *Matrix) Add(p Product) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.products[p.ID]; !exists {
		if len(m.products) >= m.MaxCompare {
			return ErrMaxExceeded
		}
		m.order = append(m.order, p.ID)
	}
	m.products[p.ID] = p
	return nil
}

// Remove deletes a product from the matrix.
func (m *Matrix) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.products, id)
	newOrder := m.order[:0]
	for _, oid := range m.order {
		if oid != id {
			newOrder = append(newOrder, oid)
		}
	}
	m.order = newOrder
}

// Products returns all products in insertion order.
func (m *Matrix) Products() []Product {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Product, 0, len(m.order))
	for _, id := range m.order {
		if p, ok := m.products[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

// Compare analyses a slice of products and returns shared and differing attribute names.
func Compare(products []Product) ComparisonResult {
	if len(products) == 0 {
		return ComparisonResult{}
	}

	// Build attribute name -> set of values, one entry per product position.
	type valueSet map[string]struct{}
	attrValues := make(map[string]valueSet)

	for _, p := range products {
		for _, a := range p.Attributes {
			if attrValues[a.Name] == nil {
				attrValues[a.Name] = make(valueSet)
			}
			attrValues[a.Name][a.Value] = struct{}{}
		}
	}

	// Count how many products have each attribute.
	attrPresence := make(map[string]int)
	for _, p := range products {
		seen := make(map[string]struct{})
		for _, a := range p.Attributes {
			if _, already := seen[a.Name]; !already {
				attrPresence[a.Name]++
				seen[a.Name] = struct{}{}
			}
		}
	}

	var shared, differing []string
	for name, vals := range attrValues {
		if attrPresence[name] == len(products) && len(vals) == 1 {
			shared = append(shared, name)
		} else {
			differing = append(differing, name)
		}
	}

	return ComparisonResult{
		Products:            products,
		SharedAttributes:    shared,
		DifferingAttributes: differing,
	}
}

// Recommender scores and ranks candidate products against a reference.
type Recommender struct{}

// Score computes Jaccard similarity between candidate and reference attribute name sets.
func (r Recommender) Score(candidate, reference Product) float64 {
	refNames := attrNameSet(reference)
	candNames := attrNameSet(candidate)

	intersection := 0
	for n := range candNames {
		if refNames[n] {
			intersection++
		}
	}

	union := len(refNames)
	for n := range candNames {
		if !refNames[n] {
			union++
		}
	}

	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// BestMatch returns the candidate with the highest Jaccard score against reference.
// Returns nil if candidates is empty.
func (r Recommender) BestMatch(candidates []Product, reference Product) *Product {
	if len(candidates) == 0 {
		return nil
	}
	best := &candidates[0]
	bestScore := r.Score(candidates[0], reference)
	for i := 1; i < len(candidates); i++ {
		s := r.Score(candidates[i], reference)
		if s > bestScore {
			bestScore = s
			best = &candidates[i]
		}
	}
	return best
}

func attrNameSet(p Product) map[string]bool {
	m := make(map[string]bool, len(p.Attributes))
	for _, a := range p.Attributes {
		m[a.Name] = true
	}
	return m
}
