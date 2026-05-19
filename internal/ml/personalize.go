package ml

import (
	"hash/fnv"
	"sort"
)

type UserProfile struct {
	UserID    string
	Purchases []string
	Browsed   []string
	Prefs     map[string]float64 // category -> affinity score
}

type Item struct {
	ID       string
	Category string
}

type RankedItem struct {
	Item  Item
	Score float64
}

type Arm struct {
	Name    string
	Reward  float64
	Pulls   int
}

type Context struct {
	UserID string
	Page   string
}

// ContentRank ranks items by preference match.
func ContentRank(profile UserProfile, items []Item) []RankedItem {
	var ranked []RankedItem
	for _, it := range items {
		score := profile.Prefs[it.Category]
		ranked = append(ranked, RankedItem{Item: it, Score: score})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
	return ranked
}

// ABVariant deterministically assigns a variant based on userID + experiment hash.
func ABVariant(userID, experiment string) string {
	h := fnv.New32a()
	h.Write([]byte(experiment + ":" + userID))
	variants := []string{"control", "variant_a", "variant_b"}
	return variants[int(h.Sum32())%len(variants)]
}

// ContextualBandit selects arm via epsilon-greedy (epsilon=0.1).
func ContextualBandit(arms []Arm, ctx Context) Arm {
	if len(arms) == 0 {
		return Arm{}
	}
	// Epsilon exploration: use hash to get consistent pseudo-random
	h := fnv.New32a()
	h.Write([]byte(ctx.UserID + ctx.Page))
	if int(h.Sum32())%10 == 0 { // 10% explore
		return arms[int(h.Sum32())%len(arms)]
	}
	// Greedy exploit: pick best average reward
	best := arms[0]
	for _, arm := range arms[1:] {
		if arm.Pulls > 0 && best.Pulls == 0 {
			best = arm
		} else if arm.Pulls > 0 && arm.Reward/float64(arm.Pulls) > best.Reward/float64(best.Pulls) {
			best = arm
		}
	}
	return best
}
