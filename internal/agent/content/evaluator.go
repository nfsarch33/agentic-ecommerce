package content

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

var (
	wordRe        = regexp.MustCompile(`[A-Za-z0-9]+(?:'[A-Za-z0-9]+)?`)
	sentenceEndRe = regexp.MustCompile(`[.!?]+`)
)

type Evaluator struct{}

func NewEvaluator() Evaluator {
	return Evaluator{}
}

func (Evaluator) Evaluate(input EvaluationInput) Evaluation {
	text := strings.TrimSpace(strings.Join([]string{
		input.Output.Description,
		input.Output.SEOTitle,
		input.Output.MetaDescription,
	}, " "))

	words := wordsOf(input.Output.Description)
	wordCount := len(words)
	maxWords := input.MaxWords
	lengthOK := maxWords <= 0 || wordCount <= maxWords
	readability := readabilityScore(input.Output.Description)
	density := keywordDensity(text, input.Keywords)
	tone := evaluateTone(input.Style, input.Output)
	factualIssues := factualIssues(input.Product, input.Output)

	score := 100
	if readability < 50 {
		score -= 12
	} else if readability < 60 {
		score -= 6
	}
	if !lengthOK {
		score -= 18
	}
	if !tone.Pass {
		score -= 8 * len(tone.Issues)
	}
	for _, value := range density {
		if value == 0 {
			score -= 8
		} else if value > 8 {
			score -= 4
		}
	}
	score -= 20 * len(factualIssues)
	if score < 0 {
		score = 0
	}

	return Evaluation{
		Score:            score,
		Pass:             score >= 75 && lengthOK && len(factualIssues) == 0 && tone.Pass,
		ReadabilityScore: round2(readability),
		KeywordDensity:   density,
		Tone:             tone,
		Length: LengthEvaluation{
			WordCount:   wordCount,
			MaxWords:    maxWords,
			WithinLimit: lengthOK,
		},
		FactualIssues: factualIssues,
	}
}

func keywordDensity(text string, keywords []string) map[string]float64 {
	out := make(map[string]float64, len(keywords))
	words := wordsOf(text)
	if len(words) == 0 {
		for _, keyword := range keywords {
			out[strings.ToLower(strings.TrimSpace(keyword))] = 0
		}
		return out
	}
	lower := strings.ToLower(text)
	for _, keyword := range keywords {
		key := strings.ToLower(strings.TrimSpace(keyword))
		if key == "" {
			continue
		}
		count := strings.Count(lower, key)
		out[key] = round2(float64(count) / float64(len(words)) * 100)
	}
	return out
}

func evaluateTone(style Style, output GeneratedContent) ToneEvaluation {
	issues := make([]string, 0)
	text := strings.ToLower(output.Description + " " + output.SEOTitle + " " + output.MetaDescription)
	switch style {
	case StyleProfessional:
		if strings.Contains(text, "awesome") || strings.Contains(text, "super ") {
			issues = append(issues, "professional tone is too casual")
		}
	case StyleCasual:
		if strings.Contains(text, "aforementioned") || strings.Contains(text, "therefore") {
			issues = append(issues, "casual tone is too formal")
		}
	case StyleLuxury:
		if strings.Contains(text, "cheap") || strings.Contains(text, "budget") {
			issues = append(issues, "luxury tone uses discount language")
		}
	case StyleTechnical:
		if strings.Contains(text, "magical") || strings.Contains(text, "dreamy") {
			issues = append(issues, "technical tone is too vague")
		}
	}
	return ToneEvaluation{Style: style, Pass: len(issues) == 0, Issues: issues}
}

func factualIssues(product ProductInfo, output GeneratedContent) []string {
	text := strings.TrimSpace(output.Description + " " + output.SEOTitle + " " + output.MetaDescription)
	lower := strings.ToLower(text)
	issues := make([]string, 0)
	for _, marker := range []string{"todo", "lorem ipsum", "{{", "}}", "[", "]", "tbd"} {
		if strings.Contains(lower, marker) {
			issues = append(issues, "placeholder content present")
			break
		}
	}
	if product.Title != "" && !strings.Contains(lower, strings.ToLower(product.Title)) {
		issues = append(issues, "product title not referenced")
	}
	return issues
}

func readabilityScore(text string) float64 {
	words := wordsOf(text)
	if len(words) == 0 {
		return 0
	}
	sentences := sentenceCount(text)
	syllables := 0
	for _, word := range words {
		syllables += countSyllables(word)
	}
	score := 206.835 - 1.015*(float64(len(words))/float64(sentences)) - 84.6*(float64(syllables)/float64(len(words)))
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func sentenceCount(text string) int {
	parts := sentenceEndRe.FindAllString(text, -1)
	if len(parts) == 0 {
		return 1
	}
	return len(parts)
}

func wordsOf(text string) []string {
	return wordRe.FindAllString(strings.ToLower(text), -1)
}

func countSyllables(word string) int {
	word = strings.ToLower(strings.TrimFunc(word, func(r rune) bool {
		return !unicode.IsLetter(r)
	}))
	if word == "" {
		return 0
	}
	vowels := "aeiouy"
	count := 0
	prevVowel := false
	for _, r := range word {
		isVowel := strings.ContainsRune(vowels, r)
		if isVowel && !prevVowel {
			count++
		}
		prevVowel = isVowel
	}
	if strings.HasSuffix(word, "e") && count > 1 {
		count--
	}
	if count == 0 {
		return 1
	}
	return count
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
