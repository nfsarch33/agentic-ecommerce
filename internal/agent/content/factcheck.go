package content

import (
	"context"
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/rag"
)

var (
	ErrMissingEvidenceSearcher = errors.New("missing evidence searcher")
	claimMarkerRe              = regexp.MustCompile(`(?i)\b(\d+|includes?|contains?|features?|supports?|provides?|has|is|are|made from|constructed from|certified|rated|levels?)\b`)
	claimSentenceRe            = regexp.MustCompile(`[^.!?]+[.!?]?`)
)

type ClaimStatus string

const (
	ClaimSupported    ClaimStatus = "supported"
	ClaimUnsupported  ClaimStatus = "unsupported"
	ClaimContradicted ClaimStatus = "contradicted"
	ClaimAmbiguous    ClaimStatus = "ambiguous"
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
			Status:     classifyClaim(claim.Text, evidence, confidence, c.options.MinConfidence),
			Confidence: confidence,
			Evidence:   evidence,
		}
		if check.Status != ClaimSupported {
			result.Pass = false
			result.Issues = append(result.Issues, string(check.Status)+" claim: "+claim.Text)
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

func classifyClaim(claim string, evidence []rag.SearchResult, confidence, minConfidence float64) ClaimStatus {
	if len(evidence) == 0 {
		return ClaimUnsupported
	}
	if hasNumericContradiction(claim, evidence) {
		return ClaimContradicted
	}
	if confidence >= minConfidence {
		return ClaimSupported
	}
	return ClaimAmbiguous
}

func hasNumericContradiction(claim string, evidence []rag.SearchResult) bool {
	claimNumbers := numericFacts(claim)
	if len(claimNumbers) == 0 {
		return false
	}
	for _, item := range evidence {
		evidenceNumbers := numericFacts(item.Text)
		if len(evidenceNumbers) == 0 {
			continue
		}
		for number := range claimNumbers {
			if evidenceNumbers[number] {
				return false
			}
		}
		return true
	}
	return false
}

func numericFacts(text string) map[string]bool {
	words := wordsOf(text)
	out := make(map[string]bool)
	for _, word := range words {
		if normalized, ok := numberWords[word]; ok {
			out[normalized] = true
			continue
		}
		if digitFactRe.MatchString(word) {
			out[word] = true
		}
	}
	return out
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

var (
	digitFactRe = regexp.MustCompile(`^\d+$`)
	numberWords = map[string]string{
		"zero": "0", "one": "1", "two": "2", "three": "3", "four": "4",
		"five": "5", "six": "6", "seven": "7", "eight": "8", "nine": "9",
		"ten": "10", "eleven": "11", "twelve": "12", "thirteen": "13",
		"fourteen": "14", "fifteen": "15", "sixteen": "16", "seventeen": "17",
		"eighteen": "18", "nineteen": "19", "twenty": "20",
	}
)

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
