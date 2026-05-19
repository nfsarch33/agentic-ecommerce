package search

import "sort"

type Suggestion struct {
	Term   string
	Weight int
}

type trieNode struct {
	children map[rune]*trieNode
	term     string
	weight   int
	isEnd    bool
}

// Trie supports prefix-based autocomplete.
type Trie struct {
	root *trieNode
}

func NewTrie() *Trie {
	return &Trie{root: &trieNode{children: make(map[rune]*trieNode)}}
}

func (t *Trie) Insert(term string, weight int) {
	node := t.root
	for _, ch := range term {
		if node.children[ch] == nil {
			node.children[ch] = &trieNode{children: make(map[rune]*trieNode)}
		}
		node = node.children[ch]
	}
	node.isEnd = true
	node.term = term
	node.weight = weight
}

func (t *Trie) Delete(term string) {
	t.delete(t.root, []rune(term), 0)
}

func (t *Trie) delete(node *trieNode, runes []rune, depth int) {
	if node == nil {
		return
	}
	if depth == len(runes) {
		node.isEnd = false
		return
	}
	ch := runes[depth]
	t.delete(node.children[ch], runes, depth+1)
}

func (t *Trie) PrefixSearch(prefix string, limit int) []Suggestion {
	node := t.root
	for _, ch := range prefix {
		if node.children[ch] == nil {
			return nil
		}
		node = node.children[ch]
	}
	var results []Suggestion
	collectAll(node, &results)
	sort.Slice(results, func(i, j int) bool { return results[i].Weight > results[j].Weight })
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

func collectAll(node *trieNode, out *[]Suggestion) {
	if node == nil {
		return
	}
	if node.isEnd {
		*out = append(*out, Suggestion{Term: node.term, Weight: node.weight})
	}
	for _, child := range node.children {
		collectAll(child, out)
	}
}

func PopularityBoost(suggestions []Suggestion, trending []string) []Suggestion {
	set := make(map[string]bool, len(trending))
	for _, t := range trending {
		set[t] = true
	}
	out := make([]Suggestion, len(suggestions))
	copy(out, suggestions)
	for i, s := range out {
		if set[s.Term] {
			out[i].Weight += 1000
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Weight > out[j].Weight })
	return out
}

// EditDistance computes Levenshtein distance between a and b.
func EditDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if ra[i-1] == rb[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				min3 := dp[i-1][j]
				if dp[i][j-1] < min3 {
					min3 = dp[i][j-1]
				}
				if dp[i-1][j-1] < min3 {
					min3 = dp[i-1][j-1]
				}
				dp[i][j] = min3 + 1
			}
		}
	}
	return dp[la][lb]
}
