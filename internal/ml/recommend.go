package ml

import "sort"

type Interaction struct {
	UserID    string
	ProductID string
	Count     int
}

type Product struct {
	ID       string
	Category string
}

type ProductScore struct {
	ProductID string
	Score     float64
}

// CollaborativeFilter scores products by co-purchase frequency with the user's interactions.
func CollaborativeFilter(userID string, interactions []Interaction) []ProductScore {
	// Build co-occurrence: products this user has interacted with
	userProds := make(map[string]int)
	for _, it := range interactions {
		if it.UserID == userID {
			userProds[it.ProductID] += it.Count
		}
	}
	// Score other products co-purchased by users who bought same products
	scores := make(map[string]float64)
	for _, it := range interactions {
		if it.UserID == userID {
			continue
		}
		if _, ok := userProds[it.ProductID]; ok {
			// Co-occurrence: boost all products bought by this other user
			for _, other := range interactions {
				if other.UserID == it.UserID && other.ProductID != it.ProductID {
					scores[other.ProductID] += float64(other.Count)
				}
			}
		}
	}
	return toSortedScores(scores)
}

// ContentBased scores products by category match with the reference product.
func ContentBased(productID string, catalog []Product) []ProductScore {
	var refCategory string
	for _, p := range catalog {
		if p.ID == productID {
			refCategory = p.Category
			break
		}
	}
	scores := make(map[string]float64)
	for _, p := range catalog {
		if p.ID == productID {
			continue
		}
		if p.Category == refCategory {
			scores[p.ID] = 1.0
		}
	}
	return toSortedScores(scores)
}

// HybridScore merges CF and CB scores with weights [cfWeight, cbWeight].
func HybridScore(cf, cb []ProductScore, weights [2]float64) []ProductScore {
	merged := make(map[string]float64)
	for _, s := range cf {
		merged[s.ProductID] += s.Score * weights[0]
	}
	for _, s := range cb {
		merged[s.ProductID] += s.Score * weights[1]
	}
	return toSortedScores(merged)
}

// TopN returns the highest-scoring N products.
func TopN(scores []ProductScore, n int) []ProductScore {
	if n >= len(scores) {
		return scores
	}
	return scores[:n]
}

func toSortedScores(m map[string]float64) []ProductScore {
	var out []ProductScore
	seen := make(map[string]bool)
	for id, s := range m {
		if !seen[id] {
			out = append(out, ProductScore{ProductID: id, Score: s})
			seen[id] = true
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
