package content

import (
	"context"
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/rag"
)

var (
	ErrMissingEvidenceSearcher = errors.New("missing evidence searcher")
	claimMarkerRe              = regexp.MustCompile(`(?i)\b(\d+|includes?|contains?|features?|supports?|provides?|has|is|are|made from|constructed from|certified|rated|levels?)\b`)
	claimSentenceRe            = regexp.MustCompile(`[^.!?]+[.!?]?`)
)

type ClaimStatus string

const (
	ClaimSupported   ClaimStatus = "supported"
	ClaimUnsupported ClaimStatus = "unsupported"
)

type Claim struct {
	Text string `json:"text"`
}

type ClaimCheck struct {
	Claim      Claim              `json:"claim"`
	Text       string             `json:"text"`
	Status     ClaimStatus        `json:"status"`
	Confidence float64            `json:"confidence"`
	Evidence   []rag.SearchResult `json:"evidence"`
}

type FactCheckResult struct {
	ID         string       `json:"id,omitempty"`
	ProductID  string       `json:"product_id,omitempty"`
	Pass       bool         `json:"pass"`
	Confidence float64      `json:"confidence"`
	Claims     []ClaimCheck `json:"claims"`
	Issues     []string     `json:"issues"`
	CheckedAt  time.Time    `json:"checked_at,omitempty"`
}

type FactCheckOptions struct {
	MinConfidence float64
	TopK          int
}

type FactChecker struct {
	searcher rag.EvidenceSearcher
	options  FactCheckOptions
}

func NewFactChecker(searcher rag.EvidenceSearcher, options FactCheckOptions) *FactChecker {
	if options.MinConfidence <= 0 {
		options.MinConfidence = 0.72
	}
	if options.TopK <= 0 {
		options.TopK = 5
	}
	return &FactChecker{searcher: searcher, options: options}
}

func ExtractClaims(text string) []Claim {
	sentences := claimSentenceRe.FindAllString(strings.TrimSpace(text), -1)
	claims := make([]Claim, 0, len(sentences))
	for _, sentence := range sentences {
		normalized := strings.TrimSpace(sentence)
		if normalized == "" || !claimMarkerRe.MatchString(normalized) {
			continue
		}
		claims = append(claims, Claim{Text: normalized})
	}
	return claims
}

func (c *FactChecker) Check(ctx context.Context, generated GeneratedContent) (FactCheckResult, error) {
	if c.searcher == nil {
		return FactCheckResult{}, ErrMissingEvidenceSearcher
	}
	text := strings.TrimSpace(strings.Join([]string{generated.Description, generated.SEOTitle, generated.MetaDescription}, " "))
	claims := ExtractClaims(text)
	result := FactCheckResult{
		Pass:      true,
		Claims:    make([]ClaimCheck, 0, len(claims)),
		CheckedAt: time.Now().UTC(),
	}
	if len(claims) == 0 {
		result.Confidence = 1
		return result, nil
	}

	var total float64
	for _, claim := range claims {
		evidence, err := c.searcher.SearchText(ctx, claim.Text, c.options.TopK)
		if err != nil {
			return FactCheckResult{}, err
		}
		confidence := evidenceConfidence(claim.Text, evidence)
		check := ClaimCheck{
			Claim:      claim,
			Text:       claim.Text,
			Status:     ClaimSupported,
			Confidence: confidence,
			Evidence:   evidence,
		}
		if confidence < c.options.MinConfidence {
			check.Status = ClaimUnsupported
			result.Pass = false
			result.Issues = append(result.Issues, "unsupported claim: "+claim.Text)
		}
		result.Claims = append(result.Claims, check)
		total += confidence
	}
	result.Confidence = roundFactConfidence(total / float64(len(claims)))
	return result, nil
}

func evidenceConfidence(claim string, evidence []rag.SearchResult) float64 {
	if len(evidence) == 0 {
		return 0
	}
	best := 0.0
	for _, item := range evidence {
		score := math.Max(item.Score, lexicalOverlap(claim, item.Text))
		if score > best {
			best = score
		}
	}
	return roundFactConfidence(best)
}

func lexicalOverlap(a, b string) float64 {
	aWords := significantWords(a)
	if len(aWords) == 0 {
		return 0
	}
	bWords := significantWords(b)
	matches := 0
	for word := range aWords {
		if bWords[word] {
			matches++
		}
	}
	return float64(matches) / float64(len(aWords))
}

func significantWords(text string) map[string]bool {
	words := wordsOf(text)
	out := make(map[string]bool, len(words))
	for _, word := range words {
		if len(word) <= 2 || factStopWords[word] {
			continue
		}
		out[word] = true
	}
	return out
}

var factStopWords = map[string]bool{
	"and": true, "are": true, "for": true, "from": true, "has": true,
	"includes": true, "include": true, "into": true, "the": true, "this": true,
	"with": true, "made": true,
}

func roundFactConfidence(v float64) float64 {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return math.Round(v*100) / 100
}

func SortClaimChecks(checks []ClaimCheck) {
	sort.SliceStable(checks, func(i, j int) bool {
		return checks[i].Text < checks[j].Text
	})
}
