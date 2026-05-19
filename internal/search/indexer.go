package search

import (
	"math"
	"strings"
	"sync"
)

// Product is a searchable product document.
type Product struct {
	ID       string
	Title    string
	Category string
}

// Order is a searchable order document.
type Order struct {
	ID            string
	CustomerEmail string
	Status        string
}

// SearchFilter constrains search results.
type SearchFilter struct {
	Category string
}

// SearchResult is a scored document returned from search.
type SearchResult struct {
	ID       string
	Type     string
	Category string
	Score    float64
}

type document struct {
	id       string
	docType  string
	category string
	tokens   map[string]int
}

// Indexer maintains an in-memory TF-IDF search index.
type Indexer struct {
	mu   sync.RWMutex
	docs []document
}

func NewIndexer() *Indexer {
	return &Indexer{}
}

func (idx *Indexer) IndexProduct(p Product) {
	idx.add(document{
		id:       p.ID,
		docType:  "product",
		category: p.Category,
		tokens:   tokenize(p.Title + " " + p.Category),
	})
}

func (idx *Indexer) IndexOrder(o Order) {
	idx.add(document{
		id:      o.ID,
		docType: "order",
		tokens:  tokenize(o.CustomerEmail + " " + o.Status),
	})
}

func (idx *Indexer) add(d document) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.docs = append(idx.docs, d)
}

// Search returns documents matching the query, sorted by TF-IDF score descending.
func (idx *Indexer) Search(query string, f SearchFilter) []SearchResult {
	qTokens := tokenize(query)
	if len(qTokens) == 0 {
		return []SearchResult{}
	}
	idx.mu.RLock()
	docs := idx.docs
	idx.mu.RUnlock()
	if len(docs) == 0 {
		return []SearchResult{}
	}
	idf := computeIDF(qTokens, docs)
	results := scoreDocuments(docs, qTokens, idf, f)
	sortByScore(results)
	return results
}

func computeIDF(qTokens map[string]int, docs []document) map[string]float64 {
	n := float64(len(docs))
	idf := make(map[string]float64, len(qTokens))
	for tok := range qTokens {
		df := docFrequency(tok, docs)
		if df > 0 {
			idf[tok] = math.Log(n/df) + 1
		}
	}
	return idf
}

func docFrequency(tok string, docs []document) float64 {
	df := 0.0
	for _, d := range docs {
		if d.tokens[tok] > 0 {
			df++
		}
	}
	return df
}

func scoreDocuments(docs []document, qTokens map[string]int, idf map[string]float64, f SearchFilter) []SearchResult {
	var results []SearchResult
	for _, d := range docs {
		if f.Category != "" && d.category != f.Category {
			continue
		}
		score := tfidfScore(d, qTokens, idf)
		if score > 0 {
			results = append(results, SearchResult{
				ID:       d.id,
				Type:     d.docType,
				Category: d.category,
				Score:    score,
			})
		}
	}
	return results
}

func tfidfScore(d document, qTokens map[string]int, idf map[string]float64) float64 {
	score := 0.0
	for tok, qtf := range qTokens {
		tf := float64(d.tokens[tok])
		if tf > 0 {
			score += tf * float64(qtf) * idf[tok]
		}
	}
	return score
}

func tokenize(text string) map[string]int {
	tokens := make(map[string]int)
	// split on whitespace and common delimiters including @ for emails
	normalized := strings.Map(func(r rune) rune {
		switch r {
		case '@', '.', ',', '!', '?', ';', ':':
			return ' '
		}
		return r
	}, strings.ToLower(text))
	for _, word := range strings.Fields(normalized) {
		if word != "" {
			tokens[word]++
		}
	}
	return tokens
}

func sortByScore(results []SearchResult) {
	// insertion sort (small N in typical use)
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}
