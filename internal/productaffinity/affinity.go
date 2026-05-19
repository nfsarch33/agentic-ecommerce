// Package productaffinity provides product affinity analysis: co-purchase mining and association rules.
package productaffinity

import (
	"sort"
	"sync"
)

// Order represents a customer order containing a set of product IDs.
type Order struct {
	ID         string
	ProductIDs []string
}

// pairKey returns a canonical string key for an unordered product pair.
// Always puts the lexicographically smaller product first.
func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b
}

// AffinityMiner is a thread-safe store for product co-occurrence data.
type AffinityMiner struct {
	mu           sync.RWMutex
	coOccurrence map[string]int    // pairKey -> count
	productCount map[string]int    // product -> order count
	pairsByProd  map[string][]string // product -> list of co-occurring products (may have dups)
}

// NewAffinityMiner creates a new AffinityMiner.
func NewAffinityMiner() *AffinityMiner {
	return &AffinityMiner{
		coOccurrence: make(map[string]int),
		productCount: make(map[string]int),
		pairsByProd:  make(map[string][]string),
	}
}

// Ingest processes an order, updating co-occurrence counts.
func (m *AffinityMiner) Ingest(order Order) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Deduplicate product IDs within this order.
	seen := make(map[string]struct{}, len(order.ProductIDs))
	unique := make([]string, 0, len(order.ProductIDs))
	for _, pid := range order.ProductIDs {
		if _, ok := seen[pid]; !ok {
			seen[pid] = struct{}{}
			unique = append(unique, pid)
		}
	}

	// Count each product.
	for _, pid := range unique {
		m.productCount[pid]++
	}

	// Count all pairs.
	for i := 0; i < len(unique); i++ {
		for j := i + 1; j < len(unique); j++ {
			key := pairKey(unique[i], unique[j])
			if m.coOccurrence[key] == 0 {
				// First time seeing this pair: register in pairsByProd.
				m.pairsByProd[unique[i]] = append(m.pairsByProd[unique[i]], unique[j])
				m.pairsByProd[unique[j]] = append(m.pairsByProd[unique[j]], unique[i])
			}
			m.coOccurrence[key]++
		}
	}
}

// CoOccurrences returns the number of orders in which productA and productB appear together.
func (m *AffinityMiner) CoOccurrences(productA, productB string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.coOccurrence[pairKey(productA, productB)]
}

// Support returns the fraction of orders containing both productA and productB.
// Returns 0 if totalOrders is 0.
func (m *AffinityMiner) Support(productA, productB string, totalOrders int) float64 {
	if totalOrders == 0 {
		return 0
	}
	coOcc := m.CoOccurrences(productA, productB)
	return float64(coOcc) / float64(totalOrders)
}

// productOrderCount returns the number of orders containing the given product (used internally).
func (m *AffinityMiner) productOrderCount(product string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.productCount[product]
}

// AssociationRule describes a mined association rule: if antecedent is purchased, consequent is likely.
type AssociationRule struct {
	Antecedent string
	Consequent string
	Support    float64
	Confidence float64
}

// MineRules mines association rules above the given minSupport and minConfidence thresholds.
// confidence(A->B) = coOccurrence(A,B) / productCount(A)
func MineRules(miner *AffinityMiner, totalOrders int, minSupport, minConfidence float64) []AssociationRule {
	miner.mu.RLock()
	// Snapshot co-occurrence and product counts to avoid holding lock during computation.
	coOcc := make(map[string]int, len(miner.coOccurrence))
	for k, v := range miner.coOccurrence {
		coOcc[k] = v
	}
	productCounts := make(map[string]int, len(miner.productCount))
	for k, v := range miner.productCount {
		productCounts[k] = v
	}
	miner.mu.RUnlock()

	var rules []AssociationRule

	// For each pair, generate two directional rules (A->B and B->A).
	type pair struct{ a, b string }
	visited := make(map[string]struct{})
	for key, count := range coOcc {
		if _, ok := visited[key]; ok {
			continue
		}
		visited[key] = struct{}{}

		sup := float64(count) / float64(totalOrders)
		if totalOrders == 0 || sup < minSupport {
			continue
		}

		// Decode the pair.
		a, b := decodePairKey(key)

		// A -> B
		if pcA := productCounts[a]; pcA > 0 {
			conf := float64(count) / float64(pcA)
			if conf >= minConfidence {
				rules = append(rules, AssociationRule{
					Antecedent: a,
					Consequent: b,
					Support:    sup,
					Confidence: conf,
				})
			}
		}

		// B -> A
		if pcB := productCounts[b]; pcB > 0 {
			conf := float64(count) / float64(pcB)
			if conf >= minConfidence {
				rules = append(rules, AssociationRule{
					Antecedent: b,
					Consequent: a,
					Support:    sup,
					Confidence: conf,
				})
			}
		}
	}

	// Sort for deterministic output.
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Antecedent != rules[j].Antecedent {
			return rules[i].Antecedent < rules[j].Antecedent
		}
		return rules[i].Consequent < rules[j].Consequent
	})

	return rules
}

// decodePairKey splits a pairKey back into its two products.
func decodePairKey(key string) (string, string) {
	for i, c := range key {
		if c == '\x00' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

// Recommend returns the top-N products most associated (by support) with the given productID.
// Products not seen with productID return an empty slice.
func Recommend(miner *AffinityMiner, productID string, topN int, totalOrders int) []string {
	miner.mu.RLock()
	related := make(map[string]int)
	for key, count := range miner.coOccurrence {
		a, b := decodePairKey(key)
		if a == productID {
			related[b] += count
		} else if b == productID {
			related[a] += count
		}
	}
	miner.mu.RUnlock()

	if len(related) == 0 {
		return []string{}
	}

	type productScore struct {
		id    string
		count int
	}

	scores := make([]productScore, 0, len(related))
	for pid, cnt := range related {
		scores = append(scores, productScore{pid, cnt})
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].count != scores[j].count {
			return scores[i].count > scores[j].count
		}
		return scores[i].id < scores[j].id
	})

	n := topN
	if n > len(scores) {
		n = len(scores)
	}

	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = scores[i].id
	}
	return result
}
